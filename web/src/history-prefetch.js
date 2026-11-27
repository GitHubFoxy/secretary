// Окно умной предзагрузки истории: при открытии чата грузим сразу
// HISTORY_INITIAL_LIMIT сообщений (показываем хвост HISTORY_VISIBLE_TAIL),
// дальше фоном держим запас HISTORY_PREFETCH_THRESHOLD_PX px сверху —
// это ~10 сообщений и даёт время на догрузку даже при быстрой прокрутке.
export const HISTORY_INITIAL_LIMIT = 30;
export const HISTORY_VISIBLE_TAIL = 10;
export const HISTORY_PREFETCH_THRESHOLD_PX = 600;
export const HISTORY_RETRY_DELAYS = [800, 2000, 5000];

// Экспоненциальная пауза тихого автоповтора (без кнопки).
export function retryDelay(attempt) {
  const index = Number.isFinite(attempt) && attempt > 0 ? Math.floor(attempt) : 0;
  return HISTORY_RETRY_DELAYS[Math.min(index, HISTORY_RETRY_DELAYS.length - 1)];
}

// Видимое окно из загруженной истории: всегда показываем последние сообщения.
export function initialWindow(messages, tail = HISTORY_VISIBLE_TAIL) {
  if (!Array.isArray(messages)) return [];
  return messages.slice(-Math.max(0, tail));
}

// Пора ли фоном догружать older: пользователь приблизился к верху списка.
export function shouldPrefetch({connected, hasMore, loading, loadingOlder, scrollTop, threshold = HISTORY_PREFETCH_THRESHOLD_PX}) {
  return Boolean(connected && hasMore && !loading && !loadingOlder && (scrollTop ?? Infinity) <= threshold);
}

// Склейка older-страницы без дублей (по cursor), по возрастанию.
export function mergeOlder(history, page) {
  const current = Array.isArray(history) ? history : [];
  const older = Array.isArray(page) ? page : [];
  const known = new Set(current.map(message => message?.cursor));
  const merged = current.slice();
  for (const message of older) {
    if (message && !known.has(message.cursor)) { merged.push(message); known.add(message.cursor); }
  }
  merged.sort((a, b) => a.cursor - b.cursor);
  return merged;
}

// Нужна ли дотяжка первички до полного окна (второй read при открытии чата).
export function needsInitialTopup(history, hasMore, limit = HISTORY_INITIAL_LIMIT) {
  const length = Array.isArray(history) ? history.length : 0;
  return Boolean(hasMore && length > 0 && length < limit);
}
