<script lang="ts">
  // Küçük çizgi grafik (kütüphanesiz, satır içi SVG). Negatif değerler boşluktur;
  // `fail` değeri (ör. başarısız kontrol) altta kırmızı çentik olarak işaretlenir.
  let {
    values,
    w = 72,
    h = 22,
    min,
    max,
    fail,
    tone = 'accent',
    label,
  }: {
    values: number[];
    w?: number;
    h?: number;
    /** Ölçeğin alt/üst sınırı; verilmezse değerlerden hesaplanır. */
    min?: number;
    max?: number;
    /** Bu değer başarısız nokta olarak işaretlenir (çizgi kesilir). */
    fail?: number;
    tone?: 'accent' | 'warn' | 'down' | 'muted';
    label?: string;
  } = $props();

  const PAD = 2;

  const geo = $derived.by(() => {
    const n = values.length;
    const ok = values.filter((v) => v >= 0);
    if (n < 2 || ok.length === 0) return null;
    let lo = min ?? Math.min(...ok);
    let hi = max ?? Math.max(...ok);
    if (min === undefined && max === undefined) {
      // Dar aralıkta küçük oynamalar dağ gibi görünmesin: aralık en az üst değerin %40'ı.
      const span = Math.max(hi - lo, hi * 0.4, 4);
      const mid = (hi + lo) / 2;
      lo = Math.max(0, mid - span / 2);
      hi = lo + span;
    }
    if (hi <= lo) hi = lo + 1;
    const step = (w - PAD * 2) / (n - 1);
    const x = (i: number) => PAD + i * step;
    const y = (v: number) => PAD + (h - PAD * 2) * (1 - (Math.min(hi, Math.max(lo, v)) - lo) / (hi - lo));
    // Boşluklarla bölünmüş çizgi parçaları.
    const segs: [number, number][][] = [];
    let cur: [number, number][] = [];
    const fails: number[] = [];
    values.forEach((v, i) => {
      if (v >= 0) cur.push([x(i), y(v)]);
      else {
        if (cur.length) segs.push(cur);
        cur = [];
        if (fail !== undefined && v === fail) fails.push(x(i));
      }
    });
    if (cur.length) segs.push(cur);
    const line = segs.map((s) => (s.length === 1 ? `M${s[0][0] - 1.2},${s[0][1]}h2.4` : 'M' + s.map((p) => `${p[0].toFixed(1)},${p[1].toFixed(1)}`).join('L'))).join('');
    const area = segs
      .filter((s) => s.length > 1)
      .map((s) => `M${s[0][0].toFixed(1)},${h}L` + s.map((p) => `${p[0].toFixed(1)},${p[1].toFixed(1)}`).join('L') + `L${s[s.length - 1][0].toFixed(1)},${h}Z`)
      .join('');
    const last = segs.length ? segs[segs.length - 1][segs[segs.length - 1].length - 1] : null;
    const lastIsEnd = values[n - 1] >= 0;
    return { line, area, fails, last: lastIsEnd ? last : null };
  });
</script>

<svg
  class="spark {tone}"
  width={w}
  height={h}
  viewBox="0 0 {w} {h}"
  role={label ? 'img' : undefined}
  aria-label={label}
  aria-hidden={label ? undefined : 'true'}
>
  {#if geo}
    <path class="area" d={geo.area} />
    <path class="line" d={geo.line} />
    {#each geo.fails as fx, i (i)}<rect class="fail" x={fx - 1} y={h - 5} width="2" height="5" rx="1" />{/each}
    {#if geo.last}<circle class="end" cx={geo.last[0]} cy={geo.last[1]} r="1.8" />{/if}
  {:else}
    <line class="none" x1={PAD} x2={w - PAD} y1={h / 2} y2={h / 2} />
  {/if}
</svg>

<style>
  .spark {
    display: block;
    flex-shrink: 0;
    overflow: visible;
    --c: var(--spark);
  }
  .spark.warn {
    --c: var(--pending);
  }
  .spark.down {
    --c: var(--down);
  }
  .spark.muted {
    --c: var(--paused);
  }
  .area {
    fill: var(--c);
    opacity: 0.13;
  }
  .line {
    fill: none;
    stroke: var(--c);
    stroke-width: 1.5;
    stroke-linejoin: round;
    stroke-linecap: round;
  }
  .end {
    fill: var(--c);
  }
  .fail {
    fill: var(--down);
  }
  .none {
    stroke: var(--border-strong);
    stroke-width: 1.5;
    stroke-dasharray: 2 3;
  }
</style>
