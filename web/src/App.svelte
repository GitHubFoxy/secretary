<script>
  import { onMount } from 'svelte';
  import { createConnection } from './connection.js';
  import { createInterruptFeedback, interruptSessionFor } from './interrupt-feedback.js';
  import { createUpdateChecker } from './updates.js';
  import { keyboardInset } from './viewport.js';
  import TopBar from './components/TopBar.svelte';
  import SessionDrawer from './components/SessionDrawer.svelte';
  import SettingsPanel from './components/SettingsPanel.svelte';
  import MessageList from './components/MessageList.svelte';
  import Composer from './components/Composer.svelte';
  import Modal from './components/Modal.svelte';
  import Icon from './components/Icon.svelte';

  let historyLoaded = false;
  let historyHasMore = false, historyLoadingOlder = false, historyRequestId = null, historyRequestAll = false, historyVersion = 0, historyStartAtTop = false;
  let sessions = [], history = [], queue = [], selected = 1, message = '', error = '', harness = 'claude', model = 'opus', reasoning = null, connected = false;
  let drawerOpen = false, settingsOpen = false, renameOpen = false, newName = '', images = [], pendingSend = null;
  let profileOpen = false, agents = '', agentsLoaded = false, agentsStatus = '';
  let agentsOriginal = '', agentsSaving = false;
  let interruptStates = {};
  const interruptFeedback = createInterruptFeedback({onChange: (id, state) => { interruptStates = {...interruptStates, [id]:state}; }});
  const updates = createUpdateChecker({
    currentVersion: __SECRETARY_BUILD_ID__,
    fetchVersion: async () => {
      const response = await fetch('/build-version.json', {cache:'no-store'});
      if (!response.ok) throw new Error('Version unavailable');
      return (await response.json()).version;
    },
    isBusy: () => Boolean(message || images.length || pendingSend || Object.values(interruptStates).some(state => state?.pending) || renameOpen || settingsOpen || agentsSaving || (profileOpen && (!agentsLoaded || agents !== agentsOriginal))),
    reload: () => location.reload(),
  });
  const connection = createConnection({
    url: `${location.protocol === 'https:' ? 'wss' : 'ws'}://${location.host}/ws`,
    onOpen: () => { connected = true; select(selected); updates.check(); },
    onClose: () => { connected = false; historyLoadingOlder = false; interruptFeedback.disconnect(); if (pendingSend) { pendingSend = null; error = 'Соединение прервалось до подтверждения отправки. Проверьте историю перед повторной отправкой.'; } },
    onMessage: handleMessage,
  });
  const send = value => connection.send(value);
  function requestHistory(id, all = false) { historyRequestId = crypto.randomUUID(); historyRequestAll = all; historyLoadingOlder = all; if (!send({type:'read', id, all, requestId:historyRequestId})) historyLoadingOlder = false; }
  function select(id) { if (selected !== id) { history = []; queue = []; } selected = id; historyLoaded = false; historyHasMore = false; drawerOpen = false; requestHistory(id); }
  function handleMessage(e) {
    const msg = JSON.parse(e.data);
    if (msg.type === 'sessions') { sessions = msg.sessions; if (!sessions.some(s => s.id === selected) && sessions.length) { message = ''; settingsOpen = false; select(sessions[0].id); } }
    if (msg.type === 'history' && msg.id === selected && msg.requestId === historyRequestId) { history = msg.messages; queue = msg.queue; historyLoaded = true; historyHasMore = msg.hasMore; historyLoadingOlder = false; historyStartAtTop = historyRequestAll; historyVersion++; }
    if (msg.type === 'message' && msg.sessionId === selected) history = [...history, msg.message];
    if (msg.type === 'queue' && msg.sessionId === selected) queue = msg.items;
    if (msg.type === 'created') { select(msg.id); settingsOpen = false; }
    if (msg.type === 'sent' && pendingSend?.id === msg.requestId) { if (message === pendingSend.text && images === pendingSend.images) { message = ''; images = []; } pendingSend = null; }
    if (msg.type === 'interrupt_ack') interruptFeedback.ack(msg.sessionId, msg.requestId, msg.accepted);
    if (msg.type === 'interrupt_result') interruptFeedback.result(msg.sessionId, msg.requestId, msg.outcome);
    if (msg.type === 'error') {
      if (msg.requestId === historyRequestId) historyLoadingOlder = false;
      const interrupted = interruptSessionFor(interruptStates, msg.requestId);
      if (interrupted !== null) interruptFeedback.fail(interrupted, msg.requestId, msg.message);
      else error = msg.message;
      if (pendingSend?.id === msg.requestId) pendingSend = null;
    }
  }
  function submit(immediate = false) { if (!connected || pendingSend || (!message.trim() && !images.length)) return; const id = crypto.randomUUID(); pendingSend = {id, text:message, images}; if (!interruptStates[selected]?.pending) interruptFeedback.clear(selected); send({type:'send',id:selected,text:message,images,immediate,requestId:id}); }
  // The settings form edits a copy of the selected session's harness, model and reasoning.
  function loadSettings() { if (!current) return; harness = current.harness; model = current.model; reasoning = current.reasoning ?? null; }
  function saveSettings() {
    if (!current) return;
    if (current.harness !== harness && !confirm('Текущий контекст будет потерян. Продолжить?')) { loadSettings(); return; }
    send({type:'settings', id:selected, harness, model, reasoning}); settingsOpen = false;
  }
  function stop() { send({type:'stop', id:selected}); }
  function interrupt() {
    if (!connected || interruptStates[selected]?.pending) return;
    const id = selected, requestId = crypto.randomUUID();
    interruptFeedback.start(id, requestId);
    try { if (!send({type:'interrupt', id, requestId})) interruptFeedback.fail(id, requestId, 'нет соединения'); }
    catch { interruptFeedback.fail(id, requestId, 'нет соединения'); }
  }
  function hideSession(session) {
    if (!connected) return;
    error = '';
    send({type:'visibility', id:session.id, hidden:true});
  }
  function restoreSession(session) { if (connected) { error = ''; send({type:'visibility', id:session.id, hidden:false}); } }
  function openSettings() { loadSettings(); settingsOpen = true; }
  function openRename() { newName = current?.name || (selected === 1 ? 'Secretary' : `Сессия #${selected}`); settingsOpen = false; renameOpen = true; }
  function rename() { if (!connected || !newName.trim()) return; error = ''; send({type:'rename', id:selected, name:newName}); renameOpen = false; }
  function createThread() { if (current) send({type:'create', harness, model, folder:current.folder}); }
  async function loadAgents() {
    agentsLoaded = false;
    agentsStatus = 'Загрузка…';
    try {
      const response = await fetch('/settings/agents');
      if (!response.ok) throw new Error(await response.text());
      agents = await response.text();
      agentsOriginal = agents;
      agentsLoaded = true;
      agentsStatus = '';
    } catch (err) {
      agentsStatus = `Не удалось загрузить: ${err.message || err}`;
    }
  }
  async function saveAgents() {
    agentsStatus = 'Сохранение…';
    agentsSaving = true;
    try {
      const saved = agents;
      const response = await fetch('/settings/agents', {method:'PUT', body: saved});
      if (!response.ok) throw new Error(await response.text());
      agentsOriginal = saved;
      agentsStatus = 'Сохранено';
    } catch (err) {
      agentsStatus = `Не удалось сохранить: ${err.message || err}`;
    } finally { agentsSaving = false; }
  }
  function openProfile() { drawerOpen = false; profileOpen = true; loadAgents(); }
  // Queued messages are not accepted by the agent yet, so they come after history.
  $: messages = [
    ...history.map((m, index) => ({...m, key:`${selected}:${index}`})),
    ...queue.map(item => ({kind:'queued', text:item.text, images:item.images, urgent:item.urgent, key:`${selected}:queue:${item.id}`})),
  ];
  $: current = sessions.find(s => s.id === selected);

  // Высота поля ввода и высота экранной клавиатуры (iOS не сжимает layout viewport, поэтому берём visualViewport).
  let composerHeight = 0, keyboard = 0, chatViewport;
  $: if (chatViewport) keyboard = keyboardInset({ viewport: window.visualViewport, container: chatViewport, activeElement: document.activeElement });
  onMount(() => {
    connection.connect();
    const reconnect = () => { if (document.visibilityState === 'visible') { connection.connect(); updates.check(); } };
    const updateTimer = setInterval(() => { if (document.visibilityState === 'visible') updates.check(); }, 5000);
    window.addEventListener('online', reconnect);
    document.addEventListener('visibilitychange', reconnect);
    const vv = window.visualViewport;
    const update = () => { keyboard = keyboardInset({ viewport: vv, container: chatViewport, activeElement: document.activeElement }); };
    if (vv) { vv.addEventListener('resize', update); vv.addEventListener('scroll', update); }
    window.addEventListener('resize', update);
    document.addEventListener('focusin', update);
    document.addEventListener('focusout', update);
    update();
    return () => { clearInterval(updateTimer); updates.stop(); interruptFeedback.dispose(); connection.stop(); window.removeEventListener('online', reconnect); document.removeEventListener('visibilitychange', reconnect); window.removeEventListener('resize', update); document.removeEventListener('focusin', update); document.removeEventListener('focusout', update); vv?.removeEventListener('resize', update); vv?.removeEventListener('scroll', update); };
  });
