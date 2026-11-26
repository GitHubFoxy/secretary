import { test, expect } from 'bun:test';
import { shouldNotify } from './browser-notifications.js';
test('уведомление только для непустого ответа в скрытой вкладке с разрешением', () => {
  const state = {enabled:true, permission:'granted', text:'Ответ', visibility:'hidden', native:false};
  expect(shouldNotify(state)).toBe(true);
  for (const change of [{enabled:false}, {permission:'denied'}, {text:'  '}, {visibility:'visible'}, {native:true}]) expect(shouldNotify({...state, ...change})).toBe(false);
});
