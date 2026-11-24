<script>
  import MessageItem from './MessageItem.svelte';
  import Icon from './Icon.svelte';
  import { tick } from 'svelte';
  // bottomInset — высота поля ввода поверх списка (плюс клавиатура), под ней должны оставаться видимыми последние сообщения.
  let { messages = [], sessions = [], historyLoaded = false, connected = false, historyHasMore = false, historyLoadingOlder = false, onloadOlder, bottomInset = 0, onselect, onsendQueued, onretryVoice, onrestorePreparing, restoreDisabled = false } = $props();
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
      <MessageItem message={m} {sessions} {connected} {restoreDisabled} {onselect} {onrestorePreparing} {onsendQueued} {onretryVoice} />
    {/each}
  </div>
</div>
<!-- Поверх списка, а не в нём: строка в потоке сдвинула бы сообщения во время подгрузки. -->
{#if historyLoadingOlder}<p role="status" class="pointer-events-none absolute left-1/2 top-2 z-20 -translate-x-1/2 rounded-full bg-neutral-800 px-3 py-1 text-xs text-neutral-300">Загрузка…</p>{/if}
{#if !pinned}
  <button type="button" aria-label="К последнему сообщению" class="absolute right-4 z-20 flex size-10 items-center justify-center rounded-full border border-neutral-700 bg-neutral-800 text-neutral-100 shadow-lg" style:bottom="{bottomInset + 12}px" onclick={() => { pinned = true; scroller.scrollTo({top:scroller.scrollHeight, behavior:'smooth'}); }}><Icon name="chevron" /></button>
{/if}
