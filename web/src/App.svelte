<script>
  import { visibleMessage } from './messages.js';
  import TopBar from './components/TopBar.svelte';
  import SessionDrawer from './components/SessionDrawer.svelte';
  import SettingsPanel from './components/SettingsPanel.svelte';
  import MessageList from './components/MessageList.svelte';
  import Composer from './components/Composer.svelte';
  import Modal from './components/Modal.svelte';

  let settingsLoaded = false;
  let sessions = [], events = [], selected = 1, message = '', error = '', models = [], harness = 'claude', model = 'opus', connected = false;
  let drawerOpen = false, settingsOpen = false, folder = '';
  const socket = new WebSocket(`${location.protocol === 'https:' ? 'wss' : 'ws'}://${location.host}/ws`);
  const send = value => socket.send(JSON.stringify(value));
  const refresh = () => send({type:'list'});
  function select(id) { selected = id; events = []; drawerOpen = false; send({type:'read', id}); const s = sessions.find(s => s.id === id); if (s) { harness = s.harness; model = s.model; settingsLoaded = true; } }
  socket.onopen = () => { connected = true; select(selected); };
  socket.onclose = () => { connected = false; };
  socket.onmessage = e => {
    const msg = JSON.parse(e.data);
    if (msg.type === 'sessions') { sessions = msg.sessions; const s = sessions.find(s => s.id === selected); if (s && !settingsLoaded) { harness = s.harness; model = s.model; settingsLoaded = true; } }
    if (msg.type === 'history' && msg.id === selected) events = msg.events;
    if (msg.type === 'event') { if (msg.sessionId === selected) events = [...events, msg.event]; refresh(); }
    if (msg.type === 'created') { select(msg.id); settingsOpen = false; }
    if (msg.type === 'error') error = msg.message;
  };
  fetch('/models').then(r => r.json()).then(data => models = data).catch(() => {});
  function submit() { if (!message.trim()) return; send({type:'send',id:selected,text:message}); message = ''; }
  function saveSettings() {
    const s = sessions.find(s => s.id === selected);
    if (!s) return;
    if (s.harness !== harness && !confirm('Текущий контекст будет потерян. Продолжить?')) { harness = s.harness; model = s.model; return; }
    send({type:'settings', id:selected, harness, model}); refresh(); settingsOpen = false;
  }
  function stop() { send({type:'stop', id:selected}); }
  function openSettings() { folder = sessions.find(s => s.id === selected)?.folder ?? folder; settingsOpen = true; }
  function createThread() { if (!folder.trim()) return; send({type:'create', harness, model, folder:folder.trim()}); }
  function text(event) {
    if (event.type === 'user' && typeof event.message?.content === 'string') return {role:'Вы', text:event.message.content};
    if (event.type === 'result') return {role:'Агент', text:event.result};
    if (event.method === 'item/completed' && event.params.item.type === 'agentMessage') return {role:'Агент', text:event.params.item.text};
    if (event.type === 'error') return {role:'Ошибка', text:event.message};
    return null;
  }
  $: messages = events.map(text).filter(visibleMessage);
  $: current = sessions.find(s => s.id === selected);
</script>

<svelte:head><title>Secretary</title></svelte:head>

<div class="flex h-dvh flex-col bg-neutral-950 text-neutral-200">
  <TopBar
    title={`Сессия #${selected}`}
    status={current?.status}
    {connected}
    onmenu={() => (drawerOpen = true)}
    onsettings={openSettings}
  />

  <Modal open={settingsOpen} title="Настройки сессии" onclose={() => (settingsOpen = false)}>
    <SettingsPanel bind:harness bind:model bind:folder {models} onsave={saveSettings} onstop={stop} oncreate={createThread} />
  </Modal>

  {#if error}
    <p role="alert" class="border-b border-neutral-900 px-4 py-2 text-center text-sm text-red-400">{error}</p>
  {/if}

  <MessageList {messages} />

  <Composer bind:value={message} disabled={!connected} onsubmit={submit} />

  <SessionDrawer open={drawerOpen} {sessions} {selected} onselect={select} onclose={() => (drawerOpen = false)} />
</div>
