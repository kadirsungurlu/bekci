<script lang="ts" module>
  export interface ChartSeries {
    label: string;
    /** CSS rengi; tema uyumu için değişken önerilir (ör. var(--chart-1)). */
    color: string;
    values: (number | null)[];
    /** Yığılı alan: stack=true olan seriler sırayla üst üste toplanır (RAM: kullanılan + önbellek). */
    stack?: boolean;
    /** Çizginin altını hafifçe doldur (yığılı olmayan seri). */
    fill?: boolean;
    dashed?: boolean;
  }

  /** İki değer arasındaki bant (ör. CPU ortalaması ile tepe değeri). */
  export interface ChartBand {
    label: string;
    color: string;
    lo: (number | null)[];
    hi: (number | null)[];
  }
</script>

<script lang="ts">
  // Genel amaçlı, elle yazılmış SVG çizgi grafiği (PingChart ile aynı görsel dil).
  // Çok seri, isteğe bağlı bant, yığılı alan, boşlukta kesilen çizgi ve
  // fare/dokunmatik ile tüm serileri gösteren ipucu. Kütüphane kullanılmaz.
  import { fmtShortDate, fmtTime, tzDayStart, tzOffset } from '../lib/format';

  let {
    times,
    series,
    band = null,
    from,
    to,
    interval,
    format,
    axisFormat,
    max,
    minTop = 0,
    bytes = false,
    label,
    height,
  }: {
    times: number[];
    series: ChartSeries[];
    band?: ChartBand | null;
    from: number;
    to: number;
    /** Noktalar arası beklenen süre (sn); bunun 2 katından uzun boşlukta çizgi kesilir. */
    interval: number;
    /** İpucundaki değer biçimi. */
    format: (v: number) => string;
    /** Eksen etiketi biçimi (verilmezse format). */
    axisFormat?: (v: number) => string;
    /** Sabit üst sınır (ör. yüzde için 100). */
    max?: number;
    /** Eksen en az bu değere kadar çizilir (düz sıfır çizgisi ekranı kaplamasın). */
    minTop?: number;
    /** Değerler bayt ise eksen adımları 1024'ün katlarına oturur. */
    bytes?: boolean;
    /** Ekran okuyucu için grafik adı. */
    label: string;
    height?: number;
  } = $props();

  const uid = $props.id();

  let w = $state(0);
  const H = $derived(height ?? (w < 520 ? 170 : 210));
  const axisFmt = $derived(axisFormat ?? format);
  // Sol boşluk eksen etiketinin uzunluğuna göre (bayt hızları daha geniş).
  const PAD = $derived({ l: bytes ? 66 : 44, r: 10, t: 12, b: 24 });
  const plotW = $derived(Math.max(10, w - PAD.l - PAD.r));
  const plotH = $derived(H - PAD.t - PAD.b);
  const gap = $derived(Math.max(interval * 2, 1));

  // Yığılı serilerin kümülatif değerleri: [alt, üst] çiftleri.
  const layers = $derived.by(() => {
    const n = times.length;
    const acc = new Array<number>(n).fill(0);
    return series.map((s) => {
      if (!s.stack) return { lo: null, hi: s.values };
      const lo = acc.slice();
      const hi = s.values.map((v, i) => (v === null ? null : (acc[i] += v)));
      return { lo, hi };
    });
  });

  function niceStep(raw: number): number {
    const p = Math.pow(10, Math.floor(Math.log10(raw)));
    const n = raw / p;
    return (n <= 1 ? 1 : n <= 2 ? 2 : n <= 2.5 ? 2.5 : n <= 5 ? 5 : 10) * p;
  }

  const yScale = $derived.by(() => {
    if (max !== undefined && max > 0) {
      const step = max / 4;
      return { top: max, ticks: [0, step, step * 2, step * 3, max] };
    }
    let hi = 0;
    for (const l of layers) for (const v of l.hi) if (v !== null && v > hi) hi = v;
    if (band) for (const v of band.hi) if (v !== null && v > hi) hi = v;
    hi = Math.max(hi, minTop);
    if (hi <= 0) hi = 1;
    let step: number;
    if (bytes) {
      // Birim (KB, MB…) seçilip adım o birimde yuvarlanır: "256 KB/sn", "1 MB/sn".
      let unit = 1;
      while (hi / unit >= 1024) unit *= 1024;
      step = niceStep((hi * 1.08) / unit / 4) * unit;
    } else {
      step = niceStep((hi * 1.08) / 4);
    }
    const top = Math.ceil((hi * 1.02) / step) * step;
    const ticks: number[] = [];
    for (let v = 0; v <= top + step * 1e-6; v += step) ticks.push(v);
    return { top, ticks };
  });

  const xOf = (t: number) => PAD.l + ((t - from) / Math.max(1, to - from)) * plotW;
  const yOf = (v: number) => PAD.t + plotH - (Math.max(0, Math.min(v, yScale.top)) / yScale.top) * plotH;

  /** Değeri olan, arada büyük boşluk bulunmayan ardışık nokta grupları. */
  function segments(vals: (number | null)[], lo?: (number | null)[] | null): number[][] {
    const out: number[][] = [];
    let cur: number[] = [];
    for (let i = 0; i < times.length; i++) {
      const ok = vals[i] !== null && vals[i] !== undefined && (!lo || (lo[i] !== null && lo[i] !== undefined));
      if (!ok || (cur.length && times[i] - times[cur[cur.length - 1]] > gap)) {
        if (cur.length) out.push(cur);
        cur = [];
      }
      if (ok) cur.push(i);
    }
    if (cur.length) out.push(cur);
    return out;
  }

  const f1 = (n: number) => n.toFixed(1);
  const lineOf = (idx: number[], vals: (number | null)[]) =>
    idx.map((i, k) => `${k ? 'L' : 'M'}${f1(xOf(times[i]))},${f1(yOf(vals[i]!))}`).join('');
  // Üst kenar soldan sağa, alt kenar sağdan sola: kapalı alan.
  const areaOf = (idx: number[], hi: (number | null)[], lo: ((number | null)[] | null) | 'base') => {
    const top = lineOf(idx, hi);
    const bottom = idx
      .slice()
      .reverse()
      .map((i) => `L${f1(xOf(times[i]))},${f1(lo === 'base' || !lo ? PAD.t + plotH : yOf(lo[i] ?? 0))}`)
      .join('');
    return `${top}${bottom}Z`;
  };

  const paths = $derived.by(() =>
    series.map((s, si) => {
      const { lo, hi } = layers[si];
      let line = '';
      let area = '';
      for (const idx of segments(hi, lo)) {
        // Tek noktalı parça da görünsün diye kısa yatay çizgi.
        line += idx.length === 1 ? `M${f1(xOf(times[idx[0]]) - 1.5)},${f1(yOf(hi[idx[0]]!))}h3` : lineOf(idx, hi);
        if (idx.length > 1 && (s.stack || s.fill)) area += areaOf(idx, hi, s.stack ? lo : 'base');
      }
      return { line, area };
    }),
  );

  const bandPath = $derived.by(() => {
    if (!band) return '';
    let d = '';
    for (const idx of segments(band.hi, band.lo)) if (idx.length > 1) d += areaOf(idx, band.hi, band.lo);
    return d;
  });

  const xTicks = $derived.by(() => {
    const range = to - from;
    const out: { x: number; label: string }[] = [];
    const maxTicks = Math.max(2, Math.floor(plotW / 72));
    if (range <= 2 * 86400) {
      // Dakika adımları İstanbul saatine oturur: 10:00, 10:15 …
      const steps = [5, 10, 15, 20, 30, 60, 120, 180, 240, 360, 720];
      const stepM = steps.find((m) => range / (m * 60) <= maxTicks) ?? 720;
      const step = stepM * 60;
      const off = tzOffset(from);
      let t = Math.ceil((from + off) / step) * step - off;
      while (t < to) {
        out.push({ x: xOf(t), label: fmtTime(t) });
        t += step;
      }
    } else {
      const days = range / 86400;
      const stepD = [1, 2, 3, 5, 7, 10, 15].find((d) => days / d <= maxTicks) ?? 15;
      let t = tzDayStart(tzDayStart(from) + 86400 + 3600);
      let i = 0;
      while (t < to) {
        if (i % stepD === 0) out.push({ x: xOf(t), label: fmtShortDate(t) });
        t = tzDayStart(t + 86400 + 3600);
        i++;
      }
    }
    // Kenara taşan etiketleri at.
    return out.filter((tk) => tk.x > PAD.l + 14 && tk.x < w - PAD.r - 14);
  });

  // Ekran okuyucu özeti: ilk serinin en düşük / ortalama / en yüksek değeri.
  const summary = $derived.by(() => {
    const s = series[0];
    if (!s) return label;
    let min = Infinity;
    let maxV = -Infinity;
    let sum = 0;
    let n = 0;
    for (const v of s.values) {
      if (v === null) continue;
      min = Math.min(min, v);
      maxV = Math.max(maxV, v);
      sum += v;
      n++;
    }
    if (!n) return `${label}: bu aralıkta veri yok`;
    return `${label}. ${s.label}: en düşük ${format(min)}, ortalama ${format(sum / n)}, en yüksek ${format(maxV)}.`;
  });

  const hasData = $derived(series.some((s) => s.values.some((v) => v !== null)));

  // İpucu -----------------------------------------------------------------------------
  let hover = $state<number | null>(null);
  let svgEl: SVGSVGElement | undefined = $state();

  function nearest(t: number): number {
    let lo = 0;
    let hi = times.length - 1;
    while (lo < hi) {
      const mid = (lo + hi) >> 1;
      if (times[mid] < t) lo = mid + 1;
      else hi = mid;
    }
    if (lo > 0 && Math.abs(times[lo - 1] - t) < Math.abs(times[lo] - t)) lo--;
    return lo;
  }

  function move(e: PointerEvent) {
    if (!svgEl || times.length === 0) return;
    const r = svgEl.getBoundingClientRect();
    const x = e.clientX - r.left;
    if (x < PAD.l - 6 || x > w - PAD.r + 6) {
      hover = null;
      return;
    }
    const i = nearest(from + ((x - PAD.l) / plotW) * (to - from));
    // Veri olmayan bölgede (boşluk) ipucu gösterme.
    hover = Math.abs(xOf(times[i]) - x) < Math.max(20, (gap / Math.max(1, to - from)) * plotW) ? i : null;
  }

  const hx = $derived(hover !== null ? xOf(times[hover]) : 0);
  const tipRight = $derived(hx > PAD.l + plotW / 2);

  function hoverTitle(t: number): string {
    return `${fmtShortDate(t)} ${fmtTime(t)}`;
  }
