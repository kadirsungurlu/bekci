<script lang="ts">
  // Canlı topoloji tuvali (geniş ekran): gruplar + panel kartı HTML, bağlantılar SVG.
  // Yerleşim lib/topolayout.ts (genişliğe göre), kurallar lib/topoview.ts.
  import { t } from '../lib/i18n';
  import type { Topology } from '../lib/api';
  import {
    layoutTopology,
    meshArc,
    vCurve,
    type GroupKey,
    type GroupLayout,
    type Rect,
  } from '../lib/topolayout';
  import {
    channelClass,
    channelSub,
    checkEdge,
    counts,
    edgeTip,
    groupLevel,
    linkEdge,
    meshEdge,
    metricEdge,
    monClass,
    monitorSub,
    notifyEdge,
    probeClass,
    probeSub,
    serverClass,
    serverSub,
    sortChannels,
    sortMonitors,
    sortProbes,
    sortServers,
    worstEdge,
    type Cls,
    type EdgeCls,
  } from '../lib/topoview';
  import Icon, { type IconName } from './Icon.svelte';

  let { d, now, canEdit, rate }: { d: Topology; now: number; canEdit: boolean; rate: string | null } = $props();

  let width = $state(1200);
  let expanded = $state<Record<GroupKey, boolean>>({ monitors: false, channels: false, probes: false, servers: false });

  interface Node {
    id: number;
    name: string;
    sub: string;
    cls: Cls;
    href: string | null;
  }
  const monitors = $derived<Node[]>(
    sortMonitors(d.monitors).map((m) => ({ id: m.id, name: m.name, sub: monitorSub(m), cls: monClass(m), href: `#/monitors/${m.id}` })),
  );
  const probes = $derived<Node[]>(
    sortProbes(d.probes).map((p) => ({
      id: p.id,
      name: p.name,
      sub: probeSub(p, d.admin),
      cls: probeClass(p),
      href: d.admin ? '#/settings/probes' : null,
    })),
  );
  const servers = $derived<Node[]>(
    sortServers(d.servers).map((s) => ({ id: s.id, name: s.name, sub: serverSub(s), cls: serverClass(s), href: `#/servers/${s.id}` })),
  );
  const channels = $derived<Node[] | null>(
    d.channels
      ? sortChannels(d.channels).map((c) => ({ id: c.id, name: c.name, sub: channelSub(c, now), cls: channelClass(c), href: '#/notifications' }))
      : null,
  );
  const nodes = $derived<Record<GroupKey, Node[]>>({ monitors, probes, servers, channels: channels ?? [] });
  const meshOn = $derived(!!d.admin && !!d.mesh && d.mesh.nodes.length >= 2);

  const L = $derived(
    layoutTopology(width, {
      monitors: { key: 'monitors', ids: monitors.map((n) => n.id), expanded: expanded.monitors, maxRows: 3 },
      channels: channels ? { key: 'channels', ids: channels.map((n) => n.id), expanded: expanded.channels, maxRows: 2 } : null,
      probes: { key: 'probes', ids: probes.map((n) => n.id), expanded: expanded.probes, maxRows: 2, mesh: meshOn },
      servers: { key: 'servers', ids: servers.map((n) => n.id), expanded: expanded.servers, maxRows: 2 },
    }),
  );
  const groupList = $derived(Object.values(L.groups) as GroupLayout[]);
  const nodeOf = (k: GroupKey, id: number) => nodes[k].find((n) => n.id === id)!;

  interface Edge {
    id: string;
    cls: EdgeCls;
    d: string;
    mx: number;
    my: number;
    label: string;
  }

  /** Panel kartında bağlantıların değdiği x aralığı: aynı sırada iki grup varsa ikiye bölünür. */
  function hubRange(hub: Rect, side: 'left' | 'right' | 'full'): [number, number] {
    const a = hub.x + 18;
    const b = hub.x + hub.w - 18;
    const mid = hub.x + hub.w / 2;
    return side === 'left' ? [a, mid - 8] : side === 'right' ? [mid + 8, b] : [a, b];
  }
  const lerp = ([a, b]: [number, number], i: number, n: number) => (n <= 1 ? (a + b) / 2 : a + ((b - a) * i) / (n - 1));

  function groupEdges(
    g: GroupLayout | undefined,
    side: 'left' | 'right' | 'full',
    prefix: string,
    clsOf: (id: number) => EdgeCls,
    normal: EdgeCls,
    up: boolean, // true: grup üstte (panel kartına alttan değil üstten bağlanır)
    fromHub: boolean, // akış panelden gruba mı
  ): Edge[] {
    if (!g || g.empty) return [];
    const range = hubRange(L.hub, side);
    const hy = up ? L.hub.y : L.hub.y + L.hub.h;
    const mk = (x: number, y: number, hx: number) => (fromHub ? vCurve(hx, hy, x, y) : vCurve(x, y, hx, hy));
    if (g.perCard) {
      return g.cards.map((c, i) => {
        // Alttaki grupta bağlantı kutunun üst kenarında biter: başlığın üstünden geçmez.
        const y = up ? c.y + c.h : g.box.y;
        const curve = mk(c.x + c.w / 2, y, lerp(range, i, g.cards.length));
        const tip = edgeTip(`${prefix}:${c.id}`, d, now);
        return { id: `${prefix}:${c.id}`, cls: clsOf(c.id), ...curve, label: tip?.title ?? '' };
      });
    }
    const y = up ? g.box.y + g.box.h : g.box.y;
    const curve = mk(g.box.x + g.box.w / 2, y, (range[0] + range[1]) / 2);
    const all = nodes[g.key].map((n) => clsOf(n.id));
    const tip = edgeTip(`${prefix}:*`, d, now);
    return [{ id: `${prefix}:*`, cls: worstEdge(all, normal), ...curve, label: tip?.title ?? '' }];
  }

  const edges = $derived.by<Edge[]>(() => {
    const G = L.groups;
    const top2 = !!G.monitors && !!G.channels;
    const bot2 = !!G.probes && !!G.servers;
    const mById = new Map(d.monitors.map((m) => [m.id, m]));
    const pById = new Map(d.probes.map((p) => [p.id, p]));
    const sById = new Map(d.servers.map((s) => [s.id, s]));
    const cById = new Map((d.channels ?? []).map((c) => [c.id, c]));
    const out: Edge[] = [
      ...groupEdges(G.monitors, top2 ? 'left' : 'full', 'chk', (id) => checkEdge(mById.get(id)!), 'check', true, true),
      ...groupEdges(G.channels, 'right', 'ntf', (id) => notifyEdge(cById.get(id)!), 'notify', true, true),
      ...groupEdges(G.probes, bot2 ? 'left' : 'full', 'lnk', (id) => linkEdge(pById.get(id)!), 'link', false, false),
      ...groupEdges(G.servers, bot2 ? 'right' : 'full', 'met', (id) => metricEdge(sById.get(id)!), 'metric', false, false),
    ];
    const pg = G.probes;
    if (pg?.meshArcs && d.mesh) {
      const idx = new Map(pg.cards.map((c, i) => [c.id, i]));
      for (const p of d.mesh.pairs) {
        const ia = idx.get(p.a);
        const ib = idx.get(p.b);
        if (ia === undefined || ib === undefined) continue;
        const [l, r] = ia < ib ? [ia, ib] : [ib, ia];
        const id = `msh:${p.a}:${p.b}`;
        out.push({ id, cls: meshEdge(p.ab, p.ba), ...meshArc(pg.cards[l], pg.cards[r], r - l), label: edgeTip(id, d, now)?.title ?? '' });
      }
    }
    return out;
  });

  // --- İpucu -------------------------------------------------------------------------------
  let tip = $state<{ id: string; x: number; y: number; pinned: boolean } | null>(null);
  let wrap: HTMLDivElement;
  let tipEl = $state<HTMLDivElement>();
  const tipData = $derived(tip ? edgeTip(tip.id, d, now) : null);
  const TIP_W = 300;
  const tipPos = $derived.by(() => {
    if (!tip) return { left: 0, top: 0 };
    const h = tipEl?.offsetHeight ?? 160;
    // Alt sınır: tuvalin ve görüntü alanının alt kenarı (ipucu ekrandan taşmasın); sığmazsa bağlantının üstüne açılır.
    const vis = wrap ? window.innerHeight - wrap.getBoundingClientRect().top : L.height;
    const bottom = Math.min(L.height, vis) - 8;
    const below = tip.y + 12;
    const top = below + h <= bottom ? below : tip.y - 12 - h;
    return {
      left: Math.max(8, Math.min(tip.x + 12, L.width - TIP_W - 8)),
      top: Math.max(8, Math.min(top, bottom - h)),
    };
  });
  function at(e: MouseEvent): [number, number] {
    const r = wrap.getBoundingClientRect();
    return [e.clientX - r.left, e.clientY - r.top];
  }
  function show(e: Edge, x: number, y: number, pinned = false) {
    tip = { id: e.id, x, y, pinned };
  }
  function close() {
    tip = null;
  }
  function onKey(ev: KeyboardEvent) {
    if (ev.key === 'Escape' && tip) close();
  }
  // Sabitlenmiş ipucunun bağlantısı kaybolduysa kapat.
  $effect(() => {
    if (tip && !tipData) tip = null;
  });

  const groupTitle = (k: GroupKey) => t(`topology.col.${k}`);
  const groupCounts = (k: GroupKey) => counts(nodes[k].map((n) => n.cls));
  const emptyLink: Record<GroupKey, { href: string; show: boolean }> = $derived({
    monitors: { href: '#/monitors/new', show: canEdit },
    channels: { href: '#/notifications', show: canEdit },
    probes: { href: '#/settings/probes', show: d.admin },
    servers: { href: '#/servers', show: canEdit },
  });
  const emptyIcon: Record<GroupKey, IconName> = { monitors: 'activity', channels: 'bell', probes: 'network', servers: 'server' };
  const style = (r: Rect) => `left:${r.x}px;top:${r.y}px;width:${r.w}px;height:${r.h}px`;
  // Kenarlık panelin kendi sağlığı (bağlı noktalar); açık olaylar kart içindeki kırmızı metinle gösterilir.
  const hubCls = $derived(d.stats.probes_total > 0 && d.stats.probes_online === 0 ? 'bad' : d.stats.probes_online < d.stats.probes_total ? 'warn' : 'ok');
