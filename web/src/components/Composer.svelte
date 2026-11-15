<script>
  import Icon from './Icon.svelte';
  let { value = $bindable(''), disabled = false, onsubmit } = $props();

  // Enter отправляет, Shift+Enter переносит строку.
  function keydown(e) {
    if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) {
      e.preventDefault();
      onsubmit();
    }
  }
</script>

<form class="border-t border-neutral-900 bg-neutral-950 px-4 py-3" onsubmit={e => { e.preventDefault(); onsubmit(); }}>
  <div class="mx-auto flex w-full max-w-3xl items-end gap-2 rounded-2xl border border-neutral-800 bg-neutral-900 p-2 transition-colors focus-within:border-neutral-600">
    <label class="sr-only" for="message">Сообщение</label>
    <textarea
      id="message"
      rows="1"
      bind:value
      onkeydown={keydown}
      placeholder="Напишите сообщение"
      class="max-h-48 min-h-10 flex-1 resize-none bg-transparent px-2 py-2 text-[15px] text-neutral-100 placeholder:text-neutral-600 focus:outline-none"
    ></textarea>
    <button
      class="grid size-10 shrink-0 cursor-pointer place-items-center rounded-xl bg-neutral-100 text-neutral-950 transition-opacity hover:opacity-80 disabled:cursor-not-allowed disabled:opacity-30"
      disabled={disabled || !value.trim()}
      aria-label="Отправить"
    >
      <Icon name="send" class="size-5" />
    </button>
  </div>
</form>
