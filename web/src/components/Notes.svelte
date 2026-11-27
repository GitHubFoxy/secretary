<script>
  import { tick, untrack } from 'svelte';
  import Icon from './Icon.svelte';
  import { noteTitle, notePreview, noteSessionId, openedNote } from '../notes.js';
  let { open, sessionId, secretary = false, connection, revision, onclose, oncount } = $props();
  let notes = $state([]), opened = $state(null), title = $state(''), text = $state(''), timer;
  let loadedKey = $state(''), listDialog = $state(), noteDialog = $state();
  const scope = () => `${secretary ? 'all' : 'one'}:${sessionId}`;
  async function load(id) {
    try {
      const result = await connection.request({type:'notes', id});
      if (id === sessionId) {
        notes = result.notes; status = '';
        oncount(notes.length);
        if (opened && !timer) {
          const note = openedNote(notes, opened);
          if (note) { title = note.title ?? ''; text = note.text; }
          else opened = null;
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
      if (loadedKey !== scope()) { opened = null; loadedKey = scope(); }
      load(sessionId);
    }
  });
  $effect(() => { if (!open) untrack(() => { back(); }); });
  $effect(() => { if (open) tick().then(() => listDialog?.focus()); });
  $effect(() => { if (opened) tick().then(() => noteDialog?.querySelector('input')?.focus()); });
  function save() {
    clearTimeout(timer); timer = null;
    const note = openedNote(notes, opened);
    if (opened) connection.request({type:'noteSave', id:noteSessionId(note, sessionId), noteId:opened, text, title}).catch(error => status = error.message);
  }
  function change(field, event) {
    if (field === 'title') title = event.target.value;
    else text = event.target.value;
    clearTimeout(timer); timer = setTimeout(save, 500);
  }
  function openNote(note) { if (timer) save(); opened = note.id; title = note.title ?? ''; text = note.text; }
  function back() { if (timer) save(); opened = null; }
  function close() { back(); onclose(); }
  function add() { if (timer) save(); opened = crypto.randomUUID(); title = ''; text = ''; save(); }
  function remove(note) {
    if (opened === note.id) opened = null;
    connection.request({type:'noteDelete', id:noteSessionId(note, sessionId), noteId:note.id}).catch(error => status = error.message);
  }
</script>
<svelte:window onkeydown={(event) => { if (event.key === 'Escape') { if (opened) { event.stopPropagation(); back(); } else if (open) { event.stopPropagation(); close(); } } }} />
{#if open}
  <div class="fixed inset-0 z-30 flex items-end justify-center sm:items-center sm:p-4">
    <button class="absolute inset-0 cursor-default bg-black/60" aria-label="Закрыть" tabindex="-1" onclick={close}></button>
    <section bind:this={listDialog} tabindex="-1" aria-label="Заметки" role="dialog" class="relative flex h-[85dvh] w-full flex-col overflow-hidden rounded-t-2xl border border-neutral-800 bg-neutral-950 outline-none sm:h-[600px] sm:max-h-[calc(100dvh-72px)] sm:max-w-md sm:rounded-2xl">
      <div class="flex shrink-0 items-center gap-1 border-b border-neutral-900 py-2 pl-4 pr-2">
        <h2 class="flex-1 text-sm font-medium text-neutral-100">Заметки <span class="font-normal text-neutral-500">· {notes.length}</span></h2>
        <button class="icon-btn" aria-label="Новая заметка" onclick={add}><Icon name="plus" class="size-4" /></button>
        <button class="icon-btn" aria-label="Закрыть" onclick={close}><Icon name="close" class="size-4" /></button>
      </div>
      {#if status}<p class="px-3 pt-2 text-sm text-red-400">{status}</p>{/if}
      <div class="min-h-0 flex-1 overflow-y-auto py-1.5">
        {#each notes as note (note.id)}
          <div class="mx-2 flex items-center gap-2 rounded-xl px-3 py-2 hover:bg-neutral-900">
            <button class="min-w-0 flex-1 text-left" onclick={() => openNote(note)}>
              <span class="block truncate text-sm font-medium text-neutral-200">{noteTitle(note)}</span>
              <span class="block truncate text-xs text-neutral-500">{secretary && note.session_name ? `${note.session_name} · ` : ''}{notePreview(note)}</span>
            </button>
            <button class="icon-btn shrink-0 text-neutral-500 hover:text-red-400" aria-label="Удалить заметку" onclick={() => remove(note)}><Icon name="trash" class="size-4" /></button>
          </div>
        {/each}
      </div>
    </section>
  </div>
  {@const note = openedNote(notes, opened)}
  {#if opened}
    <div class="fixed inset-0 z-40 flex items-end justify-center sm:items-center sm:p-4">
      <button class="absolute inset-0 cursor-default bg-black/60" aria-label="Закрыть заметку" tabindex="-1" onclick={back}></button>
      <section bind:this={noteDialog} tabindex="-1" role="dialog" aria-label={note ? noteTitle(note) : 'Заметка'} class="relative flex h-[85dvh] w-full flex-col overflow-hidden rounded-t-2xl border border-neutral-800 bg-neutral-950 outline-none sm:h-[600px] sm:max-h-[calc(100dvh-72px)] sm:max-w-md sm:rounded-2xl">
        <div class="flex shrink-0 items-center gap-1 border-b border-neutral-900 py-2 pl-4 pr-2">
          <button class="icon-btn" aria-label="К списку заметок" onclick={back}><Icon name="back" class="size-4" /></button>
          <h2 class="flex-1 truncate px-1 text-sm font-medium text-neutral-100">{note ? noteTitle(note) : 'Заметка'}</h2>
          {#if note}<button class="icon-btn text-neutral-500 hover:text-red-400" aria-label="Удалить заметку" onclick={() => remove(note)}><Icon name="trash" class="size-4" /></button>{/if}
          <button class="icon-btn" aria-label="Закрыть" onclick={close}><Icon name="close" class="size-4" /></button>
        </div>
        <div class="flex min-h-0 flex-1 flex-col gap-2 p-3">
          <input class="rounded-xl border border-neutral-800 bg-[#0a0a0a] p-3 text-sm font-medium text-neutral-100 outline-none" aria-label="Заголовок заметки" placeholder="Заголовок" maxlength="200" value={title} oninput={(event) => change('title', event)} />
          <textarea class="min-h-0 flex-1 resize-none rounded-xl border border-neutral-800 bg-[#0a0a0a] p-3 text-sm leading-relaxed text-neutral-200 outline-none" aria-label="Текст заметки" placeholder="Текст заметки" value={text} oninput={(event) => change('text', event)}></textarea>
        </div>
      </section>
    </div>
  {/if}
{/if}
