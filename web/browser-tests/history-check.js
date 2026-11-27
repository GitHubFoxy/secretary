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
const click = () =>
  ev(
    `[...document.querySelectorAll('button')].find(b=>b.textContent.includes('предыдущие сообщения')||b.textContent.includes('Повторить'))?.click()`,
  );
const reset = async (m) => {
  await mode(m);
  await ev("localStorage.clear()");
  await send("Page.reload", { ignoreCache: true });
  await Bun.sleep(600);
};
await send("Emulation.setDeviceMetricsOverride", {
  width: 375,
  height: 667,
  deviceScaleFactor: 1,
  mobile: true,
});
await reset("ok");
await until(`document.body.innerText.includes('synthetic 60')`, "initial page");
await mode("hold");
await ev(`document.querySelector('.overscroll-contain').scrollTop=0`);
await until(
  `document.body.innerText.includes('Загрузка…')`,
  "scroll starts older request",
);
let anchor = await ev(
  `(()=>{let s=document.querySelector('.overscroll-contain'), a=[...s.querySelectorAll('[data-message]')].find(x=>x.getBoundingClientRect().bottom>s.getBoundingClientRect().top);window.testAnchor=a;return a.getBoundingClientRect().top})()`,
);
await mode("release");
await until(
  `document.body.innerText.includes('synthetic 21')`,
  "scroll loads second page",
);
let restored = await ev(`window.testAnchor.getBoundingClientRect().top`);
if (Math.abs(anchor - restored) > 2)
  throw Error(`scroll position shifted ${anchor} to ${restored}`);
console.log("PASS scroll position preserved");
await mode("ok");
await ev(`document.querySelector('.overscroll-contain').scrollTop=0`);
await until(
  `document.body.innerText.includes('synthetic 1')`,
  "scroll loads third page",
);
let counts = await ev(
  `Array.from({length:60},(_,i)=>[...document.querySelectorAll('[data-message]')].filter(e=>e.textContent.trim()==='synthetic '+(i+1)).length)`,
);
if (counts.some((x) => x !== 1))
  throw Error("duplicates or missing messages " + JSON.stringify(counts));
console.log("PASS no duplicates");
await reset("group");
await until(
  `document.querySelectorAll('[data-message]').length===1 && document.body.innerText.includes('synthetic 60')`,
  "grouped page",
);
console.log(
  "dimensions",
  await ev(
    `(()=>{let s=document.querySelector('.overscroll-contain');return [s.scrollHeight,s.clientHeight]})()`,
  ),
);
await mode("fail");
await click();
await until(
  `document.body.innerText.includes('synthetic failure') && document.body.innerText.includes('Повторить')`,
  "older error and retry",
);
let before = (await mode("fail")).length;
await ev(
  `document.querySelector('.overscroll-contain').dispatchEvent(new Event('scroll'))`,
);
await Bun.sleep(400);
let after = (await mode("group")).length;
if (before !== after) throw Error("automatic retry loop");
console.log("PASS no automatic retry");
await click();
await until(
  `document.body.innerText.includes('synthetic 60') && !document.body.innerText.includes('synthetic failure')`,
  "retry succeeds",
);
await click();
await until(
  `!document.body.innerText.includes('предыдущие сообщения')`,
  "grouped three pages",
);
for (let count = 1; count <= 9; count++) {
  await reset("group" + (count - 1));
  await until(
    `document.querySelectorAll('[data-message]').length===${count}`,
    `visible items ${count}`,
  );
  let before = (await mode("group" + (count - 1))).length;
  await click();
  await Bun.sleep(150);
  if ((await mode("group" + (count - 1))).length < before + 1)
    throw Error(`older page ${count}`);
  console.log("PASS older page", count);
}
await reset("ok");
await until(
  `document.body.innerText.includes('synthetic 60')`,
  "reconnect setup",
);
await mode("hold");
await click();
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
await click();
await until(
  `document.body.innerText.includes('synthetic 21')`,
  "load after reconnect",
);
let unique = await ev(
  `[...document.querySelectorAll('[data-message]')].map(e=>e.textContent.trim())`,
);
if (new Set(unique).size !== unique.length)
  throw Error("duplicates after reconnect");
console.log("PASS reconnect without duplicates");
ws.close();
