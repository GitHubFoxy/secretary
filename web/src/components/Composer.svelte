<script>
  import { onDestroy } from 'svelte';
  import Icon from './Icon.svelte';
  import { duration, wavFromRecording } from '../voice.js';
  import { prepareImage } from '../prepare-image.js';
  import { prepareVideo } from '../prepare-video.js';
  // onsubmit(immediate): true — прервать идущий ход и отправить сразу (правая кнопка и Enter), false — добавить в очередь (левая кнопка во время хода).
  let { value = $bindable(''), images = $bindable([]), disabled = false, stopDisabled = false, running = false, sessionId, videoInterval = 1, onsubmit, oninterrupt, onvoice } = $props();
  let imageError = $state('');
  let area = $state();
  // Как в Telegram: на сенсорном экране нажатие переключает голос и видео, удержание записывает;
  // мышью удерживать неудобно, поэтому щелчок сразу начинает запись с замком.
  let mode = $state('voice'), recording = $state.raw(null), hint = $state(''), voiceError = $state(''), now = $state(0);
  let row, press, recorder, chunks, stream, audio;
  let bars = $state([4, 4, 4, 4, 4]);
  onDestroy(() => stop(false));
  $effect(() => {
    if (!recording) return;
    const timer = setInterval(() => now = Date.now(), 250);
    return () => clearInterval(timer);
  });
  function showHint(text) { hint = text; setTimeout(() => { if (hint === text) hint = ''; }, 1500); }
  function down(e) {
    if (disabled || recording) return;
    row.setPointerCapture(e.pointerId);
    if (e.pointerType === 'mouse') { start(true); return; }
    press = {x:e.clientX, y:e.clientY, timer:setTimeout(() => { press.timer = null; mode === 'voice' ? start(false) : showHint('Видеосообщения скоро'); }, 250)};
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
    recording = {token, id:sessionId, started:Date.now(), locked, dx:0, dy:0};
    now = Date.now();
    let media;
    try { media = await navigator.mediaDevices.getUserMedia({audio:true}); }
    catch (error) { recording = null; voiceError = `Не удалось записать голос: ${error.message}`; return; }
    // Пока ждали разрешения, запись могли отменить или отпустить палец.
    if (recording?.token !== token) { media.getTracks().forEach(track => track.stop()); return; }
    stream = media; chunks = [];
    recorder = new MediaRecorder(media);
    recorder.ondataavailable = event => chunks.push(event.data);
    recorder.start();
    // Полоски у таймера показывают, что микрофон слышит голос.
    audio = new AudioContext();
    const analyser = audio.createAnalyser(), samples = new Uint8Array(512);
    audio.createMediaStreamSource(media).connect(analyser);
    let level = 0;
    const tick = time => {
      if (recording?.token !== token) return;
      analyser.getByteTimeDomainData(samples);
      const loudness = Math.min(1, 4 * Math.sqrt(samples.reduce((sum, v) => sum + ((v - 128) / 128) ** 2, 0) / samples.length));
      level += (loudness - level) * (loudness > level ? 0.5 : 0.12);
      bars = bars.map((_, i) => 4 + level * 18 * (0.5 + 0.5 * Math.abs(Math.sin(time / 1000 * 7 + i * 1.3))));
      requestAnimationFrame(tick);
    };
    requestAnimationFrame(tick);
  }
  function stop(send) {
    const current = recording, active = recorder, media = stream, parts = chunks;
    recording = null; recorder = null;
    audio?.close(); audio = null; bars = bars.map(() => 4);
    if (!active) return;
    const seconds = (Date.now() - current.started) / 1000;
    active.onstop = () => {
      media.getTracks().forEach(track => track.stop());
      if (send && seconds >= 0.5) onvoice({id:current.id, key:crypto.randomUUID(), seconds, wav:wavFromRecording(parts)});
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

<form class="px-3 pb-[max(0.75rem,env(safe-area-inset-bottom))] pt-2" onsubmit={e => { e.preventDefault(); submit(); }}>
  {#if voiceError}<p role="alert" class="mx-auto max-w-3xl text-sm text-red-400">{voiceError}</p>{/if}
  {#if imageError}<p role="alert" class="mx-auto max-w-3xl text-sm text-red-400">{imageError}</p>{/if}
  <div class="mx-auto flex max-w-3xl flex-wrap gap-2">{#each images as image}<div class="relative">{#if image.video}<video class="h-20 rounded" src={image.url} muted playsinline preload="metadata"></video><span class="absolute left-1 top-1 rounded bg-black/60 p-0.5 text-white"><Icon name="video" class="size-3.5" /></span>{:else}<img class="h-20 rounded" src={image.url} alt="Изображение для отправки" />{/if}{#if image.processing}<span role="status" class="absolute bottom-0 left-0 bg-neutral-900/80 px-1 text-xs">Обработка…</span>{/if}{#if image.error}<span role="alert" class="absolute bottom-0 left-0 bg-red-900 px-1 text-xs">{image.error}</span>{/if}<button type="button" class="absolute right-0 top-0 bg-neutral-900 px-1" aria-label="Удалить изображение" onclick={() => remove(image)}>×</button></div>{/each}</div>
  <div bind:this={row} role="group" onpointermove={move} onpointerup={up} onpointercancel={() => { press = null; if (recording && !recording.locked) stop(false); }} class="relative mx-auto flex w-full max-w-3xl items-end gap-2 rounded-2xl border border-neutral-800 bg-neutral-900 p-2 shadow-lg shadow-black/40 transition-colors focus-within:border-neutral-600">
    {#if hint}<div class="pointer-events-none absolute -top-10 right-0 rounded-lg bg-neutral-800 px-3 py-1.5 text-xs text-neutral-300">{hint}</div>{/if}
    {#if recording?.locked}
    <button type="button" aria-label="Удалить запись" title="Удалить запись" class="grid size-10 shrink-0 place-items-center rounded-xl text-neutral-400 hover:text-neutral-200" onclick={() => stop(false)}><Icon name="trash" class="size-5" /></button>
    <div class="flex min-h-10 flex-1 items-center gap-2 px-1 text-[15px] tabular-nums text-neutral-100"><span class="flex h-[22px] items-center gap-[3px]" aria-hidden="true">{#each bars as height}<span class="w-[3px] rounded-full bg-red-500" style:height="{height}px"></span>{/each}</span>{duration((now - recording.started) / 1000)}</div>
    <button type="button" aria-label="Отправить голосовое сообщение" title="Отправить" class="grid size-10 shrink-0 place-items-center rounded-xl bg-neutral-100 text-neutral-950 hover:opacity-80" onclick={() => stop(true)}><Icon name="send" class="size-5" /></button>
    {:else if recording}
    <div class="flex min-h-10 items-center gap-2 px-2 text-[15px] tabular-nums text-neutral-100"><span class="flex h-[22px] items-center gap-[3px]" aria-hidden="true">{#each bars as height}<span class="w-[3px] rounded-full bg-red-500" style:height="{height}px"></span>{/each}</span>{duration((now - recording.started) / 1000)}</div>
    <div class="flex min-h-10 flex-1 items-center justify-center text-sm text-neutral-500" style:transform="translateX({recording.dx}px)" style:opacity={1 + recording.dx / 150}>‹ Влево — отмена</div>
    <div class="size-10 shrink-0"></div>
    <div class="pointer-events-none absolute -top-[72px] right-0 flex h-16 w-10 flex-col items-center justify-center gap-0.5 rounded-full border border-neutral-700 bg-neutral-900 text-neutral-400" style:transform="translateY({recording.dy / 2}px)"><Icon name="lock" class="size-4" /><Icon name="chevron" class="size-4 rotate-180" /></div>
    <div class="pointer-events-none absolute -right-3 top-1/2 -mt-8 grid size-16 place-items-center rounded-full bg-red-600 text-white shadow-[0_0_0_8px_rgba(220,38,38,0.25)]" style:transform="translate({recording.dx}px, {recording.dy}px)"><Icon name="microphone" class="size-6" /></div>
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
