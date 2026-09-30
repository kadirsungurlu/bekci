<script lang="ts">
  // Elle yazılmış, ekran genişliğine uyan yanıt süresi grafiği.
  // Ham seride (24 saat) her kontrol bir noktadır ve altında durum şeridi çizilir;
  // saatlik/günlük seride ortalama çizgisi ve min–maks bandı gösterilir.
  import type { Series } from '../lib/api';
  import { STATUS_DOWN, STATUS_MAINTENANCE, STATUS_UP } from '../lib/api';
  import {
    fmtDay,
    fmtMs,
    fmtNum,
    fmtPct,
    fmtShortDate,
    fmtTime,
    fmtTimeSec,
    pointStatusClass,
    pointStatusLabel,
    tzDayStart,
    tzOffset,
  } from '../lib/format';
  import { t } from '../lib/i18n';

  let { series, from, to, interval }: { series: Series; from: number; to: number; interval: number } = $props();

  interface Pt {
    t: number; // noktanın çizildiği zaman
    t0: number; // kova başlangıcı (ham seride t)
    span: number; // kova uzunluğu (ham seride 0)
    v: number | null;
    min: number | null;
    max: number | null;
    s: number; // ham: durum; kova: -1
    m: string;
    up: number;
    down: number;
  }

  let w = $state(0);
  const H = $derived(w < 520 ? 180 : 230);
  const PAD = { l: 50, r: 12, t: 24, b: 26 };
  const plotW = $derived(Math.max(10, w - PAD.l - PAD.r));
  const plotH = $derived(H - PAD.t - PAD.b);
  const isRaw = $derived(series.kind === 'raw');

  const pts = $derived.by<Pt[]>(() => {
    if (series.kind === 'raw') {
      return series.points.map((p) => ({
        t: p.t,
        t0: p.t,
        span: 0,
        v: p.p >= 0 ? p.p : null,
        min: null,
        max: null,
        s: p.s,
        m: p.m ?? '',
        up: 0,
        down: 0,
      }));
    }
    const span = series.kind === 'daily' ? 86400 : 3600;
    return series.points.map((b) => ({
      t: b.t + span / 2,
      t0: b.t,
      span,
      v: b.ping >= 0 ? b.ping : null,
      min: b.ping_min >= 0 ? b.ping_min : null,
      max: b.ping_max >= 0 ? b.ping_max : null,
      s: -1,
      m: '',
      up: b.up,
      down: b.down,
    }));
  });

  // Bu süreden uzun boşlukta çizgi kesilir.
  const gap = $derived(isRaw ? Math.max(interval * 3, 300) : series.kind === 'daily' ? 86400 * 1.5 : 3600 * 1.5);

  function niceStep(raw: number): number {
    const p = Math.pow(10, Math.floor(Math.log10(raw)));
    const n = raw / p;
    return (n <= 1 ? 1 : n <= 2 ? 2 : n <= 5 ? 5 : 10) * p;
  }

  const yScale = $derived.by(() => {
    let max = 0;
    for (const p of pts) if (p.v !== null && p.v > max) max = p.v;
    if (max <= 0) max = 100;
    // Çok düşük gecikmede (≤ 1 ms) eksende "0,5 / 1,5" gibi kesirli değerler çıkmasın.
    max = Math.max(max, 10);
    const step = niceStep((max * 1.1) / 4);
    const top = Math.ceil((max * 1.1) / step) * step;
    const ticks: number[] = [];
    for (let v = 0; v <= top + 1e-9; v += step) ticks.push(v);
    return { top, ticks };
  });

  const xOf = (t: number) => PAD.l + ((t - from) / Math.max(1, to - from)) * plotW;
  const yOf = (v: number) => PAD.t + plotH - (Math.min(v, yScale.top) / yScale.top) * plotH;

  const paths = $derived.by(() => {
    let line = '';
    let area = '';
    let band = '';
    // Tek noktalı parçalar (iki yanı boşluk) çizgi olarak görünmez; nokta çizilir.
    const dots: { x: number; y: number }[] = [];
    const base = PAD.t + plotH;
    let seg: Pt[] = [];
    const flush = () => {
      if (seg.length === 0) return;
      if (seg.length === 1) {
        dots.push({ x: xOf(seg[0].t), y: yOf(seg[0].v!) });
        seg = [];
        return;
      }
      const d = seg.map((p, i) => `${i ? 'L' : 'M'}${xOf(p.t).toFixed(1)},${yOf(p.v!).toFixed(1)}`).join('');
      line += d;
      if (seg.length > 1) {
        area += `${d}L${xOf(seg[seg.length - 1].t).toFixed(1)},${base}L${xOf(seg[0].t).toFixed(1)},${base}Z`;
      }
      if (!isRaw && seg.length > 1) {
        const upper = seg.map((p, i) => `${i ? 'L' : 'M'}${xOf(p.t).toFixed(1)},${yOf(p.max ?? p.v!).toFixed(1)}`).join('');
        const lower = seg
          .slice()
          .reverse()
          .map((p) => `L${xOf(p.t).toFixed(1)},${yOf(p.min ?? p.v!).toFixed(1)}`)
          .join('');
        band += `${upper}${lower}Z`;
      }
      seg = [];
    };
    let prevT = -Infinity;
    for (const p of pts) {
      if (p.v === null || p.t - prevT > gap) flush();
      if (p.v !== null) seg.push(p);
      prevT = p.t;
    }
    flush();
    return { line, area, band, dots };
  });

  // Kesinti (DOWN) aralıkları kırmızı gölge olarak.
  const downRects = $derived.by(() => {
    const out: { x: number; w: number; o: number }[] = [];
    if (isRaw) {
      let start = -1;
      let end = -1;
      for (let i = 0; i < pts.length; i++) {
        const p = pts[i];
        const next = i + 1 < pts.length ? pts[i + 1].t : Math.min(to, p.t + interval);
        const e = Math.min(next, p.t + gap);
        if (p.s === STATUS_DOWN) {
          if (start < 0) start = p.t;
          end = e;
        } else if (start >= 0) {
          out.push({ x: xOf(start), w: Math.max(2, xOf(end) - xOf(start)), o: 0.22 });
          start = -1;
        }
      }
      if (start >= 0) out.push({ x: xOf(start), w: Math.max(2, xOf(end) - xOf(start)), o: 0.22 });
    } else {
      for (const p of pts) {
        if (p.down > 0) {
          const ratio = p.down / (p.up + p.down);
          out.push({ x: xOf(p.t0), w: Math.max(2, xOf(p.t0 + p.span) - xOf(p.t0)), o: 0.1 + 0.3 * ratio });
        }
      }
    }
    return out;
  });

  const hasMaint = $derived(isRaw && pts.some((p) => p.s === STATUS_MAINTENANCE));

  // Ham seride durum şeridi parçaları.
  const strip = $derived.by(() => {
    if (!isRaw) return [];
    const out: { x: number; w: number; c: string }[] = [];
    let cur: { s: number; a: number; b: number } | null = null;
    for (let i = 0; i < pts.length; i++) {
      const p = pts[i];
      const next = i + 1 < pts.length ? pts[i + 1].t : Math.min(to, p.t + interval);
      const b = Math.min(next, p.t + gap);
      if (cur && cur.s === p.s && Math.abs(cur.b - p.t) < 1) {
        cur.b = b;
      } else {
        if (cur) out.push(seg(cur));
        cur = { s: p.s, a: p.t, b };
      }
    }
    if (cur) out.push(seg(cur));
    return out;
    function seg(c: { s: number; a: number; b: number }) {
      const color =
        c.s === STATUS_UP
          ? 'var(--up)'
          : c.s === STATUS_DOWN
            ? 'var(--down)'
            : c.s === STATUS_MAINTENANCE
              ? 'var(--maint)'
              : 'var(--pending)';
      return { x: xOf(c.a), w: Math.max(1.5, xOf(c.b) - xOf(c.a)), c: color };
    }
  });

  const xTicks = $derived.by(() => {
    const narrow = w < 520;
    const range = to - from;
    const out: { x: number; label: string }[] = [];
    if (range <= 2 * 86400) {
      // Saat işaretleri İstanbul saatinde 3'e (darda 6'ya) bölünen saatlere oturur.
      const stepH = narrow ? 6 : 3;
      const off = tzOffset(from);
      let t = Math.ceil((from + off) / 3600) * 3600;
      while (Math.floor(t / 3600) % stepH !== 0) t += 3600;
      t -= off;
      while (t < to) {
        out.push({ x: xOf(t), label: fmtTime(t) });
        t += stepH * 3600;
      }
    } else {
      // Gün sınırları sunucuyla aynı dilimde (İstanbul) hesaplanır.
      const days = range / 86400;
      const stepD = days <= 8 ? (narrow ? 2 : 1) : days <= 31 ? (narrow ? 10 : 5) : narrow ? 30 : 15;
      let t = tzDayStart(from) + 86400;
      t = tzDayStart(t + 3600); // yaz saati geçişinde tam gün başına oturt
      let i = 0;
      while (t < to) {
        if (i % stepD === 0) out.push({ x: xOf(t), label: fmtShortDate(t) });
        t = tzDayStart(t + 86400 + 3600);
        i++;
      }
    }
    return out;
  });

  const stats = $derived.by(() => {
    let min = Infinity;
    let max = -Infinity;
    let sum = 0;
    let n = 0;
    for (const p of pts) {
      if (p.v === null) continue;
      const lo = p.min ?? p.v;
      const hi = p.max ?? p.v;
      if (lo < min) min = lo;
      if (hi > max) max = hi;
      const weight = isRaw ? 1 : Math.max(1, p.up);
      sum += p.v * weight;
      n += weight;
    }
    return n ? { min, max, avg: sum / n } : null;
  });

  // Fareyle üzerine gelme -----------------------------------------------------------
  let hover = $state<number | null>(null);
  let svgEl: SVGSVGElement | undefined = $state();

  function nearest(t: number): number {
    let lo = 0;
    let hi = pts.length - 1;
    while (lo < hi) {
      const mid = (lo + hi) >> 1;
      if (pts[mid].t < t) lo = mid + 1;
      else hi = mid;
    }
    if (lo > 0 && Math.abs(pts[lo - 1].t - t) < Math.abs(pts[lo].t - t)) lo--;
    return lo;
  }

  function move(e: PointerEvent) {
    if (!svgEl || pts.length === 0) return;
    const r = svgEl.getBoundingClientRect();
    const x = e.clientX - r.left;
    if (x < PAD.l - 6 || x > w - PAD.r + 6) {
      hover = null;
      return;
    }
    const t = from + ((x - PAD.l) / plotW) * (to - from);
    const i = nearest(t);
    // Çok uzaktaki noktayı gösterme (veri olmayan bölge).
    const px = Math.abs(xOf(pts[i].t) - x);
    hover = px < Math.max(24, plotW / 40) ? i : null;
  }

  const hp = $derived(hover !== null ? pts[hover] : null);

  function hoverTitle(p: Pt): string {
    if (isRaw) return `${fmtDay(p.t)} ${fmtTimeSec(p.t)}`;
    if (p.span === 86400) return fmtDay(p.t0);
    return `${fmtShortDate(p.t0)} ${fmtTime(p.t0)} – ${fmtTime(p.t0 + 3600)}`;
  }

  let tw = $state(0);
  const tipLeft = $derived(hp ? Math.max(4, Math.min(xOf(hp.t) - (tw || 180) / 2, w - (tw || 180) - 4)) : 0);

  let chartEl: HTMLDivElement | undefined = $state();
  function leave(e: PointerEvent) {
    // Dokunmatikte parmak kalkınca ipucu kaybolmasın; başka yere dokununca kapanır.
    if (e.pointerType !== 'touch') hover = null;
  }
  function outside(e: PointerEvent) {
    if (hover !== null && chartEl && !chartEl.contains(e.target as Node)) hover = null;
  }
