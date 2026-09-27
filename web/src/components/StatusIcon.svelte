<script lang="ts">
  import type { StatusKind } from '../lib/format';

  let { kind, size = 34, pulse = false }: { kind: StatusKind; size?: number; pulse?: boolean } = $props();
</script>

<span class="si {kind}" class:pulse style="--s:{size}px" aria-hidden="true">
  <svg viewBox="0 0 24 24" width={size * 0.5} height={size * 0.5} fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round" stroke-linejoin="round">
    {#if kind === 'up'}
      <path d="m6 15 6-6 6 6" />
    {:else if kind === 'down'}
      <path d="m6 9 6 6 6-6" />
    {:else if kind === 'pending'}
      <path d="M6.5 12h.01M12 12h.01M17.5 12h.01" stroke-width="4" />
    {:else}
      <path d="M9 6v12M15 6v12" />
    {/if}
  </svg>
</span>

<style>
  .si {
    position: relative;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: var(--s);
    height: var(--s);
    border-radius: 50%;
    flex-shrink: 0;
    color: var(--on-down);
  }
  .si.up {
    background: var(--up);
    color: var(--on-up);
  }
  .si.down {
    background: var(--down);
  }
  .si.pending {
    background: var(--pending);
    color: var(--on-pending);
  }
  .si.paused {
    background: var(--paused);
    color: var(--on-paused);
  }
  .si.pulse::after {
    content: '';
    position: absolute;
    inset: 0;
    border-radius: 50%;
    background: inherit;
    z-index: -1;
    animation: ring 2.2s ease-out infinite;
  }
  .si.pulse {
    z-index: 0;
  }
  @keyframes ring {
    0% {
      transform: scale(1);
      opacity: 0.55;
    }
    100% {
      transform: scale(1.6);
      opacity: 0;
    }
  }
</style>
