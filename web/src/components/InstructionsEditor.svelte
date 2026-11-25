<script>
  import Icon from './Icon.svelte';
  // dirty — есть несохранённая правка: пока она есть, приложение не перезагружается ради обновления.
  let { role, title, hint, onback, dirty = $bindable(false) } = $props();

  let text = $state(''), saved = $state(''), system = $state('');
  let loaded = $state(false), saving = $state(false), status = $state('Загрузка…');
  $effect(() => { dirty = loaded && text !== saved; });

  async function load() {
    try {
      const response = await fetch(`/settings/agents/${role}`);
      if (!response.ok) throw new Error(await response.text());
      const data = await response.json();
      text = saved = data.text; system = data.system; loaded = true; status = '';
    } catch (err) { status = `Не удалось загрузить: ${err.message || err}`; }
  }
  async function save() {
    saving = true; status = 'Сохранение…';
    try {
      const value = text;
      const response = await fetch(`/settings/agents/${role}`, {method:'PUT', body:value});
      if (!response.ok) throw new Error(await response.text());
      saved = value; status = 'Сохранено';
    } catch (err) { status = `Не удалось сохранить: ${err.message || err}`; }
    finally { saving = false; }
  }
  function back() { if (!dirty || confirm('Выйти без сохранения?')) onback(); }
  load();
</script>

<div class="flex h-dvh flex-col bg-neutral-950 text-neutral-200">
  <header class="flex items-center gap-2 border-b border-neutral-900 px-2 py-2">
    <button class="icon-btn" aria-label="Назад" onclick={back}><Icon name="back" /></button>
    <h1 class="min-w-0 flex-1 truncate px-1 text-sm font-medium text-neutral-100">{title}</h1>
    <button class="btn px-3 py-1.5 text-xs" disabled={!dirty || saving} onclick={save}>Сохранить</button>
  </header>
  <div class="flex min-h-0 flex-1 flex-col gap-3 p-4">
    <p class="text-xs text-neutral-500">{hint} Применяется со следующего сообщения.</p>
    <textarea class="field min-h-0 flex-1 resize-none font-mono text-xs leading-relaxed" aria-label={`Инструкции: ${title}`} spellcheck="false" disabled={!loaded} placeholder="Пока пусто — работают только встроенные инструкции" bind:value={text}></textarea>
    {#if status}<p role="status" class="text-xs text-neutral-500">{status}</p>{/if}
    {#if system}
      <details class="rounded-xl border border-neutral-800 text-xs">
        <summary class="cursor-pointer px-3 py-2 text-neutral-400"><Icon name="lock" class="mr-1 inline size-3.5 align-[-2px]" />Встроенная часть · только чтение</summary>
        <pre class="max-h-60 overflow-y-auto whitespace-pre-wrap px-3 pb-3 font-mono text-[11px] leading-relaxed text-neutral-500">{system}</pre>
      </details>
    {/if}
  </div>
</div>
