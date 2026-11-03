<script lang="ts">
  // Kontrol noktaları arası canlı topoloji şeması: kutular (düğümler), aralarında
  // çift başına bir çizgi ve ortasında durum işareti. Yerleşim lib/meshlayout.ts
  // (Go aynası); SVG viewBox ile ölçeklenir. Özet satırı SVG altında HTML'dir
  // (dar ekranda satır kayabilsin).
  import type { MeshLink } from '../lib/api';
  import { BOX_H, BOX_W, SUMMARY_H, buildTopology, type MeshPairIn, type PairState, type TopoLink } from '../lib/meshlayout';
  import { fmtMs, fmtRelative } from '../lib/format';
  import { t } from '../lib/i18n';

  let {
    nodes,
    pairs,
    links = [],
  }: {
    nodes: { id: number; name: string }[];
    pairs: MeshPairIn[];
    /** Yönlü ölçüm ayrıntısı (GET /api/mesh); olay kaydında yok. */
    links?: MeshLink[];
  } = $props();

  const topo = $derived(
    buildTopology(
      nodes.map((n) => n.id),
      pairs,
    ),
  );
  const names = $derived(new Map(nodes.map((n) => [n.id, n.name])));
  const nameOf = (id: number) => names.get(id) ?? `#${id}`;
  const pos = $derived(new Map(topo.nodes.map((n) => [n.id, n])));
  const detail = $derived(new Map(links.map((l) => [`${l.src}>${l.dst}`, l])));
  const pairBy = $derived(new Map(pairs.map((p) => [`${Math.min(p.a, p.b)}-${Math.max(p.a, p.b)}`, p])));

  const MAX = 15;
  const short = (s: string) => (s.length > MAX ? s.slice(0, MAX - 1) + '…' : s);

  // Merkezden merkeze doğrunun kutu kenarıyla kesiştiği nokta (çizgi kutunun altına girmesin).
  function edgeOf(dx: number, dy: number): number {
    const fx = dx === 0 ? Infinity : BOX_W / 2 / Math.abs(dx);
    const fy = dy === 0 ? Infinity : BOX_H / 2 / Math.abs(dy);
    return Math.min(fx, fy);
  }

  interface Seg {
    l: TopoLink;
    x1: number;
    y1: number;
    x2: number;
    y2: number;
    mx: number;
    my: number;
    angle: number; // tek yönlüde çalışan yönün açısı (derece)
  }
  const segs = $derived.by(() => {
    const out: Seg[] = [];
    const placed: [number, number][] = [];
    // Çaprazlar ortada kesişir: işaret başka işaretin üstüne düşmesin diye kaydır.
    const FR = [0.5, 0.38, 0.62, 0.3, 0.7];
    for (const l of topo.links) {
      const a = pos.get(l.a);
      const b = pos.get(l.b);
      if (!a || !b) continue;
      const dx = b.x - a.x;
      const dy = b.y - a.y;
      const k = Math.min(edgeOf(dx, dy), 0.45);
      let angle = (Math.atan2(dy, dx) * 180) / Math.PI;
      if (l.state === 'oneway' && !l.forward) angle += 180;
      const x1 = a.x + dx * k;
      const y1 = a.y + dy * k;
      const x2 = b.x - dx * k;
      const y2 = b.y - dy * k;
      let mx = (x1 + x2) / 2;
      let my = (y1 + y2) / 2;
      for (const f of FR) {
        const px = x1 + (x2 - x1) * f;
        const py = y1 + (y2 - y1) * f;
        if (placed.every(([qx, qy]) => Math.hypot(px - qx, py - qy) >= 26)) {
          mx = px;
          my = py;
          break;
        }
      }
      placed.push([mx, my]);
      out.push({ l, x1, y1, x2, y2, mx, my, angle });
    }
    return out;
  });

  const dirLabel = (s: string | undefined) =>
    t(s === 'up' ? 'probes.matrix.up' : s === 'down' ? 'probes.matrix.down' : 'probes.matrix.unknown');

  // Bir yönün satırı: durum, RTT, ardışık kayıp, son başarı.
  function dirLine(src: number, dst: number, state: string | undefined, now: number): string {
    const parts = [t('probes.topo.dir', { src: nameOf(src), dst: nameOf(dst), state: dirLabel(state) })];
    const d = detail.get(`${src}>${dst}`);
    if (d) {
      if (state === 'up' && d.rtt_ms !== undefined && d.rtt_ms > 0) parts.push(fmtMs(d.rtt_ms));
      if (d.loss_streak) parts.push(t('probes.topo.loss', { n: d.loss_streak }));
      if (d.last_ok_at !== undefined && state !== 'up')
        parts.push(d.last_ok_at > 0 ? t('probes.topo.lastOk', { when: fmtRelative(d.last_ok_at, now) }) : t('probes.topo.never'));
    }
    return parts.join(' · ');
  }

  function tip(l: TopoLink): string {
    const now = Date.now() / 1000;
    const p = pairBy.get(`${l.a}-${l.b}`);
    // p.a < p.b; ab = a'nın b'yi ölçümü
    return [
      t('probes.topo.pair', { a: nameOf(l.a), b: nameOf(l.b), state: t(`probes.topo.state.${l.state}`) }),
      dirLine(l.a, l.b, p?.ab, now),
      dirLine(l.b, l.a, p?.ba, now),
    ].join('\n');
  }

  // Halka yerleşimi tepede/altta boşluk bırakabilir (ör. 3 düğümde alt yarı boş):
  // viewBox düğümlerin kapladığı alana kırpılır. Özet satırı HTML olduğundan SUMMARY_H yok.
  const box = $derived.by(() => {
    const m = 8;
    if (!topo.nodes.length) return { x: 0, y: 0, w: topo.width, h: topo.height - SUMMARY_H };
    const xs = topo.nodes.map((n) => n.x);
    const ys = topo.nodes.map((n) => n.y);
    const x = Math.min(...xs) - BOX_W / 2 - m;
    const y = Math.min(...ys) - BOX_H / 2 - m;
    return { x, y, w: Math.max(...xs) + BOX_W / 2 + m - x, h: Math.max(...ys) + BOX_H / 2 + m - y };
  });

  const total = $derived(topo.links.length);
  const healthy = $derived(total > 0 && topo.summary.up === total);
  const ORDER: PairState[] = ['down', 'oneway', 'unknown', 'up'];
