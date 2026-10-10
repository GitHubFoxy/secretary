<script>
  import Markdown from './Markdown.svelte';
  import { workerActivityRows } from './ui-model.js';
  export let events = [];
  $: rows = workerActivityRows(events);
</script>
<div class="mt-4 max-h-64 overflow-y-auto rounded-2xl bg-black/35 p-3 text-sm leading-6 text-emerald-200" aria-live="polite">
  {#if rows.length === 0}<span class="text-slate-600">No live activity yet.</span>{/if}
  {#each rows as row (row.key)}
    <div class="border-b border-white/5 pb-2 last:border-0" aria-label="Worker activity item">
      <div class="text-slate-500">{row.label}</div>
      <div class="mt-1 break-words text-emerald-100"><Markdown text={row.text} /></div>
    </div>
  {/each}
</div>
