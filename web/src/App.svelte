<script>
  import { onMount } from 'svelte';

  const ONBOARDING_KEY = 'secretary-onboarding-v1';
  let authenticated = false;
  let loading = true;
  let loginToken = '';
  let loginError = '';
  let entries = [];
  let workers = [];
  let conversationID = '';
  let connection = 'offline';
  let modelOptions = {};
  let selectedModel = '';
  let modelError = '';
  let message = '';
  let messageError = '';
  let messageStatus = 'idle';
  let sendingMessage = false;
  let observer = null;
  let observerError = '';
  let activityLines = [];
  let steerText = '';
  let queueText = '';
  let onboardingOpen = false;
  let onboardingStep = 0;
  let conversationSocket;
  let activitySocket;
  let reconnectTimer;
  let refreshTimer;

  $: modelEntries = Object.entries(modelOptions);
  $: onboardingTitle = ['Welcome to Secretary', 'Your private workbench', 'Choose a default model', 'You are ready'][onboardingStep];
  $: observerCard = observer ? workers.find((card) => card.binding?.worker_ref === observer.workerRef) : null;

  async function request(path, options = {}) {
    const response = await fetch(path, { credentials: 'same-origin', ...options });
    const text = await response.text();
    let body = null;
    try { body = text ? JSON.parse(text) : null; } catch (_) { body = null; }
    if (!response.ok) throw new Error(body?.error || text || response.statusText);
    return body;
  }

  function formatDate(value) {
    if (!value) return '';
    return new Intl.DateTimeFormat(undefined, { dateStyle: 'short', timeStyle: 'short' }).format(new Date(value));
  }

  function statusFor(card) {
    const attempt = card?.attempts?.[card.attempts.length - 1];
    if (card?.task?.state === 'dispatch_failed') return 'dispatch failed';
    if (attempt?.state === 'active' || attempt?.state === 'starting') return attempt.state;
    if (attempt?.state) return attempt.state;
    return card?.task?.state || 'unknown';
  }

  function statusTone(status) {
    if (status === 'active' || status === 'succeeded') return 'border-emerald-400/30 bg-emerald-400/10 text-emerald-200';
    if (status === 'failed' || status === 'dispatch failed') return 'border-rose-400/30 bg-rose-400/10 text-rose-200';
    if (status === 'canceled' || status === 'interrupted' || status === 'closed') return 'border-slate-500/30 bg-slate-500/10 text-slate-300';
    return 'border-amber-400/30 bg-amber-400/10 text-amber-200';
  }

  function appendEntry(entry) {
    if (!entry) return;
    if (entry.kind === 'secretary' && (messageStatus === 'sending' || messageStatus === 'working')) {
      messageStatus = 'idle';
    }
    if (entries.some((item) => item.id === entry.id || item.seq === entry.seq)) return;
    entries = [...entries, entry].sort((a, b) => a.seq - b.seq);
  }

  async function loadState() {
    const state = await request('/v1/bootstrap');
    authenticated = true;
    conversationID = state.conversation_id;
    entries = (await request('/v1/conversation?after_seq=0')) || [];
    workers = state.workers || [];
    modelOptions = state.secretary?.models || {};
    selectedModel = state.secretary?.selected || '';
    onboardingOpen = !localStorage.getItem(ONBOARDING_KEY);
    loading = false;
    connectConversation();
    refreshTimer = setInterval(refreshWorkers, 5000);
  }

  async function refreshWorkers() {
    if (!authenticated) return;
    try { workers = await request('/v1/workers'); } catch (_) { /* session remains useful while a refresh fails */ }
  }

  async function bootstrapSession(token) {
    await request('/v1/web/session', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ bootstrap_token: token })
    });
  }

  async function restoreSession() {
    loading = true;
    const hash = new URLSearchParams(location.hash.slice(1));
    const token = hash.get('bootstrap');
    try {
      if (token) {
        await bootstrapSession(token);
        history.replaceState(null, '', `${location.pathname}${location.search}`);
      } else {
        await request('/v1/web/session');
      }
      await loadState();
    } catch (_) {
      loading = false;
      authenticated = false;
    }
  }

  async function login() {
    loginError = '';
    try {
      await bootstrapSession(loginToken.trim());
      loginToken = '';
      await loadState();
    } catch (error) { loginError = error.message; }
  }

  function connectConversation() {
    if (!authenticated) return;
    if (conversationSocket) conversationSocket.close();
    const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
    const lastSeq = entries.length ? entries[entries.length - 1].seq : 0;
    conversationSocket = new WebSocket(`${protocol}//${location.host}/v1/ws?after_seq=${lastSeq}`);
    conversationSocket.onopen = () => { connection = 'connected'; };
    conversationSocket.onmessage = (event) => appendEntry(JSON.parse(event.data));
    conversationSocket.onerror = () => conversationSocket.close();
    conversationSocket.onclose = () => {
      connection = 'reconnecting';
      clearTimeout(reconnectTimer);
      reconnectTimer = setTimeout(connectConversation, 1200);
    };
  }

  function handleMessageKeydown(event) {
    if (event.key !== 'Enter' || event.shiftKey || event.isComposing || event.repeat) return;
    event.preventDefault();
    void sendMessage();
  }

  async function sendMessage() {
    const body = message.trim();
    if (!body || sendingMessage) return;
    sendingMessage = true;
    messageStatus = 'sending';
    messageError = '';
    try {
      const response = await request('/v1/messages', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ external_message_id: crypto.randomUUID(), body })
      });
      appendEntry(response.entry);
      message = '';
      messageStatus = 'working';
      await refreshWorkers();
    } catch (error) {
      messageStatus = 'error';
      messageError = error.message;
    }
    finally { sendingMessage = false; }
  }

  async function changeModel() {
    modelError = '';
    try {
      const state = await request('/v1/secretary/model', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ model: selectedModel })
      });
      selectedModel = state.selected;
      modelOptions = state.models;
    } catch (error) { modelError = error.message; }
  }

  async function openWorker(workerRef) {
    if (!workerRef) return;
    observerError = '';
    if (activitySocket) activitySocket.close();
    observer = { workerRef, loading: true };
    activityLines = [];
    try {
      const [status, thread] = await Promise.all([
        request(`/v1/workers/${encodeURIComponent(workerRef)}`),
        request(`/v1/workers/${encodeURIComponent(workerRef)}/thread`)
      ]);
      observer = { workerRef, status, thread, loading: false };
      connectActivity(workerRef);
    } catch (error) {
      observer = null;
      observerError = error.message;
    }
  }

  function connectActivity(workerRef) {
    const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
    activitySocket = new WebSocket(`${protocol}//${location.host}/v1/workers/${encodeURIComponent(workerRef)}/activity`);
    activitySocket.onmessage = (event) => {
      const item = JSON.parse(event.data);
      activityLines = [...activityLines, item];
    };
  }

  function closeObserver() {
    if (activitySocket) activitySocket.close();
    activitySocket = null;
    observer = null;
    activityLines = [];
  }

  async function steer() {
    if (!observer || !steerText.trim()) return;
    observerError = '';
    try {
      await request(`/v1/workers/${encodeURIComponent(observer.workerRef)}/steer`, {
        method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ text: steerText.trim() })
      });
      steerText = '';
    } catch (error) { observerError = error.message; }
  }

  async function queueFollowUp() {
    if (!observer || !queueText.trim()) return;
    observerError = '';
    try {
      await request(`/v1/workers/${encodeURIComponent(observer.workerRef)}/queue`, {
        method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ text: queueText.trim() })
      });
      queueText = '';
    } catch (error) { observerError = error.message; }
  }

  async function stopWorker() {
    if (!observer) return;
    observerError = '';
    try {
      await request(`/v1/workers/${encodeURIComponent(observer.workerRef)}/stop`, { method: 'POST' });
      await refreshWorkers();
    } catch (error) { observerError = error.message; }
  }

  function finishOnboarding() {
    localStorage.setItem(ONBOARDING_KEY, 'done');
    onboardingOpen = false;
  }

  function nextOnboarding() {
    if (onboardingStep === 3) finishOnboarding();
    else onboardingStep += 1;
  }

  function workerRefFromBody(body) {
    const taskID = body?.match(/tsk_[A-Za-z0-9_-]+/)?.[0];
    if (!taskID) return '';
    return workers.find((card) => card.task?.id === taskID)?.binding?.worker_ref || '';
  }

  onMount(() => {
    restoreSession();
    return () => {
      clearTimeout(reconnectTimer);
      clearInterval(refreshTimer);
      conversationSocket?.close();
      activitySocket?.close();
    };
  });
