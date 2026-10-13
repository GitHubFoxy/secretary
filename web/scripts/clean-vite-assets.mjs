import { readFile, writeFile } from 'node:fs/promises';
import { join } from 'node:path';

const directory = process.argv[2];
if (!directory) throw new Error('asset directory is required');
for (const name of ['app.js', 'app.css']) {
  const path = join(directory, name);
  let content = await readFile(path, 'utf8');
  content = content.replaceAll('` \t\n', '`\\x20\\t\n');
  await writeFile(path, content);
}
