<script>
  import { notificationsEnabled, setNotificationsEnabled } from '../browser-notifications.js';
  import { Capacitor } from '@capacitor/core';
  import Icon from './Icon.svelte';
  import SkillsScreen from './SkillsScreen.svelte';
  import InstructionsEditor from './InstructionsEditor.svelte';
  import MemoryEditor from './MemoryEditor.svelte';
  import SpecialistModel from './SpecialistModel.svelte';
  import McpServers from './McpServers.svelte';
  import { harnessInfo, modelLabel } from '../agents.js';

  let {
    ttsEngine = '',
    voiceModel = '',
    videoInterval = '1',
    voiceStatus = '',
    assistantStatus = '',
    secretary = null,
    dirty = $bindable(false),
    onback,
    request,
    onsaveTtsEngine,
    onsaveVoiceModel,
    onsaveVideoInterval,
    onrequestAssistant,
    onresetHost,
  } = $props();

  const editors = {
    secretary: { title: 'Secretary Agent', hint: 'Как Secretary Agent общается с тобой и что о тебе помнит.' },
    specialist: { title: 'Specialists', hint: 'Общие инструкции для всех Specialists. Workers их не получают.' },
  };
  let browserNotifications = $state(notificationsEnabled());
  let notificationStatus = $state('');
  async function toggleNotifications(event) {
    notificationStatus = await setNotificationsEnabled(event.target.checked);
    browserNotifications = notificationsEnabled();
    event.target.checked = browserNotifications;
  }
  let screen = $state('main');
  let specialistModel = $state(undefined);

  async function loadSpecialistModel() {
    try { const response = await fetch('/settings/specialist-model'); if (response.ok) specialistModel = await response.json(); } catch {}
  }
  async function saveSpecialistModel(value) {
    const response = await fetch('/settings/specialist-model', {method:'PUT', body:JSON.stringify(value)});
    if (!response.ok) throw new Error(await response.text());
    specialistModel = value;
  }
  loadSpecialistModel();
</script>

