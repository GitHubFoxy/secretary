<script>
  import Markdown from './Markdown.svelte';
  export let messages = [];
  const labels = { pending: 'Queued', delivering: 'Delivering', delivered: 'Delivered', blocked: 'Blocked', canceled: 'Canceled' };
</script>
{#if messages.length}
  <section class="mt-4 space-y-2" aria-label="Worker message queue">
    {#each messages as message (message.id)}
      <article class="rounded-xl border border-white/10 bg-white/[0.03] p-3 text-xs text-slate-300">
        <p class="mb-2 text-slate-400">{labels[message.state] || message.state} · #{message.sequence}</p>
        <Markdown text={message.text} />
        {#if message.last_error}<p role="alert" class="mt-2 text-rose-300">{message.last_error}</p>{/if}
      </article>
    {/each}
  </section>
{/if}
