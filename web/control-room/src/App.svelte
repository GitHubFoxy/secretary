<script>
  import { onMount } from 'svelte';

  let tab = 'overview';
  let loading = true;
  let error = '';
  let overview = { workers: [], events: [] };
  let config = null;
  let configText = '';
  let configError = '';
  let profiles = null;
  let selectedProfile = '';
  let profileText = '';
  let profileError = '';
  let busyAction = '';
  let rawLog = null;
  let refreshTimer;
  let selectedModel = '';
  let modelError = '';
  $: selectedProfileData = profiles?.find((profile) => profile.name === selectedProfile) || null;

  async function request(path, options = {}) {
    const response = await fetch(path, { credentials: 'same-origin', ...options });
    const text = await response.text();
    let body = null;
    try { body = text ? JSON.parse(text) : null; } catch (_) { body = null; }
    if (!response.ok) throw new Error(body?.error || text || response.statusText);
    return body;
  }

  async function refresh() {
    try {
      overview = await request('/v1/control/overview');
      selectedModel = overview.secretary?.selected || selectedModel;
      error = '';
      if (tab === 'config' && !config) await loadConfig();
      if (tab === 'profiles' && !profiles) await loadProfiles();
    } catch (reason) { error = reason.message; }
    loading = false;
  }

  async function loadConfig() {
    try {
      config = await request('/v1/control/config');
      configText = config.content || '';
      configError = '';
    } catch (reason) { configError = reason.message; }
  }

  async function loadProfiles() {
    try {
      profiles = await request('/v1/control/profiles');
      if (!profiles.length) {
        selectedProfile = '';
        profileText = '';
        return;
      }
      const current = profiles.find((profile) => profile.name === selectedProfile) || profiles[0];
      selectedProfile = current.name;
      profileText = current.content || '';
      profileError = '';
    } catch (reason) { profileError = reason.message; }
  }

  function selectProfile(name) {
    const profile = profiles?.find((item) => item.name === name);
    if (!profile) return;
    selectedProfile = profile.name;
    profileText = profile.content || '';
    profileError = '';
  }

  async function reloadProfiles() {
    busyAction = 'profiles:reload';
    try {
      await request('/v1/control/profiles/reload', { method: 'POST' });
      await loadProfiles();
      profileError = '';
    } catch (reason) { profileError = reason.message; }
    busyAction = '';
  }

  async function saveProfile() {
    if (!selectedProfile) return;
    busyAction = `profile:${selectedProfile}`;
    try {
      await request(`/v1/control/profiles/${encodeURIComponent(selectedProfile)}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ content: profileText })
      });
      await loadProfiles();
      await refresh();
      profileError = '';
    } catch (reason) { profileError = reason.message; }
    busyAction = '';
  }

  async function selectTab(next) {
    tab = next;
    if (next === 'config' && !config) await loadConfig();
    if (next === 'profiles' && !profiles) await loadProfiles();
    if (next === 'events') await refresh();
  }

  async function action(taskID, actionName) {
    busyAction = `${taskID}:${actionName}`;
    try {
      await request(`/v1/control/tasks/${encodeURIComponent(taskID)}/${actionName}`, { method: 'POST' });
      await refresh();
    } catch (reason) { error = reason.message; }
    busyAction = '';
  }

  async function changeModel() {
    modelError = '';
    try {
      const state = await request('/v1/secretary/model', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ model: selectedModel }) });
      overview = { ...overview, secretary: state };
    } catch (reason) { modelError = reason.message; }
  }

  async function restartRuntime() {
    busyAction = 'runtime:restart';
    try { await request('/v1/control/runtime/restart', { method: 'POST' }); await refresh(); }
    catch (reason) { error = reason.message; }
    busyAction = '';
  }

  async function saveConfig() {
    busyAction = 'config:save';
    try {
      await request('/v1/control/config', { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ content: configText }) });
      await loadConfig();
      await refresh();
      configError = '';
    } catch (reason) { configError = reason.message; }
    busyAction = '';
  }

  async function reloadConfig() {
    busyAction = 'config:reload';
    try { await request('/v1/control/config/reload', { method: 'POST' }); await loadConfig(); await refresh(); }
    catch (reason) { configError = reason.message; }
    busyAction = '';
  }

  async function readRawLog(workerRef) {
    try {
      const response = await fetch(`/v1/control/raw-log/${encodeURIComponent(workerRef)}`, { credentials: 'same-origin' });
      if (!response.ok) throw new Error(await response.text() || response.statusText);
      rawLog = { workerRef, text: await response.text() };
    } catch (reason) { error = reason.message; }
  }

  function downloadExport() {
    request('/v1/control/export').then((data) => {
      const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' });
      const link = document.createElement('a');
      link.href = URL.createObjectURL(blob);
      link.download = `secretary-diagnostics-${new Date().toISOString().replaceAll(':', '-')}.json`;
      link.click();
      URL.revokeObjectURL(link.href);
    }).catch((reason) => { error = reason.message; });
  }

  function taskStatus(card) {
    const attempt = card?.attempts?.[card.attempts.length - 1];
    return attempt?.state || card?.task?.state || 'unknown';
  }

  function payloadText(value) {
    return typeof value === 'string' ? value : JSON.stringify(value, null, 2);
  }

  onMount(() => {
    refresh();
    refreshTimer = setInterval(refresh, 5000);
    return () => clearInterval(refreshTimer);
  });
</script>

<main class="min-h-screen bg-[#070b14] text-slate-100">
  <div class="mx-auto max-w-7xl px-4 py-5 sm:px-6 lg:px-8">
    <header class="flex flex-wrap items-center justify-between gap-4 border-b border-white/10 pb-5">
      <div><div class="flex items-center gap-3"><span class="grid h-10 w-10 place-items-center rounded-2xl bg-cyan-300/15 font-semibold text-cyan-200">C</span><h1 class="text-xl font-semibold tracking-tight">Control Room</h1></div><p class="mt-2 text-xs text-slate-500">Local diagnostics and lifecycle actions. Debug mode only.</p></div>
      <div class="flex items-center gap-2"><span class="rounded-full border border-amber-300/30 bg-amber-300/10 px-3 py-1 text-[10px] uppercase tracking-[0.16em] text-amber-200">debug</span><a href="/" class="rounded-xl border border-white/10 px-3 py-2 text-xs text-slate-300 hover:bg-white/10">Conversation</a></div>
    </header>

    <nav class="mt-5 flex gap-1 overflow-x-auto rounded-2xl border border-white/10 bg-white/[0.03] p-1" aria-label="Control Room sections">{#each [['overview', 'Overview'], ['events', 'Events'], ['profiles', 'Profiles'], ['config', 'Config']] as item}<button class="shrink-0 rounded-xl px-4 py-2 text-xs {tab === item[0] ? 'bg-cyan-300 text-slate-950' : 'text-slate-400 hover:bg-white/10 hover:text-slate-200'}" on:click={() => selectTab(item[0])}>{item[1]}</button>{/each}<button class="ml-auto shrink-0 rounded-xl px-4 py-2 text-xs text-slate-400 hover:bg-white/10 hover:text-slate-200" on:click={downloadExport}>Export diagnostics</button></nav>
    {#if error}<div class="mt-4 rounded-2xl border border-rose-400/20 bg-rose-400/10 p-4 text-sm text-rose-200">{error}<a class="ml-2 underline" href="/">Open User UI</a></div>{/if}
    {#if loading}<div class="mt-12 text-center text-sm tracking-[0.16em] text-slate-600 uppercase">Reading durable state...</div>{:else if tab === 'overview'}
      <section class="mt-6 grid gap-5 lg:grid-cols-[minmax(0,1fr)_19rem]">
        <div class="space-y-5">
          <div class="flex items-center justify-between"><h2 class="text-sm font-medium text-slate-300">Worker tree</h2><span class="text-xs text-slate-600">{overview.workers?.length || 0} roots</span></div>
          <div class="grid gap-3 md:grid-cols-2">{#if !overview.workers?.length}<div class="rounded-2xl border border-dashed border-white/10 p-6 text-sm text-slate-500">No durable Workers yet.</div>{/if}{#each overview.workers || [] as card (card.task.id)}<article class="rounded-2xl border border-white/10 bg-white/[0.035] p-4"><div class="flex items-start justify-between gap-3"><h3 class="text-sm leading-6 text-slate-200">{card.task.text}</h3><span class="rounded-full border border-white/10 px-2 py-1 text-[10px] uppercase tracking-wide text-slate-400">{taskStatus(card)}</span></div><p class="mt-3 text-xs text-slate-500">task {card.task.id} · {card.binding?.worker_ref || 'not bound'}{#if card.binding?.profile?.delivery}<span class="ml-2 text-cyan-300">{card.binding.profile.delivery}</span>{/if}</p>{#if card.results?.length}<pre class="mt-3 max-h-28 overflow-y-auto whitespace-pre-wrap text-xs leading-5 text-slate-400">{card.results[card.results.length - 1].summary}</pre>{/if}<div class="mt-4 flex flex-wrap gap-2"><button class="rounded-xl border border-white/10 px-3 py-2 text-xs text-slate-300 hover:bg-white/10 disabled:opacity-50" disabled={!card.binding || busyAction !== ''} on:click={() => readRawLog(card.binding?.worker_ref)}>Raw log</button><button class="rounded-xl border border-white/10 px-3 py-2 text-xs text-slate-300 hover:bg-white/10 disabled:opacity-50" disabled={busyAction !== '' || card.task.state !== 'dispatch_failed'} on:click={() => action(card.task.id, 'retry')}>Retry</button><button class="rounded-xl border border-amber-400/20 px-3 py-2 text-xs text-amber-300 hover:bg-amber-400/10 disabled:opacity-50" disabled={busyAction !== '' || !card.binding || !['starting', 'active'].includes(taskStatus(card))} on:click={() => action(card.task.id, 'cancel')}>Cancel</button><button class="rounded-xl border border-rose-400/20 px-3 py-2 text-xs text-rose-300 hover:bg-rose-400/10 disabled:opacity-50" disabled={busyAction !== '' || card.task.state === 'closed'} on:click={() => action(card.task.id, 'close')}>Close</button></div>{#if card.children?.length}<div class="mt-4 border-l border-cyan-300/20 pl-3"><p class="text-[11px] uppercase tracking-wide text-cyan-300">Children · {card.children.length}</p>{#each card.children as child}<div class="mt-2 rounded-xl bg-black/15 p-2 text-[11px] text-slate-400"><div class="flex justify-between gap-2"><span class="line-clamp-2">↳ {child.task.text}</span><span class="shrink-0 text-slate-500">{taskStatus(child)}</span></div>{#if child.results?.length}<p class="mt-1 line-clamp-2 text-slate-500">{child.results[child.results.length - 1].summary}</p>{/if}</div>{/each}</div>{/if}</article>{/each}</div>
        </div>
        <aside class="rounded-2xl border border-white/10 bg-white/[0.025] p-5"><h2 class="text-sm font-medium text-slate-300">Runtime</h2><dl class="mt-4 space-y-3 text-xs"><div class="flex justify-between gap-4"><dt class="text-slate-500">Config version</dt><dd class="max-w-32 truncate text-right text-slate-300">{overview.config?.Version || overview.config?.version || 'unknown'}</dd></div><div class="flex justify-between gap-4"><dt class="text-slate-500">Harness</dt><dd class="text-slate-300">{overview.config?.config?.runtime?.harness || overview.config?.Config?.Runtime?.Harness || 'unknown'}</dd></div><div class="flex justify-between gap-4"><dt class="text-slate-500">Reasoning effort</dt><dd class="text-slate-300">{overview.config?.config?.runtime?.reasoning || overview.config?.Config?.Runtime?.Reasoning || 'default'}</dd></div><div class="flex justify-between gap-4"><dt class="text-slate-500">Events loaded</dt><dd class="text-slate-300">{overview.events?.length || 0}</dd></div></dl>{#if Object.entries(overview.secretary?.models || {}).length}<label class="mt-6 block text-xs text-slate-500" for="control-model">Secretary model alias<select id="control-model" bind:value={selectedModel} on:change={changeModel} class="mt-2 w-full rounded-xl border border-white/10 bg-black/20 px-3 py-2 text-xs text-slate-200 outline-none focus:ring-2 focus:ring-cyan-300">{#each Object.entries(overview.secretary.models) as [key, value]}<option value={key}>{key} · configured: {value}</option>{/each}</select></label>{/if}{#if modelError}<p class="mt-2 text-xs text-rose-300">{modelError}</p>{/if}<button class="mt-4 w-full rounded-xl bg-cyan-300 px-3 py-2 text-xs font-medium text-slate-950 hover:bg-cyan-200 disabled:opacity-50" disabled={busyAction !== ''} on:click={restartRuntime}>{busyAction === 'runtime:restart' ? 'Restarting...' : 'Restart runtime'}</button></aside>
      </section>
    {:else if tab === 'events'}
      <section class="mt-6"><div class="flex items-center justify-between"><h2 class="text-sm font-medium text-slate-300">Normalized events</h2><button class="text-xs text-cyan-300 hover:text-cyan-200" on:click={refresh}>Refresh</button></div><div class="mt-4 overflow-hidden rounded-2xl border border-white/10">{#if !overview.events?.length}<p class="p-6 text-sm text-slate-500">No events.</p>{/if}{#each overview.events || [] as event (event.id)}<article class="border-b border-white/5 p-4 last:border-0"><div class="flex flex-wrap items-center justify-between gap-2"><span class="font-mono text-xs text-cyan-200">{event.kind}</span><time class="text-[11px] text-slate-600">{event.created_at}</time></div><p class="mt-2 break-all font-mono text-[11px] leading-5 text-slate-500">{event.worker_ref || ''} {event.attempt_id || ''} {event.runtime_session_id || ''}</p><pre class="mt-2 max-h-32 overflow-y-auto whitespace-pre-wrap text-xs leading-5 text-slate-400">{payloadText(event.payload)}</pre></article>{/each}</div></section>
    {:else if tab === 'profiles'}
      <section class="mt-6 grid gap-5 lg:grid-cols-[15rem_minmax(0,1fr)]"><aside class="rounded-2xl border border-white/10 bg-white/[0.025] p-3"><h2 class="px-2 py-2 text-sm font-medium text-slate-300">Markdown Profiles</h2><div class="space-y-1">{#each profiles || [] as profile}<button class="w-full rounded-xl px-3 py-3 text-left text-xs {selectedProfile === profile.name ? 'bg-cyan-300 text-slate-950' : 'text-slate-400 hover:bg-white/10 hover:text-slate-200'}" on:click={() => selectProfile(profile.name)}><span class="block font-medium">{profile.name}</span><span class="mt-1 block truncate text-[10px] opacity-70">{profile.path}</span></button>{/each}</div></aside><div><div class="flex flex-wrap items-start justify-between gap-3"><div><h2 class="text-sm font-medium text-slate-300">{selectedProfileData?.path?.split('/').pop() || 'Profile.md'}</h2><p class="mt-1 break-all text-xs text-slate-600">{selectedProfileData?.path || ''}</p></div><button class="text-xs text-cyan-300 hover:text-cyan-200 disabled:opacity-50" disabled={busyAction !== ''} on:click={reloadProfiles}>{busyAction === 'profiles:reload' ? 'Reloading...' : 'Reload disk'}</button></div>{#if selectedProfileData}<div class="mt-4 grid gap-2 rounded-2xl border border-white/10 bg-white/[0.025] p-4 text-xs text-slate-400 sm:grid-cols-3"><span>Runtime: <b class="text-slate-200">{selectedProfileData.runtime}</b></span><span>Model ID / alias: <b class="text-slate-200">{selectedProfileData.model}</b></span><span>Reasoning effort: <b class="text-slate-200">{selectedProfileData.reasoning || 'default'}</b></span></div><textarea bind:value={profileText} spellcheck="false" class="mt-4 min-h-[30rem] w-full rounded-2xl border border-white/10 bg-black/25 p-4 font-mono text-xs leading-6 text-slate-300 outline-none focus:ring-2 focus:ring-cyan-300"></textarea>{#if profileError}<p class="mt-3 text-xs text-rose-300">{profileError}</p>{/if}<div class="mt-3 flex flex-wrap items-center gap-3"><button class="rounded-xl bg-cyan-300 px-4 py-2 text-xs font-medium text-slate-950 hover:bg-cyan-200 disabled:opacity-50" disabled={busyAction !== ''} on:click={saveProfile}>{busyAction === `profile:${selectedProfile}` ? 'Saving...' : 'Validate and apply'}</button><span class="text-[11px] text-slate-600">Hash: {selectedProfileData.hash?.slice(0, 16) || 'unknown'}</span></div>{/if}</div></section>
    {:else if tab === 'config'}
      <section class="mt-6 grid gap-5 lg:grid-cols-[minmax(0,1fr)_20rem]"><div><div class="flex items-center justify-between"><div><h2 class="text-sm font-medium text-slate-300">Managed config.toml</h2><p class="mt-1 text-xs text-slate-600">{config?.path || ''}</p></div><button class="text-xs text-cyan-300 hover:text-cyan-200" on:click={reloadConfig}>Reload disk</button></div><textarea bind:value={configText} spellcheck="false" class="mt-4 min-h-[30rem] w-full rounded-2xl border border-white/10 bg-black/25 p-4 font-mono text-xs leading-6 text-slate-300 outline-none focus:ring-2 focus:ring-cyan-300"></textarea>{#if configError}<p class="mt-3 text-xs text-rose-300">{configError}</p>{/if}<button class="mt-3 rounded-xl bg-cyan-300 px-4 py-2 text-xs font-medium text-slate-950 hover:bg-cyan-200 disabled:opacity-50" disabled={busyAction !== ''} on:click={saveConfig}>{busyAction === 'config:save' ? 'Saving...' : 'Validate and apply'}</button></div><aside class="rounded-2xl border border-white/10 bg-white/[0.025] p-5"><h2 class="text-sm font-medium text-slate-300">Compiled snapshot</h2><pre class="mt-4 max-h-[34rem] overflow-y-auto whitespace-pre-wrap break-words text-[11px] leading-5 text-slate-500">{JSON.stringify(config?.snapshot || {}, null, 2)}</pre></aside></section>
    {/if}
  </div>
</main>

{#if rawLog}<div class="fixed inset-0 z-50 grid place-items-center bg-black/75 p-4"><div class="flex max-h-[85vh] w-full max-w-4xl flex-col rounded-2xl border border-white/10 bg-[#111827] p-5" role="dialog" aria-modal="true" tabindex="-1"><div class="flex items-center justify-between"><h2 class="text-sm font-medium">Raw ACP log · {rawLog.workerRef}</h2><button class="text-slate-400 hover:text-white" on:click={() => rawLog = null}>Close</button></div><pre class="mt-4 min-h-40 flex-1 overflow-auto rounded-xl bg-black/30 p-4 font-mono text-[11px] leading-5 text-emerald-200">{rawLog.text}</pre></div></div>{/if}
