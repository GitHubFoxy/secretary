// Проверка умной предзагрузки истории: кнопки «Загрузить предыдущие сообщения» нет,
// окно 30 при открытии, догрузка фоном при прокрутке вверх, тихий автоповтор без кнопки.
const cdp = process.env.HISTORY_TEST_CDP || "http://127.0.0.1:9782";
const server = process.env.HISTORY_TEST_SERVER || "http://127.0.0.1:5198";
const tabs = await (await fetch(cdp + "/json")).json(),
  ws = new WebSocket(tabs.find((t) => t.type === "page").webSocketDebuggerUrl);
await new Promise((r) => (ws.onopen = r));
let n = 0,
  p = new Map();
ws.onmessage = (e) => {
  let m = JSON.parse(e.data);
  if (p.has(m.id)) {
    p.get(m.id)(m);
    p.delete(m.id);
  }
};
const send = (method, params = {}) =>
  new Promise((r) => {
    let id = ++n;
    p.set(id, r);
    ws.send(JSON.stringify({ id, method, params }));
  });
const ev = async (expression) => {
  let r = await send("Runtime.evaluate", {
    expression,
    returnByValue: true,
    awaitPromise: true,
  });
  if (r.result.exceptionDetails)
    throw Error(JSON.stringify(r.result.exceptionDetails));
  return r.result.result.value;
};
const until = async (expr, label) => {
  for (let i = 0; i < 150; i++) {
    if (await ev(expr)) {
      console.log("PASS", label);
      return;
    }
    await Bun.sleep(50);
  }
  throw Error(label);
};
const mode = async (m) =>
  await (await fetch(server + "/control?mode=" + m)).json();
const historyButtons = () =>
  ev(
    `[...document.querySelectorAll('button')].filter(b=>b.textContent.includes('предыдущие сообщения')||b.textContent.includes('Повторить')).length`,
  );
const scrollTop = () =>
  ev(
    `(()=>{let s=document.querySelector('.overscroll-contain'); s.scrollTop=0; s.dispatchEvent(new Event('scroll')); return s.scrollTop})()`,
  );
const reset = async (m) => {
  await mode(m);
  await ev("localStorage.clear()");
  await send("Page.reload", { ignoreCache: true });
  await Bun.sleep(600);
};
// Ширина 390px по задаче.
await send("Emulation.setDeviceMetricsOverride", {
  width: 390,
  height: 700,
  deviceScaleFactor: 1,
  mobile: true,
});

// --- Открытие: сразу окно ~30+ (две страницы), кнопок нет ---
await reset("ok");
await until(`document.body.innerText.includes('synthetic 60')`, "initial page");
await until(
  `document.body.innerText.includes('synthetic 21') && !document.body.innerText.includes('synthetic 20')`,
  "initial window prefetches second page",
);
let reqs = await mode("ok");
if (JSON.stringify(reqs.slice(-2)) !== JSON.stringify([null, 41]))
  throw Error("initial reads " + JSON.stringify(reqs));
console.log("PASS initial window is two pages");
if ((await historyButtons()) !== 0) throw Error("history button still present");
console.log("PASS no load-more button");
let initialCount = await ev(`document.querySelectorAll('[data-message]').length`);
if (initialCount < 30) throw Error("initial window too small: " + initialCount);
console.log("PASS initial window shows", initialCount);

// --- Быстрая прокрутка вверх: один фоновый запрос, спиннер, позиция не прыгает ---
await mode("hold");
await scrollTop();
await scrollTop();
await scrollTop();
await until(
  `document.body.innerText.includes('Загрузка…')`,
  "fast scroll starts background prefetch",
);
await Bun.sleep(300);
reqs = await mode("hold");
if (reqs.filter((b) => b === 21).length !== 1)
  throw Error("duplicate prefetch: " + JSON.stringify(reqs));
console.log("PASS single prefetch on fast scroll");
let anchor = await ev(
  `(()=>{let s=document.querySelector('.overscroll-contain'), a=[...s.querySelectorAll('[data-message]')].find(x=>x.getBoundingClientRect().bottom>s.getBoundingClientRect().top);window.testAnchor=a;return a.getBoundingClientRect().top})()`,
);
await mode("release");
await until(
  `document.body.innerText.includes('synthetic 1')`,
  "prefetch loads third page",
);
let restored = await ev(`window.testAnchor.getBoundingClientRect().top`);
if (Math.abs(anchor - restored) > 2)
  throw Error(`scroll position shifted ${anchor} to ${restored}`);
console.log("PASS scroll position preserved");
let counts = await ev(
  `Array.from({length:60},(_,i)=>[...document.querySelectorAll('[data-message]')].filter(e=>e.textContent.trim()==='synthetic '+(i+1)).length)`,
);
if (counts.some((x) => x !== 1))
  throw Error("duplicates or missing messages " + JSON.stringify(counts));
console.log("PASS no duplicates");
if ((await historyButtons()) !== 0) throw Error("history button appeared");
reqs = await mode("ok");
await scrollTop();
await Bun.sleep(400);
if ((await mode("ok")).length !== reqs.length)
  throw Error("request after full history loaded");
