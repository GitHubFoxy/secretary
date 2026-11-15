export async function models(): Promise<string[]> {
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
        if (msg.id === 2) { if (msg.error) throw new Error(msg.error.message); return msg.result.data.map((m: any) => m.id); }
      }
    }
    throw new Error('Codex exited');
  } finally { p.kill(); }
}
