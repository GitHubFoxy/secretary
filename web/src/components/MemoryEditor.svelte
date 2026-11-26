<script>
  import Icon from './Icon.svelte';
  let { onback, dirty = $bindable(false) } = $props();
  let files = $state([]), name = $state(''), text = $state(''), saved = $state(''), version = $state('');
  let status = $state(''), busy = $state(false);
  $effect(() => { dirty = !!name && text !== saved; });
  const url = (file) => `/settings/memory?name=${encodeURIComponent(file)}`;
  async function list() {
    try {
      const response = await fetch('/settings/memory');
      if (!response.ok) throw new Error(await response.text());
      files = await response.json(); status = '';
    } catch (err) { status = `Не удалось загрузить: ${err.message || err}`; }
  }
  async function open(file) {
    try {
      const response = await fetch(url(file));
      if (!response.ok) throw new Error(await response.text());
      const data = await response.json();
      name = file; text = saved = data.text; version = data.version; status = '';
    } catch (err) { status = `Не удалось открыть: ${err.message || err}`; }
  }
  async function save() {
    busy = true;
    try {
      const value = text;
      const response = await fetch(url(name), { method: 'PUT', body: JSON.stringify({ text: value, version }) });
      if (!response.ok) throw new Error(await response.text());
      const data = await response.json(); saved = value; version = data.version; status = 'Сохранено';
    } catch (err) { status = `Не удалось сохранить: ${err.message || err}`; }
    finally { busy = false; }
  }
  async function remove() {
    if (!confirm(`Удалить ${name}?`)) return;
    busy = true;
    try {
      const response = await fetch(url(name), { method: 'DELETE', body: JSON.stringify({ version }) });
      if (!response.ok) throw new Error(await response.text());
      name = ''; dirty = false; await list();
    } catch (err) { status = `Не удалось удалить: ${err.message || err}`; }
    finally { busy = false; }
  }
  function back() {
    if (dirty && !confirm('Выйти без сохранения?')) return;
    if (name) { name = ''; dirty = false; list(); } else onback();
  }
  list();
</script>

<div class="flex h-dvh flex-col bg-neutral-950 text-neutral-200">
  <header class="flex items-center gap-2 border-b border-neutral-900 px-2 py-2">
    <button class="icon-btn" aria-label="Назад" onclick={back}><Icon name="back" /></button>
    <h1 class="min-w-0 flex-1 truncate px-1 text-sm font-medium text-neutral-100">{name || 'Память'}</h1>
    {#if name}<button class="btn px-3 py-1.5 text-xs" disabled={!dirty || busy} onclick={save}>Сохранить</button>{/if}
  </header>
  <div class="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto p-4">
    {#if name}
      <textarea class="field min-h-0 flex-1 resize-none font-mono text-xs leading-relaxed" aria-label={`Память: ${name}`} spellcheck="false" bind:value={text}></textarea>
      <button class="self-start text-sm text-red-400" disabled={busy} onclick={remove}>Удалить файл</button>
    {:else}
      <p class="text-xs text-neutral-500">Файлы памяти агентов на Host.</p>
      {#each files as file}
        <button class="rounded-xl border border-neutral-800 px-3 py-3 text-left text-sm hover:bg-neutral-800/60" onclick={() => open(file)}>{file}</button>
      {:else}
        <p class="text-sm text-neutral-500">Пока нет файлов памяти.</p>
      {/each}
    {/if}
    {#if status}<p role="status" class="text-xs text-neutral-500">{status}</p>{/if}
  </div>
</div>