</script>

{#if loading}
  <main class="flex min-h-screen items-center justify-center bg-[#090d18] px-5 text-slate-300">
    <div class="animate-pulse text-sm tracking-[0.2em] uppercase">Loading Secretary</div>
  </main>
{:else if !authenticated}
  <main class="flex min-h-screen items-center justify-center bg-[#090d18] px-5 py-10 text-slate-100">
    <section class="w-full max-w-md rounded-3xl border border-white/10 bg-white/[0.04] p-7 shadow-2xl shadow-black/30 sm:p-10">
      <div class="mb-8 flex items-center gap-3"><span class="grid h-10 w-10 place-items-center rounded-2xl bg-indigo-400/15 text-xl text-indigo-200">S</span><span class="text-sm font-medium tracking-[0.24em] text-slate-400 uppercase">Secretary</span></div>
      <h1 class="text-3xl font-semibold tracking-tight">Personal Conversation</h1>
      <p class="mt-3 leading-7 text-slate-400">A local-first place to think, delegate work, and keep the thread.</p>
      <form class="mt-8 space-y-4" on:submit|preventDefault={login}>
        <label class="block text-sm text-slate-300" for="bootstrap-token">Bootstrap token
          <input id="bootstrap-token" bind:value={loginToken} type="password" autocomplete="off" required class="mt-2 w-full rounded-2xl border border-white/10 bg-black/20 px-4 py-3 text-slate-100 outline-none ring-indigo-300 transition placeholder:text-slate-600 focus:ring-2" placeholder="Paste the token from secretaryd" />
        </label>
        <button class="w-full rounded-2xl bg-indigo-400 px-4 py-3 font-medium text-slate-950 transition hover:bg-indigo-300 focus:outline-none focus:ring-2 focus:ring-indigo-200" type="submit">Open conversation</button>
      </form>
      {#if loginError}<p class="mt-4 rounded-xl bg-rose-400/10 p-3 text-sm text-rose-200">{loginError}</p>{/if}
    </section>
  </main>
{:else}
  <main class="min-h-screen bg-[#090d18] text-slate-100">
    <div class="mx-auto flex min-h-screen w-full max-w-7xl flex-col px-4 pb-32 pt-4 sm:px-6 lg:px-8">
      <header class="flex flex-wrap items-center justify-between gap-4 border-b border-white/10 pb-4">
        <div class="flex items-center gap-3"><span class="grid h-10 w-10 place-items-center rounded-2xl bg-indigo-400/15 font-semibold text-indigo-200">S</span><div><h1 class="font-semibold tracking-tight">Personal Conversation</h1><p class="text-xs text-slate-500">{conversationID}</p></div></div>
        <div class="flex items-center gap-3 text-xs text-slate-400">
          <span class="flex items-center gap-2"><span class:animate-pulse={connection !== 'connected'} class="h-2 w-2 rounded-full {connection === 'connected' ? 'bg-emerald-300' : 'bg-amber-300'}"></span>{connection}</span>
          {#if modelEntries.length}<label class="sr-only" for="model">Secretary model</label><select id="model" bind:value={selectedModel} on:change={changeModel} class="max-w-36 rounded-xl border border-white/10 bg-white/[0.05] px-3 py-2 text-xs text-slate-200 outline-none focus:ring-2 focus:ring-indigo-300">{#each modelEntries as [key, value]}<option value={key}>{key} · configured: {value}</option>{/each}</select>{/if}
        </div>
      </header>
      {#if modelError}<p class="mt-3 text-right text-xs text-rose-300">{modelError}</p>{/if}

      <div class="mt-5 grid flex-1 gap-5 lg:grid-cols-[minmax(0,1fr)_22rem]">
        <section class="min-w-0">
          <div class="mb-3 flex items-center justify-between"><h2 class="text-sm font-medium text-slate-300">Conversation</h2><span class="text-xs text-slate-600">{entries.length} entries</span></div>
          <div class="space-y-3" aria-live="polite">
            {#if entries.length === 0}<div class="rounded-3xl border border-dashed border-white/10 p-8 text-center text-sm text-slate-500">Start with a question or a task.</div>{/if}
            {#each entries as entry (entry.id)}
              <article class="rounded-2xl border border-white/10 p-4 {entry.kind === 'user' ? 'ml-4 bg-indigo-400/[0.08] sm:ml-16' : entry.kind === 'worker_result' ? 'mr-4 bg-emerald-400/[0.06] sm:mr-16' : 'bg-white/[0.035]'}">
                <div class="mb-2 flex items-center justify-between gap-3 text-[11px] uppercase tracking-[0.12em] text-slate-500"><span>{entry.kind.replaceAll('_', ' ')}</span><time>{formatDate(entry.created_at)}</time></div>
                <p class="whitespace-pre-wrap break-words leading-7 text-slate-200">{entry.body}</p>
                {#if workerRefFromBody(entry.body)}<button class="mt-3 text-xs text-indigo-300 underline decoration-indigo-300/40 underline-offset-4 hover:text-indigo-200" on:click={() => openWorker(workerRefFromBody(entry.body))}>Open Worker</button>{/if}
              </article>
            {/each}
          </div>
        </section>

        <aside class="min-w-0">
          <div class="mb-3 flex items-center justify-between"><h2 class="text-sm font-medium text-slate-300">Workers</h2><span class="text-xs text-slate-600">{workers.length}</span></div>
          {#if observerError && !observer}<p class="mb-3 rounded-xl bg-rose-400/10 p-3 text-xs text-rose-200">{observerError}</p>{/if}
          <div class="space-y-3">
            {#if workers.length === 0}<div class="rounded-2xl border border-dashed border-white/10 p-5 text-sm text-slate-500">Workers created for delegated tasks will appear here.</div>{/if}
            {#each workers as card (card.task.id)}
              {@const workerStatus = statusFor(card)}
              <article class="rounded-2xl border border-white/10 bg-white/[0.035] p-4">
                <div class="flex items-start justify-between gap-3"><h3 class="line-clamp-3 text-sm leading-6 text-slate-200">{card.task.text}</h3><span class="shrink-0 rounded-full border px-2 py-1 text-[10px] uppercase tracking-wide {statusTone(workerStatus)}">{workerStatus}</span></div>
                {#if card.results?.length}<p class="mt-3 line-clamp-3 text-xs leading-5 text-slate-400">{card.results[card.results.length - 1].summary}</p>{/if}
                <div class="mt-4 flex items-center justify-between gap-2 text-[11px] text-slate-600"><span><time>{formatDate(card.task.created_at)}</time>{#if card.binding?.profile?.delivery}<span class="ml-2 text-slate-500">{card.binding.profile.delivery}</span>{/if}</span>{#if card.binding}<button class="text-indigo-300 hover:text-indigo-200" on:click={() => openWorker(card.binding.worker_ref)}>Observe</button>{/if}</div>
              </article>
            {/each}
          </div>
        </aside>
      </div>

      <section class="fixed inset-x-0 bottom-0 z-20 border-t border-white/10 bg-[#090d18]/95 px-4 py-3 backdrop-blur-xl sm:px-6 lg:px-8">{#if messageStatus !== 'idle'}<div class="mx-auto mb-2 flex max-w-7xl items-center gap-2 text-xs" role="status" aria-live="polite">{#if messageStatus === 'sending' || messageStatus === 'working'}<span class="h-3 w-3 animate-spin rounded-full border-2 border-indigo-200/30 border-t-indigo-200"></span><span class="text-indigo-200">{messageStatus === 'sending' ? 'Sending...' : 'Message received. Secretary is working...'}</span>{:else}<span class="text-rose-300">Could not send the message.</span>{/if}</div>{/if}<form class="mx-auto flex w-full max-w-7xl items-end gap-2" on:submit|preventDefault={sendMessage}><label class="sr-only" for="message">Message Secretary</label><textarea id="message" bind:value={message} on:keydown={handleMessageKeydown} rows="1" class="max-h-32 min-h-12 flex-1 resize-y rounded-2xl border border-white/10 bg-white/[0.06] px-4 py-3 text-sm leading-6 text-slate-100 outline-none placeholder:text-slate-600 focus:ring-2 focus:ring-indigo-300" placeholder="Enter to send · Shift+Enter for a new line"></textarea><button class="rounded-2xl bg-indigo-400 px-5 py-3 font-medium text-slate-950 hover:bg-indigo-300" type="submit">Send</button></form>{#if messageError}<p class="mx-auto mt-2 max-w-7xl text-xs text-rose-300">{messageError}</p>{/if}</section>

      {#if observer}
        <div class="fixed inset-0 z-30 bg-black/60 lg:hidden" role="presentation" on:click={closeObserver}></div>
        <section class="fixed inset-0 z-40 h-full max-h-none overflow-y-auto rounded-none border border-white/10 bg-[#111827] p-5 shadow-2xl shadow-black/50 lg:absolute lg:inset-auto lg:right-6 lg:top-24 lg:block lg:h-auto lg:max-h-[calc(100vh-8rem)] lg:w-[22rem] lg:rounded-3xl" aria-label="Worker observer">
          <div class="flex items-start justify-between gap-3"><div><p class="text-xs uppercase tracking-[0.16em] text-indigo-300">Worker observer</p><h2 class="mt-1 break-all text-sm font-medium text-slate-200">{observer.workerRef}</h2></div><button class="rounded-xl px-2 py-1 text-slate-400 hover:bg-white/10 hover:text-white" aria-label="Close observer" on:click={closeObserver}>×</button></div>
          {#if observer.loading}<p class="mt-8 text-sm text-slate-500">Loading thread...</p>{:else}
            <div class="mt-5 flex items-center justify-between"><span class="rounded-full border px-2 py-1 text-[10px] uppercase tracking-wide {statusTone(observer.status?.state)}">{observer.status?.state}</span><button class="text-xs text-rose-300 hover:text-rose-200" on:click={stopWorker}>Stop</button></div>
            <div class="mt-4 rounded-2xl bg-black/25 p-3 text-xs leading-5 text-slate-400"><p class="whitespace-pre-wrap">{observer.thread?.task?.text}</p>{#if observer.thread?.binding?.profile}<div class="mt-3 grid grid-cols-2 gap-2 text-[11px] text-slate-500"><span>Profile: {observer.thread.binding.profile.name}</span><span>Delivery: {observer.thread.binding.profile.delivery}</span><span>Runtime: {observer.thread.binding.profile.runtime}</span><span>Model ID / alias: {observer.thread.binding.profile.model}</span><span>Tools: {observer.thread.binding.profile.tools || 'none'}</span><span>Reasoning effort: {observer.thread.binding.profile.reasoning || 'default'}</span></div>{/if}{#each observer.thread?.attempts || [] as attempt}<div class="mt-2 text-slate-500">Attempt {attempt.number}: {attempt.state}</div>{/each}{#each observer.thread?.results || [] as result}<div class="mt-3 border-t border-white/5 pt-2 text-slate-400"><span class="text-slate-500">{result.status}</span> · {result.summary}</div>{/each}{#if observer.thread?.children?.length}<div class="mt-3 border-t border-white/5 pt-2"><div class="text-cyan-300">{observer.thread.children.length} child task(s)</div>{#each observer.thread.children as child}<div class="mt-2 rounded-xl bg-white/[0.03] p-2 text-[11px] text-slate-400"><div class="flex justify-between gap-2"><span class="line-clamp-2">{child.task.text}</span><span class="shrink-0 text-slate-500">{statusFor(child)}</span></div>{#if child.results?.length}<p class="mt-1 line-clamp-2 text-slate-500">{child.results[child.results.length - 1].summary}</p>{/if}</div>{/each}</div>{/if}</div>
            <div class="mt-4 max-h-52 overflow-y-auto rounded-2xl bg-black/35 p-3 font-mono text-[11px] leading-5 text-emerald-200" aria-live="polite">{#if activityLines.length === 0}<span class="text-slate-600">No live activity yet.</span>{/if}{#each activityLines as item}<div>{item.kind}: {item.text}</div>{/each}</div>
            <form class="mt-4 flex gap-2" on:submit|preventDefault={steer}><input bind:value={steerText} class="min-w-0 flex-1 rounded-xl border border-white/10 bg-white/[0.06] px-3 py-2 text-xs text-slate-100 outline-none focus:ring-2 focus:ring-indigo-300" placeholder="Steer active Worker" /><button class="rounded-xl bg-indigo-400 px-3 py-2 text-xs font-medium text-slate-950">Steer</button></form>
            <form class="mt-2 flex gap-2" on:submit|preventDefault={queueFollowUp}><input bind:value={queueText} class="min-w-0 flex-1 rounded-xl border border-white/10 bg-white/[0.06] px-3 py-2 text-xs text-slate-100 outline-none focus:ring-2 focus:ring-indigo-300" placeholder="Queue follow-up" /><button class="rounded-xl border border-white/10 px-3 py-2 text-xs text-slate-300 hover:bg-white/10">Queue</button></form>
            {#if observerError}<p class="mt-3 text-xs text-rose-300">{observerError}</p>{/if}
          {/if}
        </section>
      {/if}
    </div>
  </main>
{/if}

{#if onboardingOpen && authenticated}
  <div class="fixed inset-0 z-50 grid place-items-center bg-black/75 px-4 py-6 backdrop-blur-sm">
    <div class="w-full max-w-lg rounded-3xl border border-white/10 bg-[#111827] p-6 shadow-2xl shadow-black/50 sm:p-8" role="dialog" aria-modal="true" aria-labelledby="onboarding-title" tabindex="-1">
      <div class="flex items-center justify-between"><span class="text-xs font-medium tracking-[0.2em] text-indigo-300 uppercase">Getting started · {onboardingStep + 1}/4</span><div class="flex gap-1">{#each [0, 1, 2, 3] as step}<span class="h-1.5 w-6 rounded-full {step <= onboardingStep ? 'bg-indigo-300' : 'bg-white/10'}"></span>{/each}</div></div>
      <h2 id="onboarding-title" class="mt-8 text-2xl font-semibold tracking-tight">{onboardingTitle}</h2>
      {#if onboardingStep === 0}<p class="mt-4 leading-7 text-slate-400">Secretary keeps the conversation on this machine. You decide what to ask, and nothing leaves this server unless your runtime does it.</p>{/if}
      {#if onboardingStep === 1}<div class="mt-5 space-y-3 text-sm leading-6 text-slate-300"><p><span class="text-indigo-300">Secretary</span> stays available for the thread.</p><p><span class="text-emerald-300">Workers</span> handle delegated tasks and report back here.</p><p>Use the observer to watch activity, steer a running Worker, or queue a follow-up.</p></div>{/if}
      {#if onboardingStep === 2}<p class="mt-4 text-sm leading-6 text-slate-400">Choose an alias. Its configured model value is shown below and saved locally in the server.</p><div class="mt-5 grid gap-2 sm:grid-cols-2">{#each modelEntries as [key, value]}<button class="rounded-2xl border p-4 text-left {selectedModel === key ? 'border-indigo-300 bg-indigo-300/10' : 'border-white/10 bg-white/[0.03]'}" on:click={() => { selectedModel = key; changeModel(); }}><span class="block text-sm font-medium text-slate-200">{key}</span><span class="mt-1 block truncate text-xs text-slate-500">configured: {value}</span></button>{/each}</div>{/if}
      {#if onboardingStep === 3}<p class="mt-4 leading-7 text-slate-400">That is the whole setup. Open the composer and give Secretary a small task to start.</p>{/if}
      <div class="mt-8 flex justify-end gap-3"><button class="rounded-2xl px-4 py-3 text-sm text-slate-500 hover:text-slate-300" on:click={finishOnboarding}>Skip</button><button class="rounded-2xl bg-indigo-400 px-5 py-3 text-sm font-medium text-slate-950 hover:bg-indigo-300" on:click={nextOnboarding}>{onboardingStep === 3 ? 'Open conversation' : 'Continue'}</button></div>
    </div>
  </div>
{/if}
