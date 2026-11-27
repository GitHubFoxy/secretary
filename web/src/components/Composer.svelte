<script>
  import { onDestroy, onMount, tick } from 'svelte';
  import { Capacitor } from '@capacitor/core';
  import Icon from './Icon.svelte';
  import { clipboardImages } from '../clipboard-images.js';
  import { receiveImeImage } from '../ime-image.js';
  import { filterCommands, slashToken } from '../slash-commands.js';
  import AttachmentPopover from './AttachmentPopover.svelte';
  import AttachmentStrip from './AttachmentStrip.svelte';
  import VideoRecorderOverlay from './VideoRecorderOverlay.svelte';
  import { duration, wavFromRecording } from '../voice.js';
  import { prepareImage } from '../prepare-image.js';
  import { prepareVideo, frameFromCanvas, finishVideo } from '../prepare-video.js';
  // onsubmit(immediate): true — прервать идущий ход и отправить сразу (правая кнопка и Enter на ПК), false — добавить в очередь (левая кнопка во время хода).
  let { value = $bindable(''), images = $bindable([]), disabled = false, stopDisabled = false, running = false, sessionId, sessionHarness, sessionFolder, loadCommands, loadReferences, videoInterval = 1, onsubmit, oninterrupt, onvoice, onvideo, onmicpress, onrecordstart, oninput } = $props();
  let commands = $state([]), commandError = $state(''), menuClosed = $state(false), selectedCommand = $state(0), requestToken = 0;
  let cursor = $state(0), references = $state([]), referenceError = $state(''), referenceToken = 0;
  let atMatch = $derived(/(?:^|\s)@([^\s@]*)$/.exec(value.slice(0, cursor)));
  let slashMatch = $derived(slashToken(value.slice(0, cursor)));
  let matches = $derived(atMatch ? references : filterCommands(commands, value.slice(0, cursor)));
  let menuOpen = $derived(!menuClosed && (!!atMatch || !!slashMatch));
  $effect(() => {
    const id = sessionId, query = atMatch?.[1], token = ++referenceToken;
    references = []; referenceError = '';
    if (query !== undefined && id != null && !menuClosed) loadReferences(id, query).then(result => { if (token === referenceToken) references = result; }, error => { if (token === referenceToken) referenceError = error.message; });
  });
  $effect(() => {
    const id = sessionId;
    void sessionHarness; void sessionFolder;
    const open = menuOpen;
    const token = ++requestToken;
    commands = []; commandError = ''; selectedCommand = 0;
    if (open && !atMatch && id != null) loadCommands(id).then(result => { if (token === requestToken) commands = [{name:'compact', description:'Сжать контекст'}, ...result]; }, error => { if (token === requestToken) commandError = error.message; });
  });
  function choose(command) {
    let position;
    if (atMatch) { const start = cursor - atMatch[1].length - 1; value = value.slice(0, start) + command.value + ' ' + value.slice(cursor); position = start + command.value.length + 1; }
    else {
      const start = cursor - slashMatch[1].length - 1;
      const end = cursor + /^[^\s/]*/.exec(value.slice(cursor))[0].length;
      const before = value.slice(0, start), after = value.slice(end);
      const standaloneCompact = command.name === 'compact' && !before.trim() && !after.trim();
      const separator = standaloneCompact || /^\s/.test(after) ? '' : ' ';
      const text = `/${command.name}${separator}`;
      value = before + text + after;
      position = start + text.length;
    }
    menuClosed = true; oninput?.(value); tick().then(() => { area?.focus(); area?.setSelectionRange(position, position); cursor = position; });
  }
  function moveCursor(position) {
    const tokenStart = pos => {
      const match = /(?:^|\s)[/@]([^\s/@]*)$/.exec(value.slice(0, pos));
      return match ? pos - match[1].length - 1 : -1;
    };
    if (tokenStart(position) !== tokenStart(cursor)) { menuClosed = false; selectedCommand = 0; }
    cursor = position;
  }
  let imageError = $state(''), attachOpen = $state(false);
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
    onmicpress?.();
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
    if (mode === 'voice') onrecordstart?.();
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
    audio = new AudioContext();
    const analyser = audio.createAnalyser(), samples = new Uint8Array(512);
    audio.createMediaStreamSource(media).connect(analyser);
    let level = 0;
    const updateLevel = time => {
      if (recording?.token !== token) return;
      if (recording.paused) { requestAnimationFrame(updateLevel); return; }
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
  function togglePause() {
    if (!recorder || recorder.state === 'inactive') return;
    const time = Date.now();
    if (recording.paused) { recorder.resume(); recording = {...recording, started:recording.started + time - recording.paused}; }
    else { recorder.pause(); recording = {...recording, paused:time}; }
    now = time;
  }
  function stop(send) {
    const current = recording, active = recorder, media = stream, parts = chunks, captured = frames, videoTrack = camera, canvasStream = capture;
    recording = null; recorder = null; camera = null; capture = null;
    audio?.close(); audio = null; bars = bars.map(() => 4);
    if (!active) { media?.getTracks().forEach(track => track.stop()); videoTrack?.stop(); canvasStream?.getTracks().forEach(track => track.stop()); stream = null; return; }
    stream = null;
    const seconds = ((current.paused || Date.now()) - current.started) / 1000;
    active.onstop = () => {
      media.getTracks().forEach(track => track.stop()); videoTrack?.stop(); canvasStream?.getTracks().forEach(track => track.stop());
      if (send && seconds >= 0.5) {
        if (current.mode === 'video') {
          const file = new File(parts, 'video.' + (active.mimeType.split(/[/;]/)[1] || 'webm'), {type:active.mimeType}), length = Math.min(60, seconds);
          const ready = Promise.all(captured.filter(frame => frame.time < length).map(async frame => ({...await frame.data, video:{seconds:length, time:frame.time, note:true}})))
            .then(frames => { if (!frames.length) throw new Error('Не удалось получить кадры видео'); return finishVideo(file, frames, length, Number(videoInterval)); })
            .catch(error => ({error:error.message}));
          onvideo({id:current.id, url:URL.createObjectURL(file), video:{seconds:length, time:0, note:true}, ready});
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
  export function add(files) {
    imageError = '';
    const selected = Array.from(files);
    if (images.length + selected.length > 4) { imageError = 'Можно прикрепить не более 4 файлов'; return; }
    if ([...images, ...selected].filter(item => item.video || item.type?.startsWith('video/')).length > 1) { imageError = 'Можно прикрепить одно видео'; return; }
    if (selected.some(file => file.size > 100 * 1024 * 1024)) { imageError = 'Файл превышает 100 МБ'; return; }
    const added = selected.map(file => {
      if (!file.type.startsWith('image/') && !file.type.startsWith('video/')) return {file:true, name:file.name, size:file.size, type:file.type, ready:Promise.resolve({frames:[], file})};
      const image = {url:URL.createObjectURL(file), video:file.type.startsWith('video/'), processing:true};
      image.ready = (image.video ? prepareVideo(file, Number(videoInterval)) : prepareImage(file).then(data => ({frames:[data], file}))).then(data => data, error => ({error:error.message})).then(result => {
        images = images.map(item => item === image ? {...item, processing:false, error:result.error} : item);
        return result;
      });
      return image;
    });
    images = [...images, ...added];
  }
  function remove(image) {
    if (image.url) URL.revokeObjectURL(image.url);
    images = images.filter(item => item !== image);
  }
  onMount(() => {
    const ime = window.SecretaryIme;
    if (!ime) return;
    ime.onmessage = ({data}) => {
      receiveImeImage(data, {
        fetchFile: path => fetch(Capacitor.convertFileSrc(path)),
        isFocused: () => document.activeElement === area && !disabled,
        add: file => add([file]),
        done: id => ime.postMessage(JSON.stringify({action:'done', id})),
      }).catch(error => { imageError = error.message; });
    };
    return () => { ime.onmessage = null; ime.postMessage(JSON.stringify({action:'blur'})); };
  });
  function imeFocus(action) { window.SecretaryIme?.postMessage(JSON.stringify({action})); }
  function paste(e) {
    const files = clipboardImages(e.clipboardData);
    if (files.length) { e.preventDefault(); add(files); }
  }

  function submit() {
    if (!disabled && (value.trim() || images.length)) onsubmit(running);
  }

  // На сенсорном экране (основной ввод — палец) Enter переносит строку, отправка — кнопкой.
  const touch = matchMedia('(pointer: coarse)').matches;
  function keydown(e) {
    if (e.isComposing) return;
    if (menuOpen) {
      if (e.key === 'Escape') { e.preventDefault(); menuClosed = true; return; }
      if (matches.length && (e.key === 'ArrowDown' || e.key === 'ArrowUp')) { e.preventDefault(); selectedCommand = (selectedCommand + (e.key === 'ArrowDown' ? 1 : -1) + matches.length) % matches.length; return; }
      if (matches.length && e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); choose(matches[Math.min(selectedCommand, matches.length - 1)]); return; }
    }
    if (e.key === 'Enter' && !e.shiftKey && !e.isComposing && !touch) {
      e.preventDefault();
      submit();
    }
  }
</script>

{#if recording?.mode === 'video'}
  <VideoRecorderOverlay bind:preview {facing} locked={recording.locked} progress={(now - recording.started) / 60000} onflip={flipCamera} />
{/if}
<form class="relative px-3 pb-[max(0.75rem,env(safe-area-inset-bottom))] pt-2" onsubmit={e => { e.preventDefault(); submit(); }}>
  {#if voiceError}<p role="alert" class="mx-auto max-w-3xl text-sm text-red-400">{voiceError}</p>{/if}
  {#if imageError}<p role="alert" class="mx-auto max-w-3xl text-sm text-red-400">{imageError}</p>{/if}
  {#if menuOpen}
    <div class="absolute bottom-full left-3 right-3 z-20 mx-auto max-h-64 max-w-3xl overflow-y-auto rounded-xl border border-neutral-700 bg-neutral-900 p-1 shadow-lg" aria-label="Команды">
      {#if atMatch ? referenceError : commandError}<p class="p-2 text-sm text-red-400">{atMatch ? referenceError : commandError}</p>
      {:else if !matches.length}<p class="p-2 text-sm text-neutral-400">{atMatch ? 'Нет результатов' : 'Нет команд'}</p>
      {:else}{#each matches as command, i}
        <button type="button" class:!bg-neutral-700={i === selectedCommand} class="block w-full rounded-lg p-2 text-left text-sm hover:bg-neutral-700" onclick={() => choose(command)}><span class="text-neutral-100">{atMatch ? command.label : `/${command.name}`}</span> <span class="text-neutral-400">{command.description || ''}</span></button>
      {/each}{/if}
    </div>
  {/if}
  <AttachmentStrip {images} onremove={remove} />
  <AttachmentPopover open={attachOpen} onclose={() => (attachOpen = false)} onadd={add} />
  <div bind:this={row} role="group" onpointermove={move} onpointerup={up} onpointercancel={() => { clearTimeout(press?.timer); press = null; if (recording && !recording.locked) stop(false); }} class="relative mx-auto flex w-full max-w-3xl items-end gap-2 rounded-2xl border border-neutral-800 bg-neutral-900 p-2 shadow-lg shadow-black/40 transition-colors focus-within:border-neutral-600">
    {#if hint}<div class="pointer-events-none absolute -top-10 right-0 rounded-lg bg-neutral-800 px-3 py-1.5 text-xs text-neutral-300">{hint}</div>{/if}
    {#if recording?.locked}
    <button type="button" aria-label="Удалить запись" title="Удалить запись" class="grid size-10 shrink-0 place-items-center rounded-xl text-neutral-400 hover:text-neutral-200" onclick={() => stop(false)}><Icon name="trash" class="size-5" /></button>
    <div class="flex min-h-10 flex-1 items-center gap-2 px-1 text-[15px] tabular-nums text-neutral-100"><span class="flex h-[22px] items-center gap-[3px]" aria-hidden="true">{#each bars as height}<span class="w-[3px] rounded-full bg-red-500" style:height="{height}px"></span>{/each}</span>{duration(((recording.paused || now) - recording.started) / 1000)}</div>
    {#if recording.mode === 'voice'}
    <button type="button" aria-label={recording.paused ? 'Продолжить запись' : 'Приостановить запись'} title={recording.paused ? 'Продолжить' : 'Пауза'} class="grid size-10 shrink-0 place-items-center rounded-xl text-neutral-300" onclick={togglePause}><Icon name={recording.paused ? 'play' : 'pause'} /></button>
    {/if}
    <button type="button" aria-label={recording.mode === 'video' ? 'Отправить видеосообщение' : 'Отправить голосовое сообщение'} title="Отправить" class="grid size-10 shrink-0 place-items-center rounded-xl bg-neutral-100 text-neutral-950 hover:opacity-80" onclick={() => stop(true)}><Icon name="send" class="size-5" /></button>
    {:else if recording}
    <div class="flex min-h-10 items-center gap-2 px-2 text-[15px] tabular-nums text-neutral-100"><span class="flex h-[22px] items-center gap-[3px]" aria-hidden="true">{#each bars as height}<span class="w-[3px] rounded-full bg-red-500" style:height="{height}px"></span>{/each}</span>{duration(((recording.paused || now) - recording.started) / 1000)}</div>
    <div class="flex min-h-10 flex-1 items-center justify-center text-sm text-neutral-500" style:transform="translateX({recording.dx}px)" style:opacity={1 + recording.dx / 150}>‹ Влево — отмена</div>
    <div class="size-10 shrink-0"></div>
    <div class="pointer-events-none absolute -top-[72px] right-0 flex h-16 w-10 flex-col items-center justify-center gap-0.5 rounded-full border border-neutral-700 bg-neutral-900 text-neutral-400" style:transform="translateY({recording.dy / 2}px)"><Icon name="lock" class="size-4" /><Icon name="chevron" class="size-4 rotate-180" /></div>
    <div class="pointer-events-none absolute -right-3 top-1/2 -mt-8 grid size-16 place-items-center rounded-full bg-red-600 text-white shadow-[0_0_0_8px_rgba(220,38,38,0.25)]" style:transform="translate({recording.dx}px, {recording.dy}px)"><Icon name={recording.mode === 'video' ? 'video' : 'microphone'} class="size-6" /></div>
    {:else}
    <button type="button" class="grid size-10 shrink-0 place-items-center rounded-xl bg-neutral-800 text-neutral-400 hover:text-neutral-200 disabled:opacity-40" aria-label="Прикрепить" aria-expanded={attachOpen} title="Прикрепить" {disabled} onclick={() => (attachOpen = !attachOpen)}><Icon name="paperclip" /></button>
    <label class="sr-only" for="message">Сообщение</label>
    <textarea
      id="message"
      rows="1"
      bind:this={area}
      bind:value
      oninput={event => { cursor = event.target.selectionStart; menuClosed = false; selectedCommand = 0; oninput?.(event.target.value); }}
      onclick={event => moveCursor(event.target.selectionStart)}
      onkeyup={event => moveCursor(event.target.selectionStart)}
      onkeydown={keydown}
      onpaste={paste}
      onfocus={() => imeFocus('focus')}
      onblur={() => imeFocus('blur')}
      placeholder="Напишите сообщение"
      class="max-h-40 min-h-10 flex-1 resize-none bg-transparent [scrollbar-width:none] [&::-webkit-scrollbar]:hidden px-2 py-2 text-[15px] text-neutral-100 placeholder:text-neutral-600 focus:outline-none"
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
    <button type="button" {disabled} onpointerdown={down} onclick={e => { if (e.detail === 0) { onmicpress?.(); start(true); } }} oncontextmenu={e => e.preventDefault()} aria-label={mode === 'voice' ? 'Голосовое сообщение' : 'Видеосообщение'} title={mode === 'voice' ? 'Голосовое сообщение' : 'Видеосообщение'} class="grid size-10 shrink-0 touch-none select-none place-items-center rounded-xl bg-neutral-800 text-neutral-300 disabled:opacity-30"><Icon name={mode === 'voice' ? 'microphone' : 'video'} class="size-5" /></button>
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
