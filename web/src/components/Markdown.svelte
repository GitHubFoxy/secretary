<script>
  import { renderMarkdown } from '../markdown.js';

  // text — сырой текст ответа агента или отчёта. HTML здесь уже очищен в renderMarkdown.
  let { text = '' } = $props();
  const html = $derived(renderMarkdown(text));
</script>

<div class="md">{@html html}</div>
