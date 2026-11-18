import { expect, test } from 'bun:test';
import { chatHistory } from './web-history';

test('keeps all chat event types and discards protocol noise', () => {
  const events = [{type:'user'}, {type:'result'}, {type:'error'}, {type:'cancelled'}, {method:'item/completed',params:{item:{type:'agentMessage'}}}, {method:'item/completed',params:{item:{type:'commandExecution'}}}, {method:'item/agentMessage/delta'}, null];
  expect(chatHistory(events).map(e => e.type || e.method)).toEqual(['user','result','error','cancelled','item/completed']);
  expect(chatHistory([{type:'result',result:'ok', noisy:'secret'}])).toEqual([{type:'result',result:'ok'}]);
});
