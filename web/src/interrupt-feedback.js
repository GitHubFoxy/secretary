export function createInterruptFeedback({onChange, schedule = setTimeout, cancel = clearTimeout}) {
  const entries = new Map();
  function update(id, entry) { entries.set(id, entry); onChange(id, entry); }
  function clearTimers(entry) { if (entry?.delay != null) cancel(entry.delay); if (entry?.timeout != null) cancel(entry.timeout); }
  function find(id, requestId) { const entry = entries.get(id); return entry?.requestId === requestId ? entry : null; }
  return {
    start(id, requestId) {
      clearTimers(entries.get(id));
      const entry = {requestId, pending:true, message:'Запрос на остановку отправлен…'};
      entry.delay = schedule(() => { if (find(id, requestId)?.pending) update(id, {...entries.get(id), message:'Остановка занимает больше времени…'}); }, 3000);
      entry.timeout = schedule(() => { const current = find(id, requestId); if (current?.pending) { clearTimers(current); update(id, {...current, pending:false, message:'Не удалось подтвердить остановку. Проверьте состояние сессии.'}); } }, 20000);
      update(id, entry);
    },
    ack(id, requestId, accepted) {
      const entry = find(id, requestId);
      if (!entry || accepted) return;
      clearTimers(entry);
      update(id, {...entry, pending:false, message:'Ход уже завершился или запрос остановки выполняется.'});
    },
    result(id, requestId, outcome) {
      const entry = find(id, requestId);
      if (!entry) return;
      clearTimers(entry);
      const message = outcome === 'completed' ? 'Ход завершился до подтверждения остановки.' : outcome === 'ended' ? 'Ход завершён после запроса остановки.' : 'Результат остановки неизвестен.';
      update(id, {...entry, pending:false, message});
    },
    fail(id, requestId, message) {
      const entry = find(id, requestId);
      if (!entry) return;
      clearTimers(entry);
      update(id, {...entry, pending:false, message:`Не удалось запросить остановку: ${message}`});
    },
    disconnect() {
      for (const [id, entry] of entries) if (entry.pending) {
        clearTimers(entry);
        update(id, {...entry, pending:false, message:'Связь прервалась. Результат остановки неизвестен.'});
      }
    },
    clear(id) { clearTimers(entries.get(id)); entries.delete(id); onChange(id, null); },
    dispose() { for (const entry of entries.values()) clearTimers(entry); entries.clear(); },
  };
}
