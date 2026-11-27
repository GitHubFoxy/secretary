<script>
  import { fade, fly } from 'svelte/transition';
  import Icon from './Icon.svelte';
  import { Capacitor } from '@capacitor/core';
  import { harnessInfo, modelLabel } from '../agents.js';
  let { open = false, sessions = [], selected, connected = false, onhide, onrestore, onselect, onclose, onprofile, onlimits, onopencode, ondownloads } = $props();

  let hiddenOpen = $state(false);
  let offset = $state(0), dragging = $state(false);
  let startX, startY, direction;
  $effect(() => { if (open) offset = 0; });

  function touchStart(e) {
    if (e.touches.length !== 1) return;
    startX = e.touches[0].clientX;
    startY = e.touches[0].clientY;
    direction = undefined;
  }

  function touchMove(e) {
    if (startX === undefined) return;
    const x = e.touches[0].clientX - startX;
    const y = e.touches[0].clientY - startY;
    if (!direction && Math.max(Math.abs(x), Math.abs(y)) > 8) direction = Math.abs(x) > Math.abs(y) ? 'horizontal' : 'vertical';
    if (direction === 'horizontal') {
      dragging = true;
      offset = Math.min(0, x);
    }
  }

  function touchEnd(e) {
    if (dragging && e.type !== 'touchcancel' && -offset > e.currentTarget.offsetWidth / 3) onclose();
    else offset = 0;
    dragging = false;
    startX = undefined;
  }
  const secretary = $derived(sessions.find(s => s.secretary));
  const secretaryId = $derived(secretary?.id);
  function groups(list) {
    const ids = new Set(list.map(s => s.id));
    return list.filter(s => !s.parent_id || s.parent_id === secretaryId || !ids.has(s.parent_id))
      .map(s => ({ session: s, workers: list.filter(w => w.parent_id === s.id) }));
  }
  const visibleGroups = $derived(groups(sessions.filter(s => !s.internal && !s.hidden)));
  const hiddenSessions = $derived(sessions.filter(s => !s.internal && s.hidden));
  const hiddenGroups = $derived(groups(hiddenSessions));

  let expanded = $state(new Set(JSON.parse(localStorage.getItem('expanded-specialists') || '[]')));
  function toggle(id) {
    const next = new Set(expanded);
    next.has(id) ? next.delete(id) : next.add(id);
    expanded = next;
    localStorage.setItem('expanded-specialists', JSON.stringify([...next]));
  }

  const statusText = { running: 'работает', idle: 'свободен', interrupted: 'прерван', stopped: 'остановлен' };
  const dotClass = {
    running: 'bg-emerald-500 shadow-[0_0_0_3px_rgba(16,185,129,0.22)]',
    idle: 'bg-neutral-600',
    interrupted: 'bg-amber-500',
    stopped: 'bg-red-500/70',
  };
</script>

<svelte:window onkeydown={e => { if (open && e.key === 'Escape') onclose(); }} />

