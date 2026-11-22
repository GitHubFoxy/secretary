<script>
  import { onMount } from 'svelte';
  import { createConnection } from './connection.js';
  import { createInterruptFeedback } from './interrupt-feedback.js';
  import { createUpdateChecker } from './updates.js';
  import { keyboardInset } from './viewport.js';
  import TopBar from './components/TopBar.svelte';
  import SessionDrawer from './components/SessionDrawer.svelte';
  import SettingsPanel from './components/SettingsPanel.svelte';
  import MessageList from './components/MessageList.svelte';
  import Composer from './components/Composer.svelte';
  import Modal from './components/Modal.svelte';
  import Icon from './components/Icon.svelte';

  let cached;
  try { cached = JSON.parse(localStorage.getItem('last-chat')); } catch {}
  let historyLoaded = false, historyHasMore = false, historyLoadingOlder = false, historyVersion = 0, historyStartAtTop = false, historyToken = 0;
  let sessions = cached?.sessions ?? [], history = cached?.history ?? [], queue = [], selected = cached?.selected ?? null, message = '', error = '', harness = 'claude', model = 'opus', reasoning = null, connected = false;
  if (cached) { historyLoaded = true; historyHasMore = cached.hasMore; }
  let drawerOpen = false, settingsOpen = false, renameOpen = false, newName = '', images = [], sending = false;
  let profileOpen = false, agents = '', agentsLoaded = false, agentsStatus = '';
  let agentsOriginal = '', agentsSaving = false;
  let interruptStates = {};
  const interruptFeedback = createInterruptFeedback({onChange: (id, state) => { interruptStates = {...interruptStates, [id]:state}; }});
  const updates = createUpdateChecker({
    currentVersion: __SECRETARY_BUILD_ID__,
    fetchVersion: async () => {
      const response = await fetch('/build-version.json', {cache:'no-store'});
      if (response.status === 401) { localStorage.removeItem('last-chat'); location.assign('/?login'); }
      if (!response.ok) throw new Error('Version unavailable');
      return (await response.json()).version;
    },
    isBusy: () => Boolean(message || images.length || sending || Object.values(interruptStates).some(state => state?.pending) || renameOpen || settingsOpen || agentsSaving || (profileOpen && (!agentsLoaded || agents !== agentsOriginal))),
    reload: () => location.replace('/?update'),
  });
  const connection = createConnection({
    url: `${location.protocol === 'https:' ? 'wss' : 'ws'}://${location.host}/ws`,
    onOpen: () => { connected = true; if (selected !== null) loadHistory(selected, false, historyLoaded); updates.check(); },
    onClose: () => { connected = false; interruptFeedback.disconnect(); },
    onMessage: handleMessage,
  });
  // Commands whose only visible result is a broadcast (sessions, message) or an error.
  const command = value => connection.request(value).catch(err => { error = err.message; });
  async function loadHistory(id, all = false, sync = false) {
    const token = ++historyToken;
    historyLoadingOlder = all;
    try {
      const after = sync ? (history.at(-1)?.cursor ?? 0) : undefined;
      const page = await connection.request({type:'read', id, all, after});
      if (token !== historyToken) return;
      const merged = new Map((sync ? history : []).concat(page.messages, history.filter(m => m.cursor > (page.messages.at(-1)?.cursor ?? after ?? Infinity))).map(m => [m.cursor, m]));
      history = [...merged.values()].sort((a, b) => a.cursor - b.cursor); queue = page.queue;
      if (!sync) { historyHasMore = page.hasMore; historyStartAtTop = all; historyVersion++; }
      historyLoaded = true;
    } catch (err) { if (token === historyToken && !err.disconnected) error = err.message; }
    finally { if (token === historyToken) historyLoadingOlder = false; }
  }
  function select(id) { if (selected !== id) { history = []; queue = []; } selected = id; historyLoaded = false; historyHasMore = false; drawerOpen = false; loadHistory(id); }
  function handleMessage(msg) {
    if (msg.type === 'sessions') { sessions = msg.sessions; if (!sessions.some(s => s.id === selected) && sessions.length) { message = ''; settingsOpen = false; select((sessions.find(s => s.secretary) ?? sessions[0]).id); } }
    if (msg.type === 'message' && msg.sessionId === selected && !history.some(m => m.cursor === msg.message.cursor)) history = [...history, msg.message];
    if (msg.type === 'queue' && msg.sessionId === selected) queue = msg.items;
    if (msg.type === 'interrupt_result') interruptFeedback.result(msg.sessionId, msg.requestId, msg.outcome);
    if (msg.type === 'error') error = msg.message;
  }
  async function submit(immediate = false) {
    if (!connected || sending || (!message.trim() && !images.length)) return;
    const sent = {text:message, images};
    sending = true;
    if (!interruptStates[selected]?.pending) interruptFeedback.clear(selected);
    try {
      await connection.request({type:'send', id:selected, text:sent.text, images:sent.images, immediate});
      if (message === sent.text && images === sent.images) { message = ''; images = []; }
    } catch (err) {
      error = err.disconnected ? 'Не удалось подтвердить доставку сообщения. Проверьте историю перед повторной отправкой.' : `Не удалось доставить сообщение: ${err.message}`;
    } finally { sending = false; }
  }
  // The settings form edits a copy of the selected session's harness, model and reasoning.
  function loadSettings() { if (!current) return; harness = current.harness; model = current.model; reasoning = current.reasoning ?? null; }
  function saveSettings() {
    if (!current) return;
    if (current.harness !== harness && !confirm('Текущий контекст будет потерян. Продолжить?')) { loadSettings(); return; }
    command({type:'settings', id:selected, harness, model, reasoning}); settingsOpen = false;
  }
  function stop() { command({type:'stop', id:selected}); }
  async function interrupt() {
    if (!connected || interruptStates[selected]?.pending) return;
    const id = selected, requestId = crypto.randomUUID();
    interruptFeedback.start(id, requestId);
    try { interruptFeedback.ack(id, requestId, (await connection.request({type:'interrupt', id, requestId})).accepted); }
    catch (err) { if (!err.disconnected) interruptFeedback.fail(id, requestId, err.message); }
  }
  function setHidden(session, hidden) { if (connected) { error = ''; command({type:'visibility', id:session.id, hidden}); } }
  function openSettings() { loadSettings(); settingsOpen = true; }
  function openRename() { newName = title; settingsOpen = false; renameOpen = true; }
  function rename() { if (!connected || !newName.trim()) return; error = ''; command({type:'rename', id:selected, name:newName}); renameOpen = false; }
  // A new chat in the same folder with the saved settings of the current one.
  async function createThread() {
    if (!current) return;
    try {
      const {id} = await connection.request({type:'create', harness:current.harness, model:current.model, reasoning:current.reasoning ?? null, folder:current.folder});
      settingsOpen = false; select(id);
    } catch (err) { error = err.message; }
  }
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
    ...queue.map(item => ({kind:'queued', id:item.id, text:item.text, images:item.images, urgent:item.urgent, key:`${selected}:queue:${item.id}`})),
  ];
  $: if (historyLoaded && selected !== null) {
    try { localStorage.setItem('last-chat', JSON.stringify({selected, sessions, history:history.slice(-10), hasMore:historyHasMore || history.length > 10})); } catch { localStorage.removeItem('last-chat'); }
  }
  $: current = sessions.find(s => s.id === selected);
  $: title = current?.name || (current?.secretary ? 'Secretary' : `Сессия #${selected}`);

  // Высота поля ввода и высота экранной клавиатуры (iOS не сжимает layout viewport, поэтому берём visualViewport).
  let composerHeight = 0, keyboard = 0, chatViewport;
  $: if (chatViewport) keyboard = keyboardInset({ viewport: window.visualViewport, container: chatViewport, activeElement: document.activeElement });
  onMount(() => {
    connection.connect();
    const reconnect = () => { if (document.visibilityState === 'visible') { connection.reconnect(); updates.check(); } };
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
      {title}
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
        <MessageList {messages} {sessions} {historyLoaded} {connected} {historyHasMore} {historyLoadingOlder} startAtTop={historyStartAtTop} onloadOlder={() => loadHistory(selected, true)} bottomInset={composerHeight + keyboard} onselect={select} onsendQueued={id => command({type:'sendQueued', id:selected, messageId:id})} />
      {/key}

      <!-- Поле ввода плавает поверх списка: сообщения прокручиваются под ним. -->
      <div bind:offsetHeight={composerHeight} style:bottom="{keyboard}px" class="pointer-events-none absolute inset-x-0 z-10 bg-linear-to-t from-neutral-950 from-60% to-transparent pt-6">
        <div class="pointer-events-auto">
          {#if interruptStates[selected]?.message}<p role="status" aria-live="polite" class="mx-auto max-w-3xl px-4 text-sm text-neutral-400">{interruptStates[selected].message}</p>{/if}
          <Composer bind:value={message} bind:images disabled={!connected || sending} stopDisabled={!connected || !!interruptStates[selected]?.pending} running={current?.status === 'running'} onsubmit={submit} oninterrupt={interrupt} />
        </div>
      </div>
    </div>

    <SessionDrawer open={drawerOpen} {sessions} {selected} {connected} onhide={s => setHidden(s, true)} onrestore={s => setHidden(s, false)} onselect={select} onclose={() => (drawerOpen = false)} onprofile={openProfile} />
  </div>
{/if}
