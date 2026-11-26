<script>
  import Icon from './Icon.svelte';
  let { onback } = $props();
  let skills = $state([]), selected = $state(null), error = $state('');
  async function load() {
    try {
      const response = await fetch('/settings/skills');
      if (!response.ok) throw new Error(await response.text());
      skills = await response.json();
      if (selected) selected = skills.find(s => s.name === selected.name) || null;
      error = '';
    } catch (e) { error = String(e); }
  }
  function description(content) {
    const frontmatter = content.match(/^---\s*\n([\s\S]*?)\n---/);
    return frontmatter?.[1].match(/^description:\s*["']?(.+?)["']?\s*$/m)?.[1] || '';
  }
  async function action(method, version) {
    if (!confirm(method === 'DELETE' ? `Удалить ${selected.name}?` : 'Откатить skill к этой версии?')) return;
    try {
      const response = await fetch(`/settings/skills?name=${encodeURIComponent(selected.name)}`, { method, ...(version ? {body: JSON.stringify({version})} : {}) });
      if (!response.ok) throw new Error(await response.text());
      await load();
    } catch (e) { error = String(e); }
  }
  load();
</script>
<div class="flex h-dvh flex-col bg-neutral-950 text-neutral-200">
  <header class="flex items-center gap-2 border-b border-neutral-900 px-2 py-2">
    <button class="icon-btn" aria-label="Назад" onclick={() => selected ? selected = null : onback()}><Icon name="back" /></button>
    <h1 class="px-1 text-sm font-medium text-neutral-100">{selected?.name || 'Skills'}</h1>
  </header>
  <div class="flex-1 overflow-y-auto p-4">
    {#if error}<p role="alert" class="mb-3 text-red-400">{error}</p>{/if}
    {#if selected}
      <p class="mb-3 text-xs text-neutral-400">Текущая версия: {new Date(selected.date).toLocaleString('ru-RU')}</p>
      <pre class="whitespace-pre-wrap break-words rounded-xl bg-neutral-900 p-3 text-sm">{selected.content}</pre>
      <h2 class="mt-5 mb-2 text-sm">Версии</h2>
      {#each selected.versions as version}
        <div class="flex items-center justify-between gap-2 border-b border-neutral-800 py-2 text-sm">
          <span>{new Date(Number(version.split('-')[0])).toLocaleString('ru-RU')}</span>
          <button class="text-blue-400" onclick={() => action('POST', version)}>Откатить к этой версии</button>
        </div>
      {:else}<p class="text-sm text-neutral-500">Предыдущих версий нет</p>{/each}
      {#if !selected.builtin}<button class="mt-5 text-sm text-red-400" onclick={() => action('DELETE')}>Удалить skill</button>{/if}
    {:else}
      {#each skills as skill}
        <button class="block w-full border-b border-neutral-800 py-3 text-left" onclick={() => selected = skill}>
          <span class="block text-sm">{skill.name}</span><span class="block text-xs text-neutral-400">{description(skill.content)}</span><span class="text-xs text-neutral-500">{new Date(skill.date).toLocaleString('ru-RU')}</span>
        </button>
      {:else}<p class="text-sm text-neutral-500">Skills пока нет</p>{/each}
    {/if}
  </div>
</div>
