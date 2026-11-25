<script>
  // Окна лимитов приходят в минутах: 300 — пять часов, 10080 — неделя.
  let { limits = null, status = '' } = $props();

  const windowName = minutes => {
    if (!minutes) return 'Окно';
    if (minutes % 10080 === 0) return minutes === 10080 ? 'Неделя' : `${minutes / 10080} нед.`;
    if (minutes % 1440 === 0) return `${minutes / 1440} дн.`;
    if (minutes % 60 === 0) return `${minutes / 60} ч`;
    return `${minutes} мин`;
  };
  const resetText = seconds => new Date(seconds * 1000).toLocaleString('ru-RU', { day: 'numeric', month: 'long', hour: '2-digit', minute: '2-digit' });
  const barClass = percent => percent >= 100 ? 'bg-red-500' : percent >= 80 ? 'bg-amber-500' : 'bg-emerald-500';

  const snapshots = $derived.by(() => {
    if (!limits) return [];
    const byId = Object.values(limits.rateLimitsByLimitId ?? {}).filter(Boolean);
    return byId.length ? byId : [limits.rateLimits];
  });
</script>

<!-- Высота фиксирована, чтобы модалка не прыгала между загрузкой, данными и ошибкой. Длинный текст прокручивается внутри. -->
<div class="h-80 overflow-y-auto">
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
        {#each [snapshot.primary, snapshot.secondary].filter(Boolean) as window}
          <div class="flex flex-col gap-1.5">
            <div class="flex items-baseline justify-between text-xs text-neutral-400">
              <span>{windowName(window.windowDurationMins)}</span>
              <span class="font-mono text-neutral-200">{window.usedPercent}%</span>
            </div>
            <div class="h-1.5 overflow-hidden rounded-full bg-neutral-800" role="meter" aria-label="Использовано" aria-valuemin="0" aria-valuemax="100" aria-valuenow={window.usedPercent}>
              <div class="h-full rounded-full {barClass(window.usedPercent)}" style:width="{Math.min(100, window.usedPercent)}%"></div>
            </div>
            {#if window.resetsAt}
              <span class="text-xs text-neutral-500">Сброс: {resetText(window.resetsAt)}</span>
            {/if}
          </div>
        {/each}
      </section>
    {/each}
  </div>
{/if}
</div>
