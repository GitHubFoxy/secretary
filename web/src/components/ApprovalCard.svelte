<script>
  let { message: m, connected, ondecide } = $props();
  let password = $state(''), busy = $state(false), error = $state('');
  const labels = {
    skill: { approve: 'Принять', statuses: {approved:'Принято', rejected:'Отклонено'} },
    secret: { approve: 'Сохранить', input: 'Секрет', statuses: {saved:'Сохранён'} },
    sudo: { approve: 'Выполнить', input: 'Пароль sudo', statuses: {completed:'Выполнено', rejected:'Отклонено', timeout:'Время ожидания истекло', error:'Ошибка запуска'} },
  };
  let settings = $derived(labels[m.kind]);
  async function decide(approved) {
    busy = true; error = '';
    const value = password; password = '';
    try { await ondecide(m.kind, m.id, approved, approved && settings.input ? value : undefined); }
    catch (e) { error = e.message; }
    finally { busy = false; }
  }
  function skillDiff(before, after) {
    const oldLines = (before || '').split('\n'), newLines = (after || '').split('\n');
    while (oldLines.length && newLines.length && oldLines[0] === newLines[0]) { oldLines.shift(); newLines.shift(); }
    while (oldLines.length && newLines.length && oldLines.at(-1) === newLines.at(-1)) { oldLines.pop(); newLines.pop(); }
    return [...oldLines.map(line => `- ${line}`), ...newLines.map(line => `+ ${line}`)].join('\n');
  }
</script>

<div class="max-w-full rounded-xl border border-amber-700 bg-neutral-900 p-4 text-sm text-neutral-100">
  {#if m.kind === 'skill'}
    <p>Предложение skill: {m.name}</p>
  {:else if m.kind === 'secret'}
    <p>Агент запрашивает секрет: {m.name}</p>
  {:else}
    <p>Агент хочет выполнить от root:</p>
    <pre class="my-2 whitespace-pre-wrap break-all">{m.command}</pre>
  {/if}
  <p class="whitespace-pre-wrap break-words">Причина: {m.reason}</p>
  {#if m.kind === 'skill'}<p class="mt-2">Изменения:</p><pre class="max-h-96 overflow-auto whitespace-pre-wrap break-all">{skillDiff(m.before, m.content)}</pre>{/if}
  {#if m.status}
    <p class="mt-2">{settings.statuses[m.status] || (m.kind === 'skill' ? 'Отклонено' : m.status)}</p>
  {:else}
    {#if settings.input}<label class="mt-3 block">{settings.input} <input class="mt-1 block w-full rounded bg-neutral-800 p-2" type="password" autocomplete="off" bind:value={password} /></label>{/if}
    <div class="mt-3 flex gap-3"><button class="rounded bg-amber-700 px-3 py-2" disabled={!connected || busy || (settings.input && !password)} onclick={() => decide(true)}>{settings.approve}</button><button class="rounded bg-neutral-700 px-3 py-2" disabled={!connected || busy} onclick={() => decide(false)}>Отклонить</button></div>
    {#if error}<p class="mt-2 text-red-400">{error}</p>{/if}
  {/if}
</div>