{#snippet row(label, hint, onclick)}
  <button type="button" class="flex w-full cursor-pointer items-center gap-3 px-3.5 py-3 text-left transition-colors hover:bg-neutral-800/60" {onclick}>
    <span class="min-w-0 flex-1"><span class="block text-sm text-neutral-100">{label}</span>{#if hint}<span class="block truncate text-xs text-neutral-500">{hint}</span>{/if}</span>
    <Icon name="chevron" class="size-3.5 shrink-0 -rotate-90 text-neutral-600" />
  </button>
{/snippet}

{#snippet choice(label, value, onchange, options)}
  <label class="flex items-center justify-between gap-3 px-3.5 py-2.5 text-sm text-neutral-100"><span class="whitespace-nowrap">{label}</span>
    <select class="min-w-0 cursor-pointer bg-transparent py-1 text-right text-sm text-neutral-400 focus:outline-none disabled:opacity-40" {value} disabled={!value} {onchange}>
      {#each options as [optionValue, text, disabled]}<option class="bg-neutral-900 text-neutral-100" value={optionValue} {disabled}>{text}</option>{/each}
    </select>
  </label>
{/snippet}

{#if screen === 'mcp'}
  <McpServers {request} onback={() => (screen = 'main')} />
{:else if screen === 'secretary' || screen === 'specialist'}
  <InstructionsEditor role={screen} {...editors[screen]} bind:dirty onback={() => (screen = 'main')} />
{:else if screen === 'skills'}
  <SkillsScreen onback={() => (screen = 'main')} />
{:else if screen === 'memory'}
  <MemoryEditor bind:dirty onback={() => (screen = 'main')} />
{:else if screen === 'model' && secretary}
  <SpecialistModel value={specialistModel} {secretary} onsave={saveSpecialistModel} onback={() => (screen = 'main')} />
{:else}
  <div class="flex h-dvh flex-col bg-neutral-950 text-neutral-200">
    <header class="flex items-center gap-2 border-b border-neutral-900 px-2 py-2">
      <button class="icon-btn" aria-label="Назад" onclick={onback}><Icon name="back" /></button>
      <h1 class="min-w-0 flex-1 truncate px-1 text-sm font-medium text-neutral-100">Профиль</h1>
    </header>

    <div class="flex min-h-0 flex-1 flex-col gap-5 overflow-y-auto p-4">
      <section class="flex flex-col gap-2">
        <h2 class="text-[11px] uppercase tracking-wide text-neutral-500">Агенты</h2>
        <div class="divide-y divide-neutral-800/80 overflow-hidden rounded-xl border border-neutral-800 bg-neutral-900/50">
          {@render row('Secretary Agent', 'Инструкции: как общается и что помнит', () => (screen = 'secretary'))}
          {@render row('Skills', 'Навыки и их версии', () => (screen = 'skills'))}
          {@render row('MCP-серверы', 'Внешние инструменты агентов', () => (screen = 'mcp'))}
          {@render row('Память', 'Факты и договорённости', () => (screen = 'memory'))}
          {@render row('Specialists', 'Инструкции для всех Specialists', () => (screen = 'specialist'))}
          <button type="button" class="flex w-full cursor-pointer items-center gap-3 px-3.5 py-3 text-left transition-colors hover:bg-neutral-800/60 disabled:cursor-default" disabled={!secretary} onclick={() => (screen = 'model')}>
            <span class="flex-1 text-sm text-neutral-100">Модель новых Specialists</span>
            {#if specialistModel !== undefined}
              <span class="flex min-w-0 items-center gap-1 truncate text-xs text-neutral-400">
                {#if specialistModel}<Icon name={specialistModel.harness} class="size-3.5 shrink-0 {harnessInfo(specialistModel.harness).accent}" />{modelLabel(specialistModel.model)}{#if specialistModel.reasoning} · {specialistModel.reasoning}{/if}{:else}Как у Secretary Agent{/if}
              </span>
            {/if}
            <Icon name="chevron" class="size-3.5 shrink-0 -rotate-90 text-neutral-600" />
          </button>
        </div>
      </section>

      <section class="flex flex-col gap-2">
        <h2 class="text-[11px] uppercase tracking-wide text-neutral-500">Голос</h2>
        <div class="divide-y divide-neutral-800/80 rounded-xl border border-neutral-800 bg-neutral-900/50">
          {@render choice('Режим', 'radio', () => {}, [['radio', 'Рация'], ['live', 'Живой разговор · скоро', true]])}
          {@render choice('Голос ответов', ttsEngine, onsaveTtsEngine, [['inworld', 'Inworld · облако'], ['piper', 'Piper · на Host']])}
          {@render choice('Распознавание', voiceModel, onsaveVoiceModel, [['gigaam', 'GigaAM · русский'], ['parakeet', 'Parakeet · английский']])}
          {@render choice('Кадры видео', videoInterval, onsaveVideoInterval, [['1', '1 в секунду'], ['2', '1 в 2 секунды']])}
        </div>
        {#if voiceStatus}<p role="alert" class="text-sm text-red-400">{voiceStatus}</p>{/if}
      </section>

        <section class="flex flex-col gap-2">
          <h2 class="text-[11px] uppercase tracking-wide text-neutral-500">Это устройство</h2>
          <div class="divide-y divide-neutral-800/80 overflow-hidden rounded-xl border border-neutral-800 bg-neutral-900/50 text-sm">
            {#if Capacitor.isNativePlatform()}
            <button type="button" class="flex w-full cursor-pointer items-center justify-between gap-3 px-3.5 py-3 text-left text-neutral-100 hover:bg-neutral-800/60" onclick={onrequestAssistant}>
              Открывать Secretary по Power
              {#if assistantStatus}<span role="status" class="text-xs text-neutral-400">{assistantStatus}</span>{/if}
            </button>
            <button type="button" class="w-full cursor-pointer px-3.5 py-3 text-left text-red-400 hover:bg-red-950/30" onclick={onresetHost}>Сбросить адрес Host</button>
            {:else}
            <label class="flex items-center justify-between gap-3 px-3.5 py-3 text-neutral-100">Уведомления в браузере <input type="checkbox" checked={browserNotifications} onchange={toggleNotifications} /></label>
            {/if}
          </div>
          {#if notificationStatus}<p role="alert" class="text-sm text-red-400">{notificationStatus}</p>{/if}
        </section>
    </div>
  </div>
{/if}
