<script>
  import { onDestroy, tick } from 'svelte';
  import Icon from './Icon.svelte';
  import { duration, wavFromRecording } from '../voice.js';
  import { prepareImage } from '../prepare-image.js';
  import { prepareVideo, frameFromCanvas, finishVideo } from '../prepare-video.js';
  // onsubmit(immediate): true — прервать идущий ход и отправить сразу (правая кнопка и Enter), false — добавить в очередь (левая кнопка во время хода).
  let { value = $bindable(''), images = $bindable([]), disabled = false, stopDisabled = false, running = false, sessionId, videoInterval = 1, onsubmit, oninterrupt, onvoice, onvideo } = $props();
  let imageError = $state('');
  let area = $state();
  // Как в Telegram: на сенсорном экране нажатие переключает голос и видео, удержание записывает;
  // мышью удерживать неудобно, поэтому щелчок сразу начинает запись с замком.
  let mode = $state('voice'), recording = $state.raw(null), hint = $state(''), voiceError = $state(''), now = $state(0);
  let row, press, recorder, chunks, stream, audio, camera, canvas, capture, frames, facing = $state('user');
  let preview = $state(), bars = $state([4, 4, 4, 4, 4]);
  onDestroy(() => stop(false));
  $effect(() => {
    if (!recording) return;
    const timer = setInterval(() => { now = Date.now(); if (recording?.mode === 'video' && now - recording.started >= 60000) stop(true); }, 250);
    return () => clearInterval(timer);
  });
  function showHint(text) { hint = text; setTimeout(() => { if (hint === text) hint = ''; }, 1500); }
  function down(e) {
    if (disabled || recording) return;
    row.setPointerCapture(e.pointerId);
    if (e.pointerType === 'mouse') { start(true); return; }
    press = {x:e.clientX, y:e.clientY, timer:setTimeout(() => { press.timer = null; start(false); }, 250)};
  }
  function move(e) {
    if (!press || !recording || recording.locked) return;
    const dx = e.clientX - press.x, dy = e.clientY - press.y;
    if (dx < -120) stop(false);
    else recording = {...recording, dx:Math.min(0, dx), dy:Math.min(0, dy), locked:dy < -70};
  }
  function up() {
    if (press?.timer) { clearTimeout(press.timer); mode = mode === 'voice' ? 'video' : 'voice'; showHint(mode === 'voice' ? 'Удерживайте для записи голоса' : 'Удерживайте для видео'); }
    else if (recording && !recording.locked) stop(true);
    press = null;
  }
  async function start(locked) {
    voiceError = '';
    const token = {};
    recording = {token, id:sessionId, mode, started:Date.now(), locked, dx:0, dy:0};
    now = Date.now();
    let media;
    try { media = await navigator.mediaDevices.getUserMedia({audio:true, video:mode === 'video' ? {facingMode:'user'} : false}); }
    catch (error) { recording = null; voiceError = `Не удалось начать запись: ${error.message}`; return; }
    // Пока ждали разрешения, запись могли отменить или отпустить палец.
    if (recording?.token !== token) { media.getTracks().forEach(track => track.stop()); return; }
    stream = media; chunks = [];
    recording = {...recording, started:Date.now()};
    if (mode === 'video') {
      facing = 'user'; camera = media.getVideoTracks()[0]; frames = [];
      await tick();
      if (recording?.token !== token) { media.getTracks().forEach(track => track.stop()); return; }
      preview.srcObject = new MediaStream([camera]);
      try { await preview.play(); } catch (error) { if (recording?.token === token) { voiceError = `Не удалось начать запись: ${error.message}`; stop(false); } return; }
      if (recording?.token !== token) { media.getTracks().forEach(track => track.stop()); return; }
      canvas = document.createElement('canvas'); canvas.width = 768; canvas.height = 768;
      capture = canvas.captureStream(30);
      const output = new MediaStream([...capture.getVideoTracks(), ...media.getAudioTracks()]);
      let nextFrame = 0;
      const draw = () => {
        if (recording?.token !== token) return;
        if (preview.videoWidth) {
          const ctx = canvas.getContext('2d'), size = Math.min(preview.videoWidth, preview.videoHeight);
          ctx.save();
          if (facing === 'user') { ctx.translate(768, 0); ctx.scale(-1, 1); }
          ctx.drawImage(preview, (preview.videoWidth - size) / 2, (preview.videoHeight - size) / 2, size, size, 0, 0, 768, 768);
          ctx.restore();
          const elapsed = (Date.now() - recording.started) / 1000;
          if (elapsed >= nextFrame) { const time = nextFrame; frames.push({time, data:frameFromCanvas(canvas)}); nextFrame += Number(videoInterval); }
        }
        requestAnimationFrame(draw);
      };
      requestAnimationFrame(draw);
      recorder = new MediaRecorder(output);
    } else recorder = new MediaRecorder(media);
    recorder.ondataavailable = event => chunks.push(event.data);
    recorder.start();
    // Полоски у таймера показывают, что микрофон слышит голос.
    audio = new AudioContext();
    const analyser = audio.createAnalyser(), samples = new Uint8Array(512);
    audio.createMediaStreamSource(media).connect(analyser);
    let level = 0;
    const updateLevel = time => {
      if (recording?.token !== token) return;
      analyser.getByteTimeDomainData(samples);
      const loudness = Math.min(1, 4 * Math.sqrt(samples.reduce((sum, v) => sum + ((v - 128) / 128) ** 2, 0) / samples.length));
      level += (loudness - level) * (loudness > level ? 0.5 : 0.12);
      bars = bars.map((_, i) => 4 + level * 18 * (0.5 + 0.5 * Math.abs(Math.sin(time / 1000 * 7 + i * 1.3))));
      requestAnimationFrame(updateLevel);
    };
    requestAnimationFrame(updateLevel);
  }
  async function flipCamera() {
    const token = recording?.token, next = facing === 'user' ? 'environment' : 'user';
    camera?.stop();
    try {
      const nextStream = await navigator.mediaDevices.getUserMedia({video:{facingMode:{exact:next}}});
      if (recording?.token !== token) { nextStream.getTracks().forEach(track => track.stop()); return; }
      camera = nextStream.getVideoTracks()[0]; facing = next;
      preview.srcObject = nextStream;
      await preview.play();
    } catch (error) {
      if (recording?.token !== token) return;
      voiceError = `Не удалось сменить камеру: ${error.message}`;
      try {
        const previous = await navigator.mediaDevices.getUserMedia({video:{facingMode:{exact:facing}}});
        if (recording?.token !== token) { previous.getTracks().forEach(track => track.stop()); return; }
        camera = previous.getVideoTracks()[0]; preview.srcObject = previous; await preview.play();
      } catch { voiceError = 'Камера недоступна. Завершите или удалите запись.'; }
    }
  }
  function stop(send) {
    const current = recording, active = recorder, media = stream, parts = chunks, captured = frames, videoTrack = camera, canvasStream = capture;
    recording = null; recorder = null; camera = null; capture = null;
    audio?.close(); audio = null; bars = bars.map(() => 4);
    if (!active) { media?.getTracks().forEach(track => track.stop()); videoTrack?.stop(); canvasStream?.getTracks().forEach(track => track.stop()); stream = null; return; }
    stream = null;
    const seconds = (Date.now() - current.started) / 1000;
    active.onstop = () => {
      media.getTracks().forEach(track => track.stop()); videoTrack?.stop(); canvasStream?.getTracks().forEach(track => track.stop());
      if (send && seconds >= 0.5) {
        if (current.mode === 'video') {
          const blob = new Blob(parts, {type:active.mimeType}), length = Math.min(60, seconds);
          const ready = Promise.all(captured.filter(frame => frame.time < length).map(async frame => ({...await frame.data, video:{seconds:length, time:frame.time, note:true}})))
            .then(frames => { if (!frames.length) throw new Error('Не удалось получить кадры видео'); return finishVideo(blob, frames, length, Number(videoInterval)); })
            .catch(error => ({error:error.message}));
          onvideo({id:current.id, url:URL.createObjectURL(blob), video:{seconds:length, time:0, note:true}, ready});
        } else onvoice({id:current.id, key:crypto.randomUUID(), seconds, wav:wavFromRecording(parts)});
      }
    };
    active.stop();
  }

  // Поле растёт вместе с текстом; больше максимальной высоты (max-h в разметке) прокручивается.
  function fit() {
    area.style.height = 'auto';
    area.style.height = area.scrollHeight + 'px';
  }
  $effect(() => { void value; if (area) fit(); });
  // Высота зависит и от ширины: при запуске из кэша поле может появиться раньше, чем у него есть ширина.
  $effect(() => {
    if (!area) return;
    const observer = new ResizeObserver(fit);
    observer.observe(area);
    return () => observer.disconnect();
  });
  function add(files) {
    imageError = '';
    const selected = Array.from(files);
    if (images.length + selected.length > 4) { imageError = 'Можно прикрепить не более 4 файлов'; return; }
    if ([...images, ...selected].filter(item => item.video || item.type?.startsWith('video/')).length > 1) { imageError = 'Можно прикрепить одно видео'; return; }
    const added = selected.map(file => {
      const image = {url:URL.createObjectURL(file), video:file.type.startsWith('video/'), processing:true};
      image.ready = (image.video ? prepareVideo(file, Number(videoInterval)) : prepareImage(file).then(data => ({frames:[data]}))).then(data => data, error => ({error:error.message})).then(result => {
        images = images.map(item => item === image ? {...item, processing:false, error:result.error} : item);
        return result;
      });
      return image;
    });
    images = [...images, ...added];
  }
  function remove(image) {
    URL.revokeObjectURL(image.url);
    images = images.filter(item => item !== image);
  }
  function paste(e) {
    const files = Array.from(e.clipboardData?.files || []).filter(file => file.type.startsWith('image/'));
    if (files.length) { e.preventDefault(); add(files); }
  }

  function submit() {
    if (!disabled && (value.trim() || images.length)) onsubmit(running);
  }

  // Срочная отправка нужна только во время работы агента.
  function keydown(e) {
    if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) {
      e.preventDefault();
      submit();
    }
  }
