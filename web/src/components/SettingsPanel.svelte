<script>
  import Icon from './Icon.svelte';
  let { harness = $bindable(), model = $bindable(), onsave, onstop, oncreate } = $props();

  const harnesses = [
    { value: 'claude', label: 'Claude Code', icon: 'claude' },
    { value: 'codex', label: 'Codex', icon: 'codex' },
  ];
  let models = $state([]);
  let loading = $state(false);
  let loadError = $state('');
  let requestId = 0;

  // Номер запроса отсекает ответы, которые пришли после смены агента.
  async function loadModels(name) {
    const id = ++requestId;
    loading = true;
    loadError = '';
    try {
      const response = await fetch(`/models?harness=${name}`);
      if (!response.ok) throw new Error(await response.text());
      const list = await response.json();
      if (id !== requestId) return;
      models = list;
      if (list.length && !list.includes(model)) model = list[0];
    } catch (error) {
      if (id === requestId) loadError = `Не удалось загрузить модели: ${error.message || error}`;
    } finally {
      if (id === requestId) loading = false;
    }
  }

  $effect(() => { loadModels(harness); });
</script>

<div class="flex flex-col gap-6">
  <section class="flex flex-col gap-4">
    <div class="flex items-center gap-2">
      <div role="group" aria-label="Агент" class="flex shrink-0 rounded-lg border border-neutral-800 bg-neutral-900 p-0.5">
        {#each harnesses as item (item.value)}
          <button
            type="button"
            title={item.label}
            aria-label={item.label}
            aria-pressed={harness === item.value}
            class="grid size-8 cursor-pointer place-items-center rounded-md transition-colors {harness === item.value ? 'bg-neutral-700 text-neutral-100' : 'text-neutral-500 hover:text-neutral-300'}"
            onclick={() => (harness = item.value)}
          >
            <Icon name={item.icon} class="size-4" />
          </button>
        {/each}
      </div>
      <select class="field min-w-0 flex-1" aria-label="Модель" bind:value={model} disabled={!models.length}>
        {#each models as m (m)}<option value={m}>{m}</option>{/each}
      </select>
      <button
        type="button"
        class="icon-btn shrink-0"
        title="Обновить список моделей"
        aria-label="Обновить список моделей"
        disabled={loading}
        onclick={() => loadModels(harness)}
      >
        <Icon name="refresh" class="size-4 {loading ? 'animate-spin' : ''}" />
      </button>
    </div>
    {#if loadError}<p role="alert" class="text-xs text-red-400">{loadError}</p>{/if}
    <div class="flex justify-end gap-2">
      <button class="btn-ghost" onclick={onstop}>Остановить</button>
      <button class="btn" onclick={onsave}>Сохранить</button>
    </div>
  </section>

  <section class="flex justify-end border-t border-neutral-900 pt-6">
    <button class="btn" onclick={oncreate}>Новый тред</button>
  </section>
</div>
