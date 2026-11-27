import { expect, test } from 'bun:test';
import { createDrafts } from './drafts.js';

test('черновики синхронизируются между вкладками и сохраняются после переподключения', async () => {
  const store = new Map(), tabs = [];
  let connected = true;
  function tab() {
    let selected = 1, text = '';
    const draft = createDrafts({
      selected: () => selected,
      setText: value => { text = value; },
      onError: () => {},
      request: async command => {
        if (!connected) throw Object.assign(new Error('offline'), {disconnected:true});
        if (command.type === 'draft') return {text:store.get(command.id) || ''};
        store.set(command.id, command.text);
        for (const other of tabs) other.receive({type:'draftChanged', sessionId:command.id, from:command.from, text:command.text}, false);
        return {};
      },
    });
    tabs.push(draft);
    return {draft, text:() => text, select:id => { selected = id; draft.select(id); }};
  }
  const a = tab(), b = tab();
  a.draft.schedule('первый'); a.draft.save(1);
  await Promise.resolve();
  expect(b.text()).toBe('первый');
  connected = false; a.draft.schedule('без сети'); a.draft.save(1);
  await Promise.resolve();
  expect(a.draft.busy).toBe(true);
  connected = true; a.draft.reconnect();
  await Promise.resolve();
  expect(b.text()).toBe('без сети');
  a.select(2); await Promise.resolve(); expect(a.text()).toBe('');
  a.select(1); await Promise.resolve(); expect(a.text()).toBe('без сети');
});
