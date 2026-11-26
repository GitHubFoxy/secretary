<script>
  import { onMount, tick } from 'svelte';
  import { notificationsEnabled, shouldNotify } from './browser-notifications.js';
  import { createConnection } from './connection.js';
  import { mergeHistory, insertVoices } from './history.js';
  import { createInterruptFeedback } from './interrupt-feedback.js';
  import { createUpdateChecker } from './updates.js';
  import { keyboardInset } from './viewport.js';
  import TopBar from './components/TopBar.svelte';
  import { activityStatus } from './activity-status.js';
  import Notes from './components/Notes.svelte';
  import SessionDrawer from './components/SessionDrawer.svelte';
  import SettingsPanel from './components/SettingsPanel.svelte';
  import MessageList from './components/MessageList.svelte';
  import { voiceText } from './voice.js';
  import { createSpeechPlayer } from './speech.js';
  import Composer from './components/Composer.svelte';
  import VoiceMode from './components/VoiceMode.svelte';
  import Modal from './components/Modal.svelte';
  import ProfileScreen from './components/ProfileScreen.svelte';
  import CodexLimits from './components/CodexLimits.svelte';
  import SpeakingPill from './components/SpeakingPill.svelte';
  import { mergeSwitches } from './agents.js';
  import { Capacitor, registerPlugin } from '@capacitor/core';
  const host = registerPlugin('Host');
  const share = registerPlugin('Share');

  let cached;
  try { cached = JSON.parse(localStorage.getItem('last-chat')); } catch {}
  // Шторка поверх другого приложения (кнопка Power): голосовой режим с Secretary Agent.
  const overlay = new URLSearchParams(location.search).has('overlay');
  const overlayBridge = globalThis.SecretaryOverlay;
  // В шторке под пустым местом должно быть видно приложение, поверх которого её открыли.
  if (overlay) document.documentElement.style.background = document.body.style.background = 'transparent';
  let voiceMode = false, voiceKey = 0, voiceOpenedAt = performance.now();
  const voiceTimings = new Map();
  function finishVoiceTiming(id) {
    const timing = voiceTimings.get(id);
    if (!timing) return;
    voiceTimings.delete(id);
    const values = Object.fromEntries(['listening', 'end', 'recognized', 'event', 'audio'].map(key => [key, timing[key] == null ? null : Math.round(timing[key] - timing.press)]));
    console.info('Voice Call ms', values);
    if (connected) command({type:'voiceTiming', values});
  }
  function openVoiceMode() {
    voiceOpenedAt = performance.now();
    const secretary = sessions.find(s => s.secretary);
    if (secretary && selected !== secretary.id) select(secretary.id);
    drawerOpen = settingsOpen = renameOpen = profileOpen = false;
    voiceMode = true; voiceKey++;
  }
  if ((overlay || new URLSearchParams(location.search).has('secretary')) && !cached?.sessions?.find(s => s.id === cached.selected)?.secretary) cached = null;
  let historyLoaded = false, historyHasMore = false, historyLoadingOlder = false, historyVersion = 0, historyToken = 0;
  let sessions = cached?.sessions ?? [], history = cached?.history ?? [], queue = [], selected = cached?.selected ?? null, message = '', error = '', connected = false;
  if (cached) { historyLoaded = true; historyHasMore = cached.hasMore; }
  let voiceMessages = [], voiceModel = '', voiceStatus = '', videoInterval = '1', ttsEngine = '', speaking = false, playing = false, speechFinishedId = null;
  const speech = createSpeechPlayer(value => { speaking = value; }, AudioContext, value => { playing = value; });
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
  let assistantStatus = '';
  async function requestAssistant() {
    try { assistantStatus = (await host.requestAssistantRole()).held ? 'Готово: удерживайте Power' : 'Выберите Secretary помощником в настройках'; }
    catch { assistantStatus = 'Обновите приложение'; }
  }
  let drawerOpen = false, settingsOpen = false, renameOpen = false, newName = '', images = [], preparing = [];
  let composer, shared = [], shareBusy = false;
  async function loadShared() {
    try { shared = (await share.pending()).items; }
    catch (err) { error = `Не удалось получить общий контент: ${err.message}`; }
  }
  async function applyShared(item, secretary) {
    shareBusy = true;
    try {
      const files = await Promise.all(item.files.map(async file => {
        const response = await fetch(Capacitor.convertFileSrc(file.path));
        if (!response.ok) throw new Error(`Не удалось прочитать ${file.name}`);
        return new File([await response.blob()], file.name, {type:file.type});
      }));
      if (selected !== secretary.id) select(secretary.id);
      drawerOpen = settingsOpen = renameOpen = profileOpen = voiceMode = false;
      await tick();
      if (!composer) throw new Error('Поле ввода недоступно');
      if (item.text) { message += (message ? '\n' : '') + item.text; scheduleDraft(message); }
      if (files.length) composer.add(files);
      if (item.error) error = item.error;
      await share.acknowledge({id:item.id});
      await loadShared();
    } catch (err) { error = `Не удалось получить общий контент: ${err.message}`; shared = []; }
    finally { shareBusy = false; }
  }
  $: secretaryForShare = sessions.find(s => s.secretary);
  $: if (shared.length && secretaryForShare && !shareBusy) applyShared(shared[0], secretaryForShare);
  let limitsOpen = false, limits = null, limitsStatus = '';
  let profileOpen = false, instructionsDirty = false;
  let notesOpen = false, notesRevision = 0, notesCount = 0;
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
    isBusy: () => Boolean(speaking || draftPending.size || images.length || preparing.some(item => !item.error) || voiceMessages.some(item => !item.error) || Object.values(interruptStates).some(state => state?.pending) || notesOpen || renameOpen || settingsOpen || (profileOpen && instructionsDirty)),
    reload: () => location.replace('/?update'),
  });
  const connection = createConnection({
    url: `${location.protocol === 'https:' ? 'wss' : 'ws'}://${location.host}/ws`,
    onOpen: () => { connected = true; notesRevision++; for (const id of draftPending.keys()) savePending(id); if (selected !== null) { loadHistory(selected, false, historyLoaded); if (!draftPending.has(selected)) loadDraft(selected); } updates.check(); },
    onClose: () => { connected = false; for (const id of voiceTimings.keys()) finishVoiceTiming(id); speech.stop(); interruptFeedback.disconnect(); },
    onMessage: handleMessage,
  });
  // Commands whose only visible result is a broadcast (sessions, message) or an error.
  const command = value => connection.request(value).catch(err => { error = err.message; });
  async function loadHistory(id, older = false, sync = false) {
    const token = ++historyToken;
    historyLoadingOlder = older;
    try {
      const lastCursor = sync ? history.at(-1)?.cursor : undefined;
      const page = await connection.request({type:'read', id, before: older ? history[0]?.cursor : undefined});
      if (token !== historyToken) return;
      const merged = older ? {messages: [...page.messages, ...history], overlap: true} : mergeHistory(history, page.messages, lastCursor);
      history = merged.messages; queue = page.queue;
      if (older) historyHasMore = page.hasMore;
      else if (!sync || !merged.overlap) { historyHasMore = page.hasMore; historyVersion++; }
      historyLoaded = true;
    } catch (err) { if (token === historyToken && !err.disconnected) error = err.message; }
    finally { if (token === historyToken) historyLoadingOlder = false; }
  }
  const draftFrom = crypto.randomUUID();
  const draftPending = new Map();
  let draftToken = 0;
  function savePending(id) {
    const entry = draftPending.get(id);
    if (!entry) return;
    clearTimeout(entry.timer);
    entry.timer = null;
    connection.request({type:'draftSave', id, text:entry.text, from:draftFrom}).then(() => {
      if (draftPending.get(id) === entry) draftPending.delete(id);
    }).catch(err => { error = `Не удалось сохранить черновик: ${err.message}`; });
  }
  function scheduleDraft(text) {
    if (selected === null) return;
    const id = selected;
    clearTimeout(draftPending.get(id)?.timer);
    const entry = {text, timer:null};
    draftPending.set(id, entry);
    entry.timer = setTimeout(() => savePending(id), 500);
    draftToken++;
  }
  async function loadDraft(id) {
    const token = ++draftToken;
    try {
      const result = await connection.request({type:'draft', id});
      if (selected === id && token === draftToken) message = result.text;
    } catch (err) { if (!err.disconnected) error = err.message; }
  }
  function select(id) {
    if (selected !== id) {
      if (selected !== null) savePending(selected);
      history = []; queue = []; message = '';
      selected = id;
      if (draftPending.has(id)) { draftToken++; message = draftPending.get(id).text; }
      else loadDraft(id);
    }
    historyLoaded = false; historyHasMore = false; drawerOpen = false; loadHistory(id);
  }
  let openSecretaryPending = false;
  function openSecretary() {
    drawerOpen = settingsOpen = renameOpen = profileOpen = voiceMode = notesOpen = limitsOpen = false;
    const secretary = sessions.find(s => s.secretary);
    openSecretaryPending = !secretary;
    if (secretary) select(secretary.id);
  }
  function handleMessage(msg) {
    if (msg.type === 'secretary_reply' && shouldNotify({enabled:notificationsEnabled(), permission:globalThis.Notification?.permission, text:msg.text, visibility:document.visibilityState, native:Capacitor.isNativePlatform()})) {
      try {
        const notification = new Notification('Secretary', {body:msg.text.slice(0, 200), tag:'secretary-reply'});
        notification.onclick = () => { notification.close(); window.focus(); openSecretary(); };
      } catch {}
    }
    if (msg.type === 'draftChanged' && msg.sessionId === selected && msg.from !== draftFrom && document.activeElement?.id !== 'message') { draftToken++; clearTimeout(draftPending.get(selected)?.timer); draftPending.delete(selected); message = msg.text; }
    if (msg.type === 'notesChanged' && msg.sessionId === selected) notesRevision++;
    if (msg.type === 'speech_start') speech.start();
    if (msg.type === 'speech_event' && voiceTimings.has(msg.voiceTimingId)) voiceTimings.get(msg.voiceTimingId).event ??= performance.now();
    if (msg.type === 'speech_audio' && speech.audio(msg.data, msg.rate)) {
      const timing = voiceTimings.get(msg.voiceTimingId);
      if (timing) { timing.audio = performance.now(); finishVoiceTiming(msg.voiceTimingId); }
    }
    if (['speech_done', 'speech_cancel', 'speech_error'].includes(msg.type)) { speechFinishedId = msg.voiceTimingId; finishVoiceTiming(msg.voiceTimingId); }
    if (msg.type === 'speech_done') speech.done();
    if (msg.type === 'speech_cancel') speech.stop();
    if (msg.type === 'speech_error') { speech.stop(); error = msg.message; }
    if (msg.type === 'sessions') { sessions = msg.sessions; if (openSecretaryPending && sessions.some(s => s.secretary)) openSecretary(); if (!sessions.some(s => s.id === selected) && sessions.length) { message = ''; settingsOpen = false; select((sessions.find(s => s.secretary) ?? sessions[0]).id); } }
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
    if (!video) { message = ''; images = []; scheduleDraft(''); }
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
      const files = await Promise.all(results.filter(result => result.file).map(async result => {
        const response = await fetch(`/files?name=${encodeURIComponent(result.file.name)}`, {method:'POST', headers:{'Content-Type':result.file.type || 'application/octet-stream'}, body:result.file});
        if (!response.ok) throw new Error(await response.text());
        return response.json();
      }));
      if (JSON.stringify(frames).length > 29 * 1024 * 1024) throw new Error('Вложения слишком большие для отправки');
      await connection.request({type:'send', id:sent.sessionId, text:[sent.text, ...descriptions].filter(Boolean).join('\n\n'), images:frames, files, immediate});
      preparing = preparing.filter(item => item.id !== sent.id);
      sent.images.forEach(image => { if (image.url) URL.revokeObjectURL(image.url); });
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
    message = item.text; images = item.images; scheduleDraft(message);
    preparing = preparing.filter(entry => entry.id !== id);
  }
  // Запись хранится в памяти, пока её текст не принят: при ошибке её можно отправить повторно.
  async function sendVoice(item) {
    if (item.kind === 'voice') voiceMessages = voiceMessages.map(v => v.key === item.key ? {...v, error:''} : v);
    else voiceMessages = [...voiceMessages, {...item, kind:'voice', error:'', afterCursor: item.id === selected ? (history.at(-1)?.cursor ?? 0) : 0}];
    try {
      const wav = await item.wav;
      if (!wav) throw new Error('Не услышал речь');
      const response = await fetch('/voice', {method:'POST', headers:{'Content-Type':'audio/wav'}, body:wav});
      if (!response.ok) throw new Error(await response.text());
      const {text} = await response.json();
      if (item.timing) item.timing.recognized = performance.now();
      if (!text) throw new Error('Речь не распознана');
      if (item.timing) voiceTimings.set(item.key, item.timing);
      await connection.request({type:'send', id:item.id, text:voiceText(text, item.seconds), voice:item.speech === true, voiceTimingId:item.timing ? item.key : undefined});
      voiceMessages = voiceMessages.filter(v => v.key !== item.key);
    } catch (err) {
      if (item.timing) { if (!voiceTimings.has(item.key)) voiceTimings.set(item.key, item.timing); finishVoiceTiming(item.key); }
      voiceMessages = voiceMessages.map(v => v.key === item.key ? {...v, error:err.message} : v);
    }
  }
  function saveSettings({harness, model, reasoning}) { command({type:'settings', id:selected, harness, model, reasoning}); settingsOpen = false; }
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
  function openSettings() { settingsOpen = true; }
  function openRename() { newName = title; settingsOpen = false; renameOpen = true; }
  function rename() { if (!connected || !newName.trim()) return; error = ''; command({type:'rename', id:selected, name:newName}); renameOpen = false; }
  // A separate chat in the folder of the current one, starting from an empty context.
  async function createThread({harness, model, reasoning, name}) {
    if (!current) return;
    try {
      const {id} = await connection.request({type:'create', harness, model, reasoning, folder:current.folder});
      if (name.trim()) await connection.request({type:'rename', id, name});
      settingsOpen = false; select(id);
    } catch (err) { error = err.message; }
  }
  function openProfile() { drawerOpen = false; profileOpen = true; loadVoiceModel(); loadTtsEngine(); }
  async function loadLimits() {
    try {
      const response = await fetch('/codex/limits');
      if (!response.ok) throw new Error(await response.text());
      limits = await response.json(); limitsStatus = '';
    } catch (err) { limitsStatus = `Не удалось загрузить: ${err.message || err}`; }
  }
  function openLimits() {
    drawerOpen = false; limitsOpen = true; limits = null; limitsStatus = 'Загрузка…';
    loadLimits();
  }
  function groupSteps(items, running) {
    const grouped = [];
    for (const item of items) {
      if (item.kind === 'writing') continue;
      if (item.kind !== 'step' && item.kind !== 'thinking') { grouped.push(item); continue; }
      let group = grouped[grouped.length - 1];
      if (group?.kind !== 'steps') {
        group = {kind:'steps', key:item.key, steps:[], thinking:''};
        grouped.push(group);
      }
      if (item.kind === 'step') group.steps.push(item);
      else if (item.kind === 'thinking') group.thinking = item.text;
    }
    // Only the last group of a running turn is live; finished turns show what the agent did, not «thinks».
    const last = grouped.at(-1);
    if (last?.kind === 'steps' && running) grouped[grouped.length - 1] = {...last, live:true, activity:activityStatus('running', history)};
    return grouped;
  }
  // Queued messages are not accepted by the agent yet, so they come after history.
  $: groupedHistory = groupSteps(insertVoices(mergeSwitches(history.filter(m => m.kind !== 'sudo' || !m.status)).map(m => m.kind === 'sudo' ? {...m, status: history.findLast(e => e.kind === 'sudo' && e.id === m.id && e.status)?.status} : m).map(m => ({...m, images:[...(m.images || []), ...(m.files || [])], key:`${selected}:${m.cursor}`})), voiceMessages.filter(item => item.id === selected)), current?.status === 'running');
  $: messages = [
    ...groupedHistory,
    ...queue.map(item => ({kind:'queued', id:item.id, text:item.text, images:[...item.images, ...(item.files || [])], urgent:item.urgent, key:`${selected}:queue:${item.id}`})),
    ...preparing.filter(item => item.sessionId === selected).map(item => ({...item, kind:'preparing', key:`preparing:${item.id}`})),
    ...(current?.status === 'running' && !groupedHistory.at(-1)?.live ? [{kind:'activity', text:activityStatus('running', history), key:`${selected}:activity`}] : []),
  ];
  const userKinds = ['user', 'voice', 'queued', 'preparing', 'cancelled', 'uncertain'];
  $: turn = messages.slice(Math.max(0, messages.findLastIndex(m => userKinds.includes(m.kind))));
  $: if (historyLoaded && selected !== null) {
    try { localStorage.setItem('last-chat', JSON.stringify({selected, sessions, history:history.slice(-20).map(m => ({...m, images:m.images?.filter(image => !image.video || image.video.time === 0)})), hasMore:historyHasMore || history.length > 20})); } catch { localStorage.removeItem('last-chat'); }
  }
  $: current = sessions.find(s => s.id === selected);
  $: voiceProps = {turn, sessionId:selected, running:current?.status === 'running', processing:voiceMessages.some(item => item.id === selected && item.speech && !item.error), speaking:playing, speechFinishedId, connected, openedAt:voiceOpenedAt, onstart:() => { micPress(); recordStart(); }, onvoice:item => sendVoice({...item, speech:true}), onstopspeech:stopSpeech};
  $: title = current?.name || (current?.secretary ? 'Secretary' : `Сессия #${selected}`);

  // Высота поля ввода и высота экранной клавиатуры (iOS не сжимает layout viewport, поэтому берём visualViewport).
  let composerHeight = 0, keyboard = 0, chatViewport;
  $: if (chatViewport) keyboard = keyboardInset({ viewport: window.visualViewport, container: chatViewport, activeElement: document.activeElement });
  onMount(() => {
    loadVideoInterval();
    connection.connect();
    let shareListener;
    if (Capacitor.isNativePlatform() && Capacitor.isPluginAvailable('Share')) {
      share.addListener('received', loadShared).then(listener => { shareListener = listener; loadShared(); });
    }
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
    return () => { shareListener?.remove(); clearInterval(updateTimer); updates.stop(); interruptFeedback.dispose(); connection.stop(); window.removeEventListener('online', reconnect); document.removeEventListener('visibilitychange', reconnect); window.removeEventListener('resize', update); document.removeEventListener('focusin', update); document.removeEventListener('focusout', update); vv?.removeEventListener('resize', update); vv?.removeEventListener('scroll', update); };
  });
