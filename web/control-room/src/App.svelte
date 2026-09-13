<script>
  import { onMount } from 'svelte';

  let tab = 'overview';
  let loading = true;
  let error = '';
  let overview = { nodes: [], workers: [], clients: [], projects: [], approvals: [], events: [], deliveries: [], commands: [] };
  let config = null;
  let configText = '';
  let profiles = [];
  let selectedProfile = '';
  let profileText = '';
  let actionInFlight = '';
  let diagnostics = null;
  let timer;
  const controlRoutes = ['/v1/control/nodes', '/v1/control/workers', '/v1/control/clients', '/v1/control/projects', '/v1/control/diagnostics/', '/v1/control/export'];

  $: selectedProfileData = profiles.find((item) => item.name === selectedProfile);

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
      error = '';
    } catch (reason) { error = reason.message; }
    loading = false;
  }

  async function loadConfig() {
    try {
      config = await request('/v1/control/config');
      configText = config.content || '';
    } catch (reason) { error = reason.message; }
  }

  async function loadProfiles() {
    try {
      profiles = await request('/v1/control/profiles');
      const current = profiles.find((item) => item.name === selectedProfile) || profiles[0];
      selectedProfile = current?.name || '';
      profileText = current?.content || '';
    } catch (reason) { error = reason.message; }
  }

  async function selectTab(next) {
    tab = next;
    if (next === 'config' && !config) await loadConfig();
    if (next === 'profiles' && !profiles.length) await loadProfiles();
    if (next === 'events') await refresh();
  }

  function selectProfile(name) {
    const profile = profiles.find((item) => item.name === name);
    if (profile) { selectedProfile = profile.name; profileText = profile.content || ''; }
  }

  async function saveConfig() {
    if (!config?.editable) { error = 'Config contains opaque values. Reload it before editing.'; return; }
    actionInFlight = 'config';
    try { await request('/v1/control/config', { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ content: configText, expected_revision: config.revision }) }); await loadConfig(); await refresh(); }
    catch (reason) { error = reason.message; }
    actionInFlight = '';
  }

  async function reloadConfig() {
    actionInFlight = 'config-reload';
    try { await request('/v1/control/config/reload', { method: 'POST' }); await loadConfig(); await refresh(); }
    catch (reason) { error = reason.message; }
    actionInFlight = '';
  }

  async function saveProfile() {
    if (!selectedProfile || !selectedProfileData?.editable) { error = 'Profile contains opaque values. Reload it before editing.'; return; }
    actionInFlight = `profile:${selectedProfile}`;
    try { await request(`/v1/control/profiles/${encodeURIComponent(selectedProfile)}`, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ content: profileText, expected_revision: selectedProfileData.revision || selectedProfileData.hash }) }); await loadProfiles(); await refresh(); }
    catch (reason) { error = reason.message; }
    actionInFlight = '';
  }

  async function reloadProfiles() {
    actionInFlight = 'profiles-reload';
    try { await request('/v1/control/profiles/reload', { method: 'POST' }); await loadProfiles(); await refresh(); }
    catch (reason) { error = reason.message; }
    actionInFlight = '';
  }

  async function revoke(kind, id) {
    actionInFlight = `${kind}:${id}`;
    try { await request(`/v1/control/${kind}/${encodeURIComponent(id)}/revoke`, { method: 'POST', headers: { 'Idempotency-Key': `control-room-${kind}-${id}` } }); await refresh(); }
    catch (reason) { error = reason.message; }
    actionInFlight = '';
  }

  async function openDiagnostics(workerRef) {
    try { diagnostics = await request(`/v1/control/diagnostics/${encodeURIComponent(workerRef)}`); }
    catch (reason) { error = reason.message; }
  }

  async function downloadExport() {
    actionInFlight = 'export';
    try {
      const response = await fetch('/v1/control/export', { credentials: 'same-origin' });
      if (!response.ok) throw new Error(await response.text() || response.statusText);
      const blob = await response.blob();
      const link = document.createElement('a');
      link.href = URL.createObjectURL(blob);
      link.download = `secretary-diagnostics-${new Date().toISOString().replaceAll(':', '-')}.json`;
      link.click();
      URL.revokeObjectURL(link.href);
    } catch (reason) { error = reason.message; }
    actionInFlight = '';
  }

  function pretty(value) { return JSON.stringify(value, null, 2); }

  onMount(() => { refresh(); timer = setInterval(refresh, 5000); return () => clearInterval(timer); });
