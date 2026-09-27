<script lang="ts">
  import type { Bucket } from '../lib/api';
  import { barKind, bucketUptime, fmtMs, fmtPct, hourRange } from '../lib/format';

  let { bars }: { bars: Bucket[] } = $props();

  function tip(b: Bucket, current: boolean): string {
    const head = hourRange(b.t) + (current ? ' (devam ediyor)' : '');
    const up = bucketUptime(b);
    if (up === null) return `${head}\nVeri yok`;
    let s = `${head}\nUptime ${fmtPct(up)}`;
    if (b.down > 0) s += ` · ${b.down} hatalı kontrol`;
    if (b.ping >= 0) s += `\nOrt. yanıt ${fmtMs(b.ping)}`;
    return s;
  }

  // Ekran okuyucu için tek cümlelik özet (çubukların tek tek ipuçları fareyle görünür).
  const summary = $derived.by(() => {
    let up = 0;
    let down = 0;
    let bad = 0;
    for (const b of bars) {
      up += b.up;
      down += b.down;
      if (b.down > 0) bad++;
    }
    if (up + down === 0) return `Son ${bars.length} saat: veri yok`;
    return `Son ${bars.length} saat: uptime ${fmtPct((100 * up) / (up + down))}${bad ? `, ${bad} saatte hata` : ', hata yok'}`;
  });

  /** Hem çalışan hem çalışmayan kontrol olan saatte kırmızının payı (%). */
  function downShare(b: Bucket): number | null {
    return b.up > 0 && b.down > 0 ? Math.round((100 * b.down) / (b.up + b.down)) : null;
  }
</script>

<div class="bars" role="img" aria-label={summary}>
  {#each bars as b, i (b.t)}
    {@const share = downShare(b)}
    <!-- Karışık saat oranına göre bölünür (altta kırmızı, üstte yeşil): toparlanma
         saat bitmeden görünür. -->
    <span
      class="bar {share === null ? barKind(b) : 'split'}"
      class:current={i === bars.length - 1}
      style={share === null ? undefined : `--down-share: ${share}%`}
      data-tip={tip(b, i === bars.length - 1)}
    ></span>
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
  .bar.split {
    background: linear-gradient(to top, var(--down) var(--down-share), var(--up) var(--down-share));
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
