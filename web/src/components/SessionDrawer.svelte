<script>
  import { fade, fly } from 'svelte/transition';
  import Icon from './Icon.svelte';
  let { open = false, sessions = [], selected, onselect, onclose, onprofile } = $props();

  const lastEvent = s => s.last_event?.type === 'status' ? s.last_event.status : s.last_event?.method || s.last_event?.type || '';
</script>

<svelte:window onkeydown={e => { if (open && e.key === 'Escape') onclose(); }} />

{#if open}
  <button class="fixed inset-0 z-10 cursor-default bg-black/60" aria-label="Закрыть список" onclick={onclose} transition:fade={{ duration: 200 }}></button>
  <aside class="fixed inset-y-0 left-0 z-20 flex w-80 max-w-[85vw] flex-col border-r border-neutral-900 bg-neutral-950" transition:fly={{ x: -320, duration: 220 }}>
    <div class="flex items-center justify-between border-b border-neutral-900 px-4 py-3">
      <h2 class="text-sm font-semibold text-neutral-100">Secretary</h2>
      <button class="icon-btn" aria-label="Закрыть" onclick={onclose}>
        <Icon name="close" />
      </button>
    </div>
    <nav class="flex min-h-0 flex-1 flex-col gap-1 overflow-y-auto p-2">
      {#each sessions as s (s.id)}
        <button
          class="cursor-pointer rounded-lg px-3 py-2.5 text-left transition-colors hover:bg-neutral-900 {s.parent_id ? 'ml-5' : ''} {selected === s.id ? 'bg-neutral-900' : ''}"
          onclick={() => onselect(s.id)}
        >
          <span class="block truncate text-sm {selected === s.id ? 'text-neutral-100' : 'text-neutral-300'}">#{s.id} {s.id === 1 ? 'Secretary Agent' : s.harness}</span>
          <span class="mt-0.5 block truncate text-xs text-neutral-500">{s.status} · {s.model}</span>
          <span class="block truncate text-xs text-neutral-600">{s.folder}</span>
          {#if lastEvent(s)}<span class="block truncate text-xs text-neutral-600">{lastEvent(s)}</span>{/if}
        </button>
      {/each}
    </nav>
    <div class="border-t border-neutral-900 p-2">
      <button class="flex w-full cursor-pointer items-center gap-3 rounded-lg px-3 py-2 text-left transition-colors hover:bg-neutral-900" title="Настройки" onclick={onprofile}>
        <span class="grid size-8 shrink-0 place-items-center rounded-full bg-neutral-800 text-sm font-medium text-neutral-100" aria-hidden="true">М</span>
        <span class="truncate text-sm text-neutral-200">Mikhail</span>
      </button>
    </div>
  </aside>
{/if}
