import { randomUUID } from 'node:crypto';
import { basename, dirname, join } from 'node:path';

async function git(cwd: string, ...args: string[]) {
  const process = Bun.spawn(['git', '-C', cwd, ...args], { stdout: 'pipe', stderr: 'pipe' });
  const [out, err, code] = await Promise.all([new Response(process.stdout).text(), new Response(process.stderr).text(), process.exited]);
  if (code !== 0) throw new Error(err.trim() || 'Ошибка Git');
  return out.trim();
}

export async function createWorktree(folder: string) {
  const root = await git(folder, 'rev-parse', '--show-toplevel');
  const head = await git(folder, 'rev-parse', '--verify', 'HEAD^{commit}');
  const id = randomUUID();
  const branch = `secretary/worker-${id}`;
  const path = join(dirname(root), `${basename(root)}-worker-${id}`);
  await git(root, 'worktree', 'add', '-b', branch, path, head);
  return { folder: path, branch, async remove() {
    await git(root, 'worktree', 'remove', path);
    await git(root, 'branch', '-d', branch);
  } };
}
