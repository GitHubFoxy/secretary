<script>
  import { renderMarkdown } from '../markdown.js';
  import { mount, unmount } from 'svelte';
  import Icon from './Icon.svelte';

  let { text = '' } = $props();
  const html = $derived(renderMarkdown(text));
  let container;

  $effect(() => {
    html;
    const icons = [];
    for (const pre of container.querySelectorAll('pre')) {
      const code = pre.querySelector('code');
      if (!code) continue;
      const wrapper = document.createElement('div');
      wrapper.className = 'code-block';
      pre.replaceWith(wrapper);
      wrapper.append(pre);
      const button = document.createElement('button');
      button.type = 'button';
      button.className = 'copy-code';
      button.setAttribute('aria-label', 'Копировать');
      button.title = 'Копировать';
      wrapper.append(button);
      let icon = mount(Icon, { target: button, props: { name: 'copy', class: 'size-4' } });
      icons.push(() => unmount(icon));
      let timer;
      button.onclick = async () => {
        try {
          await navigator.clipboard.writeText(code.textContent);
        } catch {
          const selection = window.getSelection();
          const range = document.createRange();
          range.selectNodeContents(code);
          selection.removeAllRanges();
          selection.addRange(range);
          const copied = document.execCommand('copy');
          selection.removeAllRanges();
          if (!copied) return;
        }
        clearTimeout(timer);
        unmount(icon);
        icon = mount(Icon, { target: button, props: { name: 'check', class: 'size-4' } });
        button.title = 'Скопировано';
        button.setAttribute('aria-label', 'Скопировано');
        timer = setTimeout(() => {
          unmount(icon);
          icon = mount(Icon, { target: button, props: { name: 'copy', class: 'size-4' } });
          button.title = 'Копировать';
          button.setAttribute('aria-label', 'Копировать');
        }, 1000);
      };
      icons.push(() => clearTimeout(timer));
    }
    return () => icons.forEach(cleanup => cleanup());
  });
</script>

<div class="md" bind:this={container}>{@html html}</div>
