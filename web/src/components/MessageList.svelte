<script>
  import Markdown from './Markdown.svelte';
  import Report from './Report.svelte';
  import Icon from './Icon.svelte';
  import { duration, parseVoice } from '../voice.js';
  import { untrack } from 'svelte';
  // bottomInset — высота поля ввода поверх списка (плюс клавиатура), под ней должны оставаться видимыми последние сообщения.
  let { messages = [], sessions = [], historyLoaded = false, connected = false, historyHasMore = false, historyLoadingOlder = false, startAtTop = false, onloadOlder, bottomInset = 0, onselect, onsendQueued, onretryVoice, onrestorePreparing, restoreDisabled = false } = $props();
  let scroller = $state();
  let initialScroll = true;
  // Пока пользователь у самого низа, новые размеры списка не сдвигают его последние сообщения под поле ввода.
  let pinned = untrack(() => !startAtTop);

  function onscroll() {
    pinned = scroller.scrollHeight - scroller.scrollTop - scroller.clientHeight < 80;
  }

  // Новые сообщения не сдвигают пользователя, пока он читает историю выше.
  $effect(() => {
    void messages.length;
    if (scroller) {
      if (initialScroll || pinned) scroller.scrollTo({ top: initialScroll && startAtTop ? 0 : scroller.scrollHeight, behavior: initialScroll ? 'instant' : 'smooth' });
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

<div bind:this={scroller} onscroll={onscroll} class="min-h-0 flex-1 overflow-y-auto overscroll-contain" aria-live="polite">
  <div class="mx-auto flex w-full max-w-3xl flex-col gap-4 px-4 pt-6" style="padding-bottom: {bottomInset + 24}px">
    {#if historyHasMore}
      <button class="btn-ghost self-center text-sm" disabled={!connected || historyLoadingOlder} onclick={onloadOlder}>{historyLoadingOlder ? 'Загрузка…' : 'Показать предыдущие сообщения'}</button>
    {/if}
    {#if messages.length === 0}
      <p class="py-16 text-center text-sm text-neutral-600">{!connected ? 'Нет связи с Host. Повторное подключение…' : !historyLoaded ? 'Загрузка сообщений…' : 'Сообщений пока нет'}</p>
    {/if}
    {#each messages as m (m.key)}
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
        <div class="flex flex-col items-end gap-1">
          {#if m.kind === 'preparing'}<span class="text-xs text-neutral-400">{m.error ? `Не отправлено: ${m.error}` : 'Обработка…'}</span>{/if}
          {#if m.kind === 'queued'}<span class="text-xs text-neutral-500">{m.urgent ? 'В очереди, срочное' : 'В очереди'}</span>{/if}
          {#if m.kind === 'cancelled'}<span class="text-xs text-neutral-500">Не отправлено: сессия остановлена</span>{/if}
          {#if m.kind === 'uncertain'}<span class="text-xs text-neutral-400">Не удалось подтвердить доставку. Проверьте историю перед повторной отправкой.</span>{/if}
          <div class="max-w-[85%] whitespace-pre-wrap break-words rounded-2xl rounded-br-md bg-neutral-800 px-4 py-2.5 text-[15px] leading-relaxed text-neutral-100 {m.kind === 'queued' ? 'opacity-60' : ''} {m.kind === 'cancelled' ? 'line-through opacity-50' : ''}">{#each m.images.filter(image => !image.video || image.video === true || image.video.time === 0) as image}{#if image.video && image.url}<video class="my-2 max-h-80 max-w-full rounded-lg" src={image.url} muted playsinline preload="metadata"></video>{:else}<a href={image.url || 'data:' + image.type + ';base64,' + image.data} target="_blank" rel="noopener"><img class="my-2 max-h-80 max-w-full rounded-lg" src={image.url || 'data:' + image.type + ';base64,' + image.data} alt="Прикреплённое изображение" /></a>{/if}{#if image.video}<span class="flex items-center gap-1 text-xs"><Icon name="video" class="size-4" />{image.video.seconds ? duration(image.video.seconds) : 'Видео'}</span>{/if}{/each}{#if voice}<span class="mb-1 flex items-center gap-1.5 text-xs text-neutral-500"><Icon name="microphone" class="size-3.5" />{voice.duration}</span>{voice.text}{:else}{m.images.some(image => image.video) ? m.text.replace(/\[Видео [^\]\n]*\]\n?/, '').trim() : m.text}{/if}</div>
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
    {/each}
  </div>
</div>