</script>

<main class="min-h-screen bg-[#070b14] text-slate-100">
  <div class="mx-auto max-w-7xl px-4 py-5 sm:px-6 lg:px-8">
    <header class="flex flex-wrap items-center justify-between gap-4 border-b border-white/10 pb-5">
      <div><div class="flex items-center gap-3"><span class="grid h-10 w-10 place-items-center rounded-2xl bg-cyan-300/15 font-semibold text-cyan-200">C</span><h1 class="text-xl font-semibold">Control Room</h1></div><p class="mt-2 text-xs text-slate-500">Debug-only operator diagnostics. Server responses are allowlisted and recursively redacted.</p></div>
      <div class="flex items-center gap-2"><span class="rounded-full border border-amber-300/30 bg-amber-300/10 px-3 py-1 text-[10px] uppercase tracking-[0.16em] text-amber-200">debug only</span><a href="/" class="rounded-xl border border-white/10 px-3 py-2 text-xs text-slate-300 hover:bg-white/10">Conversation</a></div>
    </header>

    <nav class="mt-5 flex gap-1 overflow-x-auto rounded-2xl border border-white/10 bg-white/[0.03] p-1" aria-label="Control Room sections">
      {#each [['overview', 'Overview'], ['events', 'Events'], ['inventory', 'Inventory'], ['profiles', 'Profiles'], ['config', 'Config']] as item}<button class="shrink-0 rounded-xl px-4 py-2 text-xs {tab === item[0] ? 'bg-cyan-300 text-slate-950' : 'text-slate-400 hover:bg-white/10'}" on:click={() => selectTab(item[0])}>{item[1]}</button>{/each}
      <button class="ml-auto shrink-0 rounded-xl px-4 py-2 text-xs text-slate-400 hover:bg-white/10" disabled={actionInFlight !== ''} on:click={downloadExport}>Export diagnostics</button><button class="shrink-0 rounded-xl px-4 py-2 text-xs text-slate-400 hover:bg-white/10" on:click={refresh}>Refresh</button>
    </nav>
    {#if error}<div class="mt-4 rounded-2xl border border-rose-400/20 bg-rose-400/10 p-4 text-sm text-rose-200">{error}</div>{/if}
    {#if loading}<div class="mt-12 text-center text-sm uppercase tracking-[0.16em] text-slate-600">Reading durable state...</div>{:else if tab === 'overview'}
      <section class="mt-6 space-y-5">
        <div class="grid gap-3 md:grid-cols-3"><div class="rounded-2xl border border-white/10 bg-white/[0.03] p-4"><p class="text-xs text-slate-500">Nodes</p><p class="mt-2 text-2xl">{overview.nodes?.length || 0}</p></div><div class="rounded-2xl border border-white/10 bg-white/[0.03] p-4"><p class="text-xs text-slate-500">Workers</p><p class="mt-2 text-2xl">{overview.workers?.length || 0}</p></div><div class="rounded-2xl border border-white/10 bg-white/[0.03] p-4"><p class="text-xs text-slate-500">Pending deliveries</p><p class="mt-2 text-2xl">{overview.deliveries?.filter((item) => item.state === 'pending').length || 0}</p></div></div>
        <div class="grid gap-5 lg:grid-cols-2"><section><h2 class="text-sm font-medium text-slate-300">Nodes and harness inventory</h2><div class="mt-3 space-y-3">{#each overview.nodes || [] as node}<article class="rounded-2xl border border-white/10 bg-white/[0.035] p-4"><div class="flex justify-between gap-3"><h3 class="font-mono text-sm text-cyan-200">{node.node}</h3><span class="text-xs text-slate-400">{node.health}</span></div><p class="mt-2 text-xs text-slate-500">heartbeat {node.last_heartbeat_at || 'not observed'} · capacity {node.capacity}</p>{#each node.inventory?.instances || [] as instance}<div class="mt-3 rounded-xl bg-black/20 p-3 text-xs"><div class="flex justify-between"><span>{instance.harness} {instance.version}</span><span>{instance.status}</span></div><p class="mt-1 text-slate-500">{instance.id} · models {instance.model_ids?.join(', ') || 'none'}</p><p class="mt-1 text-slate-500">reasoning {instance.reasoning_levels?.join(', ') || 'none'} · auth {instance.authentication?.authenticated ? instance.authentication.method || 'authenticated' : 'not authenticated'}</p><p class="mt-1 text-slate-500">execution {instance.capabilities?.execution?.join(', ') || 'none'} · activity {instance.capabilities?.activity?.join(', ') || 'none'}</p></div>{/each}<button class="mt-3 rounded-xl border border-rose-400/20 px-3 py-2 text-xs text-rose-300 hover:bg-rose-400/10 disabled:opacity-50" disabled={node.revoked || actionInFlight !== ''} on:click={() => revoke('nodes', node.node)}>Revoke Node</button></article>{/each}</div></section>
          <section><h2 class="text-sm font-medium text-slate-300">Workers, Turns and Attempts</h2><div class="mt-3 space-y-3">{#each overview.workers || [] as worker}<article class="rounded-2xl border border-white/10 bg-white/[0.035] p-4"><div class="flex justify-between gap-3"><h3 class="text-sm text-slate-200">{worker.title}</h3><span class="text-xs text-slate-400">{worker.status}</span></div><p class="mt-2 font-mono text-xs text-cyan-200">{worker.worker_ref} · {worker.node} · {worker.harness_instance}</p><div class="mt-3 rounded-xl border border-cyan-300/15 bg-cyan-300/[0.04] p-3 text-xs" aria-label="Immutable Worker binding"><p class="text-[10px] uppercase tracking-[0.14em] text-cyan-200">Immutable Worker binding</p><p class="mt-2 text-slate-400">project <span class="font-mono text-slate-200">{worker.binding.project_id}</span> · node <span class="font-mono text-slate-200">{worker.binding.node}</span> · harness <span class="font-mono text-slate-200">{worker.binding.harness_instance}</span></p><p class="mt-1 text-slate-500">workspace {worker.binding.workspace || 'none'} · archived {worker.binding.archived ? 'yes' : 'no'}</p></div><p class="mt-3 text-xs text-slate-500">worker lifecycle {worker.status} · recovery {worker.recovery?.state} · health {worker.recovery?.health}</p><div class="mt-3 space-y-2"><h4 class="text-xs font-medium text-slate-300">Turns</h4>{#each worker.turns || [] as turn}<div class="rounded-xl border border-white/10 bg-black/15 p-3 text-xs" data-turn-id={turn.id}><div class="flex justify-between gap-2"><span class="font-mono text-cyan-200">Turn {turn.id}</span><span class="text-slate-300">state {turn.state}</span></div><p class="mt-1 text-slate-500">attempt {turn.current_attempt_id || 'none'} · result {turn.result_id || 'none'}</p></div>{/each}</div><div class="mt-3 space-y-2"><h4 class="text-xs font-medium text-slate-300">Attempts</h4>{#each worker.attempts || [] as attempt}<div class="rounded-xl border border-white/10 p-3 text-xs"><div class="flex justify-between gap-2"><span class="font-mono text-slate-300">Attempt {attempt.number} · {attempt.id}</span><span class="text-cyan-200">state {attempt.state}</span></div><p class="mt-1 text-slate-500">turn {attempt.turn_id} · node {attempt.node} · harness {attempt.harness_instance} · correlation {attempt.correlation_id || 'none'}</p></div>{/each}</div><div class="mt-3 space-y-2"><h4 class="text-xs font-medium text-slate-300">Attempt outcomes</h4>{#each worker.attempt_outcomes || [] as outcome}<div class="rounded-xl border border-amber-300/15 bg-amber-300/[0.03] p-3 text-xs"><div class="flex justify-between gap-2"><span class="font-mono text-slate-300">Outcome {outcome.id} · attempt {outcome.attempt_id}</span><span class="text-amber-200">status {outcome.status}</span></div><p class="mt-1 text-slate-500">classification {outcome.classification} · code {outcome.error_code || 'none'}</p>{#if outcome.error_message}<p class="mt-1 whitespace-pre-wrap text-slate-400">{outcome.error_message}</p>{/if}</div>{/each}</div><div class="mt-3 space-y-2"><h4 class="text-xs font-medium text-slate-300">Results</h4>{#each worker.results || [] as result}<div class="rounded-xl border border-emerald-300/15 bg-emerald-300/[0.03] p-3 text-xs"><div class="flex justify-between gap-2"><span class="font-mono text-slate-300">Result {result.id}</span><span class="text-emerald-200">status {result.status}</span></div><p class="mt-1 whitespace-pre-wrap text-slate-400">{result.summary}</p></div>{/each}</div><div class="mt-3 flex flex-wrap gap-2"><button class="rounded-lg border border-cyan-300/20 px-2 py-1 text-[11px] text-cyan-200 hover:bg-cyan-300/10" on:click={() => openDiagnostics(worker.worker_ref)}>Diagnostics</button></div></article>{/each}</div></section></div>
      </section>
    {:else if tab === 'events'}
      <section class="mt-6 space-y-5"><div><h2 class="text-sm font-medium text-slate-300">Normalized events</h2><div class="mt-3 overflow-hidden rounded-2xl border border-white/10">{#each overview.events || [] as event}<article class="border-b border-white/5 p-4 last:border-0"><div class="flex flex-wrap justify-between gap-2"><span class="font-mono text-xs text-cyan-200">#{event.sequence} {event.kind}</span><span class="text-[11px] text-slate-600">{event.id}</span></div><pre class="mt-2 max-h-32 overflow-auto whitespace-pre-wrap text-xs text-slate-500">{pretty(event.payload)}</pre></article>{/each}</div></div><div><h2 class="text-sm font-medium text-slate-300">Delivery and command state</h2><pre class="mt-3 max-h-72 overflow-auto rounded-2xl border border-white/10 bg-white/[0.03] p-4 text-xs text-slate-500">{pretty({ deliveries: overview.deliveries, commands: overview.commands })}</pre></div></section>
    {:else if tab === 'inventory'}
      <section class="mt-6 grid gap-5 lg:grid-cols-2"><div><h2 class="text-sm font-medium text-slate-300">Clients and pairing state</h2>{#each overview.clients || [] as client}<article class="mt-3 rounded-2xl border border-white/10 bg-white/[0.035] p-4"><div class="flex justify-between"><span>{client.display_name}</span><span class="text-xs text-slate-500">{client.status}</span></div><p class="mt-2 text-xs text-slate-500">{client.device_id} · {client.platform}</p><button class="mt-3 rounded-xl border border-rose-400/20 px-3 py-2 text-xs text-rose-300 hover:bg-rose-400/10 disabled:opacity-50" disabled={client.status === 'revoked' || actionInFlight !== ''} on:click={() => revoke('clients', client.id)}>Revoke Client</button></article>{/each}</div><div><h2 class="text-sm font-medium text-slate-300">Projects and approval audit</h2>{#each overview.projects || [] as project}<article class="mt-3 rounded-2xl border border-white/10 bg-white/[0.035] p-4"><div class="flex justify-between"><span>{project.name}</span><span class="text-xs text-slate-500">revision {project.revision}</span></div>{#each project.mappings || [] as mapping}<p class="mt-2 font-mono text-xs text-slate-500">{mapping.node}: {mapping.path}</p>{/each}</article>{/each}{#each overview.approvals || [] as approval}<div class="mt-3 rounded-xl border border-white/10 p-3 text-xs"><span class="text-cyan-200">{approval.kind}</span> · {approval.state} · {approval.action_summary}</div>{/each}</div></section>
    {:else if tab === 'profiles'}
      <section class="mt-6 grid gap-5 lg:grid-cols-[15rem_minmax(0,1fr)]"><aside class="rounded-2xl border border-white/10 bg-white/[0.025] p-3"><h2 class="px-2 py-2 text-sm">Profiles</h2>{#each profiles as profile}<button class="w-full rounded-xl px-3 py-3 text-left text-xs {selectedProfile === profile.name ? 'bg-cyan-300 text-slate-950' : 'text-slate-400 hover:bg-white/10'}" on:click={() => selectProfile(profile.name)}>{profile.name}</button>{/each}</aside><div><div class="flex justify-between"><h2 class="text-sm">{selectedProfile || 'Profile'}</h2><button class="text-xs text-cyan-200" on:click={reloadProfiles}>Reload disk</button></div><textarea bind:value={profileText} readonly={selectedProfileData && !selectedProfileData.editable} spellcheck="false" class="mt-4 min-h-[28rem] w-full rounded-2xl border border-white/10 bg-black/25 p-4 font-mono text-xs text-slate-300"></textarea>{#if selectedProfileData && !selectedProfileData.editable}<p class="mt-3 text-xs text-amber-300">Opaque values are redacted. This profile is immutable in the editor. Reload after an out-of-band change.</p>{/if}<button class="mt-3 rounded-xl bg-cyan-300 px-4 py-2 text-xs font-medium text-slate-950" disabled={!selectedProfile || !selectedProfileData?.editable || actionInFlight !== ''} on:click={saveProfile}>Validate and apply</button></div></section>
    {:else if tab === 'config'}
      <section class="mt-6 grid gap-5 lg:grid-cols-[minmax(0,1fr)_20rem]"><div><div class="flex justify-between"><h2 class="text-sm">Managed config</h2><button class="text-xs text-cyan-200" on:click={reloadConfig}>Reload disk</button></div><textarea bind:value={configText} readonly={config && !config.editable} spellcheck="false" class="mt-4 min-h-[28rem] w-full rounded-2xl border border-white/10 bg-black/25 p-4 font-mono text-xs text-slate-300"></textarea>{#if config && !config.editable}<p class="mt-3 text-xs text-amber-300">Opaque values are redacted. This config is immutable in the editor. Reload after an out-of-band change.</p>{/if}<button class="mt-3 rounded-xl bg-cyan-300 px-4 py-2 text-xs font-medium text-slate-950" disabled={actionInFlight !== '' || (config && !config.editable)} on:click={saveConfig}>Validate and apply</button></div><aside class="rounded-2xl border border-white/10 bg-white/[0.025] p-4"><h2 class="text-sm">Compiled snapshot</h2><pre class="mt-3 max-h-[34rem] overflow-auto whitespace-pre-wrap text-xs text-slate-500">{pretty(config?.snapshot || {})}</pre></aside></section>
    {/if}
  </div>
</main>

{#if diagnostics}<div class="fixed inset-0 z-50 grid place-items-center bg-black/75 p-4"><div class="flex max-h-[85vh] w-full max-w-4xl flex-col rounded-2xl border border-white/10 bg-[#111827] p-5" role="dialog" aria-modal="true"><div class="flex justify-between"><h2 class="text-sm">Diagnostics · {diagnostics.worker_ref}</h2><button class="text-slate-400" on:click={() => diagnostics = null}>Close</button></div><pre class="mt-4 flex-1 overflow-auto rounded-xl bg-black/30 p-4 font-mono text-xs text-emerald-200">{pretty(diagnostics)}</pre></div></div>{/if}
