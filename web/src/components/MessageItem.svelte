<script>
  import Markdown from './Markdown.svelte';
  import Report from './Report.svelte';
  import Icon from './Icon.svelte';
  import Modal from './Modal.svelte';
  import { duration, parseVoice } from '../voice.js';
  import { videoText } from '../prepare-video.js';
  import { fileSize } from '../file-size.js';
  import { harnessInfo, modelLabel } from '../agents.js';

  let { message: m, sessions = [], connected = false, restoreDisabled = false, onselect, onsendQueued, onretryVoice, onrestorePreparing } = $props();

  const isImage = type => type.startsWith('image/');
  const isVideo = type => type.startsWith('video/');
  // Картинку открываем внутри приложения: ссылка target="_blank" в Android-оболочке уводит в окно, откуда нет пути назад.
  let viewed = $state(null);
  let stepsOpen = $state(false);
  const fileUrl = file => `/files/${file.id}/${encodeURIComponent(file.name)}`;

  // Фото и видео показываем из images, а их оригиналы лежат в files; остальные файлы остаются карточками.
  // Оригиналы фото идут в том же порядке, что и их кадры в images.
  function shown(images) {
    const photos = images.filter(image => image.file && isImage(image.type));
    return images
      .filter(image => !image.file || !(isImage(image.type) || isVideo(image.type)))
      .map(image => image.file || image.video ? image : { ...image, original: photos.shift() });
  }
</script>

