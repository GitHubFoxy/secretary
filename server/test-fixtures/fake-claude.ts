// Фейковый `claude` для тестов: читает stream-json из stdin и отвечает как Claude Code.
// Текст с LONG держит ход, пока его не прервут. STUBBORN держит ход и игнорирует прерывание.
// FAIL завершает ход ошибкой (is_error). Каждое полученное сообщение и аргументы запуска пишутся в FAKE_LOG.
import { appendFileSync, existsSync } from 'node:fs';
import { createInterface } from 'node:readline';

const args = process.argv.slice(2);
const log = (entry: object) => appendFileSync(process.env.FAKE_LOG!, JSON.stringify(entry) + '\n');
const out = (event: object) => process.stdout.write(JSON.stringify(event) + '\n');
const sessionId = 'claude-session';
let busy: string | null = null;
let stubborn = false;

const rl = createInterface({ input: process.stdin });
rl.on('line', line => {
  const message = JSON.parse(line);
  if (message.type === 'control_request' && message.request.subtype === 'interrupt') {
    if (!busy || stubborn) return;
    busy = null;
    out({ type: 'result', subtype: 'error_during_execution', is_error: true, result: '', session_id: sessionId });
    return;
  }
  if (message.type !== 'user') return;
  const content = message.message.content;
  const text: string = typeof content === 'string' ? content : content.find((block: { type: string }) => block.type === 'text').text;
  const resume = args.indexOf('--resume');
  log({ harness: 'claude', received: text, resume: resume >= 0 ? args[resume + 1] : null });
  out({ type: 'system', subtype: 'init', session_id: sessionId });
  if (text.includes('MALFORMED')) {
    busy = text; stubborn = true;
    setInterval(() => {}, 1000);
    const timer = setInterval(() => {
      if (existsSync(`${process.env.FAKE_LOG}.malformed`)) { clearInterval(timer); process.stdout.write('not-json\n'); }
    }, 10);
    return;
  }
  if (text.includes('FAIL')) {
    out({ type: 'result', subtype: 'error_during_execution', is_error: true, result: '', session_id: sessionId });
    return;
  }
  if (text.includes('LONG') || text.includes('STUBBORN')) {
    busy = text;
    stubborn = text.includes('STUBBORN');
    return;
  }
  out({ type: 'result', subtype: 'success', is_error: false, result: `ANSWER ${text}`, session_id: sessionId });
});
rl.on('close', () => { if (!busy?.includes('MALFORMED')) process.exit(0); });
