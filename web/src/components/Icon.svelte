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
  {:else if name === 'trash'}
    <path d="M3 6h18M9 6V3h6v3M5 6l1 15h12l1-15M10 10v7M14 10v7" />
  {:else if name === 'back'}
    <path d="M19 12H5M11 6l-6 6 6 6" />
  {:else if name === 'refresh'}
    <path d="M19 12a7 7 0 1 1-2.05-4.95M19 4v4h-4" />
  {:else if name === 'claude'}
    <path d="M12 4v16M5.1 8l13.8 8M5.1 16L18.9 8" />
  {:else if name === 'codex'}
    <path d="M12 3.5l7.2 4.2v8.6L12 20.5l-7.2-4.2V7.7z" />
    <path d="M12 8.5l3.1 1.8v3.4L12 15.5l-3.1-1.8v-3.4z" />
  {/if}
</svg>
