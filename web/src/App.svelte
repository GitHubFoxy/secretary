<script>
  import { onMount } from 'svelte';
  import { createConnection } from './connection.js';
  import { createUpdateChecker } from './updates.js';
  import { keyboardInset } from './viewport.js';
  import { visibleMessage, userMessage, queuedMessage, keyedMessages } from './messages.js';
  import TopBar from './components/TopBar.svelte';
  import SessionDrawer from './components/SessionDrawer.svelte';
  import SettingsPanel from './components/SettingsPanel.svelte';
  import MessageList from './components/MessageList.svelte';
  import Composer from './components/Composer.svelte';
  import Modal from './components/Modal.svelte';
  import Icon from './components/Icon.svelte';

  let settingsLoaded = false, historyLoaded = false;
  let sessions = [], events = [], queue = [], selected = 1, message = '', error = '', harness = 'claude', model = 'opus', reasoning = null, connected = false;
  let drawerOpen = false, settingsOpen = false, renameOpen = false, newName = '', images = [], pendingSend = null;
  let profileOpen = false, agents = '', agentsLoaded = false, agentsStatus = '';
  let agentsOriginal = '', agentsSaving = false;
  const updates = createUpdateChecker({
    currentVersion: __SECRETARY_BUILD_ID__,
    fetchVersion: async () => {
      const response = await fetch('/build-version.json', {cache:'no-store'});
      if (!response.ok) throw new Error('Version unavailable');
      return (await response.json()).version;
    },
    isBusy: () => Boolean(message || images.length || pendingSend || renameOpen || settingsOpen || agentsSaving || (profileOpen && (!agentsLoaded || agents !== agentsOriginal))),
    reload: () => location.reload(),
  });
  const connection = createConnection({
    url: `${location.protocol === 'https:' ? 'wss' : 'ws'}://${location.host}/ws`,
    onOpen: () => { connected = true; select(selected); updates.check(); },
    onClose: () => { connected = false; if (pendingSend) { pendingSend = null; error = 'Соединение прервалось до подтверждения отправки. Проверьте историю перед повторной отправкой.'; } },
    onMessage: handleMessage,
  });
  const send = value => connection.send(value);
  const refresh = () => send({type:'list'});
  function select(id) { if (selected !== id) { events = []; queue = []; } selected = id; historyLoaded = false; drawerOpen = false; send({type:'read', id}); const s = sessions.find(s => s.id === id); if (s) { harness = s.harness; model = s.model; reasoning = s.reasoning ?? null; settingsLoaded = true; } }
  function handleMessage(e) {
    const msg = JSON.parse(e.data);
    if (msg.type === 'sessions') { sessions = msg.sessions; if (!sessions.some(s => s.id === selected) && sessions.length) { message = ''; settingsOpen = false; select(sessions[0].id); } const s = sessions.find(s => s.id === selected); if (s && !settingsLoaded) { harness = s.harness; model = s.model; reasoning = s.reasoning ?? null; settingsLoaded = true; } }
    if (msg.type === 'history' && msg.id === selected) { events = msg.events; queue = msg.queue || []; historyLoaded = true; }
    // Очередь приходит отдельными событиями и в историю не попадает: заменяем список целиком.
    if (msg.type === 'event' && msg.sessionId === selected && msg.event.type === 'queue') queue = msg.event.items;
    else if (msg.type === 'event') { if (msg.sessionId === selected) events = [...events, msg.event]; refresh(); }
    if (msg.type === 'created') { select(msg.id); settingsOpen = false; }
    if (msg.type === 'sent' && pendingSend?.id === msg.requestId) { if (message === pendingSend.text && images === pendingSend.images) { message = ''; images = []; } pendingSend = null; }
    if (msg.type === 'error') { error = msg.message; if (pendingSend?.id === msg.requestId) pendingSend = null; }
  }
  function submit(immediate = false) { if (!connected || pendingSend || (!message.trim() && !images.length)) return; const id = crypto.randomUUID(); pendingSend = {id, text:message, images}; send({type:'send',id:selected,text:message,images,immediate,requestId:id}); }
  function saveSettings() {
    const s = sessions.find(s => s.id === selected);
    if (!s) return;
    if (s.harness !== harness && !confirm('Текущий контекст будет потерян. Продолжить?')) { harness = s.harness; model = s.model; reasoning = s.reasoning ?? null; return; }
    send({type:'settings', id:selected, harness, model, reasoning}); settingsOpen = false;
  }
  function stop() { send({type:'stop', id:selected}); }
  function interrupt() { send({type:'interrupt', id:selected}); }
  function removeSession(session) {
    if (!connected || session.id === 1) return;
    if (!confirm(`Удалить сессию #${session.id} и её историю? Работающий агент будет остановлен. Дочерние сессии сохранятся.`)) return;
    error = '';
    send({type:'delete', id:session.id});
  }
  function openSettings() { const s = sessions.find(s => s.id === selected); if (s) { harness = s.harness; model = s.model; reasoning = s.reasoning ?? null; } settingsOpen = true; }
  function openRename() { newName = current?.name || (selected === 1 ? 'Secretary' : `Сессия #${selected}`); settingsOpen = false; renameOpen = true; }
  function rename() { if (!connected || !newName.trim()) return; error = ''; send({type:'rename', id:selected, name:newName}); renameOpen = false; }
  function createThread() { const folder = sessions.find(s => s.id === selected)?.folder; if (!folder) return; send({type:'create', harness, model, folder}); }
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
  function text(event) {
    if (event.type === 'user' && typeof event.message?.content === 'string') return {...userMessage(event.message.content), images:event.images || []};
    if (event.type === 'result') return {role:'Агент', text:event.result};
    if (event.method === 'item/completed' && event.params.item.type === 'agentMessage') return {role:'Агент', text:event.params.item.text};
    if (event.type === 'error') return {role:'Ошибка', text:event.message};
    if (event.type === 'cancelled') return {role:'Не отправлено', text:event.text, images:event.images || []};
    if (event.type === 'delivery_uncertain') return {role:'Доставка неизвестна', text:event.text, images:event.images || []};
    return null;
  }
  // Сначала история, затем очередь: сообщения из очереди ещё не приняты агентом и стоят после уже отправленных.
  $: messages = keyedMessages(events, queue, selected, text);
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
    return () => { clearInterval(updateTimer); updates.stop(); connection.stop(); window.removeEventListener('online', reconnect); document.removeEventListener('visibilitychange', reconnect); window.removeEventListener('resize', update); document.removeEventListener('focusin', update); document.removeEventListener('focusout', update); vv?.removeEventListener('resize', update); vv?.removeEventListener('scroll', update); };
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
      <MessageList {messages} {sessions} {historyLoaded} {connected} bottomInset={composerHeight + keyboard} onselect={select} />

      <!-- Поле ввода плавает поверх списка: сообщения прокручиваются под ним. -->
      <div bind:offsetHeight={composerHeight} style:bottom="{keyboard}px" class="pointer-events-none absolute inset-x-0 z-10 bg-linear-to-t from-neutral-950 from-60% to-transparent pt-6">
        <div class="pointer-events-auto">
          <Composer bind:value={message} bind:images disabled={!connected || !!pendingSend} stopDisabled={!connected} running={current?.status === 'running'} onsubmit={submit} oninterrupt={interrupt} />
        </div>
      </div>
    </div>

    <SessionDrawer open={drawerOpen} {sessions} {selected} {connected} onremove={removeSession} onselect={select} onclose={() => (drawerOpen = false)} onprofile={openProfile} />
  </div>
{/if}
