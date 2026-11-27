import { expect, test } from 'bun:test';
import { HISTORY_INITIAL_LIMIT, HISTORY_VISIBLE_TAIL, HISTORY_RETRY_DELAYS, retryDelay, initialWindow, shouldPrefetch, mergeOlder, needsInitialTopup } from './history-prefetch.js';

const messages = (...ids) => ids.map(cursor => ({cursor, text:`сообщение ${cursor}`}));

test('видимое окно — последние 10 из 30 без мутации исходника', () => {
  const history = messages(...Array.from({length: HISTORY_INITIAL_LIMIT}, (_, i) => i + 1));
  const windowed = initialWindow(history);
  expect(windowed.length).toBe(HISTORY_VISIBLE_TAIL);
  expect(windowed.map(m => m.cursor)).toEqual([21, 22, 23, 24, 25, 26, 27, 28, 29, 30]);
  expect(history.length).toBe(HISTORY_INITIAL_LIMIT);
});

test('видимое окно короткой истории — всё, что есть', () => {
  expect(initialWindow(messages(1, 2, 3)).map(m => m.cursor)).toEqual([1, 2, 3]);
  expect(initialWindow([])).toEqual([]);
});

test('предзагрузка срабатывает только у верха и только когда есть что грузить', () => {
  const idle = {connected:true, hasMore:true, loading:false, loadingOlder:false, scrollTop:0};
  expect(shouldPrefetch(idle)).toBe(true);
  expect(shouldPrefetch({...idle, scrollTop:600})).toBe(true);
  expect(shouldPrefetch({...idle, scrollTop:601})).toBe(false);
  expect(shouldPrefetch({...idle, connected:false})).toBe(false);
  expect(shouldPrefetch({...idle, hasMore:false})).toBe(false);
  expect(shouldPrefetch({...idle, loading:true})).toBe(false);
  expect(shouldPrefetch({...idle, loadingOlder:true})).toBe(false);
});

test('запас держится: порог покрывает ~10 сообщений', () => {
  // Порог в px должен срабатывать задолго до самого верха (scrollTop 0),
  // чтобы при быстрой прокрутке оставалось время на догрузку.
  expect(shouldPrefetch({connected:true, hasMore:true, loading:false, loadingOlder:false, scrollTop:599})).toBe(true);
  expect(shouldPrefetch({connected:true, hasMore:true, loading:false, loadingOlder:false, scrollTop:601})).toBe(false);
});

test('автоповтор: пауза растёт и упирается в потолок', () => {
  expect(retryDelay(0)).toBe(HISTORY_RETRY_DELAYS[0]);
  expect(retryDelay(1)).toBe(HISTORY_RETRY_DELAYS[1]);
  expect(retryDelay(2)).toBe(HISTORY_RETRY_DELAYS[2]);
  expect(retryDelay(5)).toBe(HISTORY_RETRY_DELAYS[HISTORY_RETRY_DELAYS.length - 1]);
  expect(retryDelay(99)).toBe(HISTORY_RETRY_DELAYS[HISTORY_RETRY_DELAYS.length - 1]);
  const delays = [0, 1, 2, 3].map(retryDelay);
  expect([...delays].sort((a, b) => a - b)).toEqual(delays);
});

test('склейка older-страниц без дублей и по порядку', () => {
  const merged = mergeOlder(messages(21, 22, 23), messages(19, 20, 21, 22));
  expect(merged.map(m => m.cursor)).toEqual([19, 20, 21, 22, 23]);
});

test('дотяжка первички до 30 нужна, пока окно не заполнено', () => {
  expect(needsInitialTopup(messages(11, 12), true)).toBe(true);
  expect(needsInitialTopup(messages(...Array.from({length: 30}, (_, i) => i + 1)), true)).toBe(false);
  expect(needsInitialTopup(messages(11, 12), false)).toBe(false);
  expect(needsInitialTopup([], true)).toBe(false);
});
