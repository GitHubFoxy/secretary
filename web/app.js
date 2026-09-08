const loginView = document.querySelector('#login');
const conversationView = document.querySelector('#conversation-view');
const entries = document.querySelector('#entries');
const connectionState = document.querySelector('#connection-state');
const loginError = document.querySelector('#login-error');
const messageError = document.querySelector('#message-error');
const activity = document.querySelector('#activity');
const workerStatus = document.querySelector('#worker-status');

let lastSeq = 0;
let conversationSocket;
let activitySocket;
let activeWorker;

const showError = (element, message) => {
  element.textContent = message;
  element.hidden = !message;
};

async function request(path, options = {}) {
  const response = await fetch(path, { credentials: 'same-origin', ...options });
  if (!response.ok) throw new Error(await response.text() || response.statusText);
  return response.json();
}

function renderEntry(entry) {
  if (entry.seq <= lastSeq) return;
  lastSeq = entry.seq;
  const item = document.createElement('article');
  item.className = `entry entry-${entry.kind}`;
  item.dataset.seq = entry.seq;
  item.innerHTML = `<small>${entry.kind} · ${entry.seq}</small><div></div>`;
  item.lastElementChild.textContent = entry.body;
  const match = entry.body.match(/worker(?:_ref| ref)?[:= ]+([A-Za-z0-9_-]+)/i);
  if (match) {
    const link = document.createElement('button');
    link.type = 'button';
    link.textContent = `Open Worker ${match[1]}`;
    link.addEventListener('click', () => openWorker(match[1]));
    item.append(link);
  }
  entries.append(item);
  entries.scrollTop = entries.scrollHeight;
}

async function connectConversation() {
  const history = await request('/v1/conversation?after_seq=0');
  history.forEach(renderEntry);
  const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
  conversationSocket = new WebSocket(`${protocol}//${location.host}/v1/ws?after_seq=${lastSeq}`);
  conversationSocket.onopen = () => { connectionState.textContent = 'connected'; };
  conversationSocket.onclose = () => { connectionState.textContent = 'reconnecting'; setTimeout(connectConversation, 1000); };
  conversationSocket.onerror = () => { conversationSocket.close(); };
  conversationSocket.onmessage = event => renderEntry(JSON.parse(event.data));
}

async function showConversation() {
  loginView.hidden = true;
  conversationView.hidden = false;
  await connectConversation();
}

async function bootstrapSession(token) {
  await request('/v1/web/session', {
    method: 'POST', headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({bootstrap_token: token})
  });
}

async function restoreSession() {
  const params = new URLSearchParams(location.hash.slice(1));
  const token = params.get('bootstrap');
  try {
    if (token) {
      await bootstrapSession(token);
      history.replaceState(null, '', `${location.pathname}${location.search}`);
    } else {
      await request('/v1/web/session');
    }
    await showConversation();
  } catch (_) {
    // No valid cookie yet. The login form remains visible.
  }
}

document.querySelector('#login-form').addEventListener('submit', async event => {
  event.preventDefault();
  showError(loginError, '');
  try {
    await bootstrapSession(document.querySelector('#bootstrap-token').value);
    await showConversation();
  } catch (error) { showError(loginError, error.message); }
});

restoreSession();

document.querySelector('#message-form').addEventListener('submit', async event => {
  event.preventDefault();
  showError(messageError, '');
  const input = document.querySelector('#message');
  try {
    const response = await request('/v1/messages', {
      method: 'POST', headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({external_message_id: crypto.randomUUID(), body: input.value})
    });
    renderEntry(response.entry);
    input.value = '';
  } catch (error) { showError(messageError, error.message); }
});

async function openWorker(workerRef) {
  activeWorker = workerRef;
  document.querySelector('#worker-observer').hidden = false;
  const status = await request(`/v1/workers/${encodeURIComponent(workerRef)}`);
  workerStatus.textContent = status.state;
  activity.replaceChildren();
  if (activitySocket) activitySocket.close();
  const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
  activitySocket = new WebSocket(`${protocol}//${location.host}/v1/workers/${encodeURIComponent(workerRef)}/activity`);
  activitySocket.onmessage = event => {
    const item = JSON.parse(event.data);
    const line = document.createElement('div');
    line.textContent = `${item.kind}: ${item.text}`;
    activity.append(line);
  };
  activitySocket.onclose = () => { if (workerStatus.textContent === 'active') workerStatus.textContent = 'offline'; };
}

document.querySelector('#worker-form').addEventListener('submit', async event => {
  event.preventDefault();
  try { await openWorker(document.querySelector('#worker-ref').value.trim()); }
  catch (error) { workerStatus.textContent = error.message; }
});

document.querySelector('#steer-form').addEventListener('submit', async event => {
  event.preventDefault();
  const input = document.querySelector('#steer');
  try { await request(`/v1/workers/${encodeURIComponent(activeWorker)}/steer`, {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify({text:input.value})}); input.value=''; }
  catch (error) { workerStatus.textContent = error.message; }
});

document.querySelector('#queue-form').addEventListener('submit', async event => {
  event.preventDefault();
  const input = document.querySelector('#queued');
  try { await request(`/v1/workers/${encodeURIComponent(activeWorker)}/queue`, {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify({text:input.value})}); input.value=''; }
  catch (error) { workerStatus.textContent = error.message; }
});

document.querySelector('#stop-worker').addEventListener('click', async () => {
  workerStatus.textContent = 'stopping';
  try { await request(`/v1/workers/${encodeURIComponent(activeWorker)}/stop`, {method:'POST'}); }
  catch (error) { workerStatus.textContent = error.message; }
});
