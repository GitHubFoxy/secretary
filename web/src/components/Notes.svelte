<script>
  import { untrack } from 'svelte';
  import Icon from './Icon.svelte';
  import { noteTitle, notePreview, noteSessionId, openedNote } from '../notes.js';
  let { open, sessionId, secretary = false, connection, revision, onclose, oncount } = $props();
  let notes = $state([]), editing = $state(null), title = $state(''), text = $state(''), timer;
  let loadedKey = $state(''), minimized = $state(false);
  const scope = () => `${secretary ? 'all' : 'one'}:${sessionId}`;
  async function load(id) {
    try {
      const result = await connection.request({type:'notes', id});
      if (id === sessionId) {
        notes = result.notes; status = '';
        oncount(notes.length);
        if (editing && !timer) {
          const note = openedNote(notes, editing);
          if (note) { title = note.title ?? ''; text = note.text; }
          else editing = null;
        }
      }
    }
    // Before the socket reconnects the request fails at once; revision++ on reconnect reloads, so this error is not shown.
    catch (error) { if (!error.disconnected) status = error.message; }
  }
  let status = $state('');
  $effect(() => {
    if (sessionId != null) {
      revision;
      if (loadedKey !== scope()) { editing = null; loadedKey = scope(); }
      load(sessionId);
    }
  });
  $effect(() => { if (!open) untrack(() => { back(); minimized = false; }); });
  function save() {
    clearTimeout(timer); timer = null;
    const note = openedNote(notes, editing);
    if (editing) connection.request({type:'noteSave', id:noteSessionId(note, sessionId), noteId:editing, text, title}).catch(error => status = error.message);
  }
  function change(field, event) {
    if (field === 'title') title = event.target.value;
    else text = event.target.value;
    clearTimeout(timer); timer = setTimeout(save, 500);
  }
  function back() { if (timer) save(); editing = null; }
  function close() { back(); onclose(); }
  function add() { if (timer) save(); editing = crypto.randomUUID(); title = ''; text = ''; save(); }
  function remove(note) {
    if (editing === note.id) editing = null;
    connection.request({type:'noteDelete', id:noteSessionId(note, sessionId), noteId:note.id}).catch(error => status = error.message);
  }
</script>
<svelte:window onkeydown={(event) => { if (open && event.key === 'Escape') { event.stopPropagation(); close(); } }} />
{#if open}
  {#if minimized}
    <button class="fixed right-3 top-[60px] z-20 flex items-center gap-2 rounded-full border border-neutral-800 bg-neutral-900 py-2 pl-3 pr-3.5 text-sm shadow-xl" onclick={() => minimized = false}>
      <Icon name="notes" class="size-4" /> Заметки <span class="rounded-full bg-neutral-100 px-1.5 text-[10px] font-semibold text-neutral-950">{notes.length}</span>
    </button>
  {:else}
    <section aria-label="Заметки" class="fixed inset-x-3 top-[60px] z-20 flex h-[50dvh] flex-col overflow-hidden rounded-2xl border border-neutral-800 bg-neutral-950 shadow-2xl shadow-black/60 sm:left-auto sm:right-[52px] sm:h-[500px] sm:max-h-[calc(100dvh-72px)] sm:w-[360px]">
      <div class="flex shrink-0 items-center gap-1 border-b border-neutral-900 py-2 pl-4 pr-2">
        <h2 class="flex-1 text-sm font-medium text-neutral-100">Заметки <span class="font-normal text-neutral-500">· {notes.length}</span></h2>
        <button class="icon-btn" aria-label="Новая заметка" onclick={add}><Icon name="plus" class="size-4" /></button>
        <button class="icon-btn" aria-label="Свернуть в пузырь" onclick={() => minimized = true}><Icon name="chevron" class="size-4" /></button>
        <button class="icon-btn" aria-label="Закрыть" onclick={close}><Icon name="close" class="size-4" /></button>
      </div>
      {#if status}<p class="px-3 text-sm text-red-400">{status}</p>{/if}
      <div class="max-h-[40%] shrink-0 overflow-y-auto py-1.5">
        {#each notes as note (note.id)}
          <div class="mx-2 flex items-center gap-2 rounded-xl px-3 py-2 {editing === note.id ? 'bg-neutral-900' : ''}">
            <button class="min-w-0 flex-1 text-left" onclick={() => { if (timer) save(); editing = note.id; title = note.title ?? ''; text = note.text; }}>
              <span class="block truncate text-sm font-medium text-neutral-200">{noteTitle(note)}</span>
              <span class="block truncate text-xs text-neutral-500">{secretary && note.session_name ? `${note.session_name} · ` : ''}{notePreview(note)}</span>
            </button>
            <button class="icon-btn shrink-0 text-neutral-500 hover:text-red-400" aria-label="Удалить заметку" onclick={() => remove(note)}><Icon name="trash" class="size-4" /></button>
          </div>
        {/each}
      </div>
      {#if editing}
        <div class="flex min-h-0 flex-1 flex-col gap-2 p-3">
          <input class="shrink-0 rounded-xl border border-neutral-800 bg-[#0a0a0a] p-3 text-sm font-medium text-neutral-100 outline-none" aria-label="Заголовок заметки" placeholder="Заголовок" maxlength="200" value={title} oninput={(event) => change('title', event)} />
          <textarea class="min-h-0 flex-1 resize-none rounded-xl border border-neutral-800 bg-[#0a0a0a] p-3 text-sm leading-relaxed text-neutral-200 outline-none" aria-label="Текст заметки" value={text} oninput={(event) => change('text', event)}></textarea>
        </div>
      {/if}
    </section>
  {/if}
{/if}