</script>

{#snippet mark(state: PairState)}
  {#if state === 'up'}
    <path d="M-4.5 0.5 L-1.5 3.5 L4.5 -3" />
  {:else if state === 'down'}
    <path d="M-3.5 -3.5 L3.5 3.5 M3.5 -3.5 L-3.5 3.5" />
  {:else if state === 'oneway'}
    <path d="M-4.5 0 L4 0 M0.5 -3.5 L4 0 L0.5 3.5" />
  {:else}
    <path d="M-2.6 -2.2 A2.6 2.6 0 1 1 0 0.6 L0 1.6 M0 4 L0 4.2" />
  {/if}
{/snippet}

<figure class="topo" data-testid="mesh-diagram">
  <svg
    viewBox="{box.x} {box.y} {box.w} {box.h}"
    style="max-width:{box.w}px"
    role="group"
    aria-label={t('probes.topo.label')}
  >
    {#each segs as s (s.l.a + '-' + s.l.b)}
      {@const label = tip(s.l)}
      <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
      <g class="link {s.l.state}" tabindex="0" role="img" aria-label={label.replaceAll('\n', '. ')} data-tip={label} data-state={s.l.state}>
        <line class="hit" x1={s.x1} y1={s.y1} x2={s.x2} y2={s.y2} />
        <line class="ln" x1={s.x1} y1={s.y1} x2={s.x2} y2={s.y2} />
        <g transform="translate({s.mx} {s.my})">
          <circle class="ring" r="14" />
          <circle class="badge" r="10" />
          <g class="mk" transform={s.l.state === 'oneway' ? `rotate(${s.angle})` : undefined}>{@render mark(s.l.state)}</g>
        </g>
      </g>
    {/each}
    {#each topo.nodes as n (n.id)}
      {@const name = nameOf(n.id)}
      <g class="node" class:main={n.main}>
        <title>{name}</title>
        <rect x={n.x - BOX_W / 2} y={n.y - BOX_H / 2} width={BOX_W} height={BOX_H} rx="6" />
        <text x={n.x} y={n.y} dominant-baseline="central" text-anchor="middle">{short(name)}</text>
      </g>
    {/each}
  </svg>
  <figcaption class="sum small" data-testid="mesh-summary">
    {#if healthy}
      <span class="item up">
        <svg viewBox="-7 -7 14 14" aria-hidden="true"><g class="mk">{@render mark('up')}</g></svg>{t('probes.topo.healthy')}
      </span>
    {:else}
      {#each ORDER as st (st)}
        {#if topo.summary[st]}
          <span class="item {st}">
            <svg viewBox="-7 -7 14 14" aria-hidden="true"><g class="mk">{@render mark(st)}</g></svg>{t(`probes.topo.count.${st}`, { n: topo.summary[st] })}
          </span>
        {/if}
      {/each}
    {/if}
  </figcaption>
</figure>

<style>
  .topo {
    margin: 0;
    display: grid;
    gap: 6px;
    justify-items: center;
  }
  svg {
    display: block;
    width: 100%;
    height: auto;
    overflow: visible;
  }
  .node rect {
    fill: var(--card-2);
    stroke: var(--border-strong);
    stroke-width: 1.5;
  }
  .node.main rect {
    stroke: var(--text-2);
  }
  .node text {
    fill: var(--text);
    font-size: 13px;
    font-weight: 600;
  }
  .link {
    --c: var(--muted);
    cursor: default;
    outline: none;
  }
  .link.up {
    --c: var(--up);
  }
  .link.down {
    --c: var(--down);
  }
  .link.oneway {
    --c: var(--pending);
  }
  .ln {
    stroke: var(--c);
    stroke-width: 2;
  }
  .link.unknown .ln {
    stroke-dasharray: 6 5;
  }
  .hit {
    stroke: transparent;
    stroke-width: 16;
  }
  .badge {
    fill: var(--card);
    stroke: var(--c);
    stroke-width: 1.5;
  }
  .ring {
    fill: none;
    stroke: transparent;
    stroke-width: 2;
  }
  .link:hover .ring,
  .link:focus-visible .ring {
    stroke: var(--text);
  }
  .link:focus-visible .ln {
    stroke-width: 3;
  }
  .mk {
    fill: none;
    stroke: var(--c);
    stroke-width: 2;
    stroke-linecap: round;
    stroke-linejoin: round;
  }
  .sum {
    display: flex;
    flex-wrap: wrap;
    justify-content: center;
    gap: 6px 16px;
    color: var(--text-2);
  }
  .item {
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }
  .item svg {
    width: 14px;
    height: 14px;
    flex: none;
  }
  .item.up {
    --c: var(--up);
  }
  .item.down {
    --c: var(--down);
  }
  .item.oneway {
    --c: var(--pending);
  }
  .item.unknown {
    --c: var(--muted);
  }
</style>
