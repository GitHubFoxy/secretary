<script>
  import Icon from './Icon.svelte';
  import { fileSize } from '../file-size.js';

  let { images = [], onremove } = $props();
</script>

<div class="mx-auto flex max-w-3xl flex-wrap gap-2">
  {#each images as image}
    <div class="relative">
      {#if image.file}
        <div class="flex h-20 max-w-60 items-center gap-2 rounded bg-neutral-800 px-3 pr-6">
          <Icon name="file" class="size-6 shrink-0" />
          <span class="min-w-0">
            <span class="block truncate">{image.name}</span>
            <span class="text-xs text-neutral-400">{fileSize(image.size)}</span>
          </span>
        </div>
      {:else if image.video}
        <video class="h-20 rounded" src={image.url} muted playsinline preload="metadata"></video>
        <span class="absolute left-1 top-1 rounded bg-black/60 p-0.5 text-white"><Icon name="video" class="size-3.5" /></span>
      {:else}
        <img class="h-20 rounded" src={image.url} alt="Изображение для отправки" />
      {/if}
      {#if image.processing}<span role="status" class="absolute bottom-0 left-0 bg-neutral-900/80 px-1 text-xs">Обработка…</span>{/if}
      {#if image.error}<span role="alert" class="absolute bottom-0 left-0 bg-red-900 px-1 text-xs">{image.error}</span>{/if}
      <button type="button" class="absolute right-0 top-0 bg-neutral-900 px-1" aria-label="Удалить вложение" onclick={() => onremove(image)}>×</button>
    </div>
  {/each}
</div>
