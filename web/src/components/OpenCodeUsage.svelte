<script>
  import { formatTokens } from '../opencode-usage.js';
  // Провайдер OpenCode (PAG, openai-compatible) лимитов не отдаёт — ни отдельным API,
  // ни в ответах, — поэтому карточка показывает только фактический расход из событий Secretary.
  let { usage = null, status = '', onrefresh } = $props();

  const turns = n => n === 1 ? 'ход' : n > 0 && n < 5 ? 'хода' : 'ходов';
  const dayText = day => new Date(`${day}T12:00:00`).toLocaleDateString('ru-RU', { day: 'numeric', month: 'long' });

  const rows = $derived(usage ? [
    { label: 'Сегодня', totals: usage.today },
    { label: '7 дней', totals: usage.week },
  ] : []);
</script>

<!-- Высота фиксирована, как в CodexLimits, чтобы модалка не прыгала между загрузкой, данными и ошибкой. -->
<div class="h-80 overflow-y-auto">
{#if !usage}
  <p class="break-words text-sm text-neutral-400" role="status">{status}</p>
{:else}
  <div class="flex flex-col gap-5">
    {#each rows as { label, totals }}
      <section class="flex flex-col gap-1.5">
        <div class="flex items-baseline justify-between text-xs text-neutral-400">
          <span>{label}</span>
          <span>{totals.turns} {turns(totals.turns)}</span>
        </div>
        <div class="flex items-baseline gap-4">
          <span class="text-sm text-neutral-100">Вход <span class="font-mono">{formatTokens(totals.inputTokens)}</span></span>
          <span class="text-sm text-neutral-100">Выход <span class="font-mono">{formatTokens(totals.outputTokens)}</span></span>
        </div>
      </section>
    {/each}
    {#if usage.days.length}
      <section class="flex flex-col gap-1.5">
        <h3 class="text-sm font-medium text-neutral-100">По дням</h3>
        {#each usage.days as day}
          <div class="flex items-baseline justify-between text-xs">
            <span class="text-neutral-400">{dayText(day.day)}</span>
            <span class="text-neutral-200"><span class="font-mono">{formatTokens(day.inputTokens + day.outputTokens)}</span> · {day.turns} {turns(day.turns)}</span>
          </div>
        {/each}
      </section>
    {/if}
    <p class="text-xs text-neutral-500">Всего {usage.allTime.turns} {turns(usage.allTime.turns)}: {formatTokens(usage.allTime.inputTokens + usage.allTime.outputTokens)} токенов. Лимитов у провайдера нет — это фактический расход.</p>
    <button type="button" class="btn-ghost self-start" onclick={onrefresh}>Обновить</button>
  </div>
{/if}
</div>
