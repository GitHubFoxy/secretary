<script>
  import Icon from './Icon.svelte';
  import ModelPicker from './ModelPicker.svelte';
  // value — сохранённая модель новых Specialists или null: тогда они берут модель Secretary Agent.
  let { value, secretary, onsave, onback } = $props();

  let own = $state(!!value);
  let choice = $state({ ...(value ?? { harness: secretary.harness, model: secretary.model, reasoning: secretary.reasoning ?? null }) });
  let status = $state('');

  async function save() {
    status = 'Сохранение…';
    try { await onsave(own ? choice : null); onback(); }
    catch (err) { status = `Не удалось сохранить: ${err.message || err}`; }
  }
</script>

<div class="flex h-dvh flex-col bg-neutral-950 text-neutral-200">
  <header class="flex items-center gap-2 border-b border-neutral-900 px-2 py-2">
    <button class="icon-btn" aria-label="Назад" onclick={onback}><Icon name="back" /></button>
    <h1 class="min-w-0 flex-1 truncate px-1 text-sm font-medium text-neutral-100">Модель новых Specialists</h1>
  </header>
  <div class="flex flex-col gap-4 p-4">
    <p class="text-xs text-neutral-500">С ней Secretary Agent создаёт Specialists, если ты не назвал модель сам. Уже созданные Specialists не меняются.</p>
    <div role="group" aria-label="Источник модели" class="grid grid-cols-2 gap-1 rounded-xl bg-neutral-900 p-1 text-sm">
      <button type="button" aria-pressed={!own} class="cursor-pointer rounded-lg py-2 {own ? 'text-neutral-400' : 'bg-neutral-800 text-neutral-100'}" onclick={() => (own = false)}>Как у Secretary Agent</button>
      <button type="button" aria-pressed={own} class="cursor-pointer rounded-lg py-2 {own ? 'bg-neutral-800 text-neutral-100' : 'text-neutral-400'}" onclick={() => (own = true)}>Своя</button>
    </div>
    {#if own}<ModelPicker bind:harness={choice.harness} bind:model={choice.model} bind:reasoning={choice.reasoning} />{/if}
    <button class="btn w-full rounded-xl" onclick={save}>Сохранить</button>
    {#if status}<p role="status" class="text-xs text-neutral-500">{status}</p>{/if}
  </div>
</div>
