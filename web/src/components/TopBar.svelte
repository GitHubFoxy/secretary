<script>
  import Icon from './Icon.svelte';
  let { title, status = '', connected = false, onmenu, onsettings, oncall, onnotes, notesCount = 0 } = $props();
</script>

<header class="flex items-center gap-2 border-b border-neutral-900 px-2 py-2">
  <button class="icon-btn" aria-label="Список сессий" onclick={onmenu}>
    <Icon name="menu" />
  </button>
  <div class="min-w-0 flex-1 px-1">
    <h1 class="truncate text-sm font-medium text-neutral-100">{title}</h1>
    <p class="flex items-center gap-1.5 text-xs text-neutral-500">
      <span role="status" aria-label={connected ? 'Связь с Host установлена' : 'Нет связи с Host'} class="flex items-center gap-1.5">
        <span aria-hidden="true" class="size-1.5 rounded-full {connected ? 'bg-green-500' : 'bg-neutral-500'}"></span>
        {#if !connected}Нет связи{/if}
      </span>
      {#if status}<span>{status}</span>{/if}
    </p>
  </div>
  <button class="icon-btn relative" aria-label="Заметки: {notesCount}" onclick={onnotes}><Icon name="notes" /><span class="absolute -right-0.5 -top-0.5 grid h-4 min-w-4 place-items-center rounded-full bg-neutral-100 px-1 text-[10px] font-semibold leading-none text-neutral-950">{notesCount}</span></button>
  <button class="icon-btn" aria-label="Позвонить" onclick={oncall}>
    <Icon name="phone" />
  </button>
  <button class="icon-btn" aria-label="Настройки сессии" onclick={onsettings}>
    <Icon name="more" />
  </button>
</header>
