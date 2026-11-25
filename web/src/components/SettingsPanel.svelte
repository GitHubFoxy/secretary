<script>
  import Icon from './Icon.svelte';
  import ModelPicker from './ModelPicker.svelte';
  import { harnessInfo } from '../agents.js';
  // Два разных действия на двух экранах: «этот чат» меняет агента текущего чата, «новый тред» создаёт отдельный чат.
  let { session, connected = false, onsave, oncreate, onstop, onrename } = $props();

  let screen = $state('chat');
  let chat = $state({ harness: session.harness, model: session.model, reasoning: session.reasoning ?? null });
  let thread = $state({ harness: session.harness, model: session.model, reasoning: session.reasoning ?? null, name: '' });
  const changed = $derived(chat.harness !== session.harness || chat.model !== session.model || chat.reasoning !== (session.reasoning ?? null));
  const target = $derived(harnessInfo(chat.harness).title);
</script>

{#if screen === 'chat'}
  <div class="flex flex-col gap-4">
    {#if !session.secretary}
      <button type="button" class="flex min-w-0 cursor-pointer items-center gap-2 text-left disabled:cursor-not-allowed" disabled={!connected} onclick={onrename}>
        <span class="min-w-0">
          <span class="block truncate text-base font-semibold text-neutral-100">{session.name || `Сессия #${session.id}`}</span>
          <span class="block truncate font-mono text-[11px] text-neutral-500">{session.folder}</span>
        </span>
        <Icon name="pencil" class="size-3.5 shrink-0 text-neutral-500" />
      </button>
    {/if}

    <div class="flex flex-col gap-2">
      <span class="text-[11px] uppercase tracking-wide text-neutral-500">Кто отвечает в этом чате</span>
      <ModelPicker bind:harness={chat.harness} bind:model={chat.model} bind:reasoning={chat.reasoning} current={session.harness} />
    </div>

    {#if chat.harness !== session.harness}
      <p class="text-xs leading-snug text-amber-400/90">Чат и история останутся. {target} начнёт новую сессию и получит ваши сообщения и ответы агента (~30k токенов). Шаги и вызовы инструментов не переносятся.</p>
    {/if}
    <button type="button" class="btn w-full rounded-xl" disabled={!changed || !connected} onclick={() => onsave(chat)}>
      {!changed ? 'Без изменений' : chat.harness !== session.harness ? `Переключить этот чат на ${target}` : 'Применить к этому чату'}
    </button>

    <div class="divide-y divide-neutral-800/80 overflow-hidden rounded-xl border border-neutral-800 bg-neutral-900/50 text-sm">
      {#if !session.secretary}
        <button type="button" class="flex w-full cursor-pointer items-center gap-2 px-3.5 py-2.5 text-left text-neutral-200 transition-colors hover:bg-neutral-800/60" onclick={() => (screen = 'thread')}>
          <Icon name="plus" class="size-4 text-neutral-400" />
          <span class="shrink-0">Новый тред в</span>
          <span class="min-w-0 truncate font-mono text-xs text-neutral-400">{session.folder}</span>
          <Icon name="chevron" class="ml-auto size-3.5 shrink-0 -rotate-90 text-neutral-600" />
        </button>
      {/if}
      <button type="button" class="w-full cursor-pointer px-3.5 py-2.5 text-left text-red-400 transition-colors hover:bg-red-950/30" onclick={onstop}>Остановить сессию</button>
    </div>
  </div>
{:else}
  <div class="flex flex-col gap-4">
    <button type="button" class="flex cursor-pointer items-center gap-1 self-start text-sm text-neutral-400 hover:text-neutral-200" onclick={() => (screen = 'chat')}>
      <Icon name="chevron" class="size-3.5 rotate-90" />Назад
    </button>
    <div>
      <h3 class="text-base font-semibold text-neutral-100">Новый тред</h3>
      <p class="text-xs text-neutral-500">Отдельный чат в <span class="font-mono">{session.folder}</span>. Начнёт с пустого контекста, текущий чат не изменится.</p>
    </div>
    <input class="rounded-xl border border-neutral-800 bg-transparent px-3.5 py-2.5 text-sm text-neutral-100 placeholder:text-neutral-600 focus:border-neutral-600 focus:outline-none" placeholder="Название (необязательно)" maxlength="100" bind:value={thread.name} />
    <ModelPicker bind:harness={thread.harness} bind:model={thread.model} bind:reasoning={thread.reasoning} />
    <button type="button" class="btn w-full rounded-xl" disabled={!connected} onclick={() => oncreate(thread)}>Создать тред</button>
  </div>
{/if}
