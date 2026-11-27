<script>
  // Окна Go: rolling — 5 часов, weekly — неделя, monthly — месяц. Формат — из GET /opencode/limits.
  let { limits = null, status = '', onrefresh } = $props();

  const names = { rolling: '5 часов', weekly: 'Неделя', monthly: 'Месяц' };
  const left = window => Math.max(0, 100 - window.percent);
  const barClass = percent => percent >= 100 ? 'bg-red-500' : percent >= 80 ? 'bg-amber-500' : 'bg-emerald-500';
  const resetText = iso => new Date(iso).toLocaleString('ru-RU', { day: 'numeric', month: 'long', hour: '2-digit', minute: '2-digit' });

  const minute = 60_000;
  let now = $state(Date.now());
  let lastRefresh = 0;

  const countdown = iso => {
    const left = Math.max(0, new Date(iso).getTime() - now);
    const parts = [[Math.floor(left / (24 * 60 * minute)), 'д'], [Math.floor(left / (60 * minute)) % 24, 'ч'], [Math.floor(left / minute) % 60, 'м']];
    const first = parts.findIndex(([n]) => n > 0);
    if (first < 0) return left > 0 ? '<1м' : 'сейчас';
    return parts.slice(first, first + 2).filter(([n]) => n > 0).map(([n, u]) => `${n}${u}`).join(' ');
  };

  $effect(() => {
    const timer = setInterval(() => {
      now = Date.now();
      const passed = (limits?.windows ?? []).some(w => w.resetsAt && new Date(w.resetsAt).getTime() <= now);
      if (passed && now - lastRefresh >= minute) {
        lastRefresh = now;
        onrefresh?.();
      }
    }, minute);
    return () => clearInterval(timer);
  });
</script>

{#if !limits}
  <p class="break-words text-sm text-neutral-400" role="status">{status}</p>
{:else}
  <div class="flex flex-col gap-3">
    {#each limits.windows as window}
      <div class="flex flex-col gap-1.5">
        <div class="flex items-baseline justify-between text-xs text-neutral-400">
          <span>{names[window.id] ?? window.id}{#if window.limited} · <span class="text-red-400">лимит исчерпан</span>{/if}</span>
          <span><span class="font-mono text-neutral-200">{left(window)}%</span> осталось</span>
        </div>
        <div class="h-1.5 overflow-hidden rounded-full bg-neutral-800" role="meter" aria-label="Осталось" aria-valuemin="0" aria-valuemax="100" aria-valuenow={left(window)}>
          <div class="h-full rounded-full {barClass(window.percent)}" style:width="{left(window)}%"></div>
        </div>
        {#if window.resetsAt}
          <span class="text-xs text-neutral-500">До сброса: <span class="font-mono text-neutral-300">{countdown(window.resetsAt)}</span> · {resetText(window.resetsAt)}</span>
        {/if}
      </div>
    {/each}
    <button type="button" class="btn-ghost self-start" onclick={onrefresh}>Обновить</button>
  </div>
{/if}
