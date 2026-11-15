<script>
  import { fade, fly } from 'svelte/transition';
  import { tick } from 'svelte';
  import Icon from './Icon.svelte';
  let { open = false, title, onclose, children } = $props();
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
      class="relative flex max-h-[85dvh] w-full max-w-md flex-col overflow-hidden rounded-2xl border border-neutral-800 bg-neutral-950 outline-none"
      transition:fly={{ y: 12, duration: 180 }}
    >
      <header class="flex items-center justify-between border-b border-neutral-900 px-5 py-3">
        <h2 class="text-sm font-semibold text-neutral-100">{title}</h2>
        <button class="icon-btn" aria-label="Закрыть" onclick={onclose}>
          <Icon name="close" />
        </button>
      </header>
      <div class="overflow-y-auto p-5">
        {@render children?.()}
      </div>
    </div>
  </div>
{/if}
