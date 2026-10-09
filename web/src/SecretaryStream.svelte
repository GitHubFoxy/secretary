<script>
  import Markdown from './Markdown.svelte';
  import { secretaryEventText, secretaryEventPayload, formatWorkerStatus } from './ui-model.js';
  export let events = [];
  export let entries = [];
  export let turnId = '';
  export let error = '';
  $: normalized = events.map(secretaryEventPayload);
  $: terminal = normalized.findLast((event) => event.kind === 'secretary.turn.finished');
  $: canonical = entries.some((entry) => entry.kind === 'secretary' && entry.turn_id === turnId);
  $: text = !canonical && !terminal ? normalized.filter((event) => event.kind === 'secretary.text_delta').map(secretaryEventText).join('') : '';
  $: tools = normalized.filter((event) => ['secretary.tool_call', 'secretary.tool_result'].includes(event.kind));
  $: terminalError = terminal?.error || (terminal && terminal.status !== 'succeeded' ? `Secretary: ${formatWorkerStatus(terminal.status)}` : '');
  $: missingReply = terminal && !canonical && !terminal.conversation_entry_id && !terminal.completion?.entry_present && !terminalError;
</script>
{#if events.length || error}
  <section class="rounded-2xl border border-indigo-300/20 bg-indigo-300/[0.06] p-4" aria-label="Secretary live stream">
    <div class="mb-3 text-[11px] uppercase tracking-[0.12em] text-indigo-300">Secretary · {terminal ? formatWorkerStatus(terminal.status) : 'Working'}</div>
    {#if text}<div class="text-sm leading-6 text-slate-200"><Markdown {text} /></div>{/if}
    {#if tools.length}<details class="mt-2 text-xs text-slate-400"><summary>Tools · {tools.length}</summary>{#each tools as event (event.id || event.seq)}<p class="mt-2 break-words">{secretaryEventText(event)}</p>{/each}</details>{/if}
    {#if terminalError}<p role="alert" class="mt-3 text-sm text-rose-300">{terminalError}</p>{/if}
    {#if missingReply}<p role="alert" class="mt-3 text-sm text-rose-300">Secretary: завершённый запрос не содержит сохранённого ответа. Отправьте новое сообщение, чтобы продолжить.</p>{/if}
    {#if error}<p role="alert" class="mt-3 text-xs text-rose-300">{error}</p>{/if}
  </section>
{/if}
