const cdp = process.env.USAGE_TEST_CDP || 'http://127.0.0.1:9783';
const server = process.env.USAGE_TEST_SERVER || 'http://127.0.0.1:5199';
const tabs = await (await fetch(cdp + '/json')).json();
const ws = new WebSocket(tabs.find(t => t.type === 'page').webSocketDebuggerUrl);
await new Promise(r => (ws.onopen = r));
let n = 0;
const p = new Map();
ws.onmessage = e => {
  const m = JSON.parse(e.data);
  if (p.has(m.id)) { p.get(m.id)(m); p.delete(m.id); }
};
const send = (method, params = {}) => new Promise(r => {
  const id = ++n;
  p.set(id, r);
  ws.send(JSON.stringify({ id, method, params }));
});
const ev = async expression => {
  const r = await send('Runtime.evaluate', { expression, returnByValue: true, awaitPromise: true });
  if (r.result.exceptionDetails) throw Error(JSON.stringify(r.result.exceptionDetails));
  return r.result.result.value;
};
const until = async (expr, label) => {
  for (let i = 0; i < 150; i++) {
    if (await ev(expr)) { console.log('PASS', label); return; }
    await Bun.sleep(50);
  }
  throw Error(label);
};
const mode = async m => await (await fetch(server + '/control?mode=' + m)).json();
await send('Emulation.setDeviceMetricsOverride', { width: 390, height: 700, deviceScaleFactor: 1, mobile: true });
await send('Page.navigate', { url: server + '/' });
await until(`!!document.querySelector('[aria-label="Список сессий"]')`, 'app boots with stub session');
await ev('localStorage.clear()');
await send('Page.reload', { ignoreCache: true });
await until(`!!document.querySelector('[aria-label="Список сессий"]')`, 'app boots after storage clear');
await ev(`document.querySelector('[aria-label="Список сессий"]').click()`);
await until(`!!document.querySelector('button[aria-label="Лимиты"]')`, 'single limits button in drawer');
console.log('PASS limits button in drawer');
await ev(`document.querySelector('button[aria-label="Лимиты"]').click()`);
await until(`document.body.innerText.includes('Лимиты') && document.body.innerText.includes('Codex')`, 'limits modal opens');
await until(`document.body.innerText.includes('OpenCode') && document.body.innerText.includes('5 часов')`, 'opencode section shown');
await until(`document.body.innerText.includes('Неделя') && document.body.innerText.includes('Месяц')`, 'all three go windows shown');
console.log('PASS both sections from fixtures');
await mode('fail');
await ev(`document.querySelector('[aria-label="Закрыть"]').click()`);
await until(`!document.querySelector('[role="dialog"]')`, 'modal closes');
await ev(`document.querySelector('[aria-label="Список сессий"]').click()`);
await until(`!!document.querySelector('button[aria-label="Лимиты"]')`, 'drawer reopens');
await ev(`document.querySelector('button[aria-label="Лимиты"]').click()`);
await until(`document.body.innerText.includes('Не удалось загрузить')`, 'limits error state');
console.log('PASS limits error state');
ws.close();
