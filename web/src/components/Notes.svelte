<script>
  import Modal from './Modal.svelte';
  import Icon from './Icon.svelte';
  let { open, sessionId, connection, revision, onclose } = $props();
  let notes = $state([]), editing = $state(null), text = $state(''), timer;
  let loadedSession;
  async function load(id) {
    try {
      const result = await connection.request({type:'notes', id});
      if (id === sessionId) {
        notes = result.notes;
        if (editing && !timer) {
          const note = notes.find(item => item.id === editing);
          if (note) text = note.text;
          else editing = null;
        }
      }
    }
    catch (error) { status = error.message; }
  }
  let status = $state('');
  $effect(() => {
    if (open && sessionId != null) {
      revision;
      if (loadedSession !== sessionId) { editing = null; loadedSession = sessionId; }
      load(sessionId);
    }
  });
  function save() {
    clearTimeout(timer); timer = null;
    if (editing) connection.request({type:'noteSave', id:sessionId, noteId:editing, text}).catch(error => status = error.message);
  }
  function change(event) { text = event.target.value; clearTimeout(timer); timer = setTimeout(save, 500); }
  function back() { if (timer) save(); editing = null; }
  function close() { back(); onclose(); }
  function add() { editing = crypto.randomUUID(); text = ''; save(); }
  function remove(id) { connection.request({type:'noteDelete', id:sessionId, noteId:id}).catch(error => status = error.message); }
</script>
<Modal {open} title="Заметки" onclose={close}>
  {#if status}<p class="text-red-400 text-sm">{status}</p>{/if}
  {#if editing}
    <button class="btn-ghost mb-3" onclick={back}>← Назад</button>
    <textarea class="field w-full min-h-[50vh]" aria-label="Текст заметки" value={text} oninput={change}></textarea>
  {:else}
    <button class="btn mb-3" onclick={add}>Добавить</button>
    {#each notes as note (note.id)}
      <div class="flex items-center gap-2 border-b border-neutral-800">
        <button class="min-w-0 flex-1 truncate py-3 text-left" onclick={() => { editing = note.id; text = note.text; }}>{note.text.split('\n')[0] || 'Новая заметка'}</button>
        <button class="icon-btn" aria-label="Удалить заметку" onclick={() => remove(note.id)}><Icon name="trash" /></button>
      </div>
    {/each}
  {/if}
</Modal>
