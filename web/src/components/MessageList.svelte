<script>
  import Markdown from './Markdown.svelte';
  import Report from './Report.svelte';
  import Icon from './Icon.svelte';
  import { duration, parseVoice } from '../voice.js';
  import { videoText } from '../prepare-video.js';
  import { fileSize } from '../file-size.js';
  import { tick } from 'svelte';
  // bottomInset — высота поля ввода поверх списка (плюс клавиатура), под ней должны оставаться видимыми последние сообщения.
  let { messages = [], sessions = [], historyLoaded = false, connected = false, historyHasMore = false, historyLoadingOlder = false, onloadOlder, bottomInset = 0, onselect, onsendQueued, onretryVoice, onrestorePreparing, restoreDisabled = false } = $props();
  const isImage = type => type.startsWith('image/'), isVideo = type => type.startsWith('video/');
  const fileUrl = file => `/files/${file.id}/${encodeURIComponent(file.name)}`;
  // Фото и видео показываем из images, а их оригиналы лежат в files; остальные файлы остаются карточками.
  // Оригиналы фото идут в том же порядке, что и их кадры в images.
  function shown(images) {
    const photos = images.filter(image => image.file && isImage(image.type));
    return images
      .filter(image => !image.file || !(isImage(image.type) || isVideo(image.type)))
      .map(image => image.file || image.video ? image : {...image, original: photos.shift()});
  }
  let scroller = $state();
  let initialScroll = true;
  // Пока пользователь у самого низа, новые размеры списка не сдвигают его последние сообщения под поле ввода.
  let pinned = $state(true);
  let loading = false;

  async function onscroll() {
    pinned = scroller.scrollHeight - scroller.scrollTop - scroller.clientHeight < 80;
    const items = scroller.querySelectorAll('[data-message]');
    if (!connected || !historyHasMore || historyLoadingOlder || loading || !items[9] || items[9].getBoundingClientRect().bottom < scroller.getBoundingClientRect().top) return;
    loading = true;
    const anchor = [...items].find(item => item.getBoundingClientRect().bottom > scroller.getBoundingClientRect().top);
    const offset = anchor?.offsetTop;
    try {
      await onloadOlder();
      await tick();
      if (anchor?.isConnected) scroller.scrollTop += anchor.offsetTop - offset;
    } finally { loading = false; }
  }

  // Новые сообщения не сдвигают пользователя, пока он читает историю выше.
  $effect(() => {
    void messages.length;
    if (scroller) {
      if (initialScroll || (pinned && !loading)) scroller.scrollTo({ top: scroller.scrollHeight, behavior: initialScroll ? 'instant' : 'smooth' });
      initialScroll = false;
    }
  });

  // Растущее поле ввода или клавиатура меняют нижний отступ: если мы внизу, держим последние сообщения видимыми.
  $effect(() => {
    void bottomInset;
    if (scroller && pinned) scroller.scrollTop = scroller.scrollHeight;
  });

  // Изменение высоты окна (например, клавиатура на Android) тоже не должно прятать последние сообщения.
  $effect(() => {
    if (!scroller) return;
    const observer = new ResizeObserver(() => { if (pinned) scroller.scrollTop = scroller.scrollHeight; });
    observer.observe(scroller);
    return () => observer.disconnect();
  });
</script>