</script>

<div class="lc">
  <div class="chart" bind:clientWidth={w}>
    {#if w > 0}
      {#if !hasData}
        <div class="nodata" style="height:{H}px">Bu aralıkta veri yok</div>
      {:else}
        <svg
          bind:this={svgEl}
          width={w}
          height={H}
          role="img"
          aria-label={summary}
          onpointermove={move}
          onpointerdown={move}
          onpointerleave={() => (hover = null)}
        >
          <defs>
            <clipPath id="{uid}-clip">
              <rect x={PAD.l} y={PAD.t - 2} width={plotW} height={plotH + 2} />
            </clipPath>
          </defs>

          {#each yScale.ticks as v, i (i)}
            <line class="grid" class:base={v === 0} x1={PAD.l} x2={w - PAD.r} y1={yOf(v)} y2={yOf(v)} />
            <text class="ylab" x={PAD.l - 8} y={yOf(v) + 4} text-anchor="end">{axisFmt(v)}</text>
          {/each}

          {#each xTicks as tk (tk.x)}
            <text class="xlab" x={tk.x} y={H - 6} text-anchor="middle">{tk.label}</text>
          {/each}

          <g clip-path="url(#{uid}-clip)">
            {#if bandPath && band}
              <path d={bandPath} fill={band.color} opacity="0.16" />
            {/if}
            {#each series as s, i (i)}
              {#if paths[i].area}
                <path d={paths[i].area} fill={s.color} opacity={s.stack ? 0.3 : 0.1} />
              {/if}
            {/each}
            {#each series as s, i (i)}
              <path
                d={paths[i].line}
                fill="none"
                stroke={s.color}
                stroke-width="1.6"
                stroke-linejoin="round"
                stroke-linecap="round"
                stroke-dasharray={s.dashed ? '4 3' : undefined}
              />
            {/each}
          </g>

          {#if hover !== null}
            <line class="cross" x1={hx} x2={hx} y1={PAD.t} y2={PAD.t + plotH} />
            {#if band && band.hi[hover] !== null}
              <circle cx={hx} cy={yOf(band.hi[hover]!)} r="3" fill={band.color} stroke="var(--bg)" stroke-width="1.5" />
            {/if}
            {#each series as s, i (i)}
              {@const v = layers[i].hi[hover]}
              {#if v !== null && v !== undefined}
                <circle cx={hx} cy={yOf(v)} r="4" fill={s.color} stroke="var(--bg)" stroke-width="2" />
              {/if}
            {/each}
          {/if}
        </svg>

        {#if hover !== null}
          <div class="tipbox" class:right={tipRight} style={tipRight ? `right:${w - hx + 12}px` : `left:${hx + 12}px`}>
            <div class="tt">{hoverTitle(times[hover])}</div>
            {#each series as s, i (i)}
              {@const v = s.values[hover]}
              <div class="tr">
                <i class="sw" style="background:{s.color}"></i>
                <span class="tl">{s.label}</span>
                <b>{v === null || v === undefined ? '—' : format(v)}</b>
              </div>
            {/each}
            {#if band && band.hi[hover] !== null}
              <div class="tr">
                <i class="sw band" style="background:{band.color}"></i>
                <span class="tl">{band.label}</span>
                <b>{format(band.hi[hover]!)}</b>
              </div>
            {/if}
          </div>
        {/if}
      {/if}
    {/if}
  </div>

  {#if hasData && (series.length > 1 || band)}
    <div class="legend">
      {#each series as s, i (i)}
        <span><i class="lg" class:area={s.stack} class:dashed={s.dashed} style="--c:{s.color}"></i>{s.label}</span>
      {/each}
      {#if band}
        <span><i class="lg bandlg" style="--c:{band.color}"></i>{band.label}</span>
      {/if}
    </div>
  {/if}
</div>

<style>
  .lc {
    min-width: 0;
  }
  .chart {
    position: relative;
    width: 100%;
    min-height: 60px;
    touch-action: pan-y;
    user-select: none;
    -webkit-user-select: none;
  }
  svg {
    display: block;
    overflow: visible;
  }
  .nodata {
    display: flex;
    align-items: center;
    justify-content: center;
    color: var(--muted);
    font-size: 0.88rem;
    border: 1px dashed var(--border);
    border-radius: var(--radius-sm);
  }
  .grid {
    stroke: var(--border);
    stroke-width: 1;
    stroke-dasharray: 3 4;
  }
  .grid.base {
    stroke-dasharray: none;
  }
  .ylab,
  .xlab {
    fill: var(--muted);
    font-size: 11px;
    font-variant-numeric: tabular-nums;
  }
  .cross {
    stroke: var(--text-2);
    stroke-width: 1;
    stroke-dasharray: 3 3;
  }
  .tipbox {
    position: absolute;
    top: 0;
    min-width: 150px;
    max-width: 240px;
    pointer-events: none;
    background: var(--tip-bg);
    border: 1px solid var(--border-strong);
    border-radius: 8px;
    padding: 7px 10px;
    font-size: 0.8rem;
    line-height: 1.5;
    box-shadow: var(--shadow);
    z-index: 5;
    color: var(--tip-text, var(--text));
  }
  .tt {
    color: var(--tip-text-2, var(--text-2));
    font-size: 0.74rem;
    margin-bottom: 2px;
  }
  .tr {
    display: flex;
    align-items: center;
    gap: 6px;
    white-space: nowrap;
  }
  .tl {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    color: var(--tip-text-2, var(--text-2));
  }
  .tr b {
    font-weight: 600;
    font-variant-numeric: tabular-nums;
  }
  .sw {
    width: 8px;
    height: 8px;
    border-radius: 2px;
    flex-shrink: 0;
  }
  .sw.band {
    opacity: 0.5;
  }
  .legend {
    display: flex;
    flex-wrap: wrap;
    gap: 4px 14px;
    margin-top: 8px;
    font-size: 0.78rem;
    color: var(--text-2);
  }
  .legend > span {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
  }
  .lg {
    width: 14px;
    height: 2.5px;
    border-radius: 2px;
    background: var(--c);
    flex-shrink: 0;
  }
  .lg.dashed {
    background: repeating-linear-gradient(90deg, var(--c) 0 4px, transparent 4px 7px);
  }
  .lg.area,
  .lg.bandlg {
    height: 10px;
    width: 10px;
    border-radius: 3px;
  }
  .lg.bandlg {
    opacity: 0.45;
  }
</style>
