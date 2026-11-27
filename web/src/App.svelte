<script>
  import { onMount, tick } from 'svelte';
  import { notificationsEnabled, shouldNotify } from './browser-notifications.js';
  import { createConnection } from './connection.js';
  import { createDelivery } from './delivery.js';
  import { createDrafts } from './drafts.js';
  import { createCamera } from './camera.js';
  import { mergeHistory, insertVoices, mergeApprovals } from './history.js';
  import { HISTORY_INITIAL_LIMIT, retryDelay, mergeOlder, needsInitialTopup } from './history-prefetch.js';
  import { createInterruptFeedback } from './interrupt-feedback.js';
  import { createUpdateChecker } from './updates.js';
  import { queuedCaption, shouldShowActivity, shouldShowSendNow, trackQueueSeen } from './pending-message.js';
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
  import DownloadsScreen from './components/DownloadsScreen.svelte';
  import CodexLimits from './components/CodexLimits.svelte';
  import OpenCodeLimits from './components/OpenCodeLimits.svelte';
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
  let historyLoading = false, historyLoaded = false, historyHasMore = false, historyLoadingOlder = false, historyVersion = 0, historyToken = 0;
  // Тихий автоповтор без кнопки: при ошибке read повторяем с растущей паузой, спиннер сверху остаётся видимым.
  let historyRetryTimer = null, historyRetryAttempt = 0;
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
  let opencodeLimits = null, opencodeLimitsStatus = '';
  let profileOpen = false, downloadsOpen = false, instructionsDirty = false;
  let notesOpen = false, notesRevision = 0, notesCount = 0;
  let interruptStates = {};
  // Тихая отправка: очередь видна на самом сообщении, fallback-кнопка — только при проблеме.
  // nowMs тикает для порога долгого ожидания, queueFirstSeen помнит первое появление каждого id.
  // Все присвоения очереди идут через setQueue, чтобы штампы не расползались по коду.
  let nowMs = Date.now(), queueFirstSeen = new Map();
  function setQueue(items) {
    queue = Array.isArray(items) ? items : [];
    const next = trackQueueSeen(queueFirstSeen, queue, Date.now());
    if (next !== queueFirstSeen) queueFirstSeen = next;
  }
  const delivery = createDelivery({request: value => connection.request(value), fetch: (...args) => fetch(...args), onChange: items => { preparing = items; }, revoke: url => URL.revokeObjectURL(url)});
  const interruptFeedback = createInterruptFeedback({onChange: (id, state) => { interruptStates = {...interruptStates, [id]:state}; }});
  const updates = createUpdateChecker({
    currentVersion: __SECRETARY_BUILD_ID__,
    fetchVersion: async () => {
      const response = await fetch('/build-version.json', {cache:'no-store'});
      if (response.status === 401) { localStorage.removeItem('last-chat'); location.assign('/?login'); }
      if (!response.ok) throw new Error('Version unavailable');
      return (await response.json()).version;
    },
    isBusy: () => Boolean(speaking || drafts.busy || images.length || preparing.some(item => !item.error) || voiceMessages.some(item => !item.error) || Object.values(interruptStates).some(state => state?.pending) || notesOpen || renameOpen || settingsOpen || (profileOpen && instructionsDirty)),
    reload: () => location.replace('/?update'),
  });
  const connection = createConnection({
    url: `${location.protocol === 'https:' ? 'wss' : 'ws'}://${location.host}/ws`,
    onOpen: () => { connected = true; notesRevision++; drafts.reconnect(); camera.reconnect(); if (selected !== null) loadHistory(selected, false, historyLoaded); updates.check(); },
    onClose: () => { connected = false; for (const id of voiceTimings.keys()) finishVoiceTiming(id); speech.stop(); interruptFeedback.disconnect(); },
    onMessage: handleMessage,
  });
  // Commands whose only visible result is a broadcast (sessions, message) or an error.
  const command = value => connection.request(value).catch(err => { error = err.message; });
  function clearHistoryRetry() { if (historyRetryTimer) { clearTimeout(historyRetryTimer); historyRetryTimer = null; } }
  function scheduleHistoryRetry(id, older, sync) {
    clearHistoryRetry();
    const delay = retryDelay(historyRetryAttempt++);
    historyRetryTimer = setTimeout(() => {
      historyRetryTimer = null;
      if (selected !== id) return;
      if (older && !historyHasMore) { historyLoadingOlder = false; return; }
      loadHistory(id, older, sync);
    }, delay);
  }
  async function loadHistory(id, older = false, sync = false) {
    const token = ++historyToken;
    if (older) historyLoadingOlder = true;
    else historyLoading = true;
    try {
      if (older) {
        const page = await connection.request({type:'read', id, before: history[0]?.cursor}, 15000);
        if (token !== historyToken || selected !== id) return;
        history = mergeOlder(history, page.messages);
        setQueue(page.queue);
        historyHasMore = page.hasMore;
        historyLoaded = true;
        historyRetryAttempt = 0;
      } else if (!sync || history.length === 0) {
        // Открытие чата: грузим сразу окно 30 (две серверные страницы по 20 с дедупом),
        // показываются последние ~10, остальные — запас для быстрой прокрутки вверх.
        const first = await connection.request({type:'read', id}, 15000);
        if (token !== historyToken || selected !== id) return;
        setQueue(first.queue);
        let messages = first.messages.slice();
        let hasMore = first.hasMore;
        if (needsInitialTopup(messages, hasMore, HISTORY_INITIAL_LIMIT) && messages.length) {
          const second = await connection.request({type:'read', id, before: messages[0].cursor}, 15000);
          if (token !== historyToken || selected !== id) return;
          messages = mergeOlder(messages, second.messages);
          setQueue(second.queue ?? queue);
          hasMore = second.hasMore;
        }
        history = messages;
        historyHasMore = hasMore;
        historyLoaded = true;
        historyVersion++;
        historyRetryAttempt = 0;
      } else {
        const lastCursor = history.at(-1)?.cursor;
        const page = await connection.request({type:'read', id}, 15000);
        if (token !== historyToken || selected !== id) return;
        const merged = mergeHistory(history, page.messages, lastCursor);
        history = merged.messages; setQueue(page.queue);
        if (!merged.overlap) { historyHasMore = page.hasMore; historyVersion++; }
        historyLoaded = true;
        historyRetryAttempt = 0;
      }
    } catch (err) {
      if (token !== historyToken || selected !== id) return;
      // Офлайн/разрыв: молча ждём переподключения (onOpen позовёт sync-загрузку), лишних повторов нет.
      if (err.disconnected) return;
      scheduleHistoryRetry(id, older, sync);
    }
    finally {
      // Во время паузы автоповтора спиннер остаётся: догрузка выглядит как обычная фоновая подгрузка.
      if (token === historyToken && !historyRetryTimer) {
        if (older) historyLoadingOlder = false;
        else historyLoading = false;
      }
    }
  }
  const drafts = createDrafts({request: value => connection.request(value), selected: () => selected, setText: text => { message = text; }, onError: value => { error = value; }});
  const scheduleDraft = drafts.schedule;
  function select(id) {
    if (selected !== id) {
      if (selected !== null) drafts.save(selected);
      clearHistoryRetry(); historyRetryAttempt = 0;
      history = []; setQueue([]); message = '';
      selected = id;
      drafts.select(id);
    }
    historyLoaded = false; historyHasMore = false; historyLoading = false; historyLoadingOlder = false; historyRetryAttempt = 0; drawerOpen = false; loadHistory(id);
  }
  let openSecretaryPending = false;
  function openSecretary() {
    drawerOpen = settingsOpen = renameOpen = profileOpen = voiceMode = notesOpen = limitsOpen = false;
    const secretary = sessions.find(s => s.secretary);
    openSecretaryPending = !secretary;
    if (secretary) select(secretary.id);
  }
  const camera = createCamera({request: value => connection.request(value), connected: () => connected});
  function handleMessage(msg) {
    if (camera.receive(msg)) return;
    if (msg.type === 'secretary_reply' && shouldNotify({enabled:notificationsEnabled(), permission:globalThis.Notification?.permission, text:msg.text, visibility:document.visibilityState, native:Capacitor.isNativePlatform()})) {
      try {
        const notification = new Notification('Secretary', {body:msg.text.slice(0, 200), tag:'secretary-reply'});
        notification.onclick = () => { notification.close(); window.focus(); openSecretary(); };
      } catch {}
    }
    drafts.receive(msg, document.activeElement?.id === 'message');
    if (msg.type === 'notesChanged' && (msg.sessionId === selected || current?.secretary)) notesRevision++;
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
    if (msg.type === 'queue' && msg.sessionId === selected) setQueue(msg.items);
    if (msg.type === 'interrupt_result') interruptFeedback.result(msg.sessionId, msg.requestId, msg.outcome);
    if (msg.type === 'error') error = msg.message;
  }
  async function submit(immediate = false, video = null) {
    if (!connected || (!video && !message.trim() && !images.length)) return;
    if (!video && message.trim() === '/compact') {
      if (images.length) { error = 'Команду /compact нельзя отправить с вложениями'; return; }
      if (current?.status === 'running') { error = 'Сессия работает'; return; }
      const id = selected;
      try { await connection.request({type:'compact', id}); if (selected === id && message.trim() === '/compact') { message = ''; scheduleDraft(''); } } catch (err) { error = err.message; }
      return;
    }
    const sent = video ? {id:crypto.randomUUID(), sessionId:video.id, text:'', images:[video], immediate} : {id:crypto.randomUUID(), sessionId:selected, text:message, images, immediate};
    if (!video) { message = ''; images = []; scheduleDraft(''); }
    if (!interruptStates[sent.sessionId]?.pending) interruptFeedback.clear(sent.sessionId);
    await delivery.send(sent);
  }
  function restorePreparing(id) {
    if (message.trim() || images.length) return;
    const item = delivery.restore(id);
    if (!item) return;
    message = item.text; images = item.images; scheduleDraft(message);
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
      await connection.request({type:'send', id:item.id, text:voiceText(text, item.seconds), voice:item.speech === true, immediate:item.immediate === true, voiceTimingId:item.timing ? item.key : undefined});
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
    drawerOpen = false; limitsOpen = true; limits = null; limitsStatus = ''; opencodeLimits = null; opencodeLimitsStatus = '';
    refreshLimits();
  }
  function refreshLimits() { loadLimits(); loadOpencodeLimits(); }
  async function loadOpencodeLimits() {
    try {
      const response = await fetch('/opencode/limits');
      if (!response.ok) throw new Error(await response.text());
      opencodeLimits = await response.json(); opencodeLimitsStatus = '';
    } catch (err) { opencodeLimitsStatus = `Не удалось загрузить: ${err.message || err}`; }
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
  // Оптимистично: очередь — то же своё сообщение, статус тихо на нём самом.
  // Общий индикатор хода прячем, пока есть неотправленное: иначе «В очереди» + «Пишет ответ…» спорят.
  $: groupedHistory = groupSteps(insertVoices(mergeSwitches(mergeApprovals(history)).map(m => ({...m, images:[...(m.images || []), ...(m.files || [])], key:`${selected}:${m.cursor}`})), voiceMessages.filter(item => item.id === selected)), current?.status === 'running');
  $: preparingHere = preparing.filter(item => item.sessionId === selected);
  $: queueHere = queue.map(item => ({kind:'queued', id:item.id, text:item.text, images:[...item.images, ...(item.files || [])], urgent:item.urgent, sendNow:shouldShowSendNow({urgent:item.urgent, firstSeen:queueFirstSeen.get(item.id), now:nowMs, sessionStatus:current?.status}), caption:queuedCaption(item.urgent), key:`${selected}:queue:${item.id}`}));
  $: pendingHere = queueHere.length + preparingHere.length;
  $: messages = [
    ...groupedHistory,
    ...queueHere,
    ...preparingHere.map(item => ({...item, kind:'preparing', key:`preparing:${item.id}`})),
    ...(shouldShowActivity({sessionStatus:current?.status, live:!!groupedHistory.at(-1)?.live, pendingCount:pendingHere}) ? [{kind:'activity', text:activityStatus('running', history), key:`${selected}:activity`}] : []),
  ];
  const userKinds = ['user', 'voice', 'queued', 'preparing', 'cancelled', 'uncertain'];
  $: turn = messages.slice(Math.max(0, messages.findLastIndex(m => userKinds.includes(m.kind))));
  $: if (historyLoaded && selected !== null) {
    try { localStorage.setItem('last-chat', JSON.stringify({selected, sessions, history:history.slice(-30).map(m => ({...m, images:m.images?.filter(image => !image.video || image.video.time === 0)})), hasMore:historyHasMore || history.length > 30})); } catch { localStorage.removeItem('last-chat'); }
  }
  $: current = sessions.find(s => s.id === selected);
  $: voiceProps = {turn, sessionId:selected, running:current?.status === 'running', processing:voiceMessages.some(item => item.id === selected && item.speech && !item.error), speaking:playing, speechFinishedId, connected, openedAt:voiceOpenedAt, onstart:() => { micPress(); recordStart(); }, onvoice:item => sendVoice({...item, speech:true}), onphoto:image => connection.request({type:'send', id:selected, text:'Снимок с камеры', images:[image]}), oncamera:camera.set, onscreenshot:overlayBridge?.screenshot ? async () => { const data = await overlayBridge.screenshot(); if (!data) throw new Error('Снимок экрана недоступен'); await connection.request({type:'send', id:selected, text:'Экран на момент нажатия Power', images:[{type:'image/jpeg', data}]}); } : undefined, onstopspeech:stopSpeech};
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
    // Тихий тик для порога fallback-кнопки зависшей очереди.
    const pendingTimer = setInterval(() => { if (queue.length) nowMs = Date.now(); }, 5000);
    window.addEventListener('online', reconnect);
    document.addEventListener('visibilitychange', reconnect);
    const vv = window.visualViewport;
    const update = () => { keyboard = keyboardInset({ viewport: vv, container: chatViewport, activeElement: document.activeElement }); };
    if (vv) { vv.addEventListener('resize', update); vv.addEventListener('scroll', update); }
    window.addEventListener('resize', update);
    document.addEventListener('focusin', update);
    document.addEventListener('focusout', update);
    update();
    return () => { shareListener?.remove(); clearInterval(updateTimer); clearInterval(pendingTimer); clearHistoryRetry(); updates.stop(); interruptFeedback.dispose(); connection.stop(); window.removeEventListener('online', reconnect); document.removeEventListener('visibilitychange', reconnect); window.removeEventListener('resize', update); document.removeEventListener('focusin', update); document.removeEventListener('focusout', update); vv?.removeEventListener('resize', update); vv?.removeEventListener('scroll', update); };
  });
