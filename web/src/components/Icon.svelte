<script>
  /* Paperclip and phone: Lucide Icons, ISC License.
   * Copyright (c) 2026 Lucide Icons and Contributors
   * Permission to use, copy, modify, and/or distribute this software for any
   * purpose with or without fee is hereby granted, provided that the above
   * copyright notice and this permission notice appear in all copies.
   * THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES
   * WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF
   * MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR
   * ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES
   * WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS, WHETHER IN AN
   * ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING OUT OF
   * OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.
   */
  let { name, class: className = 'size-5' } = $props();
</script>

<svg class={className} viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
  {#if name === 'menu'}
    <path d="M4 7h16M4 12h16M4 17h10" />
  {:else if name === 'more'}
    <path d="M5 12h.01M12 12h.01M19 12h.01" stroke-width="3" />
  {:else if name === 'close'}
    <path d="M6 6l12 12M18 6L6 18" />
  {:else if name === 'expand'}
    <path d="M15 3h6v6M9 21H3v-6M21 3l-7 7M3 21l7-7" />
  {:else if name === 'send'}
    <path d="M12 19V5M5.5 11.5L12 5l6.5 6.5" />
  {:else if name === 'microphone'}
    <rect x="9" y="3" width="6" height="12" rx="3" />
    <path d="M6 11a6 6 0 0 0 12 0M12 17v4M9 21h6" />
  {:else if name === 'phone'}
    <path d="M22 16.9v3a2 2 0 0 1-2.2 2 19.8 19.8 0 0 1-8.6-3.1 19.5 19.5 0 0 1-6-6A19.8 19.8 0 0 1 2.1 4.2 2 2 0 0 1 4.1 2h3a2 2 0 0 1 2 1.7l.4 2.8a2 2 0 0 1-.6 1.7L7.6 9.5a16 16 0 0 0 6.9 6.9l1.3-1.3a2 2 0 0 1 1.7-.6l2.8.4a2 2 0 0 1 1.7 2z" />
  {:else if name === 'video'}
    <rect x="3" y="6" width="13" height="12" rx="3" />
    <path d="m16 10 5-3v10l-5-3z" />
  {:else if name === 'flip'}
    <path d="M4 9a8 8 0 0 1 14-3l2 2M20 4v4h-4M20 15a8 8 0 0 1-14 3l-2-2M4 20v-4h4" />
  {:else if name === 'lock'}
    <rect x="5" y="11" width="14" height="9" rx="2" />
    <path d="M8 11V8a4 4 0 0 1 8 0v3" />
  {:else if name === 'file'}
    <path d="M6 3h8l4 4v14H6zM14 3v5h4" />
  {:else if name === 'paperclip'}
    <path d="m16 6-8.414 8.586a2 2 0 0 0 2.829 2.829l8.414-8.586a4 4 0 1 0-5.657-5.657l-8.379 8.551a6 6 0 1 0 8.485 8.485l8.379-8.551" />
  {:else if name === 'queue'}
    <path d="M4 7h11M4 12h11M4 17h6M18 14v6M15 17h6" />
  {:else if name === 'stop'}
    <rect x="7" y="7" width="10" height="10" rx="2" fill="currentColor" />
  {:else if name === 'trash'}
    <path d="M3 6h18M9 6V3h6v3M5 6l1 15h12l1-15M10 10v7M14 10v7" />
  {:else if name === 'eye'}
    <path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12z" />
    <circle cx="12" cy="12" r="3" />
  {:else if name === 'eye-off'}
    <path d="M9.9 4.2A10 10 0 0 1 12 4c6.5 0 10 8 10 8a18 18 0 0 1-2.2 3.2M6.6 6.6A18 18 0 0 0 2 12s3.5 7 10 7a9.7 9.7 0 0 0 5.4-1.6M3 3l18 18M9.9 9.9a3 3 0 0 0 4.2 4.2" />
  {:else if name === 'chevron'}
    <path d="M6 9l6 6 6-6" />
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
