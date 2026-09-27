<script lang="ts">
  import type { Bucket } from '../lib/api';
  import { barKind, bucketUptime, fmtMs, fmtPct, hourRange } from '../lib/format';

  let { bars }: { bars: Bucket[] } = $props();

  function tip(b: Bucket): string {
    const up = bucketUptime(b);
    if (up === null) return `${hourRange(b.t)}\nVeri yok`;
    let s = `${hourRange(b.t)}\nUptime ${fmtPct(up)}`;
    if (b.down > 0) s += ` · ${b.down} hata`;
    if (b.ping >= 0) s += `\nOrt. yanıt ${fmtMs(b.ping)}`;
    return s;
  }
</script>

<div class="bars">
  {#each bars as b (b.t)}
    <span class="bar {barKind(b)}" data-tip={tip(b)}></span>
  {/each}
</div>

<style>
  .bars {
    display: flex;
    align-items: stretch;
    gap: 3px;
    height: 26px;
  }
  .bar {
    flex: 1 1 0;
    min-width: 4px;
    max-width: 9px;
    border-radius: 3px;
    background: var(--empty-bar);
    transition: transform 0.1s;
  }
  .bar:hover {
    transform: scaleY(1.12);
  }
  .bar.up {
    background: var(--up);
  }
  .bar.down {
    background: var(--down);
  }
  .bar.mixed {
    background: var(--pending);
  }
  @media (max-width: 640px) {
    /* Dar ekranda son 12 saat */
    .bar:nth-child(-n + 12) {
      display: none;
    }
    .bar {
      max-width: none;
    }
  }
</style>
