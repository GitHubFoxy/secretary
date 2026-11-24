<script>
  import { Capacitor } from '@capacitor/core';
  import Icon from './Icon.svelte';

  let {
    ttsEngine = '',
    voiceModel = '',
    videoInterval = '1',
    voiceStatus = '',
    assistantStatus = '',
    agents = $bindable(''),
    agentsLoaded = false,
    agentsStatus = '',
    onback,
    onsaveTtsEngine,
    onsaveVoiceModel,
    onsaveVideoInterval,
    onrequestAssistant,
    onresetHost,
    onsaveAgents,
  } = $props();

  let voiceModeHint = $state(false);
</script>

<div class="flex h-dvh flex-col bg-neutral-950 text-neutral-200">
  <header class="flex items-center gap-2 border-b border-neutral-900 px-2 py-2">
    <button class="icon-btn" aria-label="Назад" onclick={onback}>
      <Icon name="back" />
    </button>
    <h1 class="min-w-0 flex-1 truncate px-1 text-sm font-medium text-neutral-100">Секретарь</h1>
  </header>

  <div class="flex min-h-0 flex-1 flex-col gap-4 p-4">
    <div class="flex flex-col gap-2 text-sm">Голосовые ответы
      <div class="grid grid-cols-2 gap-2">
        <button type="button" aria-pressed="true" class="flex flex-col items-start gap-1 rounded-2xl border border-neutral-500 bg-neutral-900 p-3 text-left">
          <span class="flex items-center gap-2 font-medium text-neutral-100"><Icon name="microphone" class="size-4" />Рация</span>
          <span class="text-xs leading-snug text-neutral-400">Держишь кнопку, говоришь — Secretary отвечает голосом</span>
        </button>
        <button type="button" aria-disabled="true" onclick={() => { voiceModeHint = true; setTimeout(() => voiceModeHint = false, 1500); }} class="relative flex flex-col items-start gap-1 rounded-2xl border border-neutral-800 p-3 text-left">
          <span class="flex items-center gap-2 font-medium text-neutral-500">Живой разговор<span class="rounded-full border border-neutral-700 px-1.5 text-[10px] leading-4 text-neutral-400">скоро</span></span>
          <span class="text-xs leading-snug text-neutral-600">Говоришь без кнопки, как по телефону</span>
          {#if voiceModeHint}<span role="status" class="absolute -top-9 right-0 rounded-lg bg-neutral-800 px-3 py-1.5 text-xs text-neutral-200 shadow-lg">Скоро. Пока работает Рация</span>{/if}
        </button>
      </div>
    </div>
    <label class="flex flex-col gap-2 text-sm">Голос ответов
      <select class="field" value={ttsEngine} disabled={!ttsEngine} onchange={onsaveTtsEngine}>
        <option value="inworld">Inworld — облако, звучит естественнее</option>
        <option value="piper">Piper — бесплатно, на Host</option>
      </select>
    </label>
    <label class="flex flex-col gap-2 text-sm">Распознавание речи на Host
      <select class="field" value={voiceModel} disabled={!voiceModel} onchange={onsaveVoiceModel}>
        <option value="gigaam">GigaAM — быстрее, лучше русский</option>
        <option value="parakeet">Parakeet — лучше английские слова</option>
      </select>
    </label>
    <label class="flex flex-col gap-2 text-sm">Частота кадров видео
      <select class="field" value={videoInterval} onchange={onsaveVideoInterval}>
        <option value="1">1 кадр в секунду</option>
        <option value="2">1 кадр в 2 секунды</option>
      </select>
    </label>
    {#if voiceStatus}<p role="alert" class="text-sm text-red-400">{voiceStatus}</p>{/if}
    {#if Capacitor.isNativePlatform()}
      <div class="flex flex-wrap items-center gap-3 text-sm">
        <button class="btn-ghost" onclick={onrequestAssistant}>Открывать Secretary по Power</button>
        {#if assistantStatus}<span role="status" class="text-xs text-neutral-400">{assistantStatus}</span>{/if}
      </div>
      <button class="btn-ghost self-start" onclick={onresetHost}>Сбросить адрес Host</button>
    {/if}
    <textarea
      class="field min-h-0 flex-1 resize-none font-mono"
      aria-label="Содержимое AGENTS.md"
      spellcheck="false"
      disabled={!agentsLoaded}
      bind:value={agents}
    ></textarea>
    <div class="flex items-center justify-end gap-3">
      <span role="status" class="text-xs text-neutral-500">{agentsStatus}</span>
      <button class="btn" onclick={onsaveAgents} disabled={!agentsLoaded}>Сохранить</button>
    </div>
  </div>
</div>
