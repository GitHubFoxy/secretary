<script>
  import Icon from './Icon.svelte';
  let { request, onback } = $props();
  let servers = $state([]), error = $state(''), busy = $state(false), adding = $state(false);
  let name = $state(''), type = $state('stdio'), command = $state(''), args = $state(''), url = $state(''), values = $state(''), secretary = $state(false), specialist = $state(false), confirmed = $state(false);

  async function load() {
    try { servers = (await request({type:'mcpList'})).servers; } catch (e) { error = e.message; }
  }
  load();
  async function save(next) {
    busy = true; error = '';
    try { await request({type:'mcpSave', servers:next}); servers = next; return true; }
    catch (e) { error = e.message; return false; }
    finally { busy = false; }
  }
  async function add() {
    error = '';
    try {
      const entries = values.split('\n').filter(line => line.trim()).map(line => {
        const split = line.indexOf('=');
        if (split < 1) throw new Error('Каждая строка env или headers должна иметь вид ИМЯ=значение');
        return [line.slice(0, split).trim(), line.slice(split + 1)];
      });
      if (new Set(entries.map(([key]) => key)).size !== entries.length) throw new Error('Имена env или headers повторяются');
      const base = {name: name.trim(), enabled: true, roles: [...(secretary ? ['secretary'] : []), ...(specialist ? ['specialist'] : [])]};
      const server = type === 'stdio' ? {...base, type, command:command.trim(), args:args.split('\n').filter(Boolean), env:Object.fromEntries(entries)} : {...base, type, url:url.trim(), headers:Object.fromEntries(entries)};
      if (type === 'stdio' && !confirmed) throw new Error('Подтвердите запуск сервера с вашими правами');
      if (await save([...servers, server])) { adding = false; name = ''; command = ''; args = ''; url = ''; values = ''; secretary = false; specialist = false; confirmed = false; }
    } catch (e) { error = e.message; }
  }
</script>

<div class="flex h-dvh flex-col bg-neutral-950 text-neutral-200">
  <header class="flex items-center gap-2 border-b border-neutral-900 px-2 py-2">
    <button class="icon-btn" aria-label="Назад" onclick={onback}><Icon name="back" /></button>
    <h1 class="min-w-0 flex-1 truncate px-1 text-sm font-medium text-neutral-100">MCP-серверы</h1>
  </header>
  <div class="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto p-4 text-sm">
    <p class="text-neutral-400">Изменения действуют со следующего хода. Настройки самих harness могут подключать другие серверы.</p>
    {#each servers as server (server.name)}
      <div class="rounded-xl border border-neutral-800 bg-neutral-900 p-3">
        <div class="flex items-center justify-between gap-3"><strong class="break-all">{server.name}</strong><label class="whitespace-nowrap"><input type="checkbox" checked={server.enabled} disabled={busy} onchange={e => save(servers.map(s => s.name === server.name ? {...s, enabled:e.currentTarget.checked} : s))} /> Включён</label></div>
        <p class="mt-1 break-all text-neutral-400">{server.type === 'stdio' ? server.command : server.url}</p>
        <p class="mt-1 text-neutral-400">{server.roles.join(', ') || 'Никому не назначен'}</p>
        <button class="mt-2 text-red-400" disabled={busy} onclick={() => save(servers.filter(s => s.name !== server.name))}>Удалить</button>
      </div>
    {/each}
    {#if !adding}<button class="self-start rounded bg-neutral-800 px-3 py-2" onclick={() => adding = true}>Добавить сервер</button>
    {:else}
      <form class="flex flex-col gap-3 rounded-xl border border-neutral-800 bg-neutral-900 p-3" onsubmit={e => { e.preventDefault(); add(); }}>
        <label>Имя <input class="mt-1 block w-full rounded bg-neutral-800 p-2" bind:value={name} required /></label>
        <label>Тип <select class="mt-1 block w-full rounded bg-neutral-800 p-2" bind:value={type}><option value="stdio">stdio</option><option value="http">HTTP</option></select></label>
        {#if type === 'stdio'}
          <label>Команда <input class="mt-1 block w-full rounded bg-neutral-800 p-2" bind:value={command} required /></label>
          <label>Аргументы, по одному на строку <textarea class="mt-1 block w-full rounded bg-neutral-800 p-2" bind:value={args}></textarea></label>
        {:else}
          <label>URL <input class="mt-1 block w-full rounded bg-neutral-800 p-2" type="url" bind:value={url} required /></label>
        {/if}
        <label>{type === 'stdio' ? 'Переменные окружения' : 'Заголовки'}, по одной строке ИМЯ=значение <textarea class="mt-1 block w-full rounded bg-neutral-800 p-2" bind:value={values} placeholder={'TOKEN=${secret:NAME}'}></textarea></label>
        <p class="text-neutral-400">Для секретов используйте ссылку ${'{secret:NAME}'} и запросите значение у агента.</p>
        <fieldset><legend>Для кого</legend><label class="mr-4"><input type="checkbox" bind:checked={secretary} /> Secretary Agent</label><label><input type="checkbox" bind:checked={specialist} /> Specialists</label></fieldset>
        {#if type === 'stdio'}<label class="text-amber-300"><input type="checkbox" bind:checked={confirmed} /> Подтверждаю: сервер запускается с вашими правами на Host</label>{/if}
        <div class="flex gap-3"><button class="rounded bg-amber-700 px-3 py-2" disabled={busy}>Добавить</button><button type="button" class="rounded bg-neutral-800 px-3 py-2" onclick={() => adding = false}>Отмена</button></div>
      </form>
    {/if}
    {#if error}<p role="alert" class="text-red-400">{error}</p>{/if}
  </div>
</div>
