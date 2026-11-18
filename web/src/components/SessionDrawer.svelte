<script>
  import { fade, fly } from 'svelte/transition';
  import Icon from './Icon.svelte';
  import { showInSidebar } from '../sessions.js';
  let { open = false, sessions = [], selected, connected = false, onhide, onrestore, onselect, onclose, onprofile } = $props();

  const lastEvent = s => s.last_event?.type === 'status' ? s.last_event.status : s.last_event?.method || s.last_event?.type || '';
</script>

<svelte:window onkeydown={e => { if (open && e.key === 'Escape') onclose(); }} />

{#snippet sessionRow(s)}
  <div class="flex items-center rounded-lg {s.parent_id ? 'ml-5' : ''} {selected === s.id ? 'bg-neutral-900' : ''}">
    <button class="min-w-0 flex-1 cursor-pointer rounded-lg px-3 py-2.5 text-left transition-colors hover:bg-neutral-900" onclick={() => onselect(s.id)}>
      <span class="block truncate text-sm {selected === s.id ? 'text-neutral-100' : 'text-neutral-300'}">{s.name || `Сессия #${s.id}`}</span>
      <span class="mt-0.5 block truncate text-xs text-neutral-500">{s.status} · {s.model} · {s.reasoning ?? 'по умолчанию'}</span>
      <span class="block truncate text-xs text-neutral-600">{s.folder}</span>
      {#if lastEvent(s)}<span class="block truncate text-xs text-neutral-600">{lastEvent(s)}</span>{/if}
    </button>
    {#if s.hidden}
      <button class="mr-2 rounded px-2 py-2 text-xs text-neutral-300 hover:bg-neutral-800 focus-visible:outline focus-visible:outline-2 focus-visible:outline-neutral-300" aria-label={`Вернуть сессию ${s.name || `#${s.id}`} в список`} disabled={!connected} onclick={() => onrestore(s)}>Вернуть</button>
    {:else}
      <button class="mr-2 rounded px-2 py-2 text-xs text-neutral-400 hover:bg-neutral-800 hover:text-neutral-100 focus-visible:outline focus-visible:outline-2 focus-visible:outline-neutral-300" aria-label={`Скрыть сессию ${s.name || `#${s.id}`} из списка`} disabled={!connected} onclick={() => onhide(s)}>Скрыть</button>
    {/if}
  </div>
{/snippet}

{#if open}
  <button class="fixed inset-0 z-10 cursor-default bg-black/60" aria-label="Закрыть список" onclick={onclose} transition:fade={{ duration: 200 }}></button>
  <aside class="fixed inset-y-0 left-0 z-20 flex w-80 max-w-[85vw] flex-col border-r border-neutral-900 bg-neutral-950" transition:fly={{ x: -320, duration: 220 }}>
    <div class="flex items-center justify-between border-b border-neutral-900 px-4 py-3">
      <button class="min-w-0 truncate cursor-pointer text-sm font-semibold text-neutral-100" aria-label="Открыть основной чат" onclick={() => onselect(1)}>Secretary</button>
      <button class="icon-btn" aria-label="Закрыть" onclick={onclose}>
        <Icon name="close" />
      </button>
    </div>
    <nav class="flex min-h-0 flex-1 flex-col gap-1 overflow-y-auto p-2">
      {#each sessions.filter(s => showInSidebar(s) && !s.hidden) as s (s.id)}
        {@render sessionRow(s)}
      {/each}
      <details class="mt-2 border-t border-neutral-900 pt-2">
        <summary class="cursor-pointer rounded-lg px-3 py-2 text-sm text-neutral-300 hover:bg-neutral-900 focus-visible:outline focus-visible:outline-2 focus-visible:outline-neutral-300">Скрытые сессии</summary>
        <p class="px-3 py-1 text-xs text-neutral-400">Скрытые агенты могут продолжать работать.</p>
        {#each sessions.filter(s => showInSidebar(s) && s.hidden) as s (s.id)}
          {@render sessionRow(s)}
        {:else}
          <p class="px-3 py-2 text-xs text-neutral-500">Нет скрытых сессий</p>
        {/each}
      </details>
    </nav>
    <div class="border-t border-neutral-900 p-2">
      <button class="flex w-full cursor-pointer items-center gap-3 rounded-lg px-3 py-2 text-left transition-colors hover:bg-neutral-900" title="Настройки" onclick={onprofile}>
        <span class="grid size-8 shrink-0 place-items-center rounded-full bg-neutral-800 text-sm font-medium text-neutral-100" aria-hidden="true">М</span>
        <span class="truncate text-sm text-neutral-200">Mikhail</span>
      </button>
    </div>
  </aside>
{/if}
