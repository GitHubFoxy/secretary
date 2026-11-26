import { cpSync, existsSync, mkdirSync, readFileSync, readdirSync, rmSync, statSync, writeFileSync } from 'node:fs';
import { createHash, randomUUID } from 'node:crypto';

const valid = (name: string) => {
  if (typeof name !== 'string' || !/^[a-z0-9]+(?:-[a-z0-9]+)*$/.test(name)) throw new Error('Некорректное имя skill');
  return name;
};
const hash = (s: string | null) => createHash('sha256').update(JSON.stringify(s)).digest('hex');
export class Skills {
  constructor(private root: string, private emit: (session: number, event: object) => void = () => {}) {}
  private file(name: string) { return `${this.root}/skills/${valid(name)}/SKILL.md`; }
  private proposal(id: string) {
    if (!/^[a-f0-9-]{36}$/.test(id)) throw new Error('Некорректный id');
    return `${this.root}/skill-proposals/${id}.json`;
  }
  list() { const dir = `${this.root}/skills`; return existsSync(dir) ? readdirSync(dir).filter(n => /^[a-z0-9]+(?:-[a-z0-9]+)*$/.test(n) && existsSync(this.file(n))) : []; }
  read(name: string) { const file = this.file(name); return existsSync(file) ? readFileSync(file, 'utf8') : null; }
  propose(session: number, name: string, content: string, reason: string) {
    valid(name);
    if (typeof content !== 'string' || !content.trim() || typeof reason !== 'string' || !reason.trim()) throw new Error('Укажите содержимое и причину');
    const before = this.read(name), id = randomUUID();
    const proposal = { id, session, name, content, reason, base: hash(before) };
    mkdirSync(`${this.root}/skill-proposals`, { recursive: true });
    writeFileSync(this.proposal(id), JSON.stringify(proposal), { flag: 'wx' });
    if (session) this.emit(session, { type: 'skill_request', id, name, reason, before: before ?? '', content });
    return { id };
  }
  private load(id: string) { return JSON.parse(readFileSync(this.proposal(id), 'utf8')); }
  approve(id: string) {
    const p = this.load(id), previous = this.read(p.name);
    if (hash(previous) !== p.base) throw new Error('Skill изменился после предложения; предложите новую версию');
    const file = this.file(p.name);
    mkdirSync(`${this.root}/skills/${p.name}/versions`, { recursive: true });
    if (previous !== null) writeFileSync(`${this.root}/skills/${p.name}/versions/${Date.now()}-${randomUUID()}.md`, previous, { flag: 'wx' });
    writeFileSync(file, p.content);
    this.sync();
    rmSync(this.proposal(id));
    if (p.session) this.emit(p.session, { type: 'skill_result', id, status: 'approved' });
  }
  reject(id: string) { const p = this.load(id); rmSync(this.proposal(id)); this.emit(p.session, { type: 'skill_result', id, status: 'rejected' }); }
  rollback(session: number, name: string, reason = 'Восстановить предыдущую версию') {
    const dir = `${this.root}/skills/${valid(name)}/versions`;
    const versions = existsSync(dir) ? readdirSync(dir).filter(n => n.endsWith('.md')).sort() : [];
    if (!versions.length) throw new Error('Нет предыдущей версии');
    return this.propose(session, name, readFileSync(`${dir}/${versions.at(-1)}`, 'utf8'), reason);
  }
  versions(name: string) {
    const dir = `${this.root}/skills/${valid(name)}/versions`;
    return existsSync(dir) ? readdirSync(dir).filter(n => /^\d+-[a-f0-9-]{36}\.md$/.test(n)).sort().reverse() : [];
  }
  details(name: string) {
    const content = this.read(name);
    if (content === null) throw new Error('Skill не найден');
    return { name, content, date: statSync(this.file(name)).mtime.toISOString(), versions: this.versions(name), builtin: existsSync(new URL(`./skills/${name}/SKILL.md`, import.meta.url)) };
  }
  restore(name: string, version: string) {
    if (!this.versions(name).includes(version) || this.read(name) === null) throw new Error('Версия не найдена');
    const content = readFileSync(`${this.root}/skills/${valid(name)}/versions/${version}`, 'utf8');
    const proposal = this.propose(0, name, content, 'Восстановить версию');
    this.approve(proposal.id);
  }
  delete(name: string) {
    if (existsSync(new URL(`./skills/${valid(name)}/SKILL.md`, import.meta.url))) throw new Error('Встроенный skill нельзя удалить');
    if (this.read(name) === null) throw new Error('Skill не найден');
    rmSync(`${this.root}/skills/${valid(name)}`, { recursive: true });
    for (const harness of ['.claude', '.agents']) rmSync(`${this.root}/${harness}/skills/${name}`, { recursive: true, force: true });
  }
  sync() {
    for (const name of this.list()) for (const harness of ['.claude', '.agents']) {
      const dest = `${this.root}/${harness}/skills/${name}`;
      mkdirSync(dest, { recursive: true });
      cpSync(this.file(name), `${dest}/SKILL.md`);
    }
  }
}
export function syncSkills(root: string) {
  const skills = new Skills(root);
  const builtin = new URL('./skills/', import.meta.url);
  for (const name of readdirSync(builtin)) {
    const source = new URL(`${name}/SKILL.md`, builtin);
    if (!existsSync(source) || skills.read(name) !== null) continue;
    mkdirSync(`${root}/skills/${name}`, { recursive: true });
    const existing = `${root}/.claude/skills/${name}/SKILL.md`;
    cpSync(existsSync(existing) ? existing : source, `${root}/skills/${name}/SKILL.md`);
  }
  skills.sync();
}
