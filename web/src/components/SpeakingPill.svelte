<script>
  let { onstop } = $props();
</script>

<div class="mx-auto mb-2 flex max-w-3xl justify-center px-4">
  <button type="button" onclick={onstop} class="flex items-center gap-2.5 rounded-full border border-neutral-800 bg-neutral-900 py-1.5 pl-3.5 pr-1.5 text-sm text-neutral-300 shadow-lg">
    <span class="flex h-4 items-center gap-[3px]" aria-hidden="true">{#each [0, 1, 2, 3, 4] as i}<span class="speaking-bar w-[3px] rounded-full bg-neutral-300" style:animation-delay="{i * 0.13}s"></span>{/each}</span>
    Озвучиваю
    <span class="flex items-center gap-1.5 rounded-full bg-neutral-800 px-2.5 py-1 text-neutral-100"><span class="size-2 rounded-[2px] bg-current"></span>Стоп</span>
  </button>
</div>
