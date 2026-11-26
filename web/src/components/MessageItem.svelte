<script>
  import Markdown from './Markdown.svelte';
  import MediaPlayer from './MediaPlayer.svelte';
  import Report from './Report.svelte';
  import Icon from './Icon.svelte';
  import Modal from './Modal.svelte';
  import { duration, parseVoice } from '../voice.js';
  import { videoText } from '../prepare-video.js';
  import { fileSize } from '../file-size.js';
  import { harnessInfo, modelLabel } from '../agents.js';

  let { onskill, onsudo, message: m, sessions = [], connected = false, restoreDisabled = false, onselect, onsendQueued, onretryVoice, oncancelVoice, onrestorePreparing } = $props();

  let password = $state('');
  let sudoBusy = $state(false);
  let sudoError = $state('');
  async function decide(type) {
    sudoBusy = true; sudoError = '';
    const secret = password; password = '';
    try { await onsudo(type, m.id, ['sudoApprove', 'secretApprove'].includes(type) ? secret : undefined); }
    catch (error) { sudoError = error.message; }
    finally { sudoBusy = false; }
  }

  async function decideSkill(type) {
    sudoBusy = true; sudoError = '';
    try { await onskill(type, m.id); } catch (error) { sudoError = error.message; } finally { sudoBusy = false; }
  }

  function skillDiff(before, after) {
    const oldLines = (before || '').split('\n'), newLines = (after || '').split('\n');
    while (oldLines.length && newLines.length && oldLines[0] === newLines[0]) { oldLines.shift(); newLines.shift(); }
    while (oldLines.length && newLines.length && oldLines.at(-1) === newLines.at(-1)) { oldLines.pop(); newLines.pop(); }
    return [...oldLines.map(line => `- ${line}`), ...newLines.map(line => `+ ${line}`)].join('\n');
  }

  const isImage = type => type.startsWith('image/');
  const isVideo = type => type.startsWith('video/');
  // Картинку открываем внутри приложения: ссылка target="_blank" в Android-оболочке уводит в окно, откуда нет пути назад.
  let viewed = $state(null);
  let stepsOpen = $state(false);
  let htmlFile = $state(null);
  let htmlText = $state(null);
  let htmlError = $state(false);
  const isHtml = file => /\.html$/i.test(file.name);
  async function openHtml(file) {
    htmlFile = file;
    htmlText = null;
    htmlError = false;
    try {
      const response = await fetch(fileUrl(file));
      if (!response.ok) throw new Error('Failed to load');
      const text = await response.text();
      if (htmlFile?.id === file.id) htmlText = text;
    } catch {
      if (htmlFile?.id === file.id) htmlError = true;
    }
  }
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
  {#if m.kind === 'skill'}
    <div class="max-w-full rounded-xl border border-amber-700 bg-neutral-900 p-4 text-sm text-neutral-100">
      <p>Предложение skill: {m.name}</p>
      <p class="whitespace-pre-wrap break-words">Причина: {m.reason}</p>
      <p class="mt-2">Изменения:</p><pre class="max-h-96 overflow-auto whitespace-pre-wrap break-all">{skillDiff(m.before, m.content)}</pre>
      {#if m.status}<p class="mt-2">{m.status === 'approved' ? 'Принято' : 'Отклонено'}</p>
      {:else}<div class="mt-3 flex gap-3"><button class="rounded bg-amber-700 px-3 py-2" disabled={!connected || sudoBusy} onclick={() => decideSkill('skillApprove')}>Принять</button><button class="rounded bg-neutral-700 px-3 py-2" disabled={!connected || sudoBusy} onclick={() => decideSkill('skillReject')}>Отклонить</button></div>{#if sudoError}<p class="mt-2 text-red-400">{sudoError}</p>{/if}{/if}
    </div>
  {:else if m.kind === 'secret'}
    <div class="max-w-full rounded-xl border border-amber-700 bg-neutral-900 p-4 text-sm text-neutral-100">
      <p>Агент запрашивает секрет: {m.name}</p><p class="whitespace-pre-wrap break-words">Причина: {m.reason}</p>
      {#if m.status}<p class="mt-2">{m.status === 'saved' ? 'Сохранён' : m.status}</p>
      {:else}
        <label class="mt-3 block">Секрет <input class="mt-1 block w-full rounded bg-neutral-800 p-2" type="password" autocomplete="off" bind:value={password} /></label>
        <div class="mt-3 flex gap-3"><button class="rounded bg-amber-700 px-3 py-2" disabled={!connected || sudoBusy || !password} onclick={() => decide('secretApprove')}>Сохранить</button><button class="rounded bg-neutral-700 px-3 py-2" disabled={!connected || sudoBusy} onclick={() => decide('secretReject')}>Отклонить</button></div>
        {#if sudoError}<p class="mt-2 text-red-400">{sudoError}</p>{/if}
      {/if}
    </div>
  {:else if m.kind === 'sudo'}
    <div class="max-w-full rounded-xl border border-amber-700 bg-neutral-900 p-4 text-sm text-neutral-100">
      <p>Агент хочет выполнить от root:</p>
      <pre class="my-2 whitespace-pre-wrap break-all">{m.command}</pre>
      <p class="whitespace-pre-wrap break-words">Причина: {m.reason}</p>
      {#if m.status}<p class="mt-2">{({completed:'Выполнено', rejected:'Отклонено', timeout:'Время ожидания истекло', error:'Ошибка запуска'})[m.status] || m.status}</p>
      {:else}
        <label class="mt-3 block">Пароль sudo <input class="mt-1 block w-full rounded bg-neutral-800 p-2" type="password" autocomplete="off" bind:value={password} /></label>
        <div class="mt-3 flex gap-3"><button class="rounded bg-amber-700 px-3 py-2" disabled={!connected || sudoBusy || !password} onclick={() => decide('sudoApprove')}>Выполнить</button><button class="rounded bg-neutral-700 px-3 py-2" disabled={!connected || sudoBusy} onclick={() => decide('sudoReject')}>Отклонить</button></div>
        {#if sudoError}<p class="mt-2 text-red-400">{sudoError}</p>{/if}
      {/if}
    </div>
  {:else if m.kind === 'activity'}
    <p class="truncate text-sm text-neutral-400">{m.text}</p>
  {:else if m.kind === 'steps'}
    <div class="max-w-[85%] text-sm text-neutral-400">
      <p class="truncate">{m.live ? m.activity : 'Рассуждения и шаги'}</p>
      {#if m.thinking}<p class="mt-1 line-clamp-3 whitespace-pre-wrap text-neutral-500">{m.thinking}</p>{/if}
      {#if m.steps.length}
        {@const last = m.steps[m.steps.length - 1]}
        <button type="button" class="mt-1 text-left" aria-expanded={stepsOpen} onclick={() => stepsOpen = !stepsOpen}>{stepsOpen ? '▾' : '▸'} {m.steps.length} шагов · {last.tool}{last.summary ? `: ${last.summary}` : ''}</button>
        {#if stepsOpen}<ul class="mt-1 space-y-1 pl-4">{#each m.steps as step}<li class="break-words">{step.tool}{step.summary ? `: ${step.summary}` : ''}</li>{/each}</ul>{/if}
      {/if}
    </div>
  {:else if m.kind === 'compaction'}
    <div class="flex items-center gap-2 text-xs text-neutral-400" role="note" aria-label="Контекст сжат">
      <div class="h-px flex-1 bg-neutral-800"></div>
      <span class="text-center">Контекст сжат{#if m.beforeTokens != null && m.afterTokens != null} · {new Intl.NumberFormat('ru', { notation: 'compact', maximumFractionDigits: 1 }).format(m.beforeTokens)} → {new Intl.NumberFormat('ru', { notation: 'compact', maximumFractionDigits: 1 }).format(m.afterTokens)} токенов{:else if m.beforeTokens != null} · до сжатия: {new Intl.NumberFormat('ru', { notation: 'compact', maximumFractionDigits: 1 }).format(m.beforeTokens)} токенов{/if} · <time datetime={m.time} title={new Date(m.time).toLocaleString('ru-RU')}>{new Date(m.time).toLocaleTimeString('ru-RU', {hour: '2-digit', minute: '2-digit'})}</time></span>
      <div class="h-px flex-1 bg-neutral-800"></div>
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
          <button class="text-neutral-200 underline" onclick={() => oncancelVoice(m)}>Отменить</button>
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
        {#if src}
          <MediaPlayer {src} video round poster={'data:' + frame.type + ';base64,' + frame.data} title="Видеосообщение" />
        {:else}
          <img class="size-[170px] rounded-full object-cover" src={'data:' + frame.type + ';base64,' + frame.data} alt="Кадр видеосообщения" />
        {/if}
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
                  {#if image.id && isHtml(image)}<button type="button" class="block underline" onclick={() => openHtml(image)}>Открыть</button><a class="block underline" href={fileUrl(image) + '?download'} download={image.name}>Скачать</a>{/if}
                </span>
              </span>
            {:else if image.video}
              {@const src = videoFile ? fileUrl(videoFile) : image.url}
              <span class="relative my-2 block w-fit">
                {#if src}
                  <MediaPlayer {src} video />
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
      {#if m.kind === 'queued' && !m.urgent}<button class="inline-flex cursor-pointer items-center gap-1.5 rounded-full border border-neutral-700 bg-neutral-900 px-3 py-1.5 text-xs text-neutral-200 transition-colors hover:border-neutral-500 hover:text-white disabled:cursor-not-allowed disabled:opacity-40" disabled={!connected} onclick={() => onsendQueued(m.id)}><Icon name="send" class="size-3.5" />Отправить сейчас</button>{/if}
    </div>
  {:else if m.kind === 'error'}
    <div class="flex justify-start">
      <p class="max-w-[85%] whitespace-pre-wrap break-words text-[15px] leading-relaxed text-red-400">Ошибка: {m.text}</p>
    </div>
  {:else}
    <div class="flex justify-start">
      <div class="max-w-[85%] min-w-0 px-1">
        {#if m.preview}
          <div class="mb-3 flex flex-col items-start gap-2 rounded-lg bg-neutral-800 p-3 text-sm text-neutral-100">
            <span>Страница доступна каждому, у кого есть ссылка, до {new Date(m.preview.expires_at).toLocaleString('ru-RU')}.</span>
            <a class="rounded-lg bg-neutral-700 px-3 py-2 hover:bg-neutral-600" href={m.preview.url} target="_blank" rel="noopener noreferrer">Открыть</a>
          </div>
        {/if}
        {#each m.files || [] as file}
          {@const src = fileUrl(file)}
          <div class="mb-3 flex flex-col items-start gap-2">
            {#if isImage(file.type)}
              <button type="button" aria-label="Открыть изображение" onclick={() => (viewed = { src, alt: file.name })}><img class="max-h-80 max-w-full rounded-lg" {src} alt={file.name} /></button>
            {:else if isVideo(file.type)}
              <MediaPlayer {src} video />
            {:else if file.type.startsWith('audio/')}
              <MediaPlayer {src} title={file.name} />
            {/if}
            {#if isHtml(file)}<button type="button" class="rounded-lg bg-neutral-700 px-3 py-2 text-sm text-white" onclick={() => openHtml(file)}>Открыть</button>{/if}
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

<svelte:window onkeydown={e => { if (htmlFile && e.key === 'Escape') htmlFile = null; }} />
{#if htmlFile}
  <div role="dialog" aria-modal="true" aria-label={htmlFile.name} class="fixed inset-0 z-40 flex flex-col bg-neutral-950">
    <header class="flex shrink-0 items-center justify-between gap-3 px-4 py-3 text-white">
      <span class="truncate">{htmlFile.name}</span>
      <button type="button" class="icon-btn" aria-label="Закрыть" onclick={() => htmlFile = null}><Icon name="close" /></button>
    </header>
    {#if htmlError}<p class="p-4 text-red-400">Не удалось загрузить файл</p>
    {:else if htmlText === null}<p class="p-4 text-neutral-400">Загрузка…</p>
    {:else}<iframe title={htmlFile.name} sandbox srcdoc={htmlText} class="min-h-0 w-full flex-1 bg-white"></iframe>{/if}
  </div>
{/if}