</script>

<svelte:head><title>Secretary</title></svelte:head>

{#if profileOpen}
  <div class="flex h-dvh flex-col bg-neutral-950 text-neutral-200">
    <header class="flex items-center gap-2 border-b border-neutral-900 px-2 py-2">
      <button class="icon-btn" aria-label="Назад" onclick={() => (profileOpen = false)}>
        <Icon name="back" />
      </button>
      <h1 class="min-w-0 flex-1 truncate px-1 text-sm font-medium text-neutral-100">Секретарь</h1>
    </header>

    <div class="flex min-h-0 flex-1 flex-col gap-4 p-4">
      <textarea
        class="field min-h-0 flex-1 resize-none font-mono"
        aria-label="Содержимое AGENTS.md"
        spellcheck="false"
        disabled={!agentsLoaded}
        bind:value={agents}
      ></textarea>
      <div class="flex items-center justify-end gap-3">
        <span role="status" class="text-xs text-neutral-500">{agentsStatus}</span>
        <button class="btn" onclick={saveAgents} disabled={!agentsLoaded}>Сохранить</button>
      </div>
    </div>
  </div>
{:else}
  <div class="flex h-dvh flex-col bg-neutral-950 text-neutral-200">
    <TopBar
      title={current?.name || (selected === 1 ? 'Secretary' : `Сессия #${selected}`)}
      status={current?.status}
      {connected}
      onmenu={() => (drawerOpen = true)}
      onsettings={openSettings}
    />

    <Modal open={renameOpen} title="Переименовать чат" onclose={() => (renameOpen = false)}>
      <form class="flex flex-col gap-4" onsubmit={e => { e.preventDefault(); rename(); }}>
        <label class="flex flex-col gap-2 text-sm">Название чата
          <input class="field" bind:value={newName} maxlength="100" required />
        </label>
        <div class="flex justify-end gap-2">
          <button type="button" class="btn-ghost" onclick={() => (renameOpen = false)}>Отмена</button>
          <button class="btn" disabled={!connected || !newName.trim()}>Сохранить</button>
        </div>
      </form>
    </Modal>

    <Modal open={settingsOpen} title="Настройки сессии" onclose={() => (settingsOpen = false)}>
      <SettingsPanel bind:harness bind:model bind:reasoning {connected} onsave={saveSettings} onstop={stop} oncreate={createThread} onrename={openRename} />
    </Modal>

    {#if error}
      <p role="alert" class="border-b border-neutral-900 px-4 py-2 text-center text-sm text-red-400">{error}</p>
    {/if}

    <div bind:this={chatViewport} class="relative flex min-h-0 flex-1 flex-col">
      {#key historyVersion}
        <MessageList {messages} {sessions} {historyLoaded} {connected} {historyHasMore} {historyLoadingOlder} startAtTop={historyStartAtTop} onloadOlder={() => requestHistory(selected, true)} bottomInset={composerHeight + keyboard} onselect={select} />
      {/key}

      <!-- Поле ввода плавает поверх списка: сообщения прокручиваются под ним. -->
      <div bind:offsetHeight={composerHeight} style:bottom="{keyboard}px" class="pointer-events-none absolute inset-x-0 z-10 bg-linear-to-t from-neutral-950 from-60% to-transparent pt-6">
        <div class="pointer-events-auto">
          {#if interruptStates[selected]?.message}<p role="status" aria-live="polite" class="mx-auto max-w-3xl px-4 text-sm text-neutral-400">{interruptStates[selected].message}</p>{/if}
          <Composer bind:value={message} bind:images disabled={!connected || !!pendingSend} stopDisabled={!connected || !!interruptStates[selected]?.pending} running={current?.status === 'running'} onsubmit={submit} oninterrupt={interrupt} />
        </div>
      </div>
    </div>

    <SessionDrawer open={drawerOpen} {sessions} {selected} {connected} onhide={hideSession} onrestore={restoreSession} onselect={select} onclose={() => (drawerOpen = false)} onprofile={openProfile} />
  </div>
{/if}
