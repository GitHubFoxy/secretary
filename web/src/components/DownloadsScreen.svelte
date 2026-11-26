<script>
  import { onMount } from 'svelte';
  import { registerPlugin } from '@capacitor/core';
  import Icon from './Icon.svelte';
  let { onback } = $props();
  const downloads = registerPlugin('Downloads');
  let items = $state([]), error = $state(''), loading = $state(true);
  const labels = { pending: 'Ожидание', running: 'Скачивание', complete: 'Готово', failed: 'Ошибка загрузки', unavailable: 'Файл недоступен' };
  onMount(() => {
    let stopped = false, timer;
    async function refresh() {
      try {
        const result = await downloads.list();
        if (stopped) return;
        items = result.items; error = '';
        if (items.some(item => item.status === 'pending' || item.status === 'running')) timer = setTimeout(refresh, 1000);
      } catch (err) { if (!stopped) error = `Не удалось получить загрузки: ${err.message}`; }
      finally { if (!stopped) loading = false; }
    }
    refresh();
    return () => { stopped = true; clearTimeout(timer); };
  });
  async function act(action, id) {
    try { await downloads[action]({id}); error = ''; }
    catch (err) { error = `Не удалось выполнить действие: ${err.message}`; }
  }
</script>

<div class="flex h-dvh flex-col bg-neutral-950 text-neutral-200">
  <header class="flex items-center gap-2 border-b border-neutral-900 px-2 py-2">
    <button class="icon-btn" aria-label="Назад" onclick={onback}><Icon name="back" /></button>
    <h1 class="px-1 text-sm font-medium text-neutral-100">Загрузки</h1>
  </header>
  <div class="min-h-0 flex-1 overflow-y-auto p-4">
    {#if error}<p role="alert" class="mb-4 text-sm text-red-400">{error}</p>{/if}
    {#if loading}<p class="text-sm text-neutral-400">Загрузка…</p>
    {:else if !items.length}<p class="text-sm text-neutral-400">Загрузок пока нет</p>
    {:else}
      <ul class="divide-y divide-neutral-800 rounded-xl border border-neutral-800 bg-neutral-900/50">
        {#each items as item (item.id)}
          <li class="flex flex-col gap-2 p-4">
            <span class="break-all text-sm text-neutral-100">{item.name}</span>
            <span class="text-xs text-neutral-400">{labels[item.status] ?? item.status}{#if item.size > 0 && item.status === 'running'} · {Math.min(100, Math.floor(item.progress * 100 / item.size))}%{/if}</span>
            {#if item.status === 'running' || item.status === 'pending'}
              {#if item.size > 0}<progress class="w-full" value={item.progress} max={item.size} aria-label={`Загрузка ${item.name}`}></progress>
              {:else}<progress class="w-full" aria-label={`Загрузка ${item.name}`}></progress>{/if}
            {/if}
            {#if item.status === 'complete'}
              <div class="flex gap-2"><button class="btn" onclick={() => act('open', item.id)}>Открыть</button><button class="btn-ghost" onclick={() => act('share', item.id)}>Поделиться</button></div>
            {/if}
          </li>
        {/each}
      </ul>
    {/if}
  </div>
</div>
