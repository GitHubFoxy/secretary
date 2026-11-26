import { expect, test } from 'bun:test';
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Skills, syncSkills } from './skills';

test('предложения подтверждаются по исходной версии, сохраняют историю и синхронизируются', () => {
  const root = mkdtempSync(join(tmpdir(), 'skills-'));
  try {
    syncSkills(root);
    const events: any[] = [];
    const skills = new Skills(root, (_, event) => events.push(event));
    expect(skills.list()).toContain('day-summary');
    const first = skills.propose(1, 'example', 'первая', 'причина');
    expect(skills.read('example')).toBeNull();
    skills.approve(first.id);
    expect(readFileSync(`${root}/.claude/skills/example/SKILL.md`, 'utf8')).toBe('первая');
    expect(readFileSync(`${root}/.agents/skills/example/SKILL.md`, 'utf8')).toBe('первая');
    const stale = skills.propose(1, 'example', 'устарело', 'причина');
    const second = skills.propose(1, 'example', 'вторая', 'причина');
    new Skills(root).approve(second.id);
    expect(() => skills.approve(stale.id)).toThrow();
    skills.reject(stale.id);
    const rollback = skills.rollback(1, 'example');
    skills.approve(rollback.id);
    expect(skills.read('example')).toBe('первая');
    expect(events.at(-1).status).toBe('approved');
    expect(() => skills.propose(1, '../escape', 'x', 'x')).toThrow();
    writeFileSync(`${root}/skills/day-summary/SKILL.md`, 'личная версия');
    syncSkills(root);
    expect(skills.read('day-summary')).toBe('личная версия');
  } finally { rmSync(root, { recursive: true, force: true }); }
});
