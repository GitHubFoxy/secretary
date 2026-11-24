<script>
  import Icon from './Icon.svelte';
  let { harness = $bindable(), model = $bindable(), reasoning = $bindable(), connected = false, onsave, onstop, oncreate, onrename } = $props();

  const harnesses = [
    { value: 'claude', label: 'Claude', title: 'Claude Code', icon: 'claude', accent: 'text-[#D97757]' },
    { value: 'codex', label: 'Codex', title: 'Codex', icon: 'codex', accent: 'text-neutral-100' },
  ];
  let models = $state([]);
  let details = $state([]);
  let levels = $derived(details.find(m => m.id === model)?.supportedReasoningEfforts.map(e => e.reasoningEffort) ?? []);
  let loading = $state(false);
  let loadError = $state('');
  let requestId = 0;

  // Номер запроса отсекает ответы, которые пришли после смены агента.
  async function loadModels(name) {
    const id = ++requestId;
    loading = true;
    details = [];
    models = [];
    loadError = '';
    try {
      const response = await fetch(`/model-details?harness=${name}`);
      if (!response.ok) throw new Error(await response.text());
      const list = await response.json();
      if (id !== requestId) return;
      details = list;
      models = list.map(m => m.id);
      if (models.length && !models.includes(model)) model = models[0];
    } catch (error) {
      if (id === requestId) loadError = `Не удалось загрузить модели: ${error.message || error}`;
    } finally {
      if (id === requestId) loading = false;
    }
  }

  $effect(() => { loadModels(harness); });
  $effect(() => { if (details.length && reasoning && !levels.includes(reasoning)) reasoning = null; });
</script>

<div class="flex flex-col gap-4">
  <div class="divide-y divide-neutral-800/80 rounded-xl border border-neutral-800 bg-neutral-900/50">
    <div class="flex items-center justify-between gap-3 px-3.5 py-2.5">
      <span class="text-xs text-neutral-400">Агент</span>
      <div role="group" aria-label="Агент" class="flex gap-1 rounded-lg bg-neutral-950 p-0.5">
        {#each harnesses as item (item.value)}
          <button
            type="button"
            title={item.title}
            aria-label={item.title}
            aria-pressed={harness === item.value}
            class="flex cursor-pointer items-center gap-1.5 rounded-md px-2.5 py-1 text-xs font-medium transition-colors {harness === item.value ? 'bg-neutral-800 text-neutral-100' : 'text-neutral-500 hover:text-neutral-300'}"
            onclick={() => (harness = item.value)}
          >
            <Icon name={item.icon} class="size-3.5 {harness === item.value ? item.accent : ''}" />
            {item.label}
          </button>
        {/each}
      </div>
    </div>

    <div class="flex items-center justify-between gap-2 px-3.5 py-2">
      <label for="session-model" class="shrink-0 text-xs text-neutral-400">Модель</label>
      <div class="flex min-w-0 flex-1 items-center justify-end gap-1">
        <select
          id="session-model"
          aria-label="Модель"
          class="min-w-0 max-w-full cursor-pointer truncate bg-transparent py-1 text-right font-mono text-xs text-neutral-100 focus:outline-none disabled:opacity-40"
          bind:value={model}
          disabled={!models.length}
        >
          {#each details as m (m.id)}<option class="bg-neutral-900 text-neutral-100" value={m.id}>{m.id}</option>{/each}
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
      <label for="session-reasoning" class="shrink-0 text-xs text-neutral-400">Reasoning</label>
      <select
        id="session-reasoning"
        aria-label="Уровень reasoning"
        class="min-w-0 cursor-pointer bg-transparent py-1 text-right text-xs text-neutral-100 focus:outline-none disabled:opacity-40"
        bind:value={reasoning}
        disabled={loading || !!loadError}
      >
        <option class="bg-neutral-900 text-neutral-100" value={null}>По умолчанию</option>
        {#each levels as level (level)}<option class="bg-neutral-900 text-neutral-100" value={level}>{level}</option>{/each}
      </select>
    </div>
  </div>

  {#if loadError}<p role="alert" class="text-xs text-red-400">{loadError}</p>{/if}

  <button type="button" class="btn w-full rounded-xl" onclick={onsave}>Сохранить</button>

  <div class="divide-y divide-neutral-800/80 overflow-hidden rounded-xl border border-neutral-800 bg-neutral-900/50 text-xs">
    <button
      type="button"
      class="flex w-full cursor-pointer items-center justify-between px-3.5 py-2.5 text-left text-neutral-200 transition-colors hover:bg-neutral-800/60 disabled:cursor-not-allowed disabled:opacity-40"
      disabled={!connected}
      onclick={onrename}
    >
      <span>Переименовать чат</span>
      <Icon name="chevron" class="size-3.5 -rotate-90 text-neutral-600" />
    </button>
    <button
      type="button"
      class="flex w-full cursor-pointer items-center justify-between px-3.5 py-2.5 text-left text-neutral-200 transition-colors hover:bg-neutral-800/60"
      onclick={oncreate}
    >
      <span>Новый тред в этой папке</span>
      <Icon name="chevron" class="size-3.5 -rotate-90 text-neutral-600" />
    </button>
    <button
      type="button"
      class="flex w-full cursor-pointer items-center justify-between px-3.5 py-2.5 text-left text-red-400 transition-colors hover:bg-red-950/30"
      onclick={onstop}
    >
      <span>Остановить сессию</span>
    </button>
  </div>
</div>
