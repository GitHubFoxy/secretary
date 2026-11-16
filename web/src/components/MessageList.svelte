<script>
  let { messages = [], sessions = [], onselect } = $props();
  let scroller = $state();

  // Прокручиваем вниз при каждом новом сообщении.
  $effect(() => {
    void messages.length;
    scroller?.scrollTo({ top: scroller.scrollHeight, behavior: 'smooth' });
  });
</script>

<div bind:this={scroller} class="min-h-0 flex-1 overflow-y-auto" aria-live="polite">
  <div class="mx-auto flex w-full max-w-3xl flex-col gap-4 px-4 py-6">
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
              <summary class="cursor-pointer whitespace-pre-wrap break-words text-[15px] leading-relaxed text-neutral-300">{m.text}</summary>
              <span class="text-xs text-neutral-500">Свернуть</span>
            </details>
          </div>
        </div>
      {:else if m.role === 'Вы'}
        <div class="flex justify-end">
          <div class="max-w-[85%] whitespace-pre-wrap break-words rounded-2xl rounded-br-md bg-neutral-800 px-4 py-2.5 text-[15px] leading-relaxed text-neutral-100">{#each m.images || [] as image}<a href={'data:' + image.type + ';base64,' + image.data} target="_blank" rel="noopener"><img class="my-2 max-h-80 max-w-full rounded-lg" src={'data:' + image.type + ';base64,' + image.data} alt="Прикреплённое изображение" /></a>{/each}{m.text}</div>
        </div>
      {:else if m.role === 'Ошибка'}
        <div class="flex justify-start">
          <p class="max-w-[85%] whitespace-pre-wrap break-words text-[15px] leading-relaxed text-red-400"><span class="sr-only">Ошибка: </span>{m.text}</p>
        </div>
      {:else}
        <div class="flex justify-start">
          <p class="max-w-[85%] whitespace-pre-wrap break-words px-1 text-[15px] leading-relaxed text-neutral-300">{m.text}</p>
        </div>
      {/if}
    {/each}
  </div>
</div>

<style>
  .report:not([open]) summary { display: -webkit-box; -webkit-line-clamp: 4; -webkit-box-orient: vertical; overflow: hidden; }
  .report[open] summary { display: block; }
</style>
