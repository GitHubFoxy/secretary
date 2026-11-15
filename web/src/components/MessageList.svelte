<script>
  let { messages = [] } = $props();
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
      {#if m.role === 'Вы'}
        <div class="flex justify-end">
          <p class="max-w-[85%] whitespace-pre-wrap break-words rounded-2xl rounded-br-md bg-neutral-800 px-4 py-2.5 text-[15px] leading-relaxed text-neutral-100">{m.text}</p>
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
