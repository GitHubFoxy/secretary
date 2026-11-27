<script>
  import MessageItem from './MessageItem.svelte';
  import Icon from './Icon.svelte';
  import { tick } from 'svelte';
  import { shouldPrefetch } from '../history-prefetch.js';
  // bottomInset — высота поля ввода поверх списка (плюс клавиатура), под ней должны оставаться видимыми последние сообщения.
  // Кнопки догрузки истории нет навсегда: older-страницы тянем фоном при приближении к верху (shouldPrefetch)
  // и тихим автоповтором при ошибке — сверху допустим только короткий skeleton/spinner.
  let { ondecide, messages = [], sessions = [], historyLoaded = false, historyLoading = false, connected = false, historyHasMore = false, historyLoadingOlder = false, onloadOlder, bottomInset = 0, onselect, onsendQueued, onretryVoice, oncancelVoice, onrestorePreparing, restoreDisabled = false } = $props();
  let scroller = $state();
  let content = $state();
  let pinned = $state(true);
  let loading = false;

  async function onscroll() {
    pinned = scroller.scrollHeight - scroller.scrollTop - scroller.clientHeight < 80;
    if (!shouldPrefetch({connected, hasMore: historyHasMore, loading: historyLoading, loadingOlder: historyLoadingOlder || loading, scrollTop: scroller.scrollTop})) return;
    await loadOlder();
  }

  async function loadOlder() {
    if (!connected || !historyHasMore || historyLoading || historyLoadingOlder || loading) return;
    const list = scroller;
    const items = list.querySelectorAll('[data-message]');
    loading = true;
    pinned = false;
    const anchor = [...items].find(item => item.getBoundingClientRect().bottom > list.getBoundingClientRect().top);
    const offset = anchor?.offsetTop;
    const height = list.scrollHeight;
    try {
      await onloadOlder();
      await tick();
      if (scroller !== list || !list.isConnected) return;
      if (anchor?.isConnected) list.scrollTop += anchor.offsetTop - offset;
      else list.scrollTop += list.scrollHeight - height;
    } finally { loading = false; }
  }

  // Пока пользователь внизу, любое изменение размеров — новое сообщение, картинка, поле ввода, клавиатура — оставляет его внизу.
  // Прокрутка мгновенная: плавную браузер прерывает в фоновой вкладке, и её промежуточные события сбрасывали pinned.
  $effect(() => {
    if (!scroller) return;
    const observer = new ResizeObserver(() => { if (pinned && !loading) scroller.scrollTop = scroller.scrollHeight; });
    observer.observe(scroller);
    observer.observe(content);
    return () => observer.disconnect();
  });

  // Короткий список (окно предзагрузки ещё не заполнено) дотягиваем фоном без участия пользователя.
  $effect(() => {
    if (!scroller || !connected || !historyHasMore || historyLoading || historyLoadingOlder || loading) return;
    if (historyLoaded && scroller.scrollHeight <= scroller.clientHeight + 600) loadOlder();
  });
</script>

<div bind:this={scroller} onscroll={onscroll} class="min-h-0 flex-1 overflow-y-auto overscroll-contain [overflow-anchor:none]" aria-live="polite">
  <div bind:this={content} class="mx-auto flex w-full max-w-3xl flex-col gap-4 px-4 pt-6" style="padding-bottom: {bottomInset + 24}px">
    {#if historyLoading}
      <p role="status" class="text-center text-sm text-neutral-400">Загрузка сообщений…</p>
    {:else if messages.length === 0}
      <p class="py-16 text-center text-sm text-neutral-600">{!connected ? 'Нет связи с Host. Повторное подключение…' : historyLoaded ? 'Сообщений пока нет' : ''}</p>
    {/if}
    {#each messages as m (m.key)}
      <MessageItem {ondecide} message={m} {sessions} {connected} {restoreDisabled} {onselect} {onrestorePreparing} {onsendQueued} {onretryVoice} {oncancelVoice} />
    {/each}
  </div>
</div>
<!-- Поверх списка, а не в нём: строка в потоке сдвинула бы сообщения во время подгрузки. -->
{#if historyLoadingOlder}<p role="status" class="pointer-events-none absolute left-1/2 top-2 z-20 -translate-x-1/2 rounded-full bg-neutral-800 px-3 py-1 text-xs text-neutral-300">Загрузка…</p>{/if}
{#if !pinned}
  <button type="button" aria-label="К последнему сообщению" class="absolute right-4 z-20 flex size-10 items-center justify-center rounded-full border border-neutral-700 bg-neutral-800 text-neutral-100 shadow-lg" style:bottom="{bottomInset + 12}px" onclick={() => { pinned = true; scroller.scrollTop = scroller.scrollHeight; }}><Icon name="chevron" /></button>
{/if}
