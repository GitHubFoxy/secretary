<script>
  import { mount, unmount } from 'svelte';
  import { renderMarkdown } from '../markdown.js';
  import Icon from './Icon.svelte';

  // text — сырой текст ответа агента или отчёта. HTML здесь уже очищен в renderMarkdown.
  let { text = '' } = $props();
  const html = $derived(renderMarkdown(text));
  let container;

  async function copy(code) {
    try { await navigator.clipboard.writeText(code.textContent); return true; }
    catch {
      // Android WebView без разрешения на буфер обмена: копируем через выделение.
      getSelection().selectAllChildren(code);
      const copied = document.execCommand('copy');
      getSelection().removeAllRanges();
      return copied;
    }
  }

  // Каждому блоку кода — кнопка «Копировать». Обёртка держит кнопку на месте, пока сам блок прокручивается вбок.
  $effect(() => {
    void html;
    const icons = [];
    for (const code of container.querySelectorAll('pre > code')) {
      const pre = code.parentElement, wrapper = document.createElement('div'), button = document.createElement('button');
      wrapper.className = 'code-block';
      pre.replaceWith(wrapper);
      Object.assign(button, { type: 'button', className: 'copy-code', title: 'Копировать' });
      wrapper.append(pre, button);
      const props = $state({ name: 'copy', class: 'size-4' });
      let timer;
      button.onclick = async () => {
        if (!await copy(code)) return;
        props.name = 'check'; button.title = 'Скопировано';
        clearTimeout(timer);
        timer = setTimeout(() => { props.name = 'copy'; button.title = 'Копировать'; }, 1000);
      };
      const icon = mount(Icon, { target: button, props });
      icons.push(() => { clearTimeout(timer); unmount(icon); });
    }
    return () => icons.forEach(cleanup => cleanup());
  });
</script>

<div class="md" bind:this={container}>{@html html}</div>