</script>

<svelte:document onpointerdown={outside} />

<div class="chart" bind:clientWidth={w} bind:this={chartEl}>
  {#if w > 0}
    {#if pts.length === 0}
      <div class="nodata" style="height:{H}px">{t('common.noDataRange')}</div>
    {:else}
      <svg
        bind:this={svgEl}
        width={w}
        height={H}
        role="img"
        aria-label={t('status.chart.responseChart')}
        onpointermove={move}
        onpointerdown={move}
        onpointerleave={leave}
      >
        <defs>
          <linearGradient id="pc-fill" x1="0" x2="0" y1="0" y2="1">
            <stop offset="0%" stop-color="var(--up)" stop-opacity="0.28" />
            <stop offset="100%" stop-color="var(--up)" stop-opacity="0.02" />
          </linearGradient>
          <clipPath id="pc-clip">
            <rect x={PAD.l} y={PAD.t} width={plotW} height={plotH} />
          </clipPath>
        </defs>

        {#each yScale.ticks as v (v)}
          <line class="grid" x1={PAD.l} x2={w - PAD.r} y1={yOf(v)} y2={yOf(v)} />
          <text class="ylab" x={PAD.l - 8} y={yOf(v) + 4} text-anchor="end">{fmtNum(v)}</text>
        {/each}
        <text class="ylab unit" x={PAD.l - 8} y="10" text-anchor="end">ms</text>

        {#each xTicks as tk (tk.x)}
          <text class="xlab" x={tk.x} y={H - 7} text-anchor="middle">{tk.label}</text>
        {/each}

        <g clip-path="url(#pc-clip)">
          {#each downRects as r, i (i)}
            <rect x={r.x} y={PAD.t} width={r.w} height={plotH} fill="var(--down)" opacity={r.o} />
          {/each}
          {#if paths.band}
            <path d={paths.band} fill="var(--up)" opacity="0.12" />
          {/if}
          <path d={paths.area} fill="url(#pc-fill)" />
          <path d={paths.line} fill="none" stroke="var(--up)" stroke-width="1.6" stroke-linejoin="round" />
          {#each paths.dots as d, i (i)}
            <circle cx={d.x} cy={d.y} r="2.2" fill="var(--up)" />
          {/each}
        </g>

        {#if hp}
          <line class="cross" x1={xOf(hp.t)} x2={xOf(hp.t)} y1={PAD.t} y2={PAD.t + plotH} />
          {#if hp.v !== null}
            <circle cx={xOf(hp.t)} cy={yOf(hp.v)} r="4" fill="var(--up)" stroke="var(--bg)" stroke-width="2" />
          {/if}
        {/if}
      </svg>

      {#if isRaw}
        <svg class="strip" width={w} height="12" aria-hidden="true">
          <rect x={PAD.l} y="1" width={plotW} height="10" rx="3" fill="var(--empty-bar)" />
          {#each strip as s, i (i)}
            <rect x={s.x} y="1" width={s.w} height="10" fill={s.c} />
          {/each}
        </svg>
      {/if}

      {#if hp}
        <div class="tipbox" bind:clientWidth={tw} style="left:{tipLeft}px">
          <div class="tt">{hoverTitle(hp)}</div>
          {#if isRaw}
            <div>
              <span class="st c-{pointStatusClass(hp.s)}"
                >{pointStatusLabel(hp.s)}</span
              >
              {#if hp.v !== null}· <b>{fmtMs(hp.v)}</b>{/if}
            </div>
            {#if hp.m}<div class="m">{hp.m}</div>{/if}
          {:else}
            <div>{t('status.chart.avgShort')} <b>{fmtMs(hp.v)}</b>{#if hp.min !== null && hp.max !== null}<span class="muted"> ({fmtNum(hp.min)}–{fmtNum(hp.max)})</span>{/if}</div>
            <div>
              Uptime <b class={hp.down > 0 ? 'c-down' : 'c-up'}>{hp.up + hp.down > 0 ? fmtPct((100 * hp.up) / (hp.up + hp.down)) : '—'}</b>
              {#if hp.down > 0}<span class="muted"> · {t('status.chart.errors', { count: hp.down })}</span>{/if}
            </div>
          {/if}
        </div>
      {/if}
    {/if}
  {/if}
</div>

<!-- Kontrol varsa özet satırı ve lejant her zaman: yanıt süresi ölçülemediyse
     (ör. hep çalışmıyor) değerler "—" olur, durum şeridinin renkleri yine açıklanır. -->
{#if stats || pts.length}
  <div class="stats">
    <span>{t('status.chart.min')} <b>{fmtMs(stats?.min ?? -1)}</b></span>
    <span>{t('status.chart.avg')} <b>{fmtMs(stats?.avg ?? -1)}</b></span>
    <span>{t('status.chart.max')} <b>{fmtMs(stats?.max ?? -1)}</b></span>
    {#if isRaw && pts.length}
      <span class="legend" aria-hidden="true">
        <span><i class="lg up"></i>{t('status.up')}</span>
        <span><i class="lg down"></i>{t('status.down')}</span>
        <span><i class="lg pending"></i>{t('status.retrying')}</span>
        {#if hasMaint}<span><i class="lg maint"></i>{t('status.maintenance')}</span>{/if}
      </span>
    {/if}
  </div>
{/if}

<style>
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
  }
  .nodata {
    display: flex;
    align-items: center;
    justify-content: center;
    color: var(--muted);
    border: 1px dashed var(--border);
    border-radius: var(--radius-sm);
  }
  .grid {
    stroke: var(--border);
    stroke-width: 1;
    stroke-dasharray: 3 4;
  }
  .ylab,
  .xlab {
    fill: var(--muted);
    font-size: 11px;
  }
  .unit {
    font-size: 10px;
  }
  .cross {
    stroke: var(--text-2);
    stroke-width: 1;
    stroke-dasharray: 3 3;
  }
  .strip {
    margin-top: 6px;
  }
  .tipbox {
    position: absolute;
    top: 0;
    width: 180px;
    pointer-events: none;
    background: var(--tip-bg);
    border: 1px solid var(--border-strong);
    border-radius: 8px;
    padding: 8px 10px;
    font-size: 0.8rem;
    line-height: 1.45;
    box-shadow: var(--shadow);
    z-index: 5;
  }
  .tt {
    color: var(--text-2);
    font-size: 0.75rem;
    margin-bottom: 2px;
  }
  .m {
    color: var(--down-text);
    word-break: break-word;
    margin-top: 2px;
  }
  .st::before {
    content: '';
    display: inline-block;
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: currentColor;
    margin-right: 5px;
    vertical-align: 1px;
  }
  .legend {
    display: inline-flex;
    gap: 12px;
    flex-wrap: wrap;
    margin-left: auto;
    font-size: 0.78rem;
  }
  .legend > span {
    display: inline-flex;
    align-items: center;
    gap: 5px;
  }
  .lg {
    width: 10px;
    height: 10px;
    border-radius: 3px;
    display: inline-block;
  }
  .lg.up {
    background: var(--up);
  }
  .lg.down {
    background: var(--down);
  }
  .lg.pending {
    background: var(--pending);
  }
  .lg.maint {
    background: var(--maint);
  }
  .stats {
    display: flex;
    gap: 18px;
    flex-wrap: wrap;
    margin-top: 12px;
    font-size: 0.85rem;
    color: var(--muted);
  }
  .stats b {
    color: var(--text);
    font-weight: 600;
  }
</style>
