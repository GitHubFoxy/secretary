// Тихое состояние отправки: сообщение сразу видно как своё, статус — на нём самом.
// Отдельной плашки очереди нет, дублирующего «Пишет ответ…» рядом с ожидающим — тоже.
// Кнопка «Отправить сейчас» — только fallback при реальной проблеме, не в штатном пути.

// Через столько очередь считается зависшей: только тогда показываем fallback-кнопку.
export const SEND_NOW_DELAY_MS = 30000;

// Первая встреча с каждым пунктом очереди: тихий штамп для fallback-таймера.
// Чистая функция ради тестов: входящий Map не трогаем, при изменениях возвращаем новый.
export function trackQueueSeen(previous, queue, nowMs) {
  const prev = previous instanceof Map ? previous : new Map();
  const ids = new Set((Array.isArray(queue) ? queue : []).map(item => item?.id));
  let changed = false;
  const next = new Map(prev);
  for (const id of ids) {
    if (id === undefined || next.has(id)) continue;
    next.set(id, nowMs);
    changed = true;
  }
  for (const id of [...next.keys()]) {
    if (!ids.has(id)) { next.delete(id); changed = true; }
  }
  return changed ? next : prev;
}

// Подпись очереди на самом сообщении, тихо: срочное прерывает ход, обычное ждёт.
export function queuedCaption(urgent) {
  return urgent ? 'Отправляется…' : 'В очереди…';
}

// Fallback-кнопка «Отправить сейчас»: только реальная проблема, не штатный путь.
// Штатно (ход идёт, очередь свежая) — false. Показываем, когда:
// - очередь зависла дольше SEND_NOW_DELAY_MS, либо
// - сессия уже не работает (stopped/interrupted), а сообщение всё ещё в очереди.
// Срочные не продвигаем: они и так идут следующими после прерывания.
export function shouldShowSendNow({ urgent, firstSeen, now, sessionStatus, delayMs = SEND_NOW_DELAY_MS } = {}) {
  if (urgent) return false;
  if (!Number.isFinite(firstSeen)) return false;
  const current = Number.isFinite(now) ? now : firstSeen;
  if (sessionStatus === 'stopped' || sessionStatus === 'interrupted') return true;
  return current - firstSeen >= delayMs;
}

// Общий индикатор «Пишет ответ…/Думает…» не дублируем рядом с ожидающим сообщением:
// пока есть неотправленное (готовится или в очереди), статуса на нём достаточно.
export function shouldShowActivity({ sessionStatus, live, pendingCount } = {}) {
  if (sessionStatus !== 'running') return false;
  if (live) return false;
  return !(Number(pendingCount) > 0);
}
