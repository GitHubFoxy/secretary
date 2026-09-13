<script>
  import { onMount } from 'svelte';
  import { formatWorkerStatus, mergeSequenced, secretaryEventText, workerCard } from './ui-model.js';

  const ONBOARDING_KEY = 'secretary-onboarding-v1';
  let authenticated = false;
  let loading = true;
  let loginToken = '';
  let loginError = '';
  let entries = [];
  let workers = [];
  let projects = [];
  let nodes = [];
  let conversationID = '';
  let connection = 'offline';
  let modelOptions = {};
  let selectedModel = '';
  let modelError = '';
  let message = '';
  let messageError = '';
  let messageStatus = 'idle';
  let pendingMessageID = '';
  let messageRequestID = '';
  let messageRequestBody = '';
  let pendingActions = {};
  let userSaveKey = '';
  let secretaryTurnID = '';
  let secretaryStream = [];
  let secretaryStreamError = '';
  let userDocument = null;
  let userDraft = '';
  let userStatus = 'idle';
  let userError = '';
  let observer = null;
  let observerError = '';
  let activityLines = [];
  let activityCursor = 0;
  let steerText = '';
  let followUpText = '';
  let onboardingOpen = false;
  let onboardingStep = 0;
  let conversationSocket;
  let secretarySocket;
  let activitySocket;
  let reconnectTimer;
  let secretaryReconnectTimer;
  let activityReconnectTimer;
  let refreshTimer;

  $: workerCards = workers.map((worker) => workerCard(worker, projects, nodes));
  $: modelEntries = Object.entries(modelOptions);
  $: onboardingTitle = ['Welcome to Secretary', 'Your private workbench', 'Choose a default model', 'You are ready'][onboardingStep];
  $: currentWorker = observer?.details?.worker;
  $: currentTurn = observer?.details?.turns?.find((turn) => turn.id === currentWorker?.current_turn_id);

  async function request(path, options = {}) {
    const headers = new Headers(options.headers || {});
    const method = (options.method || 'GET').toUpperCase();
    if (method !== 'GET' && method !== 'HEAD' && !headers.has('Idempotency-Key')) {
      headers.set('Idempotency-Key', crypto.randomUUID());
    }
    const response = await fetch(path, { ...options, headers, credentials: 'same-origin' });
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

  function statusTone(status) {
    if (['succeeded', 'idle', 'saved'].includes(status)) return 'border-emerald-400/30 bg-emerald-400/10 text-emerald-200';
    if (['failed', 'blocked'].includes(status)) return 'border-rose-400/30 bg-rose-400/10 text-rose-200';
    if (['canceled', 'interrupted', 'closed', 'offline'].includes(status)) return 'border-slate-500/30 bg-slate-500/10 text-slate-300';
    return 'border-amber-400/30 bg-amber-400/10 text-amber-200';
  }

  function appendEntry(entry) {
    if (!entry) return;
    entries = mergeSequenced(entries, [entry]);
  }

  function appendSecretaryEvent(event) {
    if (!event) return;
    secretaryStream = mergeSequenced(secretaryStream, [event]);
    const kind = event.kind || '';
    if (kind === 'secretary.turn.queued') messageStatus = 'queued';
    if (kind === 'secretary.turn.started') messageStatus = 'working';
    if (kind === 'secretary.turn.finished') messageStatus = event.error ? 'error' : 'succeeded';
  }

  function appendActivity(event) {
    if (!event || (Number.isFinite(event.seq) && event.seq <= activityCursor)) return;
    if (Number.isFinite(event.seq)) activityCursor = event.seq;
    activityLines = mergeSequenced(activityLines, [event]);
  }

  async function loadState() {
    const state = await request('/v1/bootstrap');
    authenticated = true;
    conversationID = state.conversation_id;
    const [history, workerList, projectList, nodeList, document] = await Promise.all([
      request('/v1/conversation?after_seq=0'),
      request('/v1/workers'),
      request('/v1/projects'),
      request('/v1/nodes'),
      request('/v1/user'),
    ]);
    entries = history || [];
    workers = workerList || state.workers || [];
    projects = projectList || [];
    nodes = nodeList || [];
    userDocument = document;
    userDraft = document?.content || '';
    modelOptions = state.secretary?.models || {};
    selectedModel = state.secretary?.selected || '';
    onboardingOpen = !localStorage.getItem(ONBOARDING_KEY);
    loading = false;
    connectConversation();
    refreshTimer = setInterval(refreshState, 5000);
  }

  async function refreshState() {
    if (!authenticated) return;
    try {
      const [workerList, nodeList] = await Promise.all([request('/v1/workers'), request('/v1/nodes')]);
      workers = workerList || [];
      nodes = nodeList || [];
      if (observer?.workerRef) await refreshObserver(observer.workerRef);
    } catch (_) { /* durable state stays visible while a refresh is unavailable */ }
  }

  async function refreshObserver(workerRef) {
    try {
      const details = await request(`/v1/workers/${encodeURIComponent(workerRef)}`);
      if (observer?.workerRef === workerRef) observer = { ...observer, details };
    } catch (_) { /* observer reconnect will retry */ }
  }

  async function bootstrapSession(token) {
    await request('/v1/web/session', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ bootstrap_token: token }),
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
      } else await request('/v1/web/session');
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
    conversationSocket?.close();
    const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
    const lastSeq = entries.at(-1)?.seq || 0;
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

  async function connectSecretaryStream(turnID) {
    secretarySocket?.close();
    secretaryStreamError = '';
    secretaryTurnID = turnID;
    try {
      const replay = await request(`/v1/secretary/turns/${encodeURIComponent(turnID)}/stream?after_seq=0`);
      secretaryStream = mergeSequenced([], replay?.events || []);
      const cursor = secretaryStream.at(-1)?.seq || 0;
      const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
      secretarySocket = new WebSocket(`${protocol}//${location.host}/v1/secretary/turns/${encodeURIComponent(turnID)}/stream/ws?after_seq=${cursor}`);
      secretarySocket.onmessage = (event) => appendSecretaryEvent(JSON.parse(event.data));
      secretarySocket.onerror = () => secretarySocket.close();
      secretarySocket.onclose = () => {
        if (secretaryTurnID !== turnID) return;
        clearTimeout(secretaryReconnectTimer);
        secretaryReconnectTimer = setTimeout(() => connectSecretaryStream(turnID), 1200);
      };
    } catch (error) { secretaryStreamError = error.message; }
  }

  function handleMessageKeydown(event) {
    if (event.key !== 'Enter' || event.shiftKey || event.isComposing || event.repeat) return;
    event.preventDefault();
    void sendMessage();
  }

  async function sendMessage() {
    const body = message.trim();
    if (!body || pendingMessageID) return;
    if (messageRequestBody && messageRequestBody !== body) messageRequestID = '';
    messageRequestID ||= crypto.randomUUID();
    messageRequestBody = body;
    pendingMessageID = messageRequestID;
    messageStatus = 'accepted';
    messageError = '';
    try {
      const response = await request('/v1/messages', {
        method: 'POST', headers: { 'Content-Type': 'application/json', 'Idempotency-Key': pendingMessageID },
        body: JSON.stringify({ external_message_id: pendingMessageID, body }),
      });
      appendEntry(response.entry);
      message = '';
      messageRequestID = '';
      messageRequestBody = '';
      messageStatus = response.state || 'saved';
      if (response.turn_id) await connectSecretaryStream(response.turn_id);
      await refreshState();
    } catch (error) {
      messageStatus = 'error';
      messageError = error.message;
    } finally { pendingMessageID = ''; }
  }

  async function changeModel() {
    modelError = '';
    try {
      const state = await request('/v1/secretary/model', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ model: selectedModel }),
      });
      selectedModel = state.selected;
      modelOptions = state.models;
    } catch (error) { modelError = error.message; }
  }

  async function saveUserDocument() {
    if (!userDocument) return;
    userStatus = 'saving';
    userError = '';
    try {
      userSaveKey ||= crypto.randomUUID();
      const document = await request('/v1/user', {
        method: 'PUT', headers: { 'Content-Type': 'application/json', 'Idempotency-Key': userSaveKey },
        body: JSON.stringify({ content: userDraft, expected_revision: userDocument.revision }),
      });
      userDocument = document;
      userDraft = document.content;
      userSaveKey = '';
      userStatus = 'saved';
    } catch (error) {
      userStatus = 'error';
      userError = error.message;
      try { userDocument = await request('/v1/user'); } catch (_) { /* keep the draft and error visible */ }
    }
  }

  async function openWorker(workerRef) {
    if (!workerRef) return;
    observerError = '';
    activitySocket?.close();
    clearTimeout(activityReconnectTimer);
    observer = { workerRef, loading: true, details: null };
    activityLines = [];
    activityCursor = 0;
    try {
      const [details, replay] = await Promise.all([
        request(`/v1/workers/${encodeURIComponent(workerRef)}`),
        request(`/v1/workers/${encodeURIComponent(workerRef)}/activity?after_seq=0`),
      ]);
      activityLines = mergeSequenced([], replay || []);
      activityCursor = activityLines.at(-1)?.seq || 0;
      observer = { workerRef, details, loading: false };
      connectActivity(workerRef, activityCursor);
    } catch (error) {
      observer = null;
      observerError = error.message;
    }
  }

  function connectActivity(workerRef, afterSeq = activityCursor) {
    const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
    activitySocket?.close();
    activitySocket = new WebSocket(`${protocol}//${location.host}/v1/workers/${encodeURIComponent(workerRef)}/activity/ws?after_seq=${afterSeq}`);
    activitySocket.onmessage = (event) => appendActivity(JSON.parse(event.data));
    activitySocket.onerror = () => activitySocket.close();
    activitySocket.onclose = () => {
      if (observer?.workerRef !== workerRef) return;
      clearTimeout(activityReconnectTimer);
      activityReconnectTimer = setTimeout(() => connectActivity(workerRef, activityCursor), 1200);
    };
  }

  function closeObserver() {
    activitySocket?.close();
    activitySocket = null;
    observer = null;
    activityLines = [];
  }

  async function workerAction(action, text = '') {
    if (!observer?.workerRef || !text.trim() && action === 'message') return;
    observerError = '';
    const actionID = `${observer.workerRef}:${action}:${text}`;
    pendingActions[actionID] ||= crypto.randomUUID();
    try {
      const response = await request(`/v1/workers/${encodeURIComponent(observer.workerRef)}/${action}`, {
        method: 'POST', headers: { 'Content-Type': 'application/json', 'Idempotency-Key': pendingActions[actionID] },
        body: JSON.stringify({ text }),
      });
      if (response?.worker) observer = { ...observer, details: response };
      delete pendingActions[actionID];
      if (action === 'message') { steerText = ''; followUpText = ''; }
      await refreshState();
    } catch (error) { observerError = error.message; }
  }

  async function approve(approvalID) {
    if (!approvalID) return;
    observerError = '';
    const actionID = `approval:${approvalID}`;
    pendingActions[actionID] ||= crypto.randomUUID();
    try {
      const response = await request(`/v1/approvals/${encodeURIComponent(approvalID)}/approve`, {
        method: 'POST', headers: { 'Content-Type': 'application/json', 'Idempotency-Key': pendingActions[actionID] },
        body: JSON.stringify({}),
      });
      delete pendingActions[actionID];
      if (response?.worker) observer = { ...observer, details: response };
      await refreshState();
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

  onMount(() => {
    restoreSession();
    return () => {
      clearTimeout(reconnectTimer);
      clearTimeout(secretaryReconnectTimer);
      clearTimeout(activityReconnectTimer);
      clearInterval(refreshTimer);
      conversationSocket?.close();
      secretarySocket?.close();
      activitySocket?.close();
    };
  });
</script>

{#if loading}
  <main class="flex min-h-screen items-center justify-center bg-[#090d18] px-5 text-slate-300"><div class="text-sm tracking-[0.2em] uppercase">Loading Secretary</div></main>
{:else if !authenticated}
  <main class="flex min-h-screen items-center justify-center bg-[#090d18] px-5 py-10 text-slate-100">
    <section class="w-full max-w-md rounded-3xl border border-white/10 bg-white/[0.04] p-7 shadow-2xl shadow-black/30 sm:p-10">
      <div class="mb-8 flex items-center gap-3"><span class="grid h-10 w-10 place-items-center rounded-2xl bg-indigo-400/15 text-xl text-indigo-200">S</span><span class="text-sm font-medium tracking-[0.24em] text-slate-400 uppercase">Secretary</span></div>
      <h1 class="text-3xl font-semibold tracking-tight">Personal Conversation</h1><p class="mt-3 leading-7 text-slate-400">A local-first place to think, delegate work, and keep the thread.</p>
      <form class="mt-8 space-y-4" on:submit|preventDefault={login}><label class="block text-sm text-slate-300" for="bootstrap-token">Bootstrap token<input id="bootstrap-token" bind:value={loginToken} type="password" autocomplete="off" required class="mt-2 w-full rounded-2xl border border-white/10 bg-black/20 px-4 py-3 text-slate-100 outline-none ring-indigo-300 transition placeholder:text-slate-600 focus:ring-2" placeholder="Paste the token from secretaryd" /></label><button class="w-full rounded-2xl bg-indigo-400 px-4 py-3 font-medium text-slate-950" type="submit">Open conversation</button></form>
      {#if loginError}<p class="mt-4 rounded-xl bg-rose-400/10 p-3 text-sm text-rose-200">{loginError}</p>{/if}
    </section>
  </main>
{:else}
  <main class="min-h-screen bg-[#090d18] text-slate-100">
    <div class="mx-auto flex min-h-screen w-full max-w-7xl flex-col px-4 pb-32 pt-4 sm:px-6 lg:px-8">
      <header class="flex flex-wrap items-center justify-between gap-4 border-b border-white/10 pb-4"><div class="flex items-center gap-3"><span class="grid h-10 w-10 place-items-center rounded-2xl bg-indigo-400/15 font-semibold text-indigo-200">S</span><div><h1 class="font-semibold tracking-tight">Personal Conversation</h1><p class="text-xs text-slate-500">{conversationID}</p></div></div><div class="flex items-center gap-3 text-xs text-slate-400"><span class="flex items-center gap-2"><span class="h-2 w-2 rounded-full {connection === 'connected' ? 'bg-emerald-300' : 'bg-amber-300'}"></span>{connection}</span>{#if modelEntries.length}<label class="sr-only" for="model">Secretary model</label><select id="model" bind:value={selectedModel} on:change={changeModel} class="max-w-36 rounded-xl border border-white/10 bg-white/[0.05] px-3 py-2 text-xs text-slate-200"><option value="" disabled>Select model</option>{#each modelEntries as [key, value]}<option value={key}>{key} · {value}</option>{/each}</select>{/if}</div></header>
      {#if modelError}<p class="mt-3 text-right text-xs text-rose-300">{modelError}</p>{/if}

      <div class="mt-5 grid flex-1 gap-5 lg:grid-cols-[minmax(0,1fr)_24rem]">
        <section class="min-w-0">
          <div class="mb-3 flex items-center justify-between"><h2 class="text-sm font-medium text-slate-300">Conversation</h2><span class="text-xs text-slate-600">{entries.length} entries</span></div>
          <div class="space-y-3" aria-live="polite">
            {#if secretaryStream.length > 0}<section class="rounded-2xl border border-indigo-300/20 bg-indigo-300/[0.06] p-4" aria-label="Secretary live stream"><div class="mb-3 flex items-center justify-between text-[11px] uppercase tracking-[0.12em] text-indigo-300"><span>Secretary live stream</span><span>{formatWorkerStatus(messageStatus)}</span></div><div class="space-y-2 text-sm leading-6">{#each secretaryStream as event (event.id || event.seq)}<div class="flex gap-3"><span class="mt-2 h-1.5 w-1.5 shrink-0 rounded-full {event.kind === 'secretary.text_delta' ? 'bg-indigo-200' : 'bg-cyan-300'}"></span><div><span class="mr-2 text-[10px] uppercase tracking-wide text-slate-500">{event.kind.replace('secretary.', '')}</span><span class="whitespace-pre-wrap text-slate-200">{secretaryEventText(event)}</span></div></div>{/each}</div>{#if secretaryStreamError}<p class="mt-3 text-xs text-rose-300">{secretaryStreamError}</p>{/if}</section>{/if}
            {#if entries.length === 0}<div class="rounded-3xl border border-dashed border-white/10 p-8 text-center text-sm text-slate-500">Start with a question or a task.</div>{/if}
            {#each entries as entry (entry.id)}
              <article class="rounded-2xl border border-white/10 p-4 {entry.kind === 'user' ? 'ml-4 bg-indigo-400/[0.08] sm:ml-16' : entry.kind === 'worker_result' ? 'mr-4 bg-emerald-400/[0.06] sm:mr-16' : 'bg-white/[0.035]'}"><div class="mb-2 flex items-center justify-between gap-3 text-[11px] uppercase tracking-[0.12em] text-slate-500"><span>{entry.kind.replaceAll('_', ' ')}</span><time>{formatDate(entry.created_at)}</time></div><p class="whitespace-pre-wrap break-words leading-7 text-slate-200">{entry.body}</p></article>
            {/each}
          </div>
        </section>

        <aside class="min-w-0 space-y-5">
          <section><div class="mb-3 flex items-center justify-between"><h2 class="text-sm font-medium text-slate-300">Workers</h2><span class="text-xs text-slate-600">{workerCards.length}</span></div>{#if observerError && !observer}<p class="mb-3 rounded-xl bg-rose-400/10 p-3 text-xs text-rose-200">{observerError}</p>{/if}<div class="space-y-3">{#if workerCards.length === 0}<div class="rounded-2xl border border-dashed border-white/10 p-5 text-sm text-slate-500">Workers will appear here after Secretary accepts delegated work.</div>{/if}{#each workerCards as card (card.worker.worker_ref)}<article class="rounded-2xl border border-white/10 bg-white/[0.035] p-4"><div class="flex items-start justify-between gap-3"><h3 class="line-clamp-3 text-sm leading-6 text-slate-200">{card.worker.title || card.worker.intent}</h3><span class="shrink-0 rounded-full border px-2 py-1 text-[10px] uppercase tracking-wide {statusTone(card.status)}">{card.statusLabel}</span></div><div class="mt-3 grid grid-cols-2 gap-1 text-[11px] text-slate-500"><span>Project: {card.project}</span><span>Node: {card.node}</span><span class="col-span-2">HarnessInstance: {card.harness}</span></div>{#if card.nodeState !== 'online'}<p class="mt-2 text-xs {card.nodeState === 'blocked' ? 'text-rose-300' : 'text-amber-300'}">{card.nodeState === 'blocked' ? 'Blocked: binding is immutable' : 'Offline: waiting for the bound Node'}</p>{/if}<div class="mt-4 flex items-center justify-between gap-2 text-[11px] text-slate-600"><time>{formatDate(card.worker.created_at)}</time><button class="text-indigo-300 hover:text-indigo-200" on:click={() => openWorker(card.worker.worker_ref)}>Observe</button></div></article>{/each}</div></section>

          <section class="rounded-2xl border border-white/10 bg-white/[0.025] p-4"><div class="flex items-center justify-between"><h2 class="text-sm font-medium text-slate-300">user.md</h2><span class="text-[11px] text-slate-500">revision {userDocument?.revision || 0}</span></div><textarea bind:value={userDraft} rows="7" class="mt-3 w-full resize-y rounded-xl border border-white/10 bg-black/20 p-3 text-xs leading-5 text-slate-200 outline-none focus:ring-2 focus:ring-indigo-300" aria-label="External user.md"></textarea><div class="mt-3 flex items-center justify-between gap-3"><span class="text-xs {userStatus === 'error' ? 'text-rose-300' : 'text-slate-500'}">{userStatus === 'saved' ? 'Saved' : userStatus === 'saving' ? 'Saving...' : userStatus === 'error' ? userError : 'Changes apply to the next Secretary turn'}</span><button class="rounded-xl bg-indigo-400 px-3 py-2 text-xs font-medium text-slate-950" on:click={saveUserDocument}>Save</button></div></section>
        </aside>
      </div>

      <section class="fixed inset-x-0 bottom-0 z-20 border-t border-white/10 bg-[#090d18]/95 px-4 py-3 backdrop-blur-xl sm:px-6 lg:px-8">{#if messageStatus !== 'idle'}<div class="mx-auto mb-2 flex max-w-7xl items-center gap-2 text-xs" role="status" aria-live="polite"><span class="rounded-full border px-2 py-1 {statusTone(messageStatus)}">{formatWorkerStatus(messageStatus)}</span>{#if messageError}<span class="text-rose-300">{messageError}</span>{:else if secretaryTurnID}<span class="text-slate-500">turn {secretaryTurnID}</span>{/if}</div>{/if}<form class="mx-auto flex w-full max-w-7xl items-end gap-2" on:submit|preventDefault={sendMessage}><label class="sr-only" for="message">Message Secretary</label><textarea id="message" bind:value={message} on:keydown={handleMessageKeydown} rows="1" class="max-h-32 min-h-12 flex-1 resize-y rounded-2xl border border-white/10 bg-white/[0.06] px-4 py-3 text-sm leading-6 text-slate-100 outline-none placeholder:text-slate-600 focus:ring-2 focus:ring-indigo-300" placeholder="Enter to send · Shift+Enter for a new line"></textarea><button class="rounded-2xl bg-indigo-400 px-5 py-3 font-medium text-slate-950" type="submit">Send</button></form></section>

      {#if observer}<div class="fixed inset-0 z-30 bg-black/60 lg:hidden" role="presentation" on:click={closeObserver}></div><section class="fixed inset-0 z-40 h-full max-h-none overflow-y-auto rounded-none border border-white/10 bg-[#111827] p-5 shadow-2xl shadow-black/50 lg:absolute lg:inset-auto lg:right-6 lg:top-24 lg:block lg:h-auto lg:max-h-[calc(100vh-8rem)] lg:w-[30rem] lg:rounded-3xl" aria-label="Worker observer"><div class="flex items-start justify-between gap-3"><div><p class="text-xs uppercase tracking-[0.16em] text-indigo-300">Worker observer</p><h2 class="mt-1 break-all text-sm font-medium text-slate-200">{observer.workerRef}</h2></div><button class="rounded-xl px-2 py-1 text-slate-400 hover:bg-white/10 hover:text-white" aria-label="Close observer" on:click={closeObserver}>×</button></div>{#if observer.loading}<p class="mt-8 text-sm text-slate-500">Loading durable Worker state...</p>{:else if observer.details}<div class="mt-5 flex flex-wrap items-center gap-2"><span class="rounded-full border px-2 py-1 text-[10px] uppercase tracking-wide {statusTone(observer.details.worker.status)}">{formatWorkerStatus(observer.details.worker.status)}</span>{#if currentTurn}<span class="rounded-full border border-white/10 px-2 py-1 text-[10px] uppercase tracking-wide text-slate-400">Turn: {formatWorkerStatus(currentTurn.state)}</span>{/if}<button class="ml-auto text-xs text-rose-300 hover:text-rose-200" on:click={() => workerAction('cancel')}>Stop</button><button class="text-xs text-slate-300 hover:text-white" on:click={() => workerAction('close')}>Close</button></div><div class="mt-4 grid grid-cols-2 gap-2 rounded-2xl bg-black/25 p-3 text-[11px] text-slate-500"><span>Project: {observer.details.worker.project_id}</span><span>Node: {observer.details.worker.node_id}</span><span class="col-span-2">HarnessInstance: {observer.details.worker.harness_instance_id}</span></div>{#if observer.details.approvals?.length}<div class="mt-4 space-y-2">{#each observer.details.approvals as approval}<div class="rounded-xl border border-amber-300/20 bg-amber-300/[0.06] p-3 text-xs text-amber-100"><p>{approval.action_summary}</p><button class="mt-2 rounded-lg bg-amber-300 px-3 py-1.5 text-xs text-slate-950" on:click={() => approve(approval.id)}>Approve</button></div>{/each}</div>{/if}<div class="mt-4 max-h-64 overflow-y-auto rounded-2xl bg-black/35 p-3 font-mono text-[11px] leading-5 text-emerald-200" aria-live="polite">{#if activityLines.length === 0}<span class="text-slate-600">No live activity yet.</span>{/if}{#each activityLines as item (item.id || item.seq)}<div><span class="text-slate-500">{item.kind}</span> {secretaryEventText(item)}</div>{/each}</div><div class="mt-4 grid gap-2"><form class="flex gap-2" on:submit|preventDefault={() => workerAction('message', steerText)}><input bind:value={steerText} class="min-w-0 flex-1 rounded-xl border border-white/10 bg-white/[0.06] px-3 py-2 text-xs text-slate-100" placeholder="Message active Worker" /><button class="rounded-xl bg-indigo-400 px-3 py-2 text-xs font-medium text-slate-950">Message</button></form><form class="flex gap-2" on:submit|preventDefault={() => workerAction('message', followUpText)}><input bind:value={followUpText} class="min-w-0 flex-1 rounded-xl border border-white/10 bg-white/[0.06] px-3 py-2 text-xs text-slate-100" placeholder="Follow-up for this Worker" /><button class="rounded-xl border border-white/10 px-3 py-2 text-xs text-slate-300">Follow-up</button></form></div><details class="mt-4 rounded-xl border border-white/10 p-3 text-xs text-slate-500"><summary class="cursor-pointer text-slate-400">Diagnostics</summary><div class="mt-3 space-y-2">{#each observer.details.attempts || [] as attempt}<div>Attempt {attempt.number}: {formatWorkerStatus(attempt.state)}</div>{/each}{#each observer.details.outcomes || [] as outcome}<div>{outcome.status} · {outcome.classification}{#if outcome.error_message} · {outcome.error_message}{/if}</div>{/each}</div></details>{#if observerError}<p class="mt-3 text-xs text-rose-300">{observerError}</p>{/if}{/if}</section>{/if}
    </div>
  </main>
{/if}

{#if onboardingOpen && authenticated}<div class="fixed inset-0 z-50 grid place-items-center bg-black/75 px-4 py-6 backdrop-blur-sm"><div class="w-full max-w-lg rounded-3xl border border-white/10 bg-[#111827] p-6 shadow-2xl sm:p-8" role="dialog" aria-modal="true" aria-labelledby="onboarding-title"><div class="flex items-center justify-between"><span class="text-xs font-medium tracking-[0.2em] text-indigo-300 uppercase">Getting started · {onboardingStep + 1}/4</span><div class="flex gap-1">{#each [0, 1, 2, 3] as step}<span class="h-1.5 w-6 rounded-full {step <= onboardingStep ? 'bg-indigo-300' : 'bg-white/10'}"></span>{/each}</div></div><h2 id="onboarding-title" class="mt-8 text-2xl font-semibold tracking-tight">{onboardingTitle}</h2>{#if onboardingStep === 0}<p class="mt-4 leading-7 text-slate-400">Secretary keeps the conversation and durable Worker state on this server.</p>{:else if onboardingStep === 1}<div class="mt-5 space-y-3 text-sm leading-6 text-slate-300"><p><span class="text-indigo-300">Secretary</span> owns intent and shows its live stream.</p><p><span class="text-emerald-300">Workers</span> execute on their immutable Project and Node binding.</p></div>{:else if onboardingStep === 2}<p class="mt-4 text-sm leading-6 text-slate-400">Choose the Secretary model. Its configured value is shown below.</p><div class="mt-5 grid gap-2 sm:grid-cols-2">{#each modelEntries as [key, value]}<button class="rounded-2xl border p-4 text-left {selectedModel === key ? 'border-indigo-300 bg-indigo-300/10' : 'border-white/10 bg-white/[0.03]'}" on:click={() => { selectedModel = key; changeModel(); }}><span class="block text-sm font-medium text-slate-200">{key}</span><span class="mt-1 block truncate text-xs text-slate-500">configured: {value}</span></button>{/each}</div>{:else}<p class="mt-4 leading-7 text-slate-400">Use Enter to send. Use Shift+Enter for a new line.</p>{/if}<div class="mt-8 flex justify-end gap-3"><button class="rounded-2xl px-4 py-3 text-sm text-slate-500" on:click={finishOnboarding}>Skip</button><button class="rounded-2xl bg-indigo-400 px-5 py-3 text-sm font-medium text-slate-950" on:click={nextOnboarding}>{onboardingStep === 3 ? 'Open conversation' : 'Continue'}</button></div></div></div>{/if}