</script>

{#if recording?.mode === 'video'}
  <div class="pointer-events-none fixed inset-0 bg-black/60 backdrop-blur-[3px]"></div>
  <div class="pointer-events-none absolute bottom-full left-1/2 mb-8 flex -translate-x-1/2 flex-col items-center">
    <div class="relative size-[242px]">
      <video bind:this={preview} muted playsinline class="absolute inset-[6px] size-[230px] rounded-full object-cover" style:transform={facing === 'user' ? 'scaleX(-1)' : ''}></video>
      <svg class="absolute inset-0" width="242" height="242" viewBox="0 0 242 242" aria-hidden="true"><circle cx="121" cy="121" r="119" fill="none" stroke="rgba(255,255,255,.15)" stroke-width="4" /><circle cx="121" cy="121" r="119" fill="none" stroke="#ef4444" stroke-width="4" stroke-linecap="round" stroke-dasharray="747.7" stroke-dashoffset={747.7 * (1 - Math.min(1, (now - recording.started) / 60000))} transform="rotate(-90 121 121)" /></svg>
      {#if recording.locked}<button type="button" aria-label="Сменить камеру" class="pointer-events-auto absolute bottom-0 right-1 grid size-[34px] place-items-center rounded-full border border-neutral-700 bg-neutral-900/90 text-neutral-200" onclick={flipCamera}><Icon name="flip" class="size-4" /></button>{/if}
    </div>
    {#if !recording.locked}<p class="mt-3 text-xs text-neutral-400">Вверх — запись без рук</p>{/if}
  </div>
{/if}
<form class="relative px-3 pb-[max(0.75rem,env(safe-area-inset-bottom))] pt-2" onsubmit={e => { e.preventDefault(); submit(); }}>
  {#if voiceError}<p role="alert" class="mx-auto max-w-3xl text-sm text-red-400">{voiceError}</p>{/if}
  {#if imageError}<p role="alert" class="mx-auto max-w-3xl text-sm text-red-400">{imageError}</p>{/if}
  <div class="mx-auto flex max-w-3xl flex-wrap gap-2">{#each images as image}<div class="relative">{#if image.video}<video class="h-20 rounded" src={image.url} muted playsinline preload="metadata"></video><span class="absolute left-1 top-1 rounded bg-black/60 p-0.5 text-white"><Icon name="video" class="size-3.5" /></span>{:else}<img class="h-20 rounded" src={image.url} alt="Изображение для отправки" />{/if}{#if image.processing}<span role="status" class="absolute bottom-0 left-0 bg-neutral-900/80 px-1 text-xs">Обработка…</span>{/if}{#if image.error}<span role="alert" class="absolute bottom-0 left-0 bg-red-900 px-1 text-xs">{image.error}</span>{/if}<button type="button" class="absolute right-0 top-0 bg-neutral-900 px-1" aria-label="Удалить изображение" onclick={() => remove(image)}>×</button></div>{/each}</div>
  <div bind:this={row} role="group" onpointermove={move} onpointerup={up} onpointercancel={() => { clearTimeout(press?.timer); press = null; if (recording && !recording.locked) stop(false); }} class="relative mx-auto flex w-full max-w-3xl items-end gap-2 rounded-2xl border border-neutral-800 bg-neutral-900 p-2 shadow-lg shadow-black/40 transition-colors focus-within:border-neutral-600">
    {#if hint}<div class="pointer-events-none absolute -top-10 right-0 rounded-lg bg-neutral-800 px-3 py-1.5 text-xs text-neutral-300">{hint}</div>{/if}
    {#if recording?.locked}
    <button type="button" aria-label="Удалить запись" title="Удалить запись" class="grid size-10 shrink-0 place-items-center rounded-xl text-neutral-400 hover:text-neutral-200" onclick={() => stop(false)}><Icon name="trash" class="size-5" /></button>
    <div class="flex min-h-10 flex-1 items-center gap-2 px-1 text-[15px] tabular-nums text-neutral-100"><span class="flex h-[22px] items-center gap-[3px]" aria-hidden="true">{#each bars as height}<span class="w-[3px] rounded-full bg-red-500" style:height="{height}px"></span>{/each}</span>{duration((now - recording.started) / 1000)}</div>
    <button type="button" aria-label={recording.mode === 'video' ? 'Отправить видеосообщение' : 'Отправить голосовое сообщение'} title="Отправить" class="grid size-10 shrink-0 place-items-center rounded-xl bg-neutral-100 text-neutral-950 hover:opacity-80" onclick={() => stop(true)}><Icon name="send" class="size-5" /></button>
    {:else if recording}
    <div class="flex min-h-10 items-center gap-2 px-2 text-[15px] tabular-nums text-neutral-100"><span class="flex h-[22px] items-center gap-[3px]" aria-hidden="true">{#each bars as height}<span class="w-[3px] rounded-full bg-red-500" style:height="{height}px"></span>{/each}</span>{duration((now - recording.started) / 1000)}</div>
    <div class="flex min-h-10 flex-1 items-center justify-center text-sm text-neutral-500" style:transform="translateX({recording.dx}px)" style:opacity={1 + recording.dx / 150}>‹ Влево — отмена</div>
    <div class="size-10 shrink-0"></div>
    <div class="pointer-events-none absolute -top-[72px] right-0 flex h-16 w-10 flex-col items-center justify-center gap-0.5 rounded-full border border-neutral-700 bg-neutral-900 text-neutral-400" style:transform="translateY({recording.dy / 2}px)"><Icon name="lock" class="size-4" /><Icon name="chevron" class="size-4 rotate-180" /></div>
    <div class="pointer-events-none absolute -right-3 top-1/2 -mt-8 grid size-16 place-items-center rounded-full bg-red-600 text-white shadow-[0_0_0_8px_rgba(220,38,38,0.25)]" style:transform="translate({recording.dx}px, {recording.dy}px)"><Icon name={recording.mode === 'video' ? 'video' : 'microphone'} class="size-6" /></div>
    {:else}
    <label class="grid size-10 shrink-0 cursor-pointer place-items-center rounded-xl bg-neutral-800 text-neutral-400 focus-within:outline focus-within:outline-2 focus-within:outline-offset-2" title="Прикрепить изображение или видео"><Icon name="paperclip" /><span class="sr-only">Прикрепить изображение или видео</span><input type="file" accept="image/*,video/*" multiple class="sr-only" {disabled} onchange={e => { add(e.currentTarget.files); e.currentTarget.value = ''; }} /></label>
    <label class="sr-only" for="message">Сообщение</label>
    <textarea
      id="message"
      rows="1"
      bind:this={area}
      bind:value
      onkeydown={keydown}
      onpaste={paste}
      placeholder="Напишите сообщение"
      class="max-h-40 min-h-10 flex-1 resize-none bg-transparent px-2 py-2 text-[15px] text-neutral-100 placeholder:text-neutral-600 focus:outline-none"
    ></textarea>
    {#if running && !value.trim() && !images.length}
      <button
        type="button"
        class="grid size-10 shrink-0 cursor-pointer place-items-center rounded-xl bg-red-700 text-white transition-colors hover:bg-red-600 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-red-400 disabled:cursor-not-allowed disabled:opacity-30"
        disabled={stopDisabled}
        aria-label="Остановить текущий ход"
        title="Остановить текущий ход"
        onclick={oninterrupt}
      ><Icon name="stop" class="size-5" /></button>
    {/if}
    {#if running && (value.trim() || images.length)}
      <button
        type="button"
        class="grid size-10 shrink-0 cursor-pointer place-items-center rounded-xl border border-neutral-700 text-neutral-100 transition-opacity hover:opacity-80 disabled:cursor-not-allowed disabled:opacity-30"
        disabled={disabled || (!value.trim() && !images.length)}
        aria-label="Добавить в очередь"
        title="Отправить после текущего хода"
        onclick={() => onsubmit(false)}
      >
        <Icon name="queue" class="size-5" />
      </button>
    {/if}
    {#if !value.trim() && !images.length}
    <button type="button" {disabled} onpointerdown={down} onclick={e => { if (e.detail === 0) start(true); }} oncontextmenu={e => e.preventDefault()} aria-label={mode === 'voice' ? 'Голосовое сообщение' : 'Видеосообщение'} title={mode === 'voice' ? 'Голосовое сообщение' : 'Видеосообщение'} class="grid size-10 shrink-0 touch-none select-none place-items-center rounded-xl bg-neutral-800 text-neutral-300 disabled:opacity-30"><Icon name={mode === 'voice' ? 'microphone' : 'video'} class="size-5" /></button>
    {/if}
    {#if value.trim() || images.length}
    <button
      class="grid size-10 shrink-0 cursor-pointer place-items-center rounded-xl bg-neutral-100 text-neutral-950 transition-opacity hover:opacity-80 disabled:cursor-not-allowed disabled:opacity-30"
      disabled={disabled || (!value.trim() && !images.length)}
      aria-label={running ? 'Прервать и отправить' : 'Отправить'}
      title={running ? 'Прервать текущий ход и отправить сразу' : 'Отправить'}
    >
      <Icon name="send" class="size-5" />
    </button>
    {/if}
    {/if}
  </div>
</form>
