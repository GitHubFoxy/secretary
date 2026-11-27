export function createDrafts({request, selected, setText, onError}) {
  const from = crypto.randomUUID();
  const pending = new Map();
  let token = 0;

  function save(id) {
    const entry = pending.get(id);
    if (!entry) return;
    clearTimeout(entry.timer);
    entry.timer = null;
    request({type:'draftSave', id, text:entry.text, from}).then(() => {
      if (pending.get(id) === entry) pending.delete(id);
    }).catch(err => onError(`Не удалось сохранить черновик: ${err.message}`));
  }
  function schedule(text) {
    const id = selected();
    if (id === null) return;
    clearTimeout(pending.get(id)?.timer);
    const entry = {text, timer:null};
    pending.set(id, entry);
    entry.timer = setTimeout(() => save(id), 500);
    token++;
  }
  async function load(id) {
    const current = ++token;
    try {
      const result = await request({type:'draft', id});
      if (selected() === id && current === token) setText(result.text);
    } catch (err) { if (!err.disconnected) onError(err.message); }
  }
  return {
    get busy() { return pending.size > 0; },
    schedule,
    save,
    select(id) {
      if (pending.has(id)) { token++; setText(pending.get(id).text); }
      else load(id);
    },
    reconnect() {
      for (const id of pending.keys()) save(id);
      const id = selected();
      if (id !== null && !pending.has(id)) load(id);
    },
    receive(msg, editing) {
      if (msg.type !== 'draftChanged' || msg.sessionId !== selected() || msg.from === from || editing) return;
      token++;
      clearTimeout(pending.get(msg.sessionId)?.timer);
      pending.delete(msg.sessionId);
      setText(msg.text);
    },
  };
}