{#snippet agentLine(s)}
  <span class="flex items-center gap-1 truncate text-xs text-neutral-500"><Icon name={s.harness} class="size-3 shrink-0 {harnessInfo(s.harness).accent}" />{modelLabel(s.model)}{#if s.reasoning}<span>· {s.reasoning}</span>{/if}</span>
{/snippet}

{#snippet sessionRow(s, worker = false, workers = [])}
  {@const title = s.name || `Сессия #${s.id}`}
  {@const active = selected === s.id}
  {@const unfolded = expanded.has(s.id) || workers.some(w => w.id === selected)}
  <div class="group flex items-center rounded-lg transition-colors {active ? 'bg-neutral-900 ring-1 ring-neutral-800' : 'hover:bg-neutral-900/70'}">
    <button class="min-w-0 flex-1 cursor-pointer rounded-lg text-left {worker ? 'px-2.5 py-1.5' : 'px-3 py-2'}" onclick={() => onselect(s.id)}>
      <span class="flex min-w-0 items-center gap-2">
        <span class="size-1.5 shrink-0 rounded-full {dotClass[s.status] ?? 'bg-neutral-600'}" aria-hidden="true" title={statusText[s.status] ?? s.status}></span>
        <span class="min-w-0 flex-1 truncate {worker ? 'text-xs' : 'text-sm'} {active ? 'text-neutral-100' : worker ? 'text-neutral-400' : 'text-neutral-200'}">{title}</span>
      </span>
      {#if active}
        <span class="mt-0.5 block truncate pl-3.5 text-xs text-neutral-500">{statusText[s.status] ?? s.status} · {modelLabel(s.model)}{#if s.reasoning}{' · '}{s.reasoning}{/if}</span>
        <span class="block truncate pl-3.5 font-mono text-[11px] text-neutral-600">{s.folder}</span>
        {#if s.branch}<span class="block truncate pl-3.5 font-mono text-[11px] text-neutral-600">{s.branch}</span>{/if}
      {/if}
    </button>
    <!-- Вне кнопки чата: так иконка стоит по центру строки, на одном уровне с кнопками справа, сколько бы строк ни было. -->
    <Icon name={s.harness} class="mx-1.5 size-3.5 shrink-0 {harnessInfo(s.harness).accent}" />
    {#if workers.length}
      <button class="flex h-8 min-w-12 shrink-0 cursor-pointer items-center justify-center gap-1 rounded-lg border border-neutral-800 px-2 text-xs text-neutral-400 transition-colors hover:border-neutral-600 hover:text-neutral-100" aria-expanded={unfolded} aria-label={`${unfolded ? 'Свернуть' : 'Показать'} Workers: ${workers.length}`} onclick={() => toggle(s.id)}>
        <Icon name="chevron" class="size-3.5 transition-transform {unfolded ? '' : '-rotate-90'}" />{workers.length}
      </button>
    {/if}
    {#if s.hidden}
      <button class="icon-btn mr-1 shrink-0 disabled:cursor-not-allowed disabled:opacity-30" aria-label={`Вернуть сессию ${title} в список`} title="Вернуть в список" disabled={!connected} onclick={() => onrestore(s)}>
        <Icon name="eye" />
      </button>
    {:else}
      <button class="icon-btn mr-1 shrink-0 disabled:cursor-not-allowed disabled:opacity-30" aria-label={`Скрыть сессию ${title} из списка`} title="Скрыть из списка" disabled={!connected} onclick={() => onhide(s)}>
        <Icon name="eye-off" />
      </button>
    {/if}
  </div>
  {#if unfolded}
    <div class="ml-3.5 mt-0.5 mb-1 flex flex-col gap-0.5 border-l border-neutral-800 pl-2.5">
      {#each workers as w (w.id)}{@render sessionRow(w, true)}{/each}
    </div>
  {/if}
{/snippet}

{#if open}
  <button class="fixed inset-0 z-10 cursor-default bg-black/60" aria-label="Закрыть список" onclick={onclose} transition:fade={{ duration: 200 }}></button>
  <div class="fixed inset-y-0 left-0 z-20 w-80 max-w-[85vw]" transition:fly={{ x: -320, duration: 220 }}>
  <aside class="flex h-full flex-col border-r border-neutral-900 bg-neutral-950" style:transform="translateX({offset}px)" style:transition={dragging ? 'none' : 'transform 220ms'} style:touch-action="pan-y" ontouchstart={touchStart} ontouchmove={touchMove} ontouchend={touchEnd} ontouchcancel={touchEnd}>
    <div class="flex items-center justify-between border-b border-neutral-900 px-4 py-3">
      <button class="min-w-0 cursor-pointer text-left" aria-label="Открыть основной чат" onclick={() => onselect(secretaryId)}>
        <span class="block truncate text-sm font-semibold text-neutral-100">Secretary</span>
        {#if secretary}{@render agentLine(secretary)}{/if}
      </button>
      <button class="icon-btn" aria-label="Закрыть" onclick={onclose}>
        <Icon name="close" />
      </button>
    </div>
    <nav class="flex min-h-0 flex-1 flex-col gap-0.5 overflow-y-auto p-2">
      {#each visibleGroups as g (g.session.id)}
        <div class="flex flex-col gap-0.5">{@render sessionRow(g.session, false, g.workers)}</div>
      {/each}
      {#if hiddenSessions.length}
        <div class="mt-2 border-t border-neutral-900 pt-2">
          <button class="flex w-full cursor-pointer items-center justify-between rounded-lg px-3 py-2 text-left text-sm text-neutral-400 transition-colors hover:bg-neutral-900 hover:text-neutral-100 focus-visible:outline focus-visible:outline-2 focus-visible:outline-neutral-300" aria-expanded={hiddenOpen} onclick={() => (hiddenOpen = !hiddenOpen)}>
            <span>Скрытые · {hiddenSessions.length}</span>
            <Icon name="chevron" class="size-4 transition-transform {hiddenOpen ? 'rotate-180' : ''}" />
          </button>
          {#if hiddenOpen}
            <div class="mt-1 flex flex-col gap-1">
              {#each hiddenGroups as g (g.session.id)}
                <div class="flex flex-col gap-0.5">{@render sessionRow(g.session, false, g.workers)}</div>
              {/each}
            </div>
          {/if}
        </div>
      {/if}
    </nav>
    {#if Capacitor.isNativePlatform() && Capacitor.isPluginAvailable('Downloads')}
      <button class="mx-2 cursor-pointer rounded-lg px-3 py-2 text-left text-sm text-neutral-200 hover:bg-neutral-900" onclick={ondownloads}>Загрузки</button>
    {/if}
    <div class="flex items-center gap-1 border-t border-neutral-900 p-2">
      <button class="flex min-w-0 flex-1 cursor-pointer items-center gap-3 rounded-lg px-3 py-2 text-left transition-colors hover:bg-neutral-900" title="Настройки" onclick={onprofile}>
        <span class="grid size-8 shrink-0 place-items-center rounded-full bg-neutral-800 text-sm font-medium text-neutral-100" aria-hidden="true">М</span>
        <span class="truncate text-sm text-neutral-200">Mikhail</span>
      </button>
      <button class="icon-btn shrink-0" aria-label="Лимиты Codex" title="Лимиты Codex" onclick={onlimits}>
        <Icon name="gauge" />
      </button>
      <button class="icon-btn shrink-0" aria-label="Расход OpenCode" title="Расход OpenCode" onclick={onopencode}>
        <Icon name="opencode" />
      </button>
    </div>
  </aside>
  </div>
{/if}