</script>

<svelte:head><title>Secretary</title></svelte:head>

{#if overlay}
  <VoiceMode variant="sheet" {...voiceProps} onclose={() => overlayBridge?.close()} onopenapp={() => overlayBridge?.openApp()} />
{:else if voiceMode}
  {#key voiceKey}<VoiceMode variant="full" {...voiceProps} onclose={() => (voiceMode = false)} />{/key}
{:else if downloadsOpen}
  <DownloadsScreen onback={() => (downloadsOpen = false)} />
{:else if profileOpen}
  <ProfileScreen
    request={value => connection.request(value)}
    {ttsEngine}
    {voiceModel}
    {videoInterval}
    {voiceStatus}
    {assistantStatus}
    {sessions}
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

    <Notes open={notesOpen} sessionId={selected} secretary={!!current?.secretary} {connection} revision={notesRevision} oncount={count => notesCount = count} onclose={() => (notesOpen = false)} />

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

    <Modal open={limitsOpen} title="Лимиты" wide onrefresh={refreshLimits} onclose={() => (limitsOpen = false)}>
      <div class="grid gap-3 sm:grid-cols-2 sm:gap-4">
        <section class="flex min-w-0 flex-col gap-2 rounded-xl border border-neutral-800/80 bg-neutral-900/40 p-3 sm:gap-3 sm:p-4">
          <h3 class="text-sm font-semibold text-neutral-100">Codex</h3>
          <CodexLimits {limits} status={limitsStatus} onrefresh={loadLimits} />
        </section>
        <section class="flex min-w-0 flex-col gap-2 rounded-xl border border-neutral-800/80 bg-neutral-900/40 p-3 sm:gap-3 sm:p-4">
          <h3 class="text-sm font-semibold text-neutral-100">OpenCode</h3>
          <OpenCodeLimits limits={opencodeLimits} status={opencodeLimitsStatus} onrefresh={loadOpencodeLimits} />
        </section>
      </div>
    </Modal>

    {#if error}
      <p role="alert" class="border-b border-neutral-900 px-4 py-2 text-center text-sm text-red-400">{error}</p>
    {/if}

    <div bind:this={chatViewport} class="relative flex min-h-0 flex-1 flex-col">
      {#key historyVersion}
        <MessageList ondecide={(kind, id, approved, password) => connection.request({type: `${kind}${approved ? 'Approve' : 'Reject'}`, [kind === 'skill' ? 'skillId' : 'sudoId']: id, ...(password ? {password} : {})})} {messages} {sessions} {historyLoaded} {historyLoading} {connected} historyHasMore={historyHasMore} {historyLoadingOlder} onloadOlder={() => loadHistory(selected, true)} bottomInset={composerHeight + keyboard} onselect={select} onrestorePreparing={restorePreparing} restoreDisabled={!!message.trim() || !!images.length} onsendQueued={id => command({type:'sendQueued', id:selected, messageId:id})} onretryVoice={sendVoice} oncancelVoice={item => voiceMessages = voiceMessages.filter(v => v.key !== item.key)} />
      {/key}

      <div bind:offsetHeight={composerHeight} style:bottom="{keyboard}px" class="pointer-events-none absolute inset-x-0 z-10 bg-linear-to-t from-neutral-950 from-60% to-transparent pt-6">
        <div class="pointer-events-auto">
          {#if interruptStates[selected]?.message}<p role="status" aria-live="polite" class="mx-auto max-w-3xl px-4 text-sm text-neutral-400">{interruptStates[selected].message}</p>{/if}
          {#if speaking && current?.secretary}
            <SpeakingPill onstop={stopSpeech} />
          {/if}
          <Composer loadReferences={(id, query) => connection.request({type:'references', id, query}).then(result => result.references)} sessionHarness={current?.harness} sessionFolder={current?.folder} loadCommands={id => connection.request({type:'slashCommands', id}).then(result => result.commands)} bind:this={composer} {videoInterval} bind:value={message} bind:images disabled={!connected} stopDisabled={!connected || !!interruptStates[selected]?.pending} running={current?.status === 'running'} sessionId={selected} oninput={scheduleDraft} onsubmit={submit} oninterrupt={interrupt} onvoice={sendVoice} onvideo={video => submit(false, video)} />
        </div>
      </div>
    </div>


    <SessionDrawer open={drawerOpen} {sessions} {selected} {connected} onhide={s => setHidden(s, true)} onrestore={s => setHidden(s, false)} onselect={select} onclose={() => (drawerOpen = false)} onprofile={openProfile} onlimits={openLimits} ondownloads={() => { drawerOpen = false; downloadsOpen = true; }} />
  </div>
{/if}
