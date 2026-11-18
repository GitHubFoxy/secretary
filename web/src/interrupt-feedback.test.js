import { expect, test } from 'bun:test';
import { createInterruptFeedback } from './interrupt-feedback.js';

function fixture() {
  const states = new Map(), timers = new Map();
  let next = 0;
  const feedback = createInterruptFeedback({
    onChange: (id, state) => states.set(id, state),
    schedule: (callback, delay) => { const id = ++next; timers.set(id, {callback, delay}); return id; },
    cancel: id => timers.delete(id),
  });
  return {feedback, states, timers, fire: delay => { for (const [id, timer] of [...timers]) if (timer.delay === delay) { timers.delete(id); timer.callback(); } }};
}

test('запрос, задержка и результат не сообщают об успехе до завершения хода', () => {
  const {feedback, states, fire, timers} = fixture();
  feedback.start(1, 'a');
  expect(states.get(1)).toMatchObject({pending:true, message:'Запрос на остановку отправлен…'});
  feedback.ack(1, 'a', true);
  expect(states.get(1).pending).toBe(true);
  fire(3000);
  expect(states.get(1).message).toBe('Остановка занимает больше времени…');
  feedback.result(1, 'a', 'ended');
  expect(states.get(1)).toMatchObject({pending:false, message:'Ход завершён после запроса остановки.'});
  expect(timers.size).toBe(0);
});

test('потеря связи, ошибка и таймаут не утверждают, что ход остановлен', () => {
  const {feedback, states, fire} = fixture();
  feedback.start(1, 'a'); feedback.disconnect();
  expect(states.get(1)).toMatchObject({pending:false, message:'Связь прервалась. Результат остановки неизвестен.'});
  feedback.start(1, 'b'); feedback.fail(1, 'b', 'ошибка');
  expect(states.get(1)).toMatchObject({pending:false, message:'Не удалось запросить остановку: ошибка'});
  feedback.start(1, 'c'); fire(20000);
  expect(states.get(1)).toMatchObject({pending:false, message:'Не удалось подтвердить остановку. Проверьте состояние сессии.'});
});

test('ответы разных запросов и сессий не смешиваются', () => {
  const {feedback, states} = fixture();
  feedback.start(1, 'old'); feedback.start(1, 'new'); feedback.start(2, 'other');
  feedback.result(1, 'old', 'ended');
  expect(states.get(1)).toMatchObject({requestId:'new', pending:true});
  feedback.ack(2, 'other', false);
  expect(states.get(2)).toMatchObject({pending:false, message:'Ход уже завершился или запрос остановки выполняется.'});
  feedback.result(1, 'new', 'completed');
  expect(states.get(1).message).toBe('Ход завершился до подтверждения остановки.');
  feedback.clear(1);
  expect(states.get(1)).toBeNull();
});