</script>

<svelte:window onkeydown={onKey} />

<div class="tc" bind:clientWidth={width} bind:this={wrap} style={`height:${L.height}px`} data-testid="topology-canvas">
  {#each groupList as g (g.key)}
    {@const c = groupCounts(g.key)}
    <section class={`grp lv-${groupLevel(c)}`} style={style(g.box)} data-testid={`grp-${g.key}`} aria-label={groupTitle(g.key)}>
      <header>
        <h3>{groupTitle(g.key)}</h3>
        {#if c.total}
          <span class="cnt" aria-label={t('topology.countsAria', { ok: c.ok, warn: c.warn, bad: c.bad })}>
            {#if c.bad}<span class="n bad">{c.bad}</span>{/if}
            {#if c.warn}<span class="n warn">{c.warn}</span>{/if}
            <span class="n ok">{c.ok}</span>
            <span class="tot">/ {c.total}</span>
          </span>
        {/if}
      </header>
      {#if g.empty}
        <div class="gempty" data-testid={`empty-${g.key}`}>
          <Icon name={emptyIcon[g.key]} size={22} />
          <p>{t(`topology.groupEmpty.${g.key}`)}</p>
          {#if emptyLink[g.key].show}<a href={emptyLink[g.key].href}>{t(`topology.groupEmptyLink.${g.key}`)}</a>{/if}
        </div>
      {/if}
      {#if g.meshFallback}<p class="note">{t('topology.meshInMatrix')}</p>{/if}
    </section>
  {/each}

  <svg class="edges" width={L.width} height={L.height} role="group" aria-label={t('topology.edgesAria')}>
    {#each edges as e (e.id)}
      <path class={`e ${e.cls}`} d={e.d} />
    {/each}
    {#each edges as e (e.id)}
      <path
        class="hit"
        class:active={tip?.id === e.id}
        d={e.d}
        tabindex="0"
        role="button"
        aria-label={e.label}
        data-edge={e.id}
        onpointerenter={(ev) => !tip?.pinned && show(e, ...at(ev))}
        onpointerleave={() => !tip?.pinned && close()}
        onclick={(ev) => show(e, ...at(ev), true)}
        onfocus={() => !tip?.pinned && show(e, e.mx, e.my)}
        onblur={() => !tip?.pinned && close()}
        onkeydown={(ev) => {
          if (ev.key === 'Enter' || ev.key === ' ') {
            ev.preventDefault();
            show(e, e.mx, e.my, true);
          }
        }}
      />
    {/each}
  </svg>

  {#each groupList as g (g.key)}
    {#each g.cards as c (c.id)}
      {@const n = nodeOf(g.key, c.id)}
      <svelte:element
        this={n.href ? 'a' : 'div'}
        href={n.href}
        class={`node c-${n.cls}`}
        style={style(c)}
        title={`${n.name} · ${n.sub}`}
        data-testid={`node-${g.key}-${c.id}`}
      >
        <span class="r1"><i class={`sd ${n.cls}`}></i><span class="nm">{n.name}</span></span>
        <span class="r2">{n.sub}</span>
      </svelte:element>
    {/each}
    {#if g.more}
      <button
        type="button"
        class="more"
        style={style(g.more)}
        aria-expanded={g.more.expanded}
        data-testid={`more-${g.key}`}
        onclick={() => (expanded[g.key] = !expanded[g.key])}
      >
        {#if g.more.expanded}
          <Icon name="chevron-down" size={14} /> {t('topology.less')}
        {:else}
          {t('topology.more', { count: g.more.hidden })}
        {/if}
      </button>
    {/if}
  {/each}

  <div class={`hub c-${hubCls}`} style={style(L.hub)} data-testid="topology-hub">
    <span class="r1"><Icon name="server" size={16} /><span class="nm">{t('topology.col.panel')}</span></span>
    <span class="r2">
      {t('topology.panel.monitors', { count: d.monitors.length })} · {t('topology.panel.online', { n: d.stats.probes_online, total: d.stats.probes_total })}
    </span>
    <span class="r2">
      {#if rate !== null}{t('topology.panel.rate', { n: rate })} ·{/if}
      {#if d.stats.open_incidents}<a href="#/incidents" class="bad-text">{t('topology.decision.count', { count: d.stats.open_incidents })}</a>
      {:else}{t('topology.panel.noIncident')}{/if}
    </span>
  </div>

  {#if tip && tipData}
    <div
      class="etip"
      bind:this={tipEl}
      role="dialog"
      aria-label={tipData.title}
      style={`left:${tipPos.left}px;top:${tipPos.top}px;width:${TIP_W}px`}
      data-testid="topology-tip"
    >
      <header>
        <strong>{tipData.title}</strong>
        <button type="button" class="x" aria-label={t('topology.tip.close')} onclick={close}><Icon name="x" size={14} /></button>
      </header>
      <dl>
        {#each tipData.rows as [k, v] (k)}<dt>{k}</dt><dd>{v}</dd>{/each}
      </dl>
    </div>
  {/if}
</div>

<style>
  .tc {
    position: relative;
    width: 100%;
    min-width: 0;
  }
  .grp {
    position: absolute;
    border: 1px solid var(--border);
    border-top-width: 3px;
    border-radius: var(--radius-sm);
    background: var(--card-2);
  }
  .grp.lv-ok {
    border-top-color: var(--up);
  }
  .grp.lv-warn {
    border-top-color: var(--warn);
  }
  .grp.lv-bad {
    border-top-color: var(--down);
  }
  /* Başlık metni ve sayaçlar bağlantı çizgilerinin üstünde kalır. */
  .grp header h3,
  .grp header .cnt {
    position: relative;
    z-index: 3;
    background: var(--card-2);
    box-shadow: 0 0 0 4px var(--card-2);
  }
  .grp header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    height: 35px;
    padding: 0 14px;
  }
  .grp h3 {
    margin: 0;
    font-size: 0.8rem;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--text-2);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .cnt {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 0.78rem;
    font-variant-numeric: tabular-nums;
    flex: none;
  }
  .n {
    min-width: 20px;
    padding: 1px 6px;
    border-radius: 999px;
    text-align: center;
    font-weight: 600;
  }
  .n.ok {
    background: var(--up-soft);
    color: var(--up-text);
  }
  .n.warn {
    background: var(--pending-soft);
    color: var(--pending-text);
  }
  .n.bad {
    background: var(--down-soft);
    color: var(--down-text);
  }
  .tot {
    color: var(--muted);
  }
  .gempty {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 6px;
    height: 100px;
    padding: 0 16px;
    color: var(--muted);
    text-align: center;
  }
  .gempty p {
    margin: 0;
    font-size: 0.85rem;
  }
  .gempty a {
    font-size: 0.85rem;
  }
  .note {
    position: absolute;
    right: 14px;
    bottom: 2px;
    margin: 0;
    font-size: 0.72rem;
    color: var(--muted);
  }
  .edges {
    position: absolute;
    inset: 0;
    overflow: visible;
    pointer-events: none;
  }
  .e {
    fill: none;
    stroke-width: 1.6;
  }
  .e.check {
    stroke: var(--spark);
    stroke-dasharray: 4 4;
    animation: flow 1.4s linear infinite;
  }
  .e.link {
    stroke: var(--accent);
    stroke-dasharray: 6 5;
    animation: flow 1.1s linear infinite;
  }
  .e.metric {
    stroke: var(--maint);
    stroke-dasharray: 2 5;
    animation: flow 2s linear infinite;
  }
  .e.mesh {
    stroke: var(--text-2);
    opacity: 0.7;
  }
  .e.notify {
    stroke: var(--muted);
  }
  .e.down {
    stroke: var(--down);
    stroke-dasharray: 2 5;
    stroke-width: 2;
  }
  .e.warn {
    stroke: var(--warn);
    stroke-dasharray: 8 4;
  }
  .e.off {
    stroke: var(--paused);
    stroke-dasharray: 1 6;
    opacity: 0.7;
  }
  @keyframes flow {
    to {
      stroke-dashoffset: -22;
    }
  }
  .hit {
    fill: none;
    stroke: transparent;
    stroke-width: 14;
    pointer-events: stroke;
    cursor: pointer;
    outline: none;
  }
  /* Vurgu nötr: sorunlu (turuncu/kırmızı) bağlantı yeşil bantla 'sağlıklı' görünmesin. */
  .hit:focus-visible,
  .hit.active {
    stroke: color-mix(in srgb, var(--text) 12%, transparent);
  }
  .hit:focus-visible {
    stroke: color-mix(in srgb, var(--text) 22%, transparent);
  }
  .node,
  .hub,
  .more {
    position: absolute;
    display: flex;
    flex-direction: column;
    justify-content: center;
    gap: 3px;
    padding: 0 12px;
    border: 1px solid var(--border);
    border-left-width: 3px;
    border-radius: var(--radius-sm);
    background: var(--card);
    color: var(--text);
    text-decoration: none;
    min-width: 0;
    overflow: hidden;
  }
  a.node:hover,
  a.node:focus-visible {
    background: var(--card-hover);
    border-color: var(--border-hover);
  }
  .c-ok {
    border-left-color: var(--up);
  }
  .c-warn,
  .c-pend {
    border-left-color: var(--warn);
  }
  .c-bad {
    border-left-color: var(--down);
    background: color-mix(in srgb, var(--down-soft) 60%, var(--card));
  }
  .c-maint {
    border-left-color: var(--maint);
  }
  .c-off {
    border-left-color: var(--paused);
  }
  .r1 {
    display: flex;
    align-items: center;
    gap: 7px;
    min-width: 0;
  }
  .nm {
    font-weight: 600;
    font-size: 0.88rem;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .r2 {
    font-size: 0.76rem;
    color: var(--muted);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .more {
    align-items: center;
    flex-direction: row;
    gap: 6px;
    border-style: dashed;
    border-color: var(--border-strong);
    border-left-width: 1px;
    background: transparent;
    color: var(--text-2);
    font: inherit;
    font-size: 0.85rem;
    cursor: pointer;
  }
  .more:hover {
    color: var(--text);
    border-color: var(--border-hover);
  }
  .hub {
    border-width: 1px;
    border-top-width: 3px;
    border-left-width: 1px;
    background: var(--card);
    box-shadow: 0 0 0 4px var(--bg);
  }
  .hub.c-ok {
    border-top-color: var(--up);
  }
  .hub.c-warn {
    border-top-color: var(--warn);
  }
  .hub.c-bad {
    border-top-color: var(--down);
  }
  .bad-text {
    color: var(--down-text-2);
  }
  .sd {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    flex: none;
    background: var(--paused);
  }
  .sd.ok {
    background: var(--up);
  }
  .sd.warn,
  .sd.pend {
    background: var(--warn);
  }
  .sd.bad {
    background: var(--down);
  }
  .sd.maint {
    background: var(--maint);
  }
  .etip {
    position: absolute;
    z-index: 5;
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-sm);
    background: var(--bg-elev);
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.35);
    font-size: 0.82rem;
  }
  .etip header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    padding: 6px 6px 6px 10px;
    border-bottom: 1px solid var(--border);
  }
  .etip strong {
    overflow-wrap: anywhere;
  }
  .x {
    display: grid;
    place-items: center;
    width: 28px;
    height: 28px;
    flex: none;
    border: 0;
    border-radius: 4px;
    background: none;
    color: var(--muted);
    cursor: pointer;
  }
  .x:hover,
  .x:focus-visible {
    color: var(--text);
    background: var(--card-hover);
  }
  .etip dl {
    display: grid;
    grid-template-columns: minmax(6em, 1fr) minmax(0, max-content);
    gap: 4px 14px;
    margin: 0;
    padding: 8px 10px;
  }
  .etip dt {
    color: var(--muted);
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .etip dd {
    margin: 0;
    text-align: right;
    font-variant-numeric: tabular-nums;
    overflow-wrap: break-word;
    min-width: 0;
  }
  @media (prefers-reduced-motion: reduce) {
    .e {
      animation: none !important;
    }
  }
</style>
