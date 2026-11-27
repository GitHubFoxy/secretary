<script>
  // Скелетон модалки «Лимиты»: столько строк, сколько окон обычно в секции.
  let { rows = 2 } = $props();
</script>

<div class="flex flex-col gap-4" role="status" aria-label="Загрузка лимитов" aria-busy="true">
  {#each Array(rows) as _, i (i)}
    <div class="flex animate-pulse flex-col gap-2" aria-hidden="true">
      <div class="flex items-center justify-between gap-3">
        <div class="h-4 w-20 rounded bg-neutral-800"></div>
        <div class="h-4 w-24 rounded bg-neutral-800"></div>
      </div>
      <div class="h-2 rounded-full bg-neutral-800"></div>
      <div class="h-3 w-44 rounded bg-neutral-800/70"></div>
    </div>
  {/each}
</div>