{#snippet agent(a)}
  {@const info = harnessInfo(a.harness)}
  <span class="inline-flex items-center gap-1 whitespace-nowrap {info.accent}"><Icon name={a.harness} class="size-3.5" />{modelLabel(a.model)}{#if a.reasoning}<span class="text-neutral-500"> · {a.reasoning}</span>{/if}</span>
{/snippet}

<div data-message>
  {#if m.kind === 'steps'}
    <div class="max-w-[85%] text-sm text-neutral-400">
      <p>{m.live ? 'Думает…' : 'Рассуждения и шаги'}</p>
      {#if m.thinking}<p class="mt-1 line-clamp-3 whitespace-pre-wrap text-neutral-500">{m.thinking}</p>{/if}
      {#if m.steps.length}
        {@const last = m.steps[m.steps.length - 1]}
        <button type="button" class="mt-1 text-left" aria-expanded={stepsOpen} onclick={() => stepsOpen = !stepsOpen}>{stepsOpen ? '▾' : '▸'} {m.steps.length} шагов · {last.tool}{last.summary ? `: ${last.summary}` : ''}</button>
        {#if stepsOpen}<ul class="mt-1 space-y-1 pl-4">{#each m.steps as step}<li class="break-words">{step.tool}{step.summary ? `: ${step.summary}` : ''}</li>{/each}</ul>{/if}
      {/if}
    </div>
  {:else if m.kind === 'switch'}
    <div class="flex items-center gap-2 text-xs text-neutral-400" role="note" aria-label="Модель переключена">
      <div class="h-px flex-1 bg-neutral-800"></div>
      <Icon name="switch" class="size-3.5 shrink-0" />
      <span class="flex flex-wrap items-center justify-center gap-x-1.5">{@render agent(m.from)}<span class="text-neutral-600">→</span>{@render agent(m.to)}</span>
      <div class="h-px flex-1 bg-neutral-800"></div>
    </div>
  {:else if m.kind === 'report'}
    <div class="flex justify-start">
      <Report message={m} session={sessions.find(s => s.id === m.report.id)} {onselect} />
    </div>
  {:else if m.kind === 'voice'}
    <div class="flex flex-col items-end gap-1">
      <div class="flex items-center gap-2 rounded-2xl rounded-br-md border bg-neutral-800 px-4 py-2.5 text-sm text-neutral-400 {m.error ? 'border-red-900' : 'border-transparent'}">
        <Icon name="microphone" class="size-4" />{duration(m.seconds)}{#if !m.error}<span class="animate-pulse">· распознаю…</span>{/if}
      </div>
      {#if m.error}
        <p class="max-w-[85%] text-right text-xs text-red-400">
          Не удалось распознать: {m.error}
          <button class="text-neutral-200 underline" disabled={!connected} onclick={() => onretryVoice(m)}>Повторить</button>
        </p>
      {/if}
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
          {#if src}
            <button type="button" class="size-full" aria-label="Воспроизвести или остановить видеосообщение" onclick={e => { const video = e.currentTarget.querySelector('video'); video.paused ? video.play() : video.pause(); }}>
              <video class="size-full object-cover" {src} poster={'data:' + frame.type + ';base64,' + frame.data} playsinline preload="metadata"><track kind="captions" /></video>
            </button>
          {:else}
            <img class="size-full object-cover" src={'data:' + frame.type + ';base64,' + frame.data} alt="Кадр видеосообщения" />
          {/if}
          <span class="pointer-events-none absolute bottom-2 left-1/2 flex -translate-x-1/2 items-center gap-1 rounded-md bg-black/60 px-1.5 py-0.5 text-xs text-white">
            <Icon name="video" class="size-3" />{duration(frame.video.seconds)}
          </span>
        </div>
        {#if video.transcript}<span class="flex max-w-[80%] gap-1.5 text-sm text-neutral-400"><Icon name="microphone" class="mt-0.5 size-3.5 shrink-0" />{video.transcript}</span>{/if}
      {:else}
        <div class="max-w-[85%] whitespace-pre-wrap break-words rounded-2xl rounded-br-md bg-neutral-800 px-4 py-2.5 text-[15px] leading-relaxed text-neutral-100 {m.kind === 'queued' ? 'opacity-60' : ''} {m.kind === 'cancelled' ? 'line-through opacity-50' : ''}">
          {#each shown(m.images).filter(image => !image.video || image.video === true || image.video.time === 0) as image}
            {#if image.file}
              <span class="my-2 flex items-center gap-2 rounded bg-neutral-700 px-3 py-2">
                <Icon name="file" class="size-6 shrink-0" />
                <span>
                  <span class="block break-all">{image.name}</span>
                  <span class="text-xs text-neutral-400">{fileSize(image.size)}</span>
                </span>
              </span>
            {:else if image.video}
              {@const src = videoFile ? fileUrl(videoFile) : image.url}
              <span class="relative my-2 block w-fit">
                {#if src}
                  <video class="max-h-80 max-w-full rounded-lg" {src} controls playsinline preload="metadata"><track kind="captions" /></video>
                {:else}
                  <img class="max-h-80 max-w-full rounded-lg" src={'data:' + image.type + ';base64,' + image.data} alt="Кадр видео" />
                {/if}
                <span class="absolute bottom-2 left-2 flex items-center gap-1 rounded-md bg-black/60 px-1.5 py-0.5 text-xs text-white">
                  <Icon name="video" class="size-3.5" />{image.video.seconds ? duration(image.video.seconds) : 'Видео'}
                </span>
              </span>
            {:else}
              <button type="button" class="block" aria-label="Открыть изображение" onclick={() => (viewed = { src: image.original ? fileUrl(image.original) : image.url || 'data:' + image.type + ';base64,' + image.data, alt: 'Прикреплённое изображение' })}>
                <img class="my-2 max-h-80 max-w-full rounded-lg" src={image.url || 'data:' + image.type + ';base64,' + image.data} alt="Прикреплённое изображение" />
              </button>
            {/if}
          {/each}
          {#if voice}
            <span class="mb-1 flex items-center gap-1.5 text-xs text-neutral-500"><Icon name="microphone" class="size-3.5" />{voice.duration}</span>{voice.text}
          {:else if m.images.some(image => image.video)}
            {@const video = videoText(m.text)}{video.text}{#if video.transcript}<span class="mt-1 flex gap-1.5 text-sm text-neutral-400"><Icon name="microphone" class="mt-0.5 size-3.5 shrink-0" />{video.transcript}</span>{/if}
          {:else}
            {m.text}
          {/if}
        </div>
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
        {#each m.files || [] as file}
          {@const src = fileUrl(file)}
          <div class="mb-3 flex flex-col items-start gap-2">
            {#if isImage(file.type)}
              <button type="button" aria-label="Открыть изображение" onclick={() => (viewed = { src, alt: file.name })}><img class="max-h-80 max-w-full rounded-lg" {src} alt={file.name} /></button>
            {:else if isVideo(file.type)}
              <video class="max-h-80 max-w-full rounded-lg" {src} controls playsinline preload="metadata"><track kind="captions" /></video>
            {:else if file.type.startsWith('audio/')}
              <audio class="w-72 max-w-full" {src} controls preload="metadata"></audio>
            {/if}
            <!-- ?download: сервер отдаёт файл как вложение, и Android WebView передаёт его системному загрузчику. -->
            <a href={src + '?download'} download={file.name} class="flex max-w-full items-center gap-2 rounded-lg bg-neutral-800 px-3 py-2 text-neutral-100 hover:bg-neutral-700">
              <Icon name="file" class="size-6 shrink-0" />
              <span class="min-w-0">
                <span class="block break-all text-sm">{file.name}</span>
                <span class="text-xs text-neutral-400">{fileSize(file.size)}</span>
              </span>
              <Icon name="download" class="ml-2 size-5 shrink-0 text-neutral-400" />
              <span class="sr-only">Скачать</span>
            </a>
          </div>
        {/each}
        {#if m.text}<Markdown text={m.text} />{/if}
      </div>
    </div>
  {/if}
</div>

<Modal open={!!viewed} title={viewed?.alt} onclose={() => (viewed = null)}>
  <img class="mx-auto max-h-[70dvh] max-w-full rounded-lg" src={viewed?.src} alt={viewed?.alt} />
</Modal>
