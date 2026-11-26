export async function findFiles(folder: string, query: string): Promise<string[]> {
  if (typeof query !== 'string' || query.length > 200 || query.includes('\n') || query.includes('..')) throw new Error('Некорректный поиск');
  const p = Bun.spawn(['rg', '--files', '--hidden', '-g', '!.git', '-0'], {cwd:folder, stdout:'pipe', stderr:'ignore', signal:AbortSignal.timeout(5000)});
  const found: string[] = [];
  const decoder = new TextDecoder();
  let buffer = '';
  try {
    for await (const chunk of p.stdout) {
      buffer += decoder.decode(chunk, {stream:true});
      let end: number;
      while ((end = buffer.indexOf('\0')) >= 0) {
        const path = buffer.slice(0, end); buffer = buffer.slice(end + 1);
        if (path.toLocaleLowerCase().includes(query.toLocaleLowerCase())) found.push(path);
        if (found.length >= 30) return found;
      }
    }
    if (await p.exited !== 0 && p.exitCode !== 1) throw new Error('Не удалось найти файлы');
    return found;
  } finally { p.kill(); }
}
