<script>
  let { harness = $bindable(), model = $bindable(), folder = $bindable(), models = [], onsave, onstop, oncreate } = $props();
  const options = $derived(harness === 'codex' ? models : ['opus', 'sonnet', 'haiku']);
</script>

<div class="flex flex-col gap-6">
  <section class="flex flex-col gap-4">
    <div class="grid gap-3 sm:grid-cols-2">
      <label class="flex flex-col gap-1.5 text-xs text-neutral-500">
        Агент
        <select class="field" bind:value={harness}>
          <option value="claude">Claude</option>
          <option value="codex">Codex</option>
        </select>
      </label>
      <label class="flex flex-col gap-1.5 text-xs text-neutral-500">
        Модель
        <input class="field" bind:value={model} list="models" />
        <datalist id="models">
          {#each options as m}<option value={m}></option>{/each}
        </datalist>
      </label>
    </div>
    <div class="flex justify-end gap-2">
      <button class="btn-ghost" onclick={onstop}>Остановить</button>
      <button class="btn" onclick={onsave}>Сохранить</button>
    </div>
  </section>

  <section class="flex flex-col gap-4 border-t border-neutral-900 pt-6">
    <div>
      <h3 class="text-sm font-medium text-neutral-100">Новый тред</h3>
      <p class="mt-0.5 text-xs text-neutral-500">Создаётся с агентом и моделью из полей выше.</p>
    </div>
    <label class="flex flex-col gap-1.5 text-xs text-neutral-500">
      Папка
      <input class="field" bind:value={folder} placeholder="~/projects/..." />
    </label>
    <div class="flex justify-end">
      <button class="btn" onclick={oncreate} disabled={!folder.trim()}>Создать тред</button>
    </div>
  </section>
</div>
