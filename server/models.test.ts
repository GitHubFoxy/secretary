import { expect, test } from 'bun:test';
import { parseClaudeModels } from './models';

test('читает модели и доступные уровни из ответа Claude Code', () => {
  const output = JSON.stringify({type:'control_response', response:{subtype:'success', request_id:'models', response:{models:[
    {value:'opus', displayName:'Opus', resolvedModel:'claude-opus-5-5', supportedEffortLevels:['low','high']},
    {value:'legacy', displayName:'Legacy'},
  ]}}});
  expect(parseClaudeModels(output)).toEqual([
    {id:'opus', label:'Opus (claude-opus-5-5)', supportedReasoningEfforts:[{reasoningEffort:'low'}, {reasoningEffort:'high'}]},
    {id:'legacy', label:'Legacy', supportedReasoningEfforts:[]},
  ]);
  expect(() => parseClaudeModels('')).toThrow();
  expect(() => parseClaudeModels(JSON.stringify({type:'control_response', response:{subtype:'success',request_id:'models',response:{models:[]}}}))).toThrow();
});
