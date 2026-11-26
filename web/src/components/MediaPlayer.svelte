<script module>
  let active;
</script>
<script>
  import { onMount } from 'svelte';
  import { sleepState } from './sleep-timer.js';
  let { src, video = false, round = false, poster = '', title = 'Медиафайл' } = $props();
  let media = $state(), playing = $state(false), current = $state(0), length = $state(0), speed = $state(1), timer = $state('off'), error = $state('');
  let deadline = null, originalVolume = 1;
  let key = $derived(`media:${src.split('?')[0]}`);
  const format = n => `${Math.floor((n || 0) / 60)}:${String(Math.floor((n || 0) % 60)).padStart(2, '0')}`;
  function save() {
    try {
      const positions = JSON.parse(localStorage.getItem('media-positions') || '{}');
      delete positions[key];
      if (media.currentTime > 0 && !media.ended) positions[key] = media.currentTime;
      localStorage.setItem('media-positions', JSON.stringify(Object.fromEntries(Object.entries(positions).slice(-100))));
    } catch {}
  }
  function restore() {
    length = Number.isFinite(media.duration) ? media.duration : 0;
    try {
      const position = JSON.parse(localStorage.getItem('media-positions') || '{}')[key];
      if (position > 0 && position < length) media.currentTime = position;
    } catch {}
  }
  function selectTimer(value) {
    media.volume = originalVolume;
    timer = value;
    deadline = value === 'off' || value === 'end' ? null : Date.now() + Number(value) * 60000;
    tick();
  }
  function tick() {
    const state = sleepState(deadline);
    if (state.expired) { media.pause(); save(); selectTimer('off'); }
    else if (!media.paused && (deadline || timer === 'end')) {
      const factor = timer === 'end' && length ? Math.max(0, Math.min(1, (length - media.currentTime) / media.playbackRate / 10)) : state.volume;
      media.volume = originalVolume * factor;
    }
  }
  function play() { media.play().catch(() => error = 'Не удалось начать воспроизведение'); }
  function onPlay() {
    if (active && active !== media) active.pause();
    active = media; playing = true; error = '';
    if ('mediaSession' in navigator) {
      navigator.mediaSession.metadata = new MediaMetadata({ title });
      for (const [action, handler] of Object.entries({ play, pause: () => media.pause(), seekbackward: () => seek(-10), seekforward: () => seek(10), seekto: e => { if (e.seekTime != null) media.currentTime = e.seekTime; } })) {
        try { navigator.mediaSession.setActionHandler(action, handler); } catch {}
      }
      navigator.mediaSession.playbackState = 'playing';
    }
    tick();
  }
  function seek(delta) { media.currentTime = Math.max(0, Math.min(length, media.currentTime + delta)); }
  function pause() { playing = false; media.volume = originalVolume; save(); if (active === media && 'mediaSession' in navigator) navigator.mediaSession.playbackState = 'paused'; }
  function ended() { save(); selectTimer('off'); }
  async function fullscreen() { try { await media.requestFullscreen(); } catch { error = 'Полный экран недоступен'; } }
  onMount(() => {
    const interval = setInterval(tick, 250);
    const saving = setInterval(() => { if (!media.paused) save(); }, 5000);
    const visibility = () => { tick(); save(); };
    document.addEventListener('visibilitychange', visibility);
    window.addEventListener('pagehide', save);
    return () => { save(); clearInterval(interval); clearInterval(saving); document.removeEventListener('visibilitychange', visibility); window.removeEventListener('pagehide', save); if (active === media) { active = null; if ('mediaSession' in navigator) { for (const action of ['play', 'pause', 'seekbackward', 'seekforward', 'seekto']) { try { navigator.mediaSession.setActionHandler(action, null); } catch {} } navigator.mediaSession.playbackState = 'none'; } } };
  });
</script>
<div class="my-2 w-80 max-w-full rounded-lg bg-neutral-900 p-2 text-sm text-neutral-100">
  {#if video}
    <video bind:this={media} {src} {poster} playsinline preload="metadata" class:rounded-full={round} class:size-[170px]={round} class:object-cover={round} class="max-h-80 max-w-full rounded-lg" onloadedmetadata={restore} ontimeupdate={() => { current = media.currentTime; }} onplay={onPlay} onpause={pause} onended={ended} onerror={() => error = 'Не удалось загрузить файл'}><track kind="captions" /></video>
  {:else}
    <audio bind:this={media} {src} preload="metadata" onloadedmetadata={restore} ontimeupdate={() => { current = media.currentTime; }} onplay={onPlay} onpause={pause} onended={ended} onerror={() => error = 'Не удалось загрузить файл'}></audio>
  {/if}
  <div class="flex flex-wrap items-center gap-2 py-2">
    <button type="button" aria-label="Назад на 10 секунд" onclick={() => seek(-10)}>−10</button>
    <button type="button" aria-label={playing ? 'Пауза' : 'Воспроизвести'} onclick={() => playing ? media.pause() : play()}>{playing ? '❚❚' : '▶'}</button>
    <button type="button" aria-label="Вперёд на 10 секунд" onclick={() => seek(10)}>+10</button>
    <select aria-label="Скорость" bind:value={speed} onchange={() => media.playbackRate = Number(speed)}>{#each [1, 1.25, 1.5, 2] as rate}<option value={rate}>{rate}×</option>{/each}</select>
    <select aria-label="Таймер сна" value={timer} onchange={e => selectTimer(e.currentTarget.value)}><option value="off">Таймер: выкл.</option><option value="15">15 мин</option><option value="30">30 мин</option><option value="60">60 мин</option><option value="end">В конце файла</option></select>
    {#if video}<button type="button" aria-label="Полный экран" onclick={fullscreen}>⛶</button>{/if}
  </div>
  <div class="flex items-center gap-2"><span>{format(current)}</span><input class="min-w-0 flex-1" type="range" aria-label="Позиция воспроизведения" min="0" max={length || 0} step="0.1" value={current} oninput={e => media.currentTime = Number(e.currentTarget.value)} /><span>{format(length)}</span></div>
  {#if error}<p role="alert" class="text-red-400">{error}</p>{/if}
</div>
