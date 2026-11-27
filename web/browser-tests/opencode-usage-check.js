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
await until(`document.body.innerText.includes('Скрытые') || document.querySelector('button[aria-label="Расход OpenCode"]')`, 'drawer opens');
const hasUsageButton = await ev(`!!document.querySelector('button[aria-label="Расход OpenCode"]')`);
if (!hasUsageButton) throw Error('no OpenCode usage button next to Codex limits');
console.log('PASS usage button in drawer');
await ev(`document.querySelector('button[aria-label="Расход OpenCode"]').click()`);
await until(`document.body.innerText.includes('Расход OpenCode') && document.body.innerText.includes('Сегодня')`, 'usage modal opens');
await until(`document.body.innerText.includes('7 дней')`, 'week totals shown');
await until(`document.body.innerText.includes('ноября')`, 'per-day rows shown');
console.log('PASS usage numbers from fixture');
await ev(`[...document.querySelectorAll('button')].find(b=>b.textContent==='Обновить').click()`);
await until(`document.body.innerText.includes('Всего 4 хода')`, 'all-time line shown');
await ev(`document.querySelector('[aria-label="Закрыть"]').click()`);
await mode('fail');
await ev(`document.querySelector('[aria-label="Список сессий"]').click()`);
await until(`!!document.querySelector('button[aria-label="Расход OpenCode"]')`, 'drawer reopens');
await ev(`document.querySelector('button[aria-label="Расход OpenCode"]').click()`);
await until(`document.body.innerText.includes('Не удалось загрузить')`, 'usage error state');
console.log('PASS usage error state');
ws.close();
