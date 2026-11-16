export function parseClaudeModels(output: string) {
  for (const line of output.split('\n')) {
    if (!line.trim()) continue;
    const msg = JSON.parse(line);
    if (msg.type !== 'control_response' || msg.response?.request_id !== 'models') continue;
    if (msg.response.subtype !== 'success') throw new Error('Claude Code не вернул список моделей');
    const list = msg.response.response?.models;
    if (!Array.isArray(list) || !list.length) throw new Error('Claude Code вернул пустой список моделей');
    return list.map((model: any) => ({
      id: model.value,
      label: model.resolvedModel ? `${model.displayName} (${model.resolvedModel})` : model.displayName || model.value,
      supportedReasoningEfforts: (model.supportedEffortLevels || []).map((reasoningEffort: string) => ({reasoningEffort})),
    }));
  }
  throw new Error('Claude Code не ответил на запрос списка моделей');
}

export async function claudeModelDetails() {
  const p = Bun.spawn(['claude', '-p', '--input-format', 'stream-json', '--output-format', 'stream-json', '--verbose'],
    {stdin:'pipe', stdout:'pipe', stderr:'ignore', signal: AbortSignal.timeout(10000)});
  try {
    p.stdin.write(JSON.stringify({type:'control_request', request_id:'models', request:{subtype:'initialize'}}) + '\n');
    p.stdin.end();
    return parseClaudeModels(await new Response(p.stdout).text());
  } finally { p.kill(); }
}

export async function modelDetails(): Promise<{id:string, supportedReasoningEfforts:{reasoningEffort:string}[]}[]> {
  const p = Bun.spawn(['codex', 'app-server'], { stdin: 'pipe', stdout: 'pipe', stderr: 'ignore' });
  const send = (message: object) => { p.stdin.write(JSON.stringify(message) + '\n'); p.stdin.flush(); };
  send({id:1,method:'initialize',params:{clientInfo:{name:'secretary',version:'0.1'},capabilities:null}});
  send({method:'initialized'});
  send({id:2,method:'model/list',params:{}});
  let buffer = '';
  try {
    for await (const chunk of p.stdout) {
      buffer += new TextDecoder().decode(chunk, {stream:true});
      let end: number;
      while ((end = buffer.indexOf('\n')) >= 0) {
        const line = buffer.slice(0,end); buffer = buffer.slice(end+1);
        if (!line.trim()) continue;
        const msg = JSON.parse(line);
        if (msg.id === 2) { if (msg.error) throw new Error(msg.error.message); return msg.result.data; }
      }
    }
    throw new Error('Codex exited');
  } finally { p.kill(); }
}

export async function models(): Promise<string[]> { return (await modelDetails()).map(m => m.id); }
