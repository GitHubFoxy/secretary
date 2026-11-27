<script>
  import { fade, fly } from 'svelte/transition';
  import { tick } from 'svelte';
  import Icon from './Icon.svelte';
  let { open = false, title, onclose, children, wide = false, onrefresh = null } = $props();
  let dialog = $state();

  // При открытии переводим фокус внутрь диалога, чтобы Escape и Tab работали с ним.
  $effect(() => {
    if (open) tick().then(() => dialog?.focus());
  });
</script>

<svelte:window onkeydown={e => { if (open && e.key === 'Escape') onclose(); }} />

{#if open}
  <div class="fixed inset-0 z-30 flex items-center justify-center p-4">
    <button class="absolute inset-0 cursor-default bg-black/60" aria-label="Закрыть" tabindex="-1" onclick={onclose} transition:fade={{ duration: 150 }}></button>
    <div
      bind:this={dialog}
      role="dialog"
      aria-modal="true"
      aria-label={title}
      tabindex="-1"
      class="relative flex max-h-[92dvh] w-full {wide ? 'max-w-2xl' : 'max-w-md'} flex-col overflow-hidden rounded-2xl border border-neutral-800 bg-neutral-950 outline-none sm:max-h-[85dvh]"
      transition:fly={{ y: 12, duration: 180 }}
    >
      <header class="flex items-center justify-between gap-2 border-b border-neutral-900 px-5 py-3">
        <h2 class="min-w-0 flex-1 truncate text-sm font-semibold text-neutral-100">{title}</h2>
        {#if onrefresh}
          <button class="icon-btn shrink-0" aria-label="Обновить" title="Обновить" onclick={onrefresh}>
            <Icon name="refresh" />
          </button>
        {/if}
        <button class="icon-btn shrink-0" aria-label="Закрыть" onclick={onclose}>
          <Icon name="close" />
        </button>
      </header>
      <div class="overflow-y-auto p-4 sm:p-5">
        {@render children?.()}
      </div>
    </div>
  </div>
{/if}
