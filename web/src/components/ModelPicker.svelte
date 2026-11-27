<script>
  import Icon from './Icon.svelte';
  import { harnesses, modelLabel } from '../agents.js';
  let { harness = $bindable(), model = $bindable(), reasoning = $bindable(), current = null } = $props();

  let details = $state([]);
  // Сессия может хранить псевдоним Claude Code («opus»): он остаётся значением своей модели в списке.
  const matches = m => m.id === model || m.aliases?.includes(model);
  let levels = $derived(details.find(matches)?.supportedReasoningEfforts.map(e => e.reasoningEffort) ?? []);
  let loading = $state(false);
  let loadError = $state('');
  let requestId = 0;

  // Номер запроса отсекает ответы, которые пришли после смены агента.
  async function loadModels(name) {
    const id = ++requestId;
    loading = true;
    details = [];
    loadError = '';
    try {
      const response = await fetch(`/model-details?harness=${name}`);
      if (!response.ok) throw new Error(await response.text());
      const list = await response.json();
      if (id !== requestId) return;
      details = list;
      if (list.length && !list.some(matches)) model = list[0].id;
    } catch (error) {
      if (id === requestId) loadError = `Не удалось загрузить модели: ${error.message || error}`;
    } finally {
      if (id === requestId) loading = false;
    }
  }

  $effect(() => { loadModels(harness); });
  $effect(() => { if (details.length && reasoning && !levels.includes(reasoning)) reasoning = null; });
</script>

<div class="flex flex-col gap-3">
  <div role="group" aria-label="Агент" class="grid grid-cols-2 gap-2">
    {#each harnesses as item (item.value)}
      {@const on = harness === item.value}
      <button
        type="button"
        aria-pressed={on}
        class="cursor-pointer rounded-2xl p-3 text-left transition-colors {on ? 'border-2 bg-neutral-800/50 ' + (item.value === 'claude' ? 'border-[#D97757]' : 'border-neutral-200') : 'border border-neutral-800 hover:bg-neutral-900'}"
        onclick={() => (harness = item.value)}
      >
        <Icon name={item.value} class="mb-2 size-6 {item.accent}" />
        <span class="block text-sm font-medium text-neutral-100">{item.title}</span>
        <span class="block h-4 text-[11px] text-neutral-500">{current === item.value ? 'сейчас' : ''}</span>
      </button>
    {/each}
  </div>

  <div class="divide-y divide-neutral-800/80 rounded-xl border border-neutral-800 bg-neutral-900/50">
    <div class="flex items-center justify-between gap-2 px-3.5 py-2">
      <label for="picker-model" class="shrink-0 text-xs text-neutral-400">Модель</label>
      <div class="flex min-w-0 flex-1 items-center justify-end gap-1">
        <select
          id="picker-model"
          class="min-w-0 max-w-full cursor-pointer truncate bg-transparent py-1 text-right text-sm text-neutral-100 focus:outline-none disabled:opacity-40"
          bind:value={model}
          disabled={!details.length}
        >
          {#each details as m (m.id)}<option class="bg-neutral-900 text-neutral-100" value={matches(m) ? model : m.id}>{modelLabel(m.id)}</option>{/each}
        </select>
        <button
          type="button"
          class="grid size-7 shrink-0 cursor-pointer place-items-center rounded-md text-neutral-500 transition-colors hover:bg-neutral-800 hover:text-neutral-200 disabled:opacity-40"
          title="Обновить список моделей"
          aria-label="Обновить список моделей"
          disabled={loading}
          onclick={() => loadModels(harness)}
        >
          <Icon name="refresh" class="size-3.5 {loading ? 'animate-spin' : ''}" />
        </button>
      </div>
    </div>
    <div class="flex items-center justify-between gap-2 px-3.5 py-2">
      <label for="picker-reasoning" class="shrink-0 text-xs text-neutral-400">Reasoning</label>
      <select
        id="picker-reasoning"
        class="min-w-0 cursor-pointer bg-transparent py-1 text-right text-sm text-neutral-100 focus:outline-none disabled:opacity-40"
        bind:value={reasoning}
        disabled={loading || !!loadError}
      >
        <option class="bg-neutral-900 text-neutral-100" value={null}>По умолчанию</option>
        {#each levels as level (level)}<option class="bg-neutral-900 text-neutral-100" value={level}>{level}</option>{/each}
      </select>
    </div>
  </div>

  {#if loadError}<p role="alert" class="text-xs text-red-400">{loadError}</p>{/if}
</div>