</script>

<svelte:head><title>Secretary</title></svelte:head>

{#if overlay}
  <VoiceMode variant="sheet" {...voiceProps} onclose={() => overlayBridge?.close()} onopenapp={() => overlayBridge?.openApp()} />
{:else if voiceMode}
  {#key voiceKey}<VoiceMode variant="full" {...voiceProps} onclose={() => (voiceMode = false)} />{/key}
{:else if profileOpen}
  <ProfileScreen
    {ttsEngine}
    {voiceModel}
    {videoInterval}
    {voiceStatus}
    {assistantStatus}
    secretary={sessions.find(s => s.secretary)}
    bind:dirty={instructionsDirty}
    onback={() => (profileOpen = false)}
    onsaveTtsEngine={saveTtsEngine}
    onsaveVoiceModel={saveVoiceModel}
    onsaveVideoInterval={saveVideoInterval}
    onrequestAssistant={requestAssistant}
    onresetHost={() => { if (confirm('Сбросить адрес Host и забыть вход?')) host.reset(); }}
  />
{:else}
  <div class="flex h-dvh flex-col bg-neutral-950 text-neutral-200">
    <TopBar
      {title}
      status={current?.status}
      {connected}
      onmenu={() => (drawerOpen = true)}
      oncall={openVoiceMode}
      {notesCount}
      onnotes={() => (notesOpen = !notesOpen)}
      onsettings={openSettings}
    />

    <Notes open={notesOpen} sessionId={selected} {connection} revision={notesRevision} oncount={count => notesCount = count} onclose={() => (notesOpen = false)} />

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

    <Modal open={settingsOpen} title={current?.secretary ? 'Secretary Agent' : 'Настройки чата'} onclose={() => (settingsOpen = false)}>
      {#if current}{#key selected}<SettingsPanel session={current} {connected} onsave={saveSettings} onstop={stop} oncreate={createThread} onrename={openRename} />{/key}{/if}
    </Modal>

    <Modal open={limitsOpen} title="Лимиты Codex" onclose={() => (limitsOpen = false)}>
      <CodexLimits {limits} status={limitsStatus} onrefresh={loadLimits} />
    </Modal>

    {#if error}
      <p role="alert" class="border-b border-neutral-900 px-4 py-2 text-center text-sm text-red-400">{error}</p>
    {/if}

    <div bind:this={chatViewport} class="relative flex min-h-0 flex-1 flex-col">
      {#key historyVersion}
        <MessageList onsudo={(type, sudoId, password) => connection.request({type, sudoId, ...(password ? {password} : {})})} {messages} {sessions} {historyLoaded} {connected} historyHasMore={historyHasMore} {historyLoadingOlder} onloadOlder={() => loadHistory(selected, true)} bottomInset={composerHeight + keyboard} onselect={select} onrestorePreparing={restorePreparing} restoreDisabled={!!message.trim() || !!images.length} onsendQueued={id => command({type:'sendQueued', id:selected, messageId:id})} onretryVoice={sendVoice} oncancelVoice={item => voiceMessages = voiceMessages.filter(v => v.key !== item.key)} />
      {/key}

      <!-- Поле ввода плавает поверх списка: сообщения прокручиваются под ним. -->
      <div bind:offsetHeight={composerHeight} style:bottom="{keyboard}px" class="pointer-events-none absolute inset-x-0 z-10 bg-linear-to-t from-neutral-950 from-60% to-transparent pt-6">
        <div class="pointer-events-auto">
          {#if interruptStates[selected]?.message}<p role="status" aria-live="polite" class="mx-auto max-w-3xl px-4 text-sm text-neutral-400">{interruptStates[selected].message}</p>{/if}
          {#if speaking && current?.secretary}
            <SpeakingPill onstop={stopSpeech} />
          {/if}
          <Composer sessionHarness={current?.harness} sessionFolder={current?.folder} loadCommands={id => connection.request({type:'slashCommands', id}).then(result => result.commands)} bind:this={composer} {videoInterval} bind:value={message} bind:images disabled={!connected} stopDisabled={!connected || !!interruptStates[selected]?.pending} running={current?.status === 'running'} sessionId={selected} oninput={scheduleDraft} onsubmit={submit} oninterrupt={interrupt} onvoice={sendVoice} onvideo={video => submit(false, video)} />
        </div>
      </div>
    </div>


    <SessionDrawer open={drawerOpen} {sessions} {selected} {connected} onhide={s => setHidden(s, true)} onrestore={s => setHidden(s, false)} onselect={select} onclose={() => (drawerOpen = false)} onprofile={openProfile} onlimits={openLimits} />
  </div>
{/if}
