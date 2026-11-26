<script>
  import { onDestroy } from 'svelte';
  import Icon from './Icon.svelte';
  import { wavFromRecording } from '../voice.js';
  // Голосовой режим: открывается и сразу слушает, отправляет, когда человек замолчал.
  // variant 'sheet' — шторка поверх другого приложения, 'full' — весь экран внутри Secretary App.
  // turn — сообщения текущего хода: последнее сообщение пользователя и всё после него.
  let { variant = 'sheet', turn = [], sessionId, running = false, processing = false, speaking = false, speechFinishedId = null, connected = false, openedAt = performance.now(), onstart, onvoice, onstopspeech, onclose, onopenapp } = $props();

  let listening = $state(false), starting = $state(false), waiting = $state(false), hint = $state(''), bars = $state([4, 4, 4, 4, 4]);
  let recording = null, audio, autostarted = false, lastKey = $state(null), timing;

  function beep(frequency) {
    const context = new AudioContext(), oscillator = context.createOscillator(), gain = context.createGain();
    oscillator.frequency.value = frequency;
    gain.gain.setValueAtTime(0.05, context.currentTime);
    gain.gain.exponentialRampToValueAtTime(0.001, context.currentTime + 0.12);
    oscillator.connect(gain).connect(context.destination);
    oscillator.start(); oscillator.stop(context.currentTime + 0.12);
    oscillator.onended = () => context.close();
  }

  // Шторка открывается раньше, чем появляется связь с Host: начинаем слушать, как только она есть.
  $effect(() => { if (connected && !autostarted) { autostarted = true; listen(openedAt); } });
  $effect(() => { if (speaking || (lastKey && speechFinishedId === lastKey) || (lastKey && turn.some(item => item.key === lastKey && item.error))) waiting = false; });
  onDestroy(() => finish(false));

  async function listen(press = performance.now()) {
    if (listening || starting || !connected) return;
    timing = {press};
    starting = true; waiting = false;
    hint = '';
    onstart?.();
    let media;
    try { media = await navigator.mediaDevices.getUserMedia({audio:true}); }
    catch (error) { starting = false; hint = `Нет доступа к микрофону: ${error.message}`; return; }
    if (!starting) { media.getTracks().forEach(track => track.stop()); return; }
    const chunks = [], started = Date.now(), recorder = new MediaRecorder(media), active = {recorder, media, send:false, timing};
    recording = active;
    recorder.ondataavailable = event => chunks.push(event.data);
    recorder.onstop = () => {
      media.getTracks().forEach(track => track.stop());
      const seconds = (Date.now() - started) / 1000;
      if (active.send) { lastKey = crypto.randomUUID(); onvoice({id:sessionId, key:lastKey, seconds, timing:active.timing, wav:wavFromRecording(chunks)}); }
    };
    recorder.start();
    starting = false; listening = true; timing.listening = performance.now();
    beep(660);
    audio = new AudioContext();
    const analyser = audio.createAnalyser(), samples = new Uint8Array(512);
    audio.createMediaStreamSource(media).connect(analyser);
    let level = 0, heard = 0, loud = 0;
    const tick = time => {
      if (recording !== active) return;
      analyser.getByteTimeDomainData(samples);
      const loudness = Math.min(1, 4 * Math.sqrt(samples.reduce((sum, v) => sum + ((v - 128) / 128) ** 2, 0) / samples.length));
      level += (loudness - level) * (loudness > level ? 0.5 : 0.12);
      bars = bars.map((_, i) => 4 + level * 18 * (0.5 + 0.5 * Math.abs(Math.sin(time / 1000 * 7 + i * 1.3))));
      const now = Date.now();
      if (now - started > 250 && loudness > 0.15) { heard ||= now; loud = now; active.lastLoud = performance.now(); }
      // Отправляем после паузы в речи; если речи не было совсем — не отправляем пустую запись.
      if (heard && now - heard > 400 && now - loud > 1500) finish(true);
      else if (!heard && now - started > 8000) { finish(false); hint = 'Не расслышал. Нажмите микрофон и повторите'; }
      else if (now - started > 60000) finish(true);
      else requestAnimationFrame(tick);
    };
    requestAnimationFrame(tick);
  }

  function finish(send) {
    starting = false; listening = false;
    bars = bars.map(() => 4);
    audio?.close(); audio = null;
    if (recording) {
      if (send) { recording.timing.end = recording.lastLoud ?? performance.now(); waiting = true; }
      recording.send = send; recording.recorder.stop(); beep(440);
    }
    recording = null;
  }

  function close() { finish(false); onclose?.(); }

  const questionError = $derived(turn.find(item => item.key === lastKey)?.error);
  const status = $derived(listening ? 'Слушаю' : speaking ? 'Говорю' : waiting || processing || running ? 'Думаю' : starting || !connected ? 'Подключаюсь' : 'Secretary');
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
        <button class="flex size-20 items-center justify-center rounded-full bg-red-600 text-white shadow-[0_0_48px_#ef4444aa]" aria-label="Отправить" onclick={() => finish(true)}>{@render wave('bg-white')}</button>
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
          <button class="btn" onclick={() => finish(true)}>Отправить</button>
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
