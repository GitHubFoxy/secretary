import { expect, test } from 'bun:test';
import { parseClaudeModels } from './models';

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