console.log("PASS no requests when history exhausted");

// --- Ошибка older: тихий автоповтор без кнопки, затем успех ---
await reset("ok");
await until(`document.body.innerText.includes('synthetic 21')`, "retry setup");
await mode("fail");
await scrollTop();
await until(
  `document.body.innerText.includes('Загрузка…')`,
  "older error keeps spinner",
);
await Bun.sleep(2500);
if ((await historyButtons()) !== 0) throw Error("retry button appeared");
if (await ev(`document.body.innerText.includes('synthetic failure')`))
  throw Error("failure text shown to user");
reqs = await mode("fail");
if (reqs.filter((b) => b === 21).length < 2)
  throw Error("no silent auto-retry: " + JSON.stringify(reqs));
console.log("PASS silent auto-retry without button");
await mode("ok");
await until(
  `document.body.innerText.includes('synthetic 1')`,
  "auto-retry succeeds",
);
if ((await historyButtons()) !== 0) throw Error("history button appeared");
console.log("PASS retry recovers history");

// --- Ошибка первички: открытие в fail, автоповтор, затем успех ---
await reset("fail");
await until(
  `document.body.innerText.includes('Загрузка сообщений')`,
  "initial load spinner on failure",
);
await Bun.sleep(2500);
if ((await historyButtons()) !== 0) throw Error("retry button on initial load");
reqs = await mode("fail");
if (reqs.filter((b) => b === null).length < 2)
  throw Error("no initial auto-retry: " + JSON.stringify(reqs.slice(-6)));
console.log("PASS initial load auto-retries");
await mode("ok");
await until(
  `document.body.innerText.includes('synthetic 21')`,
  "initial auto-retry recovers",
);
console.log("PASS initial recovery");

// --- Переподключение посреди фоновой догрузки ---
await reset("ok");
await until(`document.body.innerText.includes('synthetic 21')`, "reconnect setup");
await mode("hold");
await scrollTop();
await until(
  `document.body.innerText.includes('Загрузка…')`,
  "older request pending",
);
await mode("disconnect");
await mode("ok");
await Bun.sleep(1800);
await until(
  `!document.body.innerText.includes('Загрузка…') && document.body.innerText.includes('synthetic 60')`,
  "reconnect after interrupted request",
);
await scrollTop();
await until(
  `document.body.innerText.includes('synthetic 1')`,
  "load after reconnect",
);
let unique = await ev(
  `[...document.querySelectorAll('[data-message]')].map(e=>e.textContent.trim())`,
);
if (new Set(unique).size !== unique.length)
  throw Error("duplicates after reconnect");
console.log("PASS reconnect without duplicates");

// --- Переключение Session посреди догрузки: без ошибок и утечек ---
await reset("ok");
await until(`document.body.innerText.includes('synthetic 60')`, "switch setup");
await ev(
  `window.testErrors=[]; window.addEventListener('error', e => window.testErrors.push(String(e.error || e.message))); window.addEventListener('unhandledrejection', e => window.testErrors.push(String(e.reason)))`,
);
await mode("hold");
await scrollTop();
await until(
  `document.body.innerText.includes('Загрузка…')`,
  "older request held before switch",
);
await ev(
  `window.oldScroller=document.querySelector('.overscroll-contain'); document.querySelector('[aria-label="Список сессий"]').click()`,
);
await ev(
  `[...document.querySelectorAll('button')].find(b=>b.textContent.includes('Other')).click()`,
);
await until(
  `document.body.innerText.includes('other session') && !window.oldScroller.isConnected`,
  "switch unmounts old list",
);
await mode("release");
await Bun.sleep(300);
if (await ev(`window.testErrors.length`))
  throw Error(
    "browser error after unmount: " +
      JSON.stringify(await ev(`window.testErrors`)),
  );
if (await ev(`document.body.innerText.includes('synthetic 21')`))
  throw Error("old page leaked into other session");
console.log("PASS delayed reply after unmount");

await ev(`document.querySelector('[aria-label="Список сессий"]').click()`);
await ev(
  `[...document.querySelectorAll('button')].find(b=>b.textContent.includes('History')).click()`,
);
await until(
  `document.body.innerText.includes('synthetic 60')`,
  "error isolation setup",
);
await mode("fail");
await scrollTop();
await until(
  `document.body.innerText.includes('Загрузка…')`,
  "older error pending before reconnect",
);
await mode("disconnect");
await mode("ok");
await Bun.sleep(1800);
await until(
  `document.body.innerText.includes('synthetic 60') && !document.body.innerText.includes('Загрузка сообщений')`,
  "reconnected after older error",
);
await ev(`document.querySelector('[aria-label="Список сессий"]').click()`);
await ev(
  `[...document.querySelectorAll('button')].find(b=>b.textContent.includes('Other')).click()`,
);
await until(
  `document.body.innerText.includes('other session')`,
  "other session after reconnect",
);
if (await ev(`document.body.innerText.includes('synthetic failure')`))
  throw Error("older error leaked into other session");
console.log("PASS older error does not leak after reconnect");
ws.close();
