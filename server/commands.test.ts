import { expect, test } from 'bun:test';
import { parseClaudeCommands, parseCodexSkills, parseOpencodeCommands } from './commands';

test('команды Claude исключают compact и ошибки не скрываются', () => {
  expect(parseClaudeCommands({response:{subtype:'success', response:{commands:[{name:'compact'}, {name:'test', description:'описание'}]}}})).toEqual([{name:'test', description:'описание'}]);
  expect(() => parseClaudeCommands({response:{subtype:'error'}})).toThrow();
});

test('Codex возвращает только включённые skills с путями', () => {
  expect(parseCodexSkills({data:[{skills:[{name:'one', path:'/one', enabled:true}, {name:'two', path:'/two', enabled:false}]}]})).toEqual([{name:'one', path:'/one', description:''}]);
  expect(() => parseCodexSkills({data:[{errors:[{message:'ошибка'}]}]})).toThrow('ошибка');
});

test('OpenCode возвращает команды кроме compact, skills выполняет сам', () => {
  expect(parseOpencodeCommands({availableCommands:[{name:'compact', description:'сжать'}, {name:'review', description:'ревью'}, {name:'x'}]})).toEqual([{name:'review', description:'ревью'}, {name:'x', description:''}]);
  expect(() => parseOpencodeCommands({})).toThrow();
});
