<script>
  import MessageItem from './MessageItem.svelte';
  import Icon from './Icon.svelte';
  import { tick } from 'svelte';
  // bottomInset — высота поля ввода поверх списка (плюс клавиатура), под ней должны оставаться видимыми последние сообщения.
  let { ondecide, messages = [], sessions = [], historyLoaded = false, historyLoading = false, historyError = '', onretryHistory, connected = false, historyHasMore = false, historyLoadingOlder = false, onloadOlder, bottomInset = 0, onselect, onsendQueued, onretryVoice, oncancelVoice, onrestorePreparing, restoreDisabled = false } = $props();
  let scroller = $state();
  let content = $state();
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

  // Пока пользователь внизу, любое изменение размеров — новое сообщение, картинка, поле ввода, клавиатура — оставляет его внизу.
  // Прокрутка мгновенная: плавную браузер прерывает в фоновой вкладке, и её промежуточные события сбрасывали pinned.
  $effect(() => {
    if (!scroller) return;
    const observer = new ResizeObserver(() => { if (pinned && !loading) scroller.scrollTop = scroller.scrollHeight; });
    observer.observe(scroller);
    observer.observe(content);
    return () => observer.disconnect();
  });
</script>

<div bind:this={scroller} onscroll={onscroll} class="min-h-0 flex-1 overflow-y-auto overscroll-contain [overflow-anchor:none]" aria-live="polite">
  <div bind:this={content} class="mx-auto flex w-full max-w-3xl flex-col gap-4 px-4 pt-6" style="padding-bottom: {bottomInset + 24}px">
    {#if historyLoading}
      <p role="status" class="text-center text-sm text-neutral-400">Загрузка сообщений…</p>
    {:else if historyError}
      <p role="alert" class="text-center text-sm text-neutral-400">Не удалось загрузить сообщения: {historyError} <button class="underline" onclick={onretryHistory}>Повторить</button></p>
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
