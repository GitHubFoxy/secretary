// A fake `claude` for adapter tests: reads stream-json, answers like Claude Code, logs each message to calls.log in its cwd.
// LONG holds the turn until interrupted; STUBBORN ignores the interrupt; CRASH exits with an error on stderr.
import { appendFileSync } from 'node:fs';
import { createInterface } from 'node:readline';

const args = process.argv.slice(2);
const out = (event: object) => process.stdout.write(JSON.stringify(event) + '\n');
const session_id = 'claude-session';
let busy = '';

createInterface({ input: process.stdin }).on('line', line => {
  const message = JSON.parse(line);
  if (message.type === 'control_request') {
    if (busy && !busy.includes('STUBBORN')) { busy = ''; out({ type: 'result', is_error: true, result: '', session_id }); }
    return;
  }
  const text: string = message.message.content;
  const resume = args.indexOf('--resume');
  const prompt = args.indexOf('--append-system-prompt');
  appendFileSync('calls.log', JSON.stringify({ text, resume: resume >= 0 ? args[resume + 1] : null, prompt: prompt >= 0 ? args[prompt + 1] : null }) + '\n');
  out({ type: 'system', subtype: 'init', session_id });
  if (text.includes('CRASH')) { process.stderr.write('Invalid API key · Please run /login\n'); process.exit(1); }
  if (text.includes('LONG') || text.includes('STUBBORN')) { busy = text; return; }
  out({ type: 'result', is_error: false, result: `ANSWER ${text}`, session_id });
}).on('close', () => process.exit(0));
