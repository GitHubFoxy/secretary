<script>
  import Markdown from './Markdown.svelte';
  import { tick } from 'svelte';

  let { message, session, onselect } = $props();
  let expanded = $state(false);
  let preview = $state();
  let card = $state();

  $effect(() => {
    if (!preview) return;
    void message.text;
    const isExpanded = expanded;
    const element = preview;
    let active = true;
    const measure = () => {
      if (!active) return;
      const bounds = element.getBoundingClientRect();
      element.querySelectorAll('a').forEach(link => {
        const visible = isExpanded || [...link.getClientRects()].some(rect =>
          rect.bottom > bounds.top && rect.top < bounds.bottom && rect.right > bounds.left && rect.left < bounds.right);
        if (visible) {
          link.removeAttribute('tabindex');
          link.removeAttribute('aria-hidden');
        } else {
          link.setAttribute('tabindex', '-1');
          link.setAttribute('aria-hidden', 'true');
        }
      });
    };
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    tick().then(measure);
    return () => { active = false; observer.disconnect(); };
  });

  function toggle(event) {
    if (event.target.closest('a, button')) return;
    const selection = window.getSelection();
    if (selection && !selection.isCollapsed && (card.contains(selection.anchorNode) || card.contains(selection.focusNode))) return;
    expanded = !expanded;
  }
</script>

<div bind:this={card} class="max-w-[85%] min-w-0 cursor-pointer rounded-2xl bg-neutral-900 px-4 py-3 text-sm"
  role="presentation" onclick={toggle}>
  <div class="mb-2 flex flex-wrap gap-x-2 text-neutral-400">
    {#if session}
      <button class="cursor-pointer text-neutral-200 underline" onclick={() => onselect(message.id)}>{session.name || `Сессия #${message.id}`}</button>
    {:else}
      <span>Сессия #{message.id}</span>
    {/if}
    <span>{message.harness}</span><span class="break-all">{message.folder}</span>
    <button class="sr-only focus:not-sr-only focus:cursor-pointer focus:text-neutral-200 focus:underline" aria-expanded={expanded} onclick={() => expanded = !expanded}>{expanded ? 'Свернуть отчёт' : 'Развернуть отчёт'}</button>
  </div>
  <div class="relative min-w-0">
    <div bind:this={preview} class:collapsed={!expanded}><Markdown text={message.text} /></div>
  </div>
</div>

<style>
  .collapsed { max-height: 6rem; overflow: hidden; }
</style>
