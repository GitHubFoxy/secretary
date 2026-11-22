<script>
  import { onMount } from 'svelte';
  import { createConnection } from './connection.js';
  import { mergeHistory } from './history.js';
  import { createInterruptFeedback } from './interrupt-feedback.js';
  import { createUpdateChecker } from './updates.js';
  import { keyboardInset } from './viewport.js';
  import TopBar from './components/TopBar.svelte';
  import SessionDrawer from './components/SessionDrawer.svelte';
  import SettingsPanel from './components/SettingsPanel.svelte';
  import MessageList from './components/MessageList.svelte';
  import { voiceText } from './voice.js';
  import { createSpeechPlayer } from './speech.js';
  import Composer from './components/Composer.svelte';
  import Modal from './components/Modal.svelte';
  import Icon from './components/Icon.svelte';
  import { Capacitor, registerPlugin } from '@capacitor/core';
  const host = registerPlugin('Host');

  let cached;
  try { cached = JSON.parse(localStorage.getItem('last-chat')); } catch {}
  let historyLoaded = false, historyHasMore = false, historyLoadingOlder = false, historyVersion = 0, historyStartAtTop = false, historyToken = 0;
  let sessions = cached?.sessions ?? [], history = cached?.history ?? [], queue = [], selected = cached?.selected ?? null, message = '', error = '', harness = 'claude', model = 'opus', reasoning = null, connected = false;
  if (cached) { historyLoaded = true; historyHasMore = cached.hasMore; }
  let voiceMessages = [], voiceModel = '', voiceStatus = '', videoInterval = '1', ttsEngine = '', voiceModeHint = false, speaking = false;
  const speech = createSpeechPlayer(value => { speaking = value; });
  async function loadTtsEngine() {
    try { const response = await fetch('/settings/tts'); if (!response.ok) throw new Error(await response.text()); ttsEngine = (await response.json()).engine; }
    catch (err) { voiceStatus = `Не удалось загрузить настройку: ${err.message}`; }
  }
  async function saveTtsEngine(event) {
    const value = event.target.value;
    try { const response = await fetch('/settings/tts', {method:'PUT', body:value}); if (!response.ok) throw new Error(await response.text()); ttsEngine = value; voiceStatus = ''; }
    catch (err) { voiceStatus = `Не удалось сохранить настройку: ${err.message}`; event.target.value = ttsEngine; }
  }
  function stopSpeech() { speech.stop(); if (connected) command({type:'speechStop'}); }
  function micPress() { speech.unlock(); stopSpeech(); }
  function recordStart() { if (current?.secretary && connected) command({type:'speechWarm'}); }
  async function loadVideoInterval() {
    try { const response = await fetch('/settings/video'); if (!response.ok) throw new Error(await response.text()); videoInterval = (await response.json()).interval; }
    catch (err) { voiceStatus = `Не удалось загрузить настройку: ${err.message}`; }
  }
  async function saveVideoInterval(event) {
    const value = event.target.value;
    try { const response = await fetch('/settings/video', {method:'PUT', body:value}); if (!response.ok) throw new Error(await response.text()); videoInterval = value; voiceStatus = ''; }
    catch (err) { voiceStatus = `Не удалось сохранить настройку: ${err.message}`; event.target.value = videoInterval; }
  }
  async function loadVoiceModel() {
    try { const response = await fetch('/settings/voice'); if (!response.ok) throw new Error(await response.text()); voiceModel = (await response.json()).model; voiceStatus = ''; }
    catch (err) { voiceStatus = `Не удалось загрузить настройку: ${err.message}`; }
  }
  async function saveVoiceModel(event) {
    const value = event.target.value;
    try { const response = await fetch('/settings/voice', {method:'PUT', body:value}); if (!response.ok) throw new Error(await response.text()); voiceModel = value; voiceStatus = ''; }
    catch (err) { voiceStatus = `Не удалось сохранить настройку: ${err.message}`; event.target.value = voiceModel; }
  }
  let videoUrls = {};
  let drawerOpen = false, settingsOpen = false, renameOpen = false, newName = '', images = [], preparing = [];
  let profileOpen = false, agents = '', agentsLoaded = false, agentsStatus = '';
  let agentsOriginal = '', agentsSaving = false;
  let interruptStates = {};
  const deliveries = new Map();
  const interruptFeedback = createInterruptFeedback({onChange: (id, state) => { interruptStates = {...interruptStates, [id]:state}; }});
  const updates = createUpdateChecker({
    currentVersion: __SECRETARY_BUILD_ID__,
    fetchVersion: async () => {
      const response = await fetch('/build-version.json', {cache:'no-store'});
      if (response.status === 401) { localStorage.removeItem('last-chat'); location.assign('/?login'); }
      if (!response.ok) throw new Error('Version unavailable');
      return (await response.json()).version;
    },
    isBusy: () => Boolean(speaking || message || images.length || preparing.some(item => !item.error) || voiceMessages.some(item => !item.error) || Object.values(interruptStates).some(state => state?.pending) || renameOpen || settingsOpen || agentsSaving || (profileOpen && (!agentsLoaded || agents !== agentsOriginal))),
    reload: () => location.replace('/?update'),
  });
  const connection = createConnection({
    url: `${location.protocol === 'https:' ? 'wss' : 'ws'}://${location.host}/ws`,
    onOpen: () => { connected = true; if (selected !== null) loadHistory(selected, false, historyLoaded); updates.check(); },
    onClose: () => { connected = false; speech.stop(); interruptFeedback.disconnect(); },
    onMessage: handleMessage,
  });
  // Commands whose only visible result is a broadcast (sessions, message) or an error.
  const command = value => connection.request(value).catch(err => { error = err.message; });
  async function loadHistory(id, all = false, sync = false) {
    const token = ++historyToken;
    historyLoadingOlder = all;
    try {
      const lastCursor = sync ? history.at(-1)?.cursor : undefined;
      const page = await connection.request({type:'read', id, all});
      if (token !== historyToken) return;
      const merged = mergeHistory(history, page.messages, lastCursor);
      history = merged.messages; queue = page.queue;
      if (!sync || !merged.overlap) { historyHasMore = page.hasMore; historyStartAtTop = all; historyVersion++; }
      historyLoaded = true;
    } catch (err) { if (token === historyToken && !err.disconnected) error = err.message; }
    finally { if (token === historyToken) historyLoadingOlder = false; }
  }
  function select(id) { if (selected !== id) { history = []; queue = []; } selected = id; historyLoaded = false; historyHasMore = false; drawerOpen = false; loadHistory(id); }
  function handleMessage(msg) {
    if (msg.type === 'speech_start') speech.start();
    if (msg.type === 'speech_audio') speech.audio(msg.data, msg.rate);
    if (msg.type === 'speech_done') speech.done();
    if (msg.type === 'speech_cancel') speech.stop();
    if (msg.type === 'speech_error') { speech.stop(); error = msg.message; }
    if (msg.type === 'sessions') { sessions = msg.sessions; if (!sessions.some(s => s.id === selected) && sessions.length) { message = ''; settingsOpen = false; select((sessions.find(s => s.secretary) ?? sessions[0]).id); } }
    if (msg.type === 'message' && msg.sessionId === selected && !history.some(m => m.cursor === msg.message.cursor)) history = [...history, msg.message];
    if (msg.type === 'queue' && msg.sessionId === selected) queue = msg.items;
    if (msg.type === 'interrupt_result') interruptFeedback.result(msg.sessionId, msg.requestId, msg.outcome);
    if (msg.type === 'error') error = msg.message;
  }
  async function submit(immediate = false, video = null) {
    if (!connected || (!video && !message.trim() && !images.length)) return;
    const sent = video ? {id:crypto.randomUUID(), sessionId:video.id, text:'', images:[video], immediate} : {id:crypto.randomUUID(), sessionId:selected, text:message, images, immediate};
    const previous = deliveries.get(sent.sessionId) ?? Promise.resolve();
    let finish;
    const delivery = new Promise(resolve => { finish = resolve; });
    deliveries.set(sent.sessionId, delivery);
    if (!video) { message = ''; images = []; }
    preparing = [...preparing, sent];
    if (!interruptStates[sent.sessionId]?.pending) interruptFeedback.clear(sent.sessionId);
    try {
      const results = await Promise.all(sent.images.map(image => image.ready));
      sent.images = sent.images.map((image, index) => ({...image, processing:false, error:results[index].error}));
      const failed = results.find(result => result.error);
      if (failed) throw new Error(failed.error);
      await previous;
      let offset = 0;
      const descriptions = results.map(result => {
        const start = offset; offset += result.frames.length;
        return result.description?.replace('Кадры по порядку:', `Кадры по порядку, начиная с изображения ${start + 1}:`);
      }).filter(Boolean);
      const frames = results.flatMap(result => result.frames);
      if (JSON.stringify(frames).length > 29 * 1024 * 1024) throw new Error('Вложения слишком большие для отправки');
      if (video) videoUrls = {...videoUrls, [frames[0].data]:video.url};
      await connection.request({type:'send', id:sent.sessionId, text:[sent.text, ...descriptions].filter(Boolean).join('\n\n'), images:frames, immediate});
      preparing = preparing.filter(item => item.id !== sent.id);
      if (!video) sent.images.forEach(image => URL.revokeObjectURL(image.url));
    } catch (err) {
      const detail = err.disconnected ? 'Не удалось подтвердить доставку. Проверьте историю перед повторной отправкой.' : err.message;
      preparing = preparing.map(item => item.id === sent.id ? {...item, error:detail} : item);
    } finally {
      await previous;
      finish();
      if (deliveries.get(sent.sessionId) === delivery) deliveries.delete(sent.sessionId);
    }
  }
  function restorePreparing(id) {
    if (message.trim() || images.length) return;
    const item = preparing.find(entry => entry.id === id);
    if (!item) return;
    message = item.text; images = item.images;
    preparing = preparing.filter(entry => entry.id !== id);
  }
  // Запись хранится в памяти, пока её текст не принят: при ошибке её можно отправить повторно.
  async function sendVoice(item) {
    voiceMessages = [...voiceMessages.filter(v => v.key !== item.key), {...item, kind:'voice', error:''}];
    try {
      const response = await fetch('/voice', {method:'POST', headers:{'Content-Type':'audio/wav'}, body:await item.wav});
      if (!response.ok) throw new Error(await response.text());
      const {text} = await response.json();
      if (!text) throw new Error('Речь не распознана');
      await connection.request({type:'send', id:item.id, text:voiceText(text, item.seconds), voice:true});
      voiceMessages = voiceMessages.filter(v => v.key !== item.key);
    } catch (err) {
      voiceMessages = voiceMessages.map(v => v.key === item.key ? {...v, error:err.message} : v);
    }
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
    stopSpeech();
    if (!current?.status || current.status !== 'running' || !connected || interruptStates[selected]?.pending) return;
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
  function openProfile() { drawerOpen = false; profileOpen = true; loadAgents(); loadVoiceModel(); loadTtsEngine(); }
  // Queued messages are not accepted by the agent yet, so they come after history.
  $: messages = [
    ...history.map((m, index) => ({...m, images:m.images?.map(image => videoUrls[image.data] ? {...image, url:videoUrls[image.data]} : image), key:`${selected}:${index}`})),
    ...voiceMessages.filter(item => item.id === selected),
    ...queue.map(item => ({kind:'queued', id:item.id, text:item.text, images:item.images, urgent:item.urgent, key:`${selected}:queue:${item.id}`})),
    ...preparing.filter(item => item.sessionId === selected).map(item => ({...item, kind:'preparing', key:`preparing:${item.id}`})),
  ];
  $: if (historyLoaded && selected !== null) {
    try { localStorage.setItem('last-chat', JSON.stringify({selected, sessions, history:history.slice(-10).map(m => ({...m, images:m.images?.filter(image => !image.video || image.video.time === 0)})), hasMore:historyHasMore || history.length > 10})); } catch { localStorage.removeItem('last-chat'); }
  }
  $: current = sessions.find(s => s.id === selected);
  $: title = current?.name || (current?.secretary ? 'Secretary' : `Сессия #${selected}`);

  // Высота поля ввода и высота экранной клавиатуры (iOS не сжимает layout viewport, поэтому берём visualViewport).
  let composerHeight = 0, keyboard = 0, chatViewport;
  $: if (chatViewport) keyboard = keyboardInset({ viewport: window.visualViewport, container: chatViewport, activeElement: document.activeElement });
  onMount(() => {
    loadVideoInterval();
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
      <div class="flex flex-col gap-2 text-sm">Режим голоса
        <div class="flex gap-2"><button type="button" class="btn-ghost border border-neutral-600" aria-pressed="true">Рация</button><button type="button" class="btn-ghost opacity-50" title="Скоро" aria-disabled="true" onclick={() => voiceModeHint = true}>Живой разговор</button></div>
        {#if voiceModeHint}<span class="text-neutral-400">Скоро</span>{/if}
      </div>
      <label class="flex flex-col gap-2 text-sm">Синтез речи
        <select class="field" value={ttsEngine} disabled={!ttsEngine} onchange={saveTtsEngine}>
          <option value="inworld">Inworld — облако</option>
          <option value="piper">Piper — на Host</option>
        </select>
      </label>
      <label class="flex flex-col gap-2 text-sm">Распознавание речи на Host
        <select class="field" value={voiceModel} disabled={!voiceModel} onchange={saveVoiceModel}>
          <option value="gigaam">GigaAM — быстрее, лучше русский</option>
          <option value="parakeet">Parakeet — лучше английские слова</option>
        </select>
      </label>
      <label class="flex flex-col gap-2 text-sm">Частота кадров видео
        <select class="field" value={videoInterval} onchange={saveVideoInterval}>
          <option value="1">1 кадр в секунду</option>
          <option value="2">1 кадр в 2 секунды</option>
        </select>
      </label>
      {#if voiceStatus}<p role="alert" class="text-sm text-red-400">{voiceStatus}</p>{/if}
      {#if Capacitor.isNativePlatform()}
        <button class="btn-ghost self-start" onclick={() => { if (confirm('Сбросить адрес Host и забыть вход?')) host.reset(); }}>Сбросить адрес Host</button>
      {/if}
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
        <MessageList {messages} {sessions} {historyLoaded} {connected} {historyHasMore} {historyLoadingOlder} startAtTop={historyStartAtTop} onloadOlder={() => loadHistory(selected, true)} bottomInset={composerHeight + keyboard} onselect={select} onrestorePreparing={restorePreparing} restoreDisabled={!!message.trim() || !!images.length} onsendQueued={id => command({type:'sendQueued', id:selected, messageId:id})} onretryVoice={sendVoice} />
      {/key}

      <!-- Поле ввода плавает поверх списка: сообщения прокручиваются под ним. -->
      <div bind:offsetHeight={composerHeight} style:bottom="{keyboard}px" class="pointer-events-none absolute inset-x-0 z-10 bg-linear-to-t from-neutral-950 from-60% to-transparent pt-6">
        <div class="pointer-events-auto">
          {#if interruptStates[selected]?.message}<p role="status" aria-live="polite" class="mx-auto max-w-3xl px-4 text-sm text-neutral-400">{interruptStates[selected].message}</p>{/if}
          {#if speaking && current?.secretary}<p role="status" class="mx-auto max-w-3xl px-4 text-sm text-neutral-400">Озвучиваю ответ…</p>{/if}
          <Composer {videoInterval} speaking={speaking && current?.secretary} onmicpress={micPress} onrecordstart={recordStart} bind:value={message} bind:images disabled={!connected} stopDisabled={!connected || !!interruptStates[selected]?.pending} running={current?.status === 'running'} sessionId={selected} onsubmit={submit} oninterrupt={interrupt} onvoice={sendVoice} onvideo={video => submit(false, video)} />
        </div>
      </div>
    </div>

    <SessionDrawer open={drawerOpen} {sessions} {selected} {connected} onhide={s => setHidden(s, true)} onrestore={s => setHidden(s, false)} onselect={select} onclose={() => (drawerOpen = false)} onprofile={openProfile} />
  </div>
{/if}