<div bind:this={scroller} onscroll={onscroll} class="min-h-0 flex-1 overflow-y-auto overscroll-contain [overflow-anchor:none]" aria-live="polite">
  <div class="mx-auto flex w-full max-w-3xl flex-col gap-4 px-4 pt-6" style="padding-bottom: {bottomInset + 24}px">
    {#if messages.length === 0}
      <p class="py-16 text-center text-sm text-neutral-600">{!connected ? 'Нет связи с Host. Повторное подключение…' : !historyLoaded ? 'Загрузка сообщений…' : 'Сообщений пока нет'}</p>
    {/if}
    {#each messages as m (m.key)}
      <div data-message>
      {#if m.kind === 'report'}
        <div class="flex justify-start">
          <Report message={m} session={sessions.find(s => s.id === m.report.id)} {onselect} />
        </div>
      {:else if m.kind === 'voice'}
        <div class="flex flex-col items-end gap-1">
          <div class="flex items-center gap-2 rounded-2xl rounded-br-md border bg-neutral-800 px-4 py-2.5 text-sm text-neutral-400 {m.error ? 'border-red-900' : 'border-transparent'}"><Icon name="microphone" class="size-4" />{duration(m.seconds)}{#if !m.error}<span class="animate-pulse">· распознаю…</span>{/if}</div>
          {#if m.error}<p class="max-w-[85%] text-right text-xs text-red-400">Не удалось распознать: {m.error} <button class="text-neutral-200 underline" disabled={!connected} onclick={() => onretryVoice(m)}>Повторить</button></p>{/if}
        </div>
      {:else if m.kind === 'user' || m.kind === 'queued' || m.kind === 'cancelled' || m.kind === 'uncertain' || m.kind === 'preparing'}
        {@const voice = parseVoice(m.text)}
        {@const videoFile = m.images.find(image => image.file && isVideo(image.type))}
        <div class="flex flex-col items-end gap-1">
          {#if m.kind === 'preparing'}<span class="text-xs text-neutral-400">{m.error ? `Не отправлено: ${m.error}` : 'Обработка…'}</span>{/if}
          {#if m.kind === 'queued'}<span class="text-xs text-neutral-500">{m.urgent ? 'В очереди, срочное' : 'В очереди'}</span>{/if}
          {#if m.kind === 'cancelled'}<span class="text-xs text-neutral-500">Не отправлено: сессия остановлена</span>{/if}
          {#if m.kind === 'uncertain'}<span class="text-xs text-neutral-400">Не удалось подтвердить доставку. Проверьте историю перед повторной отправкой.</span>{/if}
          {#if m.images.some(image => image.video?.note)}
            {@const frame = m.images.find(image => image.video?.note)}
            {@const video = videoText(m.text)}
            {@const src = videoFile ? fileUrl(videoFile) : frame.url}
            <div class="relative size-[170px] overflow-hidden rounded-full bg-neutral-800">
              {#if src}<button type="button" class="size-full" aria-label="Воспроизвести или остановить видеосообщение" onclick={e => { const video = e.currentTarget.querySelector('video'); video.paused ? video.play() : video.pause(); }}><video class="size-full object-cover" {src} poster={'data:' + frame.type + ';base64,' + frame.data} playsinline preload="metadata"><track kind="captions" /></video></button>{:else}<img class="size-full object-cover" src={'data:' + frame.type + ';base64,' + frame.data} alt="Кадр видеосообщения" />{/if}
              <span class="pointer-events-none absolute bottom-2 left-1/2 flex -translate-x-1/2 items-center gap-1 rounded-md bg-black/60 px-1.5 py-0.5 text-xs text-white"><Icon name="video" class="size-3" />{duration(frame.video.seconds)}</span>
            </div>
            {#if video.transcript}<span class="flex max-w-[80%] gap-1.5 text-sm text-neutral-400"><Icon name="microphone" class="mt-0.5 size-3.5 shrink-0" />{video.transcript}</span>{/if}
          {:else}
          <div class="max-w-[85%] whitespace-pre-wrap break-words rounded-2xl rounded-br-md bg-neutral-800 px-4 py-2.5 text-[15px] leading-relaxed text-neutral-100 {m.kind === 'queued' ? 'opacity-60' : ''} {m.kind === 'cancelled' ? 'line-through opacity-50' : ''}">{#each shown(m.images).filter(image => !image.video || image.video === true || image.video.time === 0) as image}{#if image.file}<span class="my-2 flex items-center gap-2 rounded bg-neutral-700 px-3 py-2"><Icon name="file" class="size-6 shrink-0" /><span><span class="block break-all">{image.name}</span><span class="text-xs text-neutral-400">{fileSize(image.size)}</span></span></span>{:else if image.video}{@const src = videoFile ? fileUrl(videoFile) : image.url}<span class="relative my-2 block w-fit">{#if src}<video class="max-h-80 max-w-full rounded-lg" {src} controls playsinline preload="metadata"><track kind="captions" /></video>{:else}<img class="max-h-80 max-w-full rounded-lg" src={'data:' + image.type + ';base64,' + image.data} alt="Кадр видео" />{/if}<span class="absolute bottom-2 left-2 flex items-center gap-1 rounded-md bg-black/60 px-1.5 py-0.5 text-xs text-white"><Icon name="video" class="size-3.5" />{image.video.seconds ? duration(image.video.seconds) : 'Видео'}</span></span>{:else}<a href={image.original ? fileUrl(image.original) : image.url || 'data:' + image.type + ';base64,' + image.data} target="_blank" rel="noopener"><img class="my-2 max-h-80 max-w-full rounded-lg" src={image.url || 'data:' + image.type + ';base64,' + image.data} alt="Прикреплённое изображение" /></a>{/if}{/each}{#if voice}<span class="mb-1 flex items-center gap-1.5 text-xs text-neutral-500"><Icon name="microphone" class="size-3.5" />{voice.duration}</span>{voice.text}{:else if m.images.some(image => image.video)}{@const video = videoText(m.text)}{video.text}{#if video.transcript}<span class="mt-1 flex gap-1.5 text-sm text-neutral-400"><Icon name="microphone" class="mt-0.5 size-3.5 shrink-0" />{video.transcript}</span>{/if}{:else}{m.text}{/if}</div>
          {/if}
          {#if m.kind === 'preparing' && m.error}<button class="btn-ghost text-xs" disabled={restoreDisabled} onclick={() => onrestorePreparing(m.id)}>Вернуть в поле ввода</button>{/if}
          {#if m.kind === 'queued' && !m.urgent}<button class="btn-ghost text-xs" disabled={!connected} onclick={() => onsendQueued(m.id)}>Отправить сейчас</button>{/if}
        </div>
      {:else if m.kind === 'error'}
        <div class="flex justify-start">
          <p class="max-w-[85%] whitespace-pre-wrap break-words text-[15px] leading-relaxed text-red-400">Ошибка: {m.text}</p>
        </div>
      {:else}
        <div class="flex justify-start">
          <div class="max-w-[85%] min-w-0 px-1">
            <Markdown text={m.text} />
          </div>
        </div>
      {/if}
      </div>
    {/each}
  </div>
</div>
<!-- Поверх списка, а не в нём: строка в потоке сдвинула бы сообщения во время подгрузки. -->
{#if historyLoadingOlder}<p role="status" class="pointer-events-none absolute left-1/2 top-2 z-20 -translate-x-1/2 rounded-full bg-neutral-800 px-3 py-1 text-xs text-neutral-300">Загрузка…</p>{/if}
{#if !pinned}
  <button type="button" aria-label="К последнему сообщению" class="absolute right-4 z-20 flex size-10 items-center justify-center rounded-full border border-neutral-700 bg-neutral-800 text-neutral-100 shadow-lg" style:bottom="{bottomInset + 12}px" onclick={() => { pinned = true; scroller.scrollTo({top:scroller.scrollHeight, behavior:'smooth'}); }}><Icon name="chevron" /></button>
{/if}
