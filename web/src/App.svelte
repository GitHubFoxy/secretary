<script>
  let settingsLoaded = false;
  let sessions = [], events = [], selected = 1, message = '', error = '', models = [], harness = 'claude', model = 'opus', connected = false;
  const socket = new WebSocket(`${location.protocol === 'https:' ? 'wss' : 'ws'}://${location.host}/ws`);
  const send = value => socket.send(JSON.stringify(value));
  const refresh = () => send({type:'list'});
  function select(id) { selected = id; events = []; send({type:'read', id}); const s = sessions.find(s => s.id === id); if (s) { harness = s.harness; model = s.model; settingsLoaded = true; } }
  socket.onopen = () => { connected = true; select(selected); };
  socket.onclose = () => { connected = false; };
  socket.onmessage = e => {
    const msg = JSON.parse(e.data);
    if (msg.type === 'sessions') { sessions = msg.sessions; const s = sessions.find(s => s.id === selected); if (s && !settingsLoaded) { harness = s.harness; model = s.model; settingsLoaded = true; } }
    if (msg.type === 'history' && msg.id === selected) events = msg.events;
    if (msg.type === 'event') { if (msg.sessionId === selected) events = [...events, msg.event]; refresh(); }
    if (msg.type === 'error') error = msg.message;
  };
  fetch('/models').then(r => r.json()).then(data => models = data).catch(() => {});
  function submit(e) { e.preventDefault(); if (!message.trim()) return; send({type:'send',id:selected,text:message}); message = ''; }
  function saveSettings() {
    const s = sessions.find(s => s.id === selected);
    if (s.harness !== harness && !confirm('Текущий контекст будет потерян. Продолжить?')) { harness = s.harness; model = s.model; return; }
    send({type:'settings', id:selected, harness, model}); refresh();
  }
  function text(event) {
    if (event.type === 'user' && typeof event.message?.content === 'string') return {role:'Вы', text:event.message.content};
    if (event.type === 'result') return {role:'Агент', text:event.result};
    if (event.method === 'item/completed' && event.params.item.type === 'agentMessage') return {role:'Агент', text:event.params.item.text};
    if (event.type === 'error') return {role:'Ошибка', text:event.message};
    return null;
  }
</script>

<svelte:head><title>Secretary</title></svelte:head>
<div class="layout">
  <aside>
    <h1>Secretary</h1><p class="muted">{connected ? 'Подключено' : 'Нет соединения — обновите страницу'}</p>
    {#each sessions as s}
      <button class:active={selected === s.id} class:child={s.parent_id} class="session" onclick={() => select(s.id)}>
        <strong>#{s.id} {s.id === 1 ? 'Secretary Agent' : s.harness}</strong><br><small>{s.status} · {s.model}<br>{s.folder}<br>{s.last_event?.type === 'status' ? s.last_event.status : s.last_event?.method || s.last_event?.type || ''}</small>
      </button>
    {/each}
  </aside>
  <main>
    <header><h2>Сессия #{selected}</h2><button onclick={() => send({type:'stop', id:selected})}>Остановить</button></header>
    <div class="settings">
      <label>Агент <select bind:value={harness}><option value="claude">Claude</option><option value="codex">Codex</option></select></label>
      <label>Модель <input bind:value={model} list="models"><datalist id="models">{#each harness === 'codex' ? models : ['opus','sonnet','haiku'] as m}<option value={m}></option>{/each}</datalist></label>
      <button onclick={saveSettings}>Сохранить</button>
    </div>
    {#if error}<p role="alert">{error}</p>{/if}
    <div class="messages" aria-live="polite">
      {#each events as event}
        {@const entry = text(event)}
        {#if entry && entry.text}<article><strong>{entry.role}</strong><p>{entry.text}</p></article>{/if}
      {/each}
    </div>
    <form onsubmit={submit}><label class="sr" for="message">Сообщение</label><textarea id="message" bind:value={message} placeholder="Напишите сообщение"></textarea><button disabled={!connected}>Отправить</button></form>
  </main>
</div>
<style>
  :global(body){margin:0;font:16px system-ui;color:#292524;background:#f5f5f4} :global(*){box-sizing:border-box} button,input,textarea,select{font:inherit} button{cursor:pointer;border:1px solid #d6d3d1;background:white;border-radius:8px;padding:.6rem} button:hover,.active{background:#e7e5e4} .layout{display:flex;min-height:100vh} aside{width:270px;padding:1rem;background:#e7e5e4} h1{font-size:1.3rem} .muted,small{color:#57534e} .session{display:block;text-align:left;width:100%;margin:.5rem 0;overflow-wrap:anywhere} .child{margin-left:1rem;width:calc(100% - 1rem)} main{flex:1;min-width:0;padding:1rem;display:flex;flex-direction:column} header,.settings,form{display:flex;gap:.7rem;align-items:center} header{justify-content:space-between}.settings{flex-wrap:wrap}.settings label{display:flex;gap:.3rem}.messages{flex:1;min-height:50vh} article{background:white;border-radius:10px;padding:1rem;margin:1rem 0;overflow-wrap:anywhere} article p{white-space:pre-wrap} textarea{flex:1;min-height:4rem;padding:.6rem} .sr{position:absolute;left:-9999px}@media(max-width:650px){.layout{display:block}aside{width:auto} .messages{min-height:35vh}}
</style>
