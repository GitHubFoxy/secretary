import { expect, test } from 'bun:test';
import { parseClaudeModels, parseOpencodeModels } from './models';

test('читает модели и доступные уровни из ответа Claude Code', () => {
  const output = JSON.stringify({type:'control_response', response:{subtype:'success', request_id:'models', response:{models:[
    {value:'default', displayName:'Default', resolvedModel:'claude-opus-5-5', supportedEffortLevels:['low','high']},
    {value:'opus', displayName:'Opus', resolvedModel:'claude-opus-5-5', supportedEffortLevels:['low','high']},
    {value:'sonnet', displayName:'Sonnet', resolvedModel:'claude-sonnet-5-5', supportedEffortLevels:['low']},
    {value:'legacy', displayName:'Legacy'},
  ]}}});
  expect(parseClaudeModels(output)).toEqual([
    {id:'claude-opus-5-5', aliases:['default', 'opus'], supportedReasoningEfforts:[{reasoningEffort:'low'}, {reasoningEffort:'high'}]},
    {id:'claude-sonnet-5-5', aliases:['sonnet'], supportedReasoningEfforts:[{reasoningEffort:'low'}]},
  ]);
  expect(() => parseClaudeModels('')).toThrow();
  expect(() => parseClaudeModels(JSON.stringify({type:'control_response', response:{subtype:'success',request_id:'models',response:{models:[]}}}))).toThrow();
  expect(() => parseClaudeModels(JSON.stringify({type:'control_response', response:{subtype:'success',request_id:'models',response:{models:[{value:'legacy', displayName:'Legacy'}]}}}))).toThrow();
});

test('уровни effort у моделей OpenCode разные, default не уровень', () => {
  expect(parseOpencodeModels(
    ['pag/beam-free', 'pag/deepseek-v4-pro', 'pag/unknown'],
    { 'pag/beam-free': ['low', 'medium', 'high', 'xhigh', 'max', 'default'], 'pag/deepseek-v4-pro': ['high', 'max', 'default'] },
  )).toEqual([
    { id: 'pag/beam-free', supportedReasoningEfforts: [{ reasoningEffort: 'low' }, { reasoningEffort: 'medium' }, { reasoningEffort: 'high' }, { reasoningEffort: 'xhigh' }, { reasoningEffort: 'max' }] },
    { id: 'pag/deepseek-v4-pro', supportedReasoningEfforts: [{ reasoningEffort: 'high' }, { reasoningEffort: 'max' }] },
    { id: 'pag/unknown', supportedReasoningEfforts: [] },
  ]);
});
