<script>
  import { blockedBySpentWeek } from '../codex-limits.js';
  // Окна лимитов приходят в минутах: 300 — пять часов, 10080 — неделя.
  let { limits = null, status = '', onrefresh } = $props();

  const windowName = minutes => {
    if (!minutes) return 'Окно';
    if (minutes % 10080 === 0) return minutes === 10080 ? 'Неделя' : `${minutes / 10080} нед.`;
    if (minutes % 1440 === 0) return `${minutes / 1440} дн.`;
    if (minutes % 60 === 0) return `${minutes / 60} ч`;
    return `${minutes} мин`;
  };
  const resetText = seconds => new Date(seconds * 1000).toLocaleString('ru-RU', { day: 'numeric', month: 'long', hour: '2-digit', minute: '2-digit' });
  const left = window => Math.max(0, 100 - window.usedPercent);
  const barClass = percent => percent >= 100 ? 'bg-red-500' : percent >= 80 ? 'bg-amber-500' : 'bg-emerald-500';

  const minute = 60_000;
  let now = $state(Date.now());
  let lastRefresh = 0;

  const countdown = seconds => {
    const left = Math.max(0, seconds * 1000 - now);
    const parts = [[Math.floor(left / (24 * 60 * minute)), 'д'], [Math.floor(left / (60 * minute)) % 24, 'ч'], [Math.floor(left / minute) % 60, 'м']];
    // Как в T3 Code: начинаем с крупнейшей ненулевой единицы. Берём две, чтобы отсчёт был виден поминутно.
    const first = parts.findIndex(([n]) => n > 0);
    if (first < 0) return left > 0 ? '<1м' : 'сейчас';
    return parts.slice(first, first + 2).filter(([n]) => n > 0).map(([n, u]) => `${n}${u}`).join(' ');
  };

  const snapshots = $derived.by(() => {
    if (!limits) return [];
    const byId = Object.values(limits.rateLimitsByLimitId ?? {}).filter(Boolean);
    return byId.length ? byId : [limits.rateLimits];
  });

  $effect(() => {
    const timer = setInterval(() => {
      now = Date.now();
      const passed = snapshots.some(s => [s.primary, s.secondary].some(w => w?.resetsAt && w.resetsAt * 1000 <= now));
      if (passed && now - lastRefresh >= minute) {
        lastRefresh = now;
        onrefresh?.();
      }
    }, minute);
    return () => clearInterval(timer);
  });
</script>

<!-- Высота и прокрутка — у модалки «Лимиты»: внутри неё секции Codex и OpenCode. -->
<div>
{#if !limits}
  <p class="break-words text-sm text-neutral-400" role="status">{status}</p>
{:else}
  <div class="flex flex-col gap-5">
    {#each snapshots as snapshot}
      <section class="flex flex-col gap-3">
        <div class="flex items-baseline justify-between gap-3">
          <h3 class="text-sm font-medium text-neutral-100">{snapshot.limitName || snapshot.limitId || 'Codex'}</h3>
          {#if snapshot.planType}<span class="font-mono text-xs text-neutral-500">{snapshot.planType}</span>{/if}
        </div>
        {#if snapshot.rateLimitReachedType}
          <p class="text-sm text-red-400">Лимит исчерпан</p>
        {/if}
        {#each blockedBySpentWeek(snapshot, now) as window}
          <div class="flex flex-col gap-1.5">
            <div class="flex items-baseline justify-between text-xs text-neutral-400">
              <span>{windowName(window.windowDurationMins)}</span>
              <span><span class="font-mono text-neutral-200">{left(window)}%</span> осталось</span>
            </div>
            <div class="h-1.5 overflow-hidden rounded-full bg-neutral-800" role="meter" aria-label="Осталось" aria-valuemin="0" aria-valuemax="100" aria-valuenow={left(window)}>
              <div class="h-full rounded-full {barClass(window.usedPercent)}" style:width="{left(window)}%"></div>
            </div>
            {#if window.resetsAt}
              <span class="text-xs text-neutral-500">До сброса: <span class="font-mono text-neutral-300">{countdown(window.resetsAt)}</span> · {resetText(window.resetsAt)}</span>
            {/if}
          </div>
        {/each}
      </section>
    {/each}
  </div>
{/if}
</div>
