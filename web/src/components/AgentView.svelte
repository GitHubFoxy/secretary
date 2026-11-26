<script>
  import Icon from './Icon.svelte';
  let { onback } = $props();
  let data = $state(null), error = $state(''), selected = $state('');
  async function load() {
    try {
      const response = await fetch(`/settings/agent-view${selected ? `?session=${encodeURIComponent(selected)}` : ''}`);
      if (!response.ok) throw new Error(await response.text());
      data = await response.json(); error = '';
    } catch (e) { error = String(e); }
  }
  load();
</script>
<div class="flex h-dvh flex-col bg-neutral-950 text-neutral-200">
  <header class="flex items-center gap-2 border-b border-neutral-900 px-2 py-2">
    <button class="icon-btn" aria-label="Назад" onclick={onback}><Icon name="back" /></button>
    <h1 class="px-1 text-sm font-medium">Что видят агенты</h1>
  </header>
  <div class="flex-1 overflow-y-auto p-4">
    {#if error}<p role="alert" class="text-red-400">{error}</p>{/if}
    {#if data}
      <label class="block mb-4 text-sm">Session
        <select class="ml-2 bg-neutral-800 p-2" bind:value={selected} onchange={load}>
          <option value="">Не выбрана</option>
          {#each data.sessions as session}<option value={String(session.id)}>{session.name || `#${session.id}`} · {session.harness}</option>{/each}
        </select>
      </label>
      {#each Object.entries({ 'Secretary Agent': data.roles.secretary, 'Specialist': data.roles.specialist, 'Worker': data.roles.worker, 'Память': data.memory, 'MCP-серверы': data.servers, 'Skills': data.skills, ...(data.session ? { 'Выбранная Session': data.session } : {}) }) as [title, value]}
        <details class="border-b border-neutral-800 py-3"><summary class="cursor-pointer">{title}</summary><pre class="mt-3 overflow-x-auto whitespace-pre-wrap break-words font-mono text-xs">{JSON.stringify(value, null, 2)}</pre></details>
      {/each}
    {/if}
  </div>
</div>
