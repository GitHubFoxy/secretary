<script>
  import Icon from './Icon.svelte';
  let { request, sessions, onback } = $props();
  let rows = $state([]), zone = $state(''), loading = $state(true), error = $state('');
  let type = $state('all'), session = $state('all');
  let filtered = $derived(rows.filter(row => (type === 'all' || (type === 'repeat') === (row.cron !== null)) && (session === 'all' || String(row.session_id) === session)));
  let sessionIds = $derived([...new Set(rows.map(row => row.session_id))]);
  function name(id) { const s = sessions.find(s => s.id === id); return s?.secretary ? 'Secretary Agent' : s?.name || `Session #${id}`; }
  function date(value) { return value == null ? '—' : new Intl.DateTimeFormat('ru-RU', {dateStyle:'medium', timeStyle:'short', timeZone:zone}).format(value); }
  function description(row) {
    if (!row.cron) return `Разовый запуск · ${date(row.next_run ?? row.last_run)}`;
    const [minute, hour, day, month, weekday] = row.cron.trim().split(/\s+/);
    if (/^\d+$/.test(minute) && /^\d+$/.test(hour) && day === '*' && month === '*' && weekday === '*') return `Каждый день в ${hour.padStart(2, '0')}:${minute.padStart(2, '0')}`;
    return `Cron: минута ${minute}, час ${hour}, день месяца ${day}, месяц ${month}, день недели ${weekday}`;
  }
  async function load() {
    loading = true; error = '';
    try { const result = await request({type:'schedules'}); rows = result.schedules; zone = result.timeZone; }
    catch (e) { error = e.message || 'Не удалось загрузить расписания'; }
    finally { loading = false; }
  }
  load();
</script>
<div class="flex h-dvh flex-col bg-neutral-950 text-neutral-200">
  <header class="flex items-center gap-2 border-b border-neutral-900 px-2 py-2">
    <button class="icon-btn" aria-label="Назад" onclick={onback}><Icon name="back" /></button>
    <h1 class="px-1 text-sm font-medium text-neutral-100">Расписания</h1>
  </header>
  <main class="min-h-0 flex-1 overflow-y-auto p-4">
    <div class="mx-auto max-w-3xl">
      <h2 class="text-xl font-semibold text-neutral-100">Расписания</h2>
      <p class="mt-2 text-sm text-neutral-400">Все расписания на Host.{#if zone} Часовой пояс Host: {zone}.{/if}</p>
      {#if loading}
        <p class="mt-6 text-sm text-neutral-400" role="status">Загрузка расписаний…</p>
      {:else if error}
        <p class="mt-6 text-sm text-red-400" role="alert">Не удалось загрузить расписания: {error}</p>
        <button class="mt-3 rounded-lg border border-neutral-700 px-3 py-2 text-sm" onclick={load}>Повторить</button>
      {:else}
        <div class="my-5 flex flex-wrap gap-2">
          <select aria-label="Тип расписания" bind:value={type} class="max-w-full rounded-lg border border-neutral-700 bg-neutral-900 p-2 text-sm"><option value="all">Все типы</option><option value="repeat">Повтор</option><option value="once">Разовый</option></select>
          <select aria-label="Session" bind:value={session} class="max-w-full rounded-lg border border-neutral-700 bg-neutral-900 p-2 text-sm"><option value="all">Все Session</option>{#each sessionIds as id}<option value={String(id)}>{name(id)}</option>{/each}</select>
        </div>
        {#if !filtered.length}
          <p class="rounded-xl border border-neutral-800 p-8 text-center text-sm text-neutral-400">{rows.length ? 'Расписаний по выбранным фильтрам нет.' : 'Сохранённых расписаний пока нет.'}</p>
        {:else}
          <div class="divide-y divide-neutral-800 overflow-hidden rounded-xl border border-neutral-800 bg-neutral-900/50">
            {#each filtered as row (row.id)}
              <details class="p-4">
                <summary class="cursor-pointer list-none"><span class="block whitespace-pre-wrap break-words font-medium text-neutral-100">{row.text}</span><span class="mt-2 block break-words text-xs text-neutral-400">{name(row.session_id)} · {description(row)}</span><span class="mt-2 block text-xs text-neutral-300">Следующий: {date(row.next_run)}</span></summary>
                <dl class="mt-4 grid grid-cols-[110px_minmax(0,1fr)] gap-2 border-t border-neutral-800 pt-3 text-sm"><dt class="text-neutral-500">Session</dt><dd class="break-words">{name(row.session_id)}</dd><dt class="text-neutral-500">Расписание</dt><dd class="break-words">{description(row)}</dd><dt class="text-neutral-500">Исходный cron</dt><dd class="break-all">{row.cron ?? '—'}</dd><dt class="text-neutral-500">Следующий</dt><dd>{date(row.next_run)}</dd><dt class="text-neutral-500">Последний</dt><dd>{date(row.last_run)}</dd></dl>
              </details>
            {/each}
          </div>
          <p class="mt-5 text-xs text-neutral-500">Последний запуск — запланированное время передачи сообщения в очередь.</p>
        {/if}
      {/if}
    </div>
  </main>
</div>
