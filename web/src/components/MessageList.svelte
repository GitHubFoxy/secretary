<script>
  import Markdown from './Markdown.svelte';
  // bottomInset — высота поля ввода поверх списка (плюс клавиатура), под ней должны оставаться видимыми последние сообщения.
  let { messages = [], sessions = [], bottomInset = 0, onselect } = $props();
  let scroller = $state();
  // Пока пользователь у самого низа, новые размеры списка не сдвигают его последние сообщения под поле ввода.
  let pinned = true;

  function onscroll() {
    pinned = scroller.scrollHeight - scroller.scrollTop - scroller.clientHeight < 80;
  }

  // Прокручиваем вниз при каждом новом сообщении.
  $effect(() => {
    void messages.length;
    scroller?.scrollTo({ top: scroller.scrollHeight, behavior: 'smooth' });
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

<div bind:this={scroller} onscroll={onscroll} class="min-h-0 flex-1 overflow-y-auto" aria-live="polite">
  <div class="mx-auto flex w-full max-w-3xl flex-col gap-4 px-4 pt-6" style="padding-bottom: {bottomInset + 24}px">
    {#if messages.length === 0}
      <p class="py-16 text-center text-sm text-neutral-600">Сообщений пока нет</p>
    {/if}
    {#each messages as m, i (i)}
      {#if m.role === 'Отчёт'}
        <div class="flex justify-start">
          <div class="max-w-[85%] min-w-0 rounded-2xl bg-neutral-900 px-4 py-3 text-sm">
            <div class="mb-2 flex flex-wrap gap-x-2 text-neutral-400">
              {#if sessions.some(s => s.id === m.id)}
                <button class="cursor-pointer text-neutral-200 underline" onclick={() => onselect(m.id)}>{sessions.find(s => s.id === m.id)?.name || `Сессия #${m.id}`}</button>
              {:else}
                <span>Сессия #{m.id}</span>
              {/if}
              <span>{m.harness}</span><span class="break-all">{m.folder}</span>
            </div>
            <details class="report">
              <summary class="cursor-pointer text-[15px] leading-relaxed text-neutral-300">
                <span class="report-preview"><Markdown text={m.text} /></span>
                <span class="report-hide text-xs text-neutral-500">Свернуть</span>
              </summary>
              <div class="mt-2 min-w-0 border-t border-neutral-800 pt-2">
                <Markdown text={m.text} />
              </div>
            </details>
          </div>
        </div>
      {:else if m.role === 'Вы' || m.role === 'В очереди' || m.role === 'Не отправлено'}
        <div class="flex flex-col items-end gap-1">
          {#if m.role === 'В очереди'}<span class="text-xs text-neutral-500">{m.urgent ? 'В очереди, срочное' : 'В очереди'}</span>{/if}
          {#if m.role === 'Не отправлено'}<span class="text-xs text-neutral-500">Не отправлено: сессия остановлена</span>{/if}
          <div class="max-w-[85%] whitespace-pre-wrap break-words rounded-2xl rounded-br-md bg-neutral-800 px-4 py-2.5 text-[15px] leading-relaxed text-neutral-100 {m.role === 'В очереди' ? 'opacity-60' : ''} {m.role === 'Не отправлено' ? 'line-through opacity-50' : ''}">{#each m.images || [] as image}<a href={'data:' + image.type + ';base64,' + image.data} target="_blank" rel="noopener"><img class="my-2 max-h-80 max-w-full rounded-lg" src={'data:' + image.type + ';base64,' + image.data} alt="Прикреплённое изображение" /></a>{/each}{m.text}</div>
        </div>
      {:else if m.role === 'Ошибка'}
        <div class="flex justify-start">
          <p class="max-w-[85%] whitespace-pre-wrap break-words text-[15px] leading-relaxed text-red-400"><span class="sr-only">Ошибка: </span>{m.text}</p>
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

<style>
  .report:not([open]) .report-preview { display: block; max-height: 6rem; overflow: hidden; pointer-events: none; }
  .report[open] .report-preview, .report:not([open]) .report-hide { display: none; }
</style>
