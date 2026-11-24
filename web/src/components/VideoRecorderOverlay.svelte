<script>
  import Icon from './Icon.svelte';

  let { preview = $bindable(), facing = 'user', locked = false, progress = 0, onflip } = $props();
</script>

<div class="pointer-events-none fixed inset-0 bg-black/60 backdrop-blur-[3px]"></div>
<div class="pointer-events-none absolute bottom-full left-1/2 mb-8 flex -translate-x-1/2 flex-col items-center">
  <div class="relative size-[242px]">
    <video bind:this={preview} muted playsinline class="absolute inset-[6px] size-[230px] rounded-full object-cover" style:transform={facing === 'user' ? 'scaleX(-1)' : ''}></video>
    <svg class="absolute inset-0" width="242" height="242" viewBox="0 0 242 242" aria-hidden="true">
      <circle cx="121" cy="121" r="119" fill="none" stroke="rgba(255,255,255,.15)" stroke-width="4" />
      <circle cx="121" cy="121" r="119" fill="none" stroke="#ef4444" stroke-width="4" stroke-linecap="round" stroke-dasharray="747.7" stroke-dashoffset={747.7 * (1 - Math.min(1, progress))} transform="rotate(-90 121 121)" />
    </svg>
    {#if locked}
      <button type="button" aria-label="Сменить камеру" class="pointer-events-auto absolute bottom-0 right-1 grid size-[34px] place-items-center rounded-full border border-neutral-700 bg-neutral-900/90 text-neutral-200" onclick={onflip}>
        <Icon name="flip" class="size-4" />
      </button>
    {/if}
  </div>
  {#if !locked}<p class="mt-3 text-xs text-neutral-400">Вверх — запись без рук</p>{/if}
</div>
