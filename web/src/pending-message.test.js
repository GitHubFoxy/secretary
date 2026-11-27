import { test, expect } from 'bun:test';
import { SEND_NOW_DELAY_MS, queuedCaption, shouldShowActivity, shouldShowSendNow, trackQueueSeen } from './pending-message.js';

test('отправка: готовится — общий индикатор скрыт, кнопка не нужна', () => {
  // Сообщение только отправлено (preparing/sending), очереди от сервера ещё нет.
  expect(shouldShowActivity({ sessionStatus: 'running', live: false, pendingCount: 1 })).toBe(false);
  expect(shouldShowActivity({ sessionStatus: 'idle', live: false, pendingCount: 1 })).toBe(false);
});

test('очередь: свежая — тихо, без кнопки и без дублирующего индикатора', () => {
  const now = 1_000_000;
  const seen = trackQueueSeen(new Map(), [{ id: 7 }], now);
  expect(seen.get(7)).toBe(now);
  expect(queuedCaption(false)).toBe('В очереди…');
  expect(queuedCaption(true)).toBe('Отправляется…');
  expect(shouldShowSendNow({ urgent: false, firstSeen: seen.get(7), now, sessionStatus: 'running' })).toBe(false);
  expect(shouldShowSendNow({ urgent: true, firstSeen: seen.get(7), now, sessionStatus: 'running' })).toBe(false);
  expect(shouldShowActivity({ sessionStatus: 'running', live: false, pendingCount: 1 })).toBe(false);
});

test('очередь: долгое ожидание — fallback-кнопка появляется, механика очереди цела', () => {
  const firstSeen = 1_000_000;
  const late = firstSeen + SEND_NOW_DELAY_MS;
  // Штатный путь: свежая очередь при идущем ходе — кнопки нет.
  expect(shouldShowSendNow({ urgent: false, firstSeen, now: firstSeen + 1000, sessionStatus: 'running' })).toBe(false);
  // Зависла: та же очередь через порог — кнопка есть (повтор через sendQueued preserved).
  expect(shouldShowSendNow({ urgent: false, firstSeen, now: late, sessionStatus: 'running' })).toBe(true);
  // Срочное не продвигаем даже при ожидании: оно и так следующее после прерывания.
  expect(shouldShowSendNow({ urgent: true, firstSeen, now: late + 60_000, sessionStatus: 'running' })).toBe(false);
});

test('очередь: сессия упала — кнопка сразу, без ожидания порога', () => {
  const firstSeen = 2_000_000;
  expect(shouldShowSendNow({ urgent: false, firstSeen, now: firstSeen + 500, sessionStatus: 'interrupted' })).toBe(true);
  expect(shouldShowSendNow({ urgent: false, firstSeen, now: firstSeen + 500, sessionStatus: 'stopped' })).toBe(true);
  expect(shouldShowSendNow({ urgent: false, firstSeen, now: firstSeen + 500, sessionStatus: 'idle' })).toBe(false);
});

test('ответ: очередь ушла в историю — индикатор снова можно, кнопки нет', () => {
  let seen = trackQueueSeen(new Map(), [{ id: 7 }], 1_000_000);
  seen = trackQueueSeen(seen, [], 1_001_000);
  expect(seen.has(7)).toBe(false);
  // История получила user-сообщение, pending пуст: обычный индикатор хода разрешён.
  expect(shouldShowActivity({ sessionStatus: 'running', live: false, pendingCount: 0 })).toBe(true);
  expect(shouldShowActivity({ sessionStatus: 'running', live: true, pendingCount: 0 })).toBe(false);
  expect(shouldShowActivity({ sessionStatus: 'idle', live: false, pendingCount: 0 })).toBe(false);
});

test('ошибка: cancelled/uncertain живут в истории, очередь их не дублирует', () => {
  // Ошибка доставки — это kind истории, а не очередь: трекер очереди пуст, кнопка не нужна.
  const seen = trackQueueSeen(new Map(), [], 3_000_000);
  expect(seen.size).toBe(0);
  expect(shouldShowSendNow({ urgent: false, firstSeen: NaN, now: 3_000_000, sessionStatus: 'running' })).toBe(false);
  expect(shouldShowActivity({ sessionStatus: 'interrupted', live: false, pendingCount: 0 })).toBe(false);
});
