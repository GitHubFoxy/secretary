<script>
  let { name, class: className = 'size-5' } = $props();
</script>

<svg class={className} viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
  {#if name === 'menu'}
    <path d="M4 7h16M4 12h16M4 17h10" />
  {:else if name === 'more'}
    <path d="M5 12h.01M12 12h.01M19 12h.01" stroke-width="3" />
  {:else if name === 'close'}
    <path d="M6 6l12 12M18 6L6 18" />
  {:else if name === 'send'}
    <path d="M12 19V5M5.5 11.5L12 5l6.5 6.5" />
  {:else if name === 'stop'}
    <rect x="7" y="7" width="10" height="10" rx="2" fill="currentColor" />
  {/if}
</svg>
