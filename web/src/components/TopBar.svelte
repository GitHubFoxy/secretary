<script>
  import Icon from './Icon.svelte';
  let { title, status = '', connected = false, onmenu, onsettings } = $props();
</script>

<header class="flex items-center gap-2 border-b border-neutral-900 px-2 py-2">
  <button class="icon-btn" aria-label="Список сессий" onclick={onmenu}>
    <Icon name="menu" />
  </button>
  <div class="min-w-0 flex-1 px-1">
    <h1 class="truncate text-sm font-medium text-neutral-100">{title}</h1>
    <p class="flex items-center gap-1.5 text-xs text-neutral-500">
      <span class="size-1.5 rounded-full {connected ? 'bg-neutral-300' : 'bg-neutral-700'}"></span>
      {connected ? (status || 'подключено') : 'Нет связи с Host. Повторное подключение…'}
    </p>
  </div>
  <button class="icon-btn" aria-label="Настройки сессии" onclick={onsettings}>
    <Icon name="more" />
  </button>
</header>
