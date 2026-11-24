<script>
  import Icon from './Icon.svelte';

  let { open = false, onclose, onadd } = $props();

  // capture открывает камеру сразу; без него Android WebView предлагает только выбор файла.
  const options = [
    { label: 'Камера', icon: 'camera', accept: 'image/*', capture: 'environment' },
    { label: 'Фото и видео', icon: 'image', accept: 'image/*,video/*' },
    { label: 'Файл', icon: 'file' },
  ];
</script>

<svelte:window onkeydown={e => { if (open && e.key === 'Escape') onclose(); }} />

{#if open}
  <div class="fixed inset-0 z-30" role="presentation" onclick={onclose}></div>
  <div class="absolute bottom-full left-3 z-40 mb-1 w-56 overflow-hidden rounded-2xl border border-neutral-800 bg-neutral-900 py-1 shadow-xl shadow-black/50">
    {#each options as option}
      <label class="flex cursor-pointer items-center gap-3 px-4 py-3 text-[15px] text-neutral-100 hover:bg-neutral-800 focus-within:bg-neutral-800">
        <Icon name={option.icon} class="size-5 text-neutral-400" />{option.label}
        <input type="file" accept={option.accept} capture={option.capture} multiple={!option.capture} class="sr-only" onchange={e => { onclose(); onadd(e.currentTarget.files); e.currentTarget.value = ''; }} />
      </label>
    {/each}
  </div>
{/if}
