<script>
  import { onDestroy } from 'svelte';
  import Icon from './Icon.svelte';
  import { wavFromRecording } from '../voice.js';
  import { VAD, voiceActivity, initialVoiceActivity } from '../voice-vad.js';
  import { isTurnComplete } from '../voice-turn.js';
  // Голосовой режим: открывается и сразу слушает, отправляет, когда человек замолчал.
  // variant 'sheet' — шторка поверх другого приложения, 'full' — весь экран внутри Secretary App.
  // turn — сообщения текущего хода: последнее сообщение пользователя и всё после него.
  let { variant = 'sheet', turn = [], sessionId, running = false, processing = false, speaking = false, speechFinishedId = null, connected = false, openedAt = performance.now(), onstart, onvoice, onphoto, onstopspeech, onclose, onopenapp } = $props();

  let listening = $state(false), starting = $state(false), waiting = $state(false), hint = $state(''), bars = $state([4, 4, 4, 4, 4]);
  let camera = $state(null), video = $state(null), facing = $state('environment'), cameraStatus = $state('');
  let cameraRequest = 0;
  let recording = null, audio, autostarted = false, disposed = false, lastKey = $state(null), timing;

  // Шторка открывается раньше, чем появляется связь с Host: начинаем слушать, как только она есть.
  $effect(() => { if (connected && !autostarted) { autostarted = true; listen(openedAt, true); } });
  $effect(() => { if (speaking || (lastKey && speechFinishedId === lastKey) || (lastKey && turn.some(item => item.key === lastKey && item.error))) waiting = false; });
  $effect(() => { if (!connected) { autostarted = false; finish(false); } });
  onDestroy(() => { disposed = true; finish(false); stopCamera(); });

  function stopCamera() { cameraRequest++; camera?.getTracks().forEach(track => track.stop()); camera = null; }
  async function openCamera() {
    stopCamera(); cameraStatus = '';
    const request = cameraRequest;
    try {
      const stream = await navigator.mediaDevices.getUserMedia({video:{facingMode:facing}, audio:false});
      if (disposed || request !== cameraRequest) { stream.getTracks().forEach(track => track.stop()); return; }
      camera = stream;
    } catch (error) { cameraStatus = `Нет доступа к камере: ${error.message}`; }
  }
  $effect(() => { if (video && camera) video.srcObject = camera; });
  async function snapshot() {
    if (!video?.videoWidth || !camera) return;
    const canvas = document.createElement('canvas');
    const scale = Math.min(1, 1568 / Math.max(video.videoWidth, video.videoHeight));
    canvas.width = Math.round(video.videoWidth * scale); canvas.height = Math.round(video.videoHeight * scale);
    canvas.getContext('2d').drawImage(video, 0, 0, canvas.width, canvas.height);
    const [header, data] = canvas.toDataURL('image/jpeg', 0.8).split(',');
    if (!header.startsWith('data:image/jpeg')) { cameraStatus = 'Не удалось создать снимок'; return; }
    try { await onphoto({type:'image/jpeg', data}); cameraStatus = 'Снимок отправлен'; }
    catch (error) { cameraStatus = `Не удалось отправить снимок: ${error.message}`; }
  }

  async function listen(press = performance.now(), first = false) {
    if (listening || starting || !connected) return;
    timing = {press};
    starting = true; waiting = false;
    hint = '';
    if (first) onstart?.();
    let media;
    try { media = await navigator.mediaDevices.getUserMedia({audio:{echoCancellation:true, noiseSuppression:true, autoGainControl:true}}); }
    catch (error) { starting = false; hint = `Нет доступа к микрофону: ${error.message}`; return; }
    if (!starting) { media.getTracks().forEach(track => track.stop()); return; }
    const chunks = [], started = Date.now(), recorder = new MediaRecorder(media), active = {recorder, media, send:false, timing};
    recording = active;
    recorder.ondataavailable = event => {
      chunks.push(event.data);
      active.snapshot?.();
      active.snapshot = null;
    };
    recorder.onstop = () => {
      media.getTracks().forEach(track => track.stop());
      const seconds = (Date.now() - started) / 1000;
      if (active.send) { lastKey = crypto.randomUUID(); onvoice({id:sessionId, key:lastKey, seconds, timing:active.timing, immediate:active.interrupted === true, wav:wavFromRecording(chunks)}); }
    };
    recorder.start();
    starting = false; listening = true; timing.listening = performance.now();
    audio = new AudioContext();
    const analyser = audio.createAnalyser();
    analyser.fftSize = 2 ** Math.round(Math.log2(audio.sampleRate * VAD.window / 1000));
    const samples = new Float32Array(analyser.fftSize);
    audio.createMediaStreamSource(media).connect(analyser);
    let level = 0, vad = initialVoiceActivity();
    async function checkTurn() {
      active.checking = true;
      const lastLoud = active.lastLoud;
      try {
        await new Promise((resolve, reject) => {
          const timeout = setTimeout(() => { active.snapshot = null; reject(new Error('Нет данных записи')); }, 1000);
          active.snapshot = () => { clearTimeout(timeout); resolve(); };
          recorder.requestData();
        });
        const wav = await wavFromRecording(chunks.slice(), {trim:false, lastSeconds:8});
        if (recording !== active || active.lastLoud !== lastLoud) return;
        const complete = await isTurnComplete(wav);
        if (recording !== active || active.lastLoud !== lastLoud) return;
        if (complete !== false) finish(true, true);
        else active.incomplete = true;
      } catch {
        if (recording === active && active.lastLoud === lastLoud) finish(true, true);
      } finally { active.checking = false; }
    }
    active.timer = setInterval(() => {
      if (recording !== active) return;
      analyser.getFloatTimeDomainData(samples);
      const rms = Math.sqrt(samples.reduce((sum, value) => sum + value * value, 0) / samples.length);
      level += (Math.min(1, rms * 4) - level) * 0.3;
      bars = bars.map((_, i) => 4 + level * 18 * (0.5 + 0.5 * Math.abs(Math.sin(performance.now() / 1000 * 7 + i * 1.3))));
      vad = voiceActivity(vad, rms, speaking && !active.interrupted);
      if (rms > Math.max(VAD.floor, vad.noise * VAD.noiseFactor)) active.lastLoud = performance.now();
      if (vad.interrupt) { active.interrupted = true; onstopspeech?.(); }
      if (vad.end && !active.checking) {
        if (active.incomplete && vad.silence >= 2000) finish(true, true);
        else if (!active.incomplete) checkTurn();
      }
      if (vad.silence === 0) active.incomplete = false;
      if (recording === active && Date.now() - started > 60000) finish(vad.speech >= VAD.speech, true);
    }, VAD.window);
  }

  function finish(send, restart = false) {
    starting = false; listening = false;
    bars = bars.map(() => 4);
    audio?.close(); audio = null;
    if (recording) {
      if (send) { recording.timing.end = recording.lastLoud ?? performance.now(); waiting = true; }
      clearInterval(recording.timer);
      recording.send = send; recording.recorder.stop();
    }
    recording = null;
    if (restart && !disposed && connected) listen();
  }

  function close() { finish(false); onclose?.(); }

  const questionError = $derived(turn.find(item => item.key === lastKey)?.error);
  const status = $derived(speaking ? 'Говорю' : listening ? 'Слушаю' : waiting || processing || running ? 'Думаю' : starting || !connected ? 'Подключаюсь' : 'Secretary');
  const full = $derived(variant === 'full');
