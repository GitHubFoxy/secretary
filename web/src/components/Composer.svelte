<script>
  import Icon from './Icon.svelte';
  // onsubmit(immediate): true — прервать идущий ход и отправить сразу (правая кнопка и Enter), false — добавить в очередь (левая кнопка во время хода).
  let { value = $bindable(''), images = $bindable([]), disabled = false, running = false, onsubmit } = $props();
  let imageError = $state('');
  let area = $state();

  // Поле растёт вместе с текстом; больше максимальной высоты (max-h в разметке) прокручивается.
  $effect(() => {
    void value;
    if (!area) return;
    area.style.height = 'auto';
    area.style.height = area.scrollHeight + 'px';
  });
  async function add(files) {
    imageError = '';
    const selected = Array.from(files);
    if (images.length + selected.length > 4) { imageError = 'Можно прикрепить не более 4 изображений'; return; }
    for (const file of selected) {
      if (!['image/png', 'image/jpeg', 'image/gif', 'image/webp'].includes(file.type) || file.size > 5 * 1024 * 1024) { imageError = 'Выберите PNG, JPEG, GIF или WebP размером до 5 МБ'; return; }
    }
    try {
      const added = await Promise.all(selected.map(file => new Promise((resolve, reject) => {
        const reader = new FileReader();
        reader.onload = () => resolve({type:file.type, data:reader.result.split(',')[1]});
        reader.onerror = () => reject(new Error('Не удалось прочитать изображение'));
        reader.readAsDataURL(file);
      })));
      images = [...images, ...added];
    } catch (e) { imageError = e.message; }
  }
  function paste(e) {
    const files = Array.from(e.clipboardData?.files || []).filter(file => file.type.startsWith('image/'));
    if (files.length) { e.preventDefault(); add(files); }
  }

  // Enter отправляет срочно, Shift+Enter переносит строку.
  function keydown(e) {
    if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) {
      e.preventDefault();
      onsubmit(true);
    }
  }
</script>

<form class="px-3 pb-[max(0.75rem,env(safe-area-inset-bottom))] pt-2" onsubmit={e => { e.preventDefault(); onsubmit(true); }}>
  {#if imageError}<p role="alert" class="mx-auto max-w-3xl text-sm text-red-400">{imageError}</p>{/if}
  <div class="mx-auto flex max-w-3xl flex-wrap gap-2">{#each images as image, i}<div class="relative"><img class="h-20 rounded" src={'data:' + image.type + ';base64,' + image.data} alt="Изображение для отправки" /><button type="button" class="absolute right-0 top-0 bg-neutral-900 px-1" aria-label="Удалить изображение" onclick={() => images = images.filter((_, index) => index !== i)}>×</button></div>{/each}</div>
  <div class="mx-auto flex w-full max-w-3xl items-end gap-2 rounded-2xl border border-neutral-800 bg-neutral-900 p-2 shadow-lg shadow-black/40 transition-colors focus-within:border-neutral-600">
    <label class="cursor-pointer p-2 text-neutral-300" title="Прикрепить изображение">📎<span class="sr-only">Прикрепить изображение</span><input type="file" accept="image/png,image/jpeg,image/gif,image/webp" multiple class="sr-only" {disabled} onchange={e => { add(e.currentTarget.files); e.currentTarget.value = ''; }} /></label>
    <label class="sr-only" for="message">Сообщение</label>
    <textarea
      id="message"
      rows="1"
      bind:this={area}
      bind:value
      onkeydown={keydown}
      onpaste={paste}
      placeholder="Напишите сообщение"
      class="max-h-40 min-h-10 flex-1 resize-none bg-transparent px-2 py-2 text-[15px] text-neutral-100 placeholder:text-neutral-600 focus:outline-none"
    ></textarea>
    {#if running}
      <button
        type="button"
        class="grid size-10 shrink-0 cursor-pointer place-items-center rounded-xl border border-neutral-700 text-neutral-100 transition-opacity hover:opacity-80 disabled:cursor-not-allowed disabled:opacity-30"
        disabled={disabled || (!value.trim() && !images.length)}
        aria-label="Добавить в очередь"
        title="Отправить после текущего хода"
        onclick={() => onsubmit(false)}
      >
        <Icon name="queue" class="size-5" />
      </button>
    {/if}
    <button
      class="grid size-10 shrink-0 cursor-pointer place-items-center rounded-xl bg-neutral-100 text-neutral-950 transition-opacity hover:opacity-80 disabled:cursor-not-allowed disabled:opacity-30"
      disabled={disabled || (!value.trim() && !images.length)}
      aria-label={running ? 'Прервать и отправить' : 'Отправить'}
      title={running ? 'Прервать текущий ход и отправить сразу' : 'Отправить'}
    >
      <Icon name="send" class="size-5" />
    </button>
  </div>
</form>
