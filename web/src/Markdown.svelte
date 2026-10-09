<script context="module">
  import MarkdownIt from 'markdown-it';
  const markdown = new MarkdownIt({ html: false, breaks: true, linkify: true });
</script>
<script>
  export let text = '';
</script>
<div class="markdown">{@html markdown.render(String(text || ''))}</div>