</script>

{#snippet wave(color)}
  <span class="flex h-5 items-center gap-[3px]" aria-hidden="true">{#each bars as height}<span class="w-[3px] rounded-full {color}" style:height="{height}px"></span>{/each}</span>
{/snippet}
{#snippet speakingWave()}
  <span class="flex h-5 items-center gap-[3px]" aria-hidden="true">{#each [0, 1, 2, 3, 4] as i}<span class="speaking-bar w-[3px] rounded-full bg-neutral-200" style:animation-delay="{i * 0.13}s"></span>{/each}</span>
{/snippet}
{#snippet dots()}
  <span class="flex gap-1" aria-hidden="true">{#each [0, 1, 2] as i}<span class="size-1.5 animate-pulse rounded-full bg-neutral-400" style:animation-delay="{i * 0.2}s"></span>{/each}</span>
{/snippet}
{#snippet body()}
  <div class="mb-4 flex flex-wrap gap-2">
    <button class="btn-ghost" onclick={() => camera ? stopCamera() : openCamera()}>{camera ? 'Выключить камеру' : 'Камера'}</button>
    {#if camera}
      <button class="btn-ghost" onclick={() => { facing = facing === 'environment' ? 'user' : 'environment'; openCamera(); }}>Переключить камеру</button>
      <button class="btn-ghost" disabled={!connected} onclick={snapshot}>Снимок</button>
    {/if}
  </div>
  {#if camera}<video bind:this={video} autoplay playsinline muted class="mb-3 max-h-48 w-full rounded-lg object-contain"></video>{/if}
  {#if cameraStatus}<p role="status" class="mb-3 text-sm text-neutral-400">{cameraStatus}</p>{/if}
  {#if listening}
    <p class="{full ? 'text-3xl' : 'text-lg'} font-medium leading-snug text-white">Говорите…</p>
    <p class="mt-2 text-sm text-neutral-500">Отправлю, когда замолчите</p>
  {:else if questionError}
    <p role="alert" class="text-red-400">Не удалось распознать: {questionError}</p>
  {/if}
  {#if hint}<p role="status" class="mt-3 text-sm text-neutral-400">{hint}</p>{/if}
  {#if !connected}<p role="status" class="mt-3 text-sm text-neutral-400">Нет связи с Host</p>{/if}
{/snippet}

{#if full}
  <div class="fixed inset-0 z-50 flex flex-col bg-neutral-950 transition-colors {listening ? 'bg-[radial-gradient(circle_at_50%_85%,#3f0d0d,#0a0a0a_65%)]' : ''}" style:padding-top="max(env(safe-area-inset-top), 1rem)" style:padding-bottom="max(env(safe-area-inset-bottom), 1.5rem)">
    <header class="flex items-center gap-2 px-5">
      <span role="status" class="flex-1 text-sm text-neutral-400">{status}</span>
      <button class="icon-btn" aria-label="Закрыть" onclick={close}><Icon name="close" /></button>
    </header>
    <div class="min-h-0 flex-1 overflow-y-auto px-6 pb-6 pt-8">{@render body()}</div>
    <div class="flex items-center justify-center gap-6">
      <span class="flex size-12 items-center justify-center">
        {#if speaking}<button class="icon-btn size-12 bg-neutral-900" aria-label="Остановить озвучку" onclick={onstopspeech}><Icon name="stop" /></button>{/if}
      </span>
      {#if listening}
        <span class="flex size-20 items-center justify-center rounded-full bg-red-600 text-white shadow-[0_0_48px_#ef4444aa]" aria-label="Микрофон включён">{@render wave('bg-white')}</span>
      {:else}
        <button class="flex size-20 items-center justify-center rounded-full bg-neutral-800 text-white disabled:opacity-40" aria-label="Говорить" disabled={!connected} onclick={() => listen()}><Icon name="microphone" class="size-7" /></button>
      {/if}
      <span class="flex size-12 items-center justify-center">
        {#if speaking}{@render speakingWave()}{/if}
      </span>
    </div>
  </div>
{:else}
  <!-- Пустое место над шторкой прозрачно: под ним видно приложение, нажатие закрывает шторку. -->
  <div class="fixed inset-0 flex flex-col justify-end p-2" style:padding-bottom="calc(max(env(safe-area-inset-bottom), 1.5rem) + 0.5rem)" role="presentation" onclick={e => { if (e.target === e.currentTarget) close(); }}>
    <div class="flex max-h-[75dvh] flex-col rounded-[28px] border border-neutral-800 bg-[#0a0a0a] p-4 transition-shadow {listening ? 'shadow-[0_0_0_1px_#ef444455,0_-8px_40px_#ef444433]' : ''}">
      <header class="flex items-center gap-2">
        <span role="status" class="flex-1 text-xs text-neutral-400">{status}</span>
        {#if listening}{@render wave('bg-red-500')}{:else if speaking}{@render speakingWave()}{:else if running}{@render dots()}{/if}
      </header>
      <div class="min-h-0 overflow-y-auto pt-3">{@render body()}</div>
      <div class="mt-4 flex items-center gap-2">
        {#if listening}
          <button class="btn-ghost" onclick={close}>Отмена</button>
          <span class="flex-1"></span>
        {:else}
          <button class="icon-btn bg-neutral-900" aria-label="Говорить" disabled={!connected} onclick={() => listen()}><Icon name="microphone" /></button>
          {#if speaking}<button class="icon-btn bg-neutral-900" aria-label="Остановить озвучку" onclick={onstopspeech}><Icon name="stop" /></button>{/if}
          <span class="flex-1"></span>
          <button class="icon-btn bg-neutral-900" aria-label="Открыть чат" onclick={onopenapp}><Icon name="expand" /></button>
          <button class="icon-btn bg-neutral-900" aria-label="Закрыть" onclick={close}><Icon name="close" /></button>
        {/if}
      </div>
    </div>
  </div>
{/if}
