const WEEK_MINS = 7 * 24 * 60;

// Как в T3 Code: пока исчерпан недельный лимит, 5-часовое окно тоже недоступно, хотя Codex сообщает его свободным.
// Окно откроется со сбросом недели, а если оно исчерпано само и сбрасывается позже — с собственным сбросом.
export function blockedBySpentWeek(snapshot, now) {
  const windows = [snapshot.primary, snapshot.secondary].filter(Boolean);
  const week = windows.find(w => w.windowDurationMins >= WEEK_MINS && w.usedPercent >= 100 && w.resetsAt * 1000 > now);
  if (!week) return windows;
  return windows.map(w => {
    if (w === week || w.windowDurationMins >= WEEK_MINS) return w;
    const resetsAt = w.usedPercent >= 100 && w.resetsAt > week.resetsAt ? w.resetsAt : week.resetsAt;
    return { ...w, usedPercent: 100, resetsAt };
  });
}
