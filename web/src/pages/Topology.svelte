<script lang="ts">
  // Canlı topoloji: paneldeki gerçek monitörler, kontrol noktaları, sunucular
  // ve kanallar. Veri lib/topology.svelte.ts (tek uç, canlı akış + yedek yoklama).
  // Bu sürüm iskelet: düğümler sütunlarda; bağlantı çizimi sonraki adımda.
  import { onDestroy, onMount } from 'svelte';
  import { TopologyStore, POLL_MS } from '../lib/topology.svelte';
  import { live } from '../lib/live.svelte';
  import { session } from '../lib/session.svelte';
  import { t } from '../lib/i18n';
  import { fmtRelative, fmtTimeSec, fmtMs, fmtNum } from '../lib/format';
  import { typeLabel } from '../lib/monitorTypes';
  import { NOTIFY_LABELS } from '../lib/notifyTypes';
  import type { TopoLocation, TopoMonitor, TopoProbe, TopoServer, TopoChannel } from '../lib/api';
  import Icon from '../components/Icon.svelte';

  const store = new TopologyStore();
  onMount(() => store.start());
  onDestroy(() => store.stop());

  // Göreli zamanlar her saniye ilerlesin (yeni veri gelmese de).
  let now = $state(Math.floor(Date.now() / 1000));
  const clock = setInterval(() => (now = Math.floor(Date.now() / 1000)), 1000);
  onDestroy(() => clearInterval(clock));

  const d = $derived(store.data);
  const isEmpty = $derived(!!d && d.monitors.length === 0 && d.probes.length === 0 && d.servers.length === 0);
  const MAX_INCIDENTS = 5;

  const stateClass: Record<TopoMonitor['state'], string> = {
    up: 'ok',
    partial: 'warn',
    down: 'bad',
    pending: 'pend',
    paused: 'off',
    maintenance: 'maint',
  };
  function locClass(l: TopoLocation): string {
    if (l.oob) return 'warn';
    return l.status === 'up' ? 'ok' : l.status === 'down' ? 'bad' : l.status === 'retrying' ? 'pend' : 'off';
  }
  const locLabel = (l: TopoLocation) => (l.oob ? t('topology.loc.oob') : t(`topology.loc.${l.status}`));
  function locTip(l: TopoLocation): string {
    const parts = [l.name, locLabel(l)];
    if (l.ping_ms >= 0 && l.status === 'up') parts.push(fmtMs(l.ping_ms));
    if (l.last_check_at) parts.push(fmtRelative(l.last_check_at, now));
    return parts.join(' · ');
  }
  const pct = (v: number | undefined) => (v === undefined ? '—' : `${Math.round(v)}%`);
  const serverClass = (s: TopoServer) => (s.state === 'offline' ? 'bad' : s.firing.length ? 'warn' : s.state === 'online' ? 'ok' : 'off');
  const probeClass = (p: TopoProbe) => (!p.online ? 'bad' : p.uplink === 'oob' ? 'warn' : 'ok');
  function channelLine(c: TopoChannel): string {
    if (!c.active) return t('topology.channel.inactive');
    if (!c.last_send) return t('topology.channel.neverSent');
    const ago = fmtRelative(c.last_send.at, now);
    return c.last_send.ok ? t('topology.channel.sentOk', { ago }) : t('topology.channel.sentFail', { ago });
  }
  const channelClass = (c: TopoChannel) => (!c.active || !c.last_send ? 'off' : c.last_send.ok ? 'ok' : 'bad');
  const notifyLabel = (type: string) => (NOTIFY_LABELS as Record<string, string>)[type] ?? type;
  const rate = (v: number) => (v >= 10 ? fmtNum(Math.round(v)) : v.toFixed(1));
</script>

<div class="page-head">
  <div class="ph">
    <h1>{t('topology.title')}<span class="dot">.</span></h1>
    <p class="muted small sub">{t('topology.subtitle')}</p>
  </div>
  <div class="live-state" data-testid="topology-live">
    {#if live.connected}
      <span class="pill ok" title={t('topology.live.sseTip')}><i class="sd"></i>{t('topology.live.sse')}</span>
    {:else}
      <span class="pill pend" title={t('topology.live.pollTip', { s: POLL_MS / 1000 })}><i class="sd"></i>{t('topology.live.poll')}</span>
    {/if}
    {#if store.updatedAt}
      <span class="muted small">{t('topology.live.updated', { time: fmtTimeSec(Math.floor(store.updatedAt / 1000)) })}</span>
    {/if}
  </div>
</div>

{#if !d && store.loading}
  <div class="board card" aria-busy="true" data-testid="topology-loading">
    <div class="stats">
      {#each [0, 1, 2] as i (i)}<div class="skeleton stat-sk"></div>{/each}
    </div>
    <div class="cols">
      {#each [0, 1, 2, 3, 4] as c (c)}
        <div class="col">
          <div class="skeleton head-sk"></div>
          {#each [0, 1, 2] as r (r)}<div class="skeleton node-sk"></div>{/each}
        </div>
      {/each}
    </div>
  </div>
{:else if !d}
  <div class="card empty" data-testid="topology-error">
    <Icon name="alert" size={28} />
    <h3>{t('topology.error')}</h3>
    <p>{store.error}</p>
    <button class="btn" onclick={() => store.fetch()}><Icon name="refresh" size={16} /> {t('topology.retry')}</button>
  </div>
{:else if isEmpty}
  <div class="card empty" data-testid="topology-empty">
    <Icon name="network" size={32} />
    <h3>{t('topology.empty.title')}</h3>
    <p>{session.restricted ? t('topology.empty.restricted') : t('topology.empty.text')}</p>
    {#if session.canEdit}<a class="btn primary" href="#/monitors/new"><Icon name="plus" size={16} /> {t('topology.empty.add')}</a>{/if}
  </div>
{:else}
  {#if store.error}<p class="err small" role="status">{t('topology.error')}: {store.error}</p>{/if}
  <section class="board card" data-testid="topology-board">
    <div class="stats">
      <div class="stat">
        <span class="cap">{t('topology.stats.probes')}</span>
        <strong>{d.stats.probes_online}<span class="muted">/{d.stats.probes_total}</span></strong>
      </div>
      {#if d.stats.results_per_sec !== undefined}
        <div class="stat">
          <span class="cap">{t('topology.stats.rate')}</span>
          <strong>{rate(d.stats.results_per_sec)}</strong>
        </div>
      {/if}
      <div class="stat" class:alarm={d.stats.open_incidents > 0}>
        <span class="cap">{t('topology.stats.incidents')}</span>
        <strong>{d.stats.open_incidents}</strong>
      </div>
    </div>

    <div class="cols">
      <div class="col" data-testid="col-monitors">
        <h3>{t('topology.col.monitors')} <span class="muted">{d.monitors.length}</span></h3>
        {#each d.monitors as m (m.id)}
          <a class="node" href={`#/monitors/${m.id}`}>
            <div class="row1">
              <i class="sd {stateClass[m.state]}"></i>
              <span class="name">{m.name}</span>
              <span class="tag {stateClass[m.state]}">{t(`topology.state.${m.state}`)}</span>
            </div>
            <div class="row2 muted">
              <span>{typeLabel(m.type)}</span>
              {#if m.locations.length}
                <span class="locs">
                  {#each m.locations as l (l.probe_id)}
                    <span class="loc {locClass(l)}" title={locTip(l)} aria-label={locTip(l)}></span>
                  {/each}
                </span>
              {/if}
            </div>
          </a>
        {:else}
          <p class="muted small">{t('topology.noneInCol')}</p>
        {/each}
      </div>

      <div class="col" data-testid="col-probes">
        <h3>{t('topology.col.probes')} <span class="muted">{d.probes.length}</span></h3>
        {#each d.probes as p (p.id)}
          <div class="node">
            <div class="row1">
              <i class="sd {probeClass(p)}"></i>
              <span class="name">{p.name}</span>
              <span class="tag {p.online ? 'ok' : 'bad'}">{p.online ? t('topology.probe.online') : t('topology.probe.offline')}</span>
            </div>
            {#if d.admin}
              <dl class="row2 muted kv">
                <div><dt>{t('topology.probe.rtt')}</dt><dd>{p.panel_rtt_ms !== undefined ? fmtMs(p.panel_rtt_ms) : '—'}</dd></div>
                <div><dt>{t('topology.probe.uplink')}</dt><dd>{p.uplink === 'oob' ? t('topology.probe.oob') : t('topology.probe.primary')}</dd></div>
                <div><dt>{t('topology.probe.peers')}</dt><dd>{p.peers ? `${p.peers_up}/${p.peers}` : '—'}</dd></div>
              </dl>
            {/if}
          </div>
        {:else}
          <p class="muted small">{t('topology.noneInCol')}</p>
        {/each}
      </div>

      <div class="col center" data-testid="col-panel">
        <h3>{t('topology.col.panel')}</h3>
        <div class="node hub" class:alarm={d.stats.open_incidents > 0}>
          <div class="row1"><Icon name="server" size={16} /><span class="name">{t('topology.col.panel')}</span></div>
          <div class="row2 muted stackline">
            <span>{t('topology.panel.monitors', { count: d.monitors.length })}</span>
            <span>{t('topology.panel.online', { n: d.stats.probes_online, total: d.stats.probes_total })}</span>
            {#if d.stats.results_per_sec !== undefined}<span>{t('topology.panel.rate', { n: rate(d.stats.results_per_sec) })}</span>{/if}
          </div>
        </div>
      </div>

      {#if d.servers.length}
        <div class="col" data-testid="col-servers">
          <h3>{t('topology.col.servers')} <span class="muted">{d.servers.length}</span></h3>
          {#each d.servers as s (s.id)}
            <a class="node" href={`#/servers/${s.id}`}>
              <div class="row1">
                <i class="sd {serverClass(s)}"></i>
                <span class="name">{s.name}</span>
              </div>
              {#if s.cpu !== undefined}
                <dl class="row2 muted kv">
                  <div><dt>{t('topology.server.cpu')}</dt><dd>{pct(s.cpu)}</dd></div>
                  <div><dt>{t('topology.server.mem')}</dt><dd>{pct(s.mem)}</dd></div>
                  <div><dt>{t('topology.server.disk')}</dt><dd>{pct(s.disk)}</dd></div>
                </dl>
              {:else}
                <div class="row2 muted">{t('topology.server.noMetrics')}</div>
              {/if}
              {#if s.firing.length}<div class="row2 warn-text">{t('topology.server.firing', { list: s.firing.join(', ') })}</div>{/if}
            </a>
          {/each}
        </div>
      {/if}

      {#if d.channels && d.channels.length}
        <div class="col" data-testid="col-channels">
          <h3>{t('topology.col.channels')} <span class="muted">{d.channels.length}</span></h3>
          {#each d.channels as c (c.id)}
            <div class="node">
              <div class="row1">
                <i class="sd {channelClass(c)}"></i>
                <span class="name">{c.name}</span>
                <span class="muted small">{notifyLabel(c.type)}</span>
              </div>
              <div class="row2 muted">{channelLine(c)}</div>
            </div>
          {/each}
        </div>
      {/if}
    </div>
  </section>

  {#if d.incidents.length}
    <section class="card decision" aria-live="polite" data-testid="topology-decision">
      <h2>{t('topology.decision.title')} <span class="muted small">{t('topology.decision.count', { count: d.stats.open_incidents })}</span></h2>
      <ul>
        {#each d.incidents.slice(0, MAX_INCIDENTS) as inc (inc.id)}
          <li>
            <a href={`#/incidents/${inc.id}`}>
              <i class="sd bad"></i>
              <span class="name">{inc.monitor_name || inc.server_name || inc.cause}</span>
            </a>
            <span class="muted small">{inc.cause} · {t('topology.decision.started', { ago: fmtRelative(inc.started_at, now) })}</span>
            {#if inc.acked}<span class="tag ok">{t('topology.decision.acked')}</span>{/if}
          </li>
        {/each}
      </ul>
      {#if d.stats.open_incidents > MAX_INCIDENTS}
        <p class="muted small">{t('topology.decision.more', { count: d.stats.open_incidents - MAX_INCIDENTS })}</p>
      {/if}
      <a class="btn" href="#/incidents">{t('topology.decision.all')}</a>
    </section>
  {/if}
{/if}

<style>
  .sub {
    margin: 4px 0 0;
  }
  .live-state {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .pill {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 3px 10px;
    border-radius: 999px;
    border: 1px solid var(--border);
    font-size: 0.8rem;
    font-weight: 600;
  }
  .pill.ok {
    color: var(--accent-text);
    border-color: var(--accent-border);
  }
  .pill.pend {
    color: var(--warn-text);
  }
  .sd {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    flex: none;
    background: var(--muted);
  }
  .pill.ok .sd,
  .sd.ok {
    background: var(--up);
  }
  .pill.pend .sd,
  .sd.pend {
    background: var(--pending);
  }
  .sd.warn {
    background: var(--warn);
  }
  .sd.bad {
    background: var(--down);
  }
  .sd.maint {
    background: var(--maint);
  }
  .sd.off {
    background: var(--paused);
  }
  .board {
    display: flex;
    flex-direction: column;
    gap: 18px;
  }
  .stats {
    display: flex;
    flex-wrap: wrap;
    gap: 12px;
  }
  .stat {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 150px;
    padding: 10px 14px;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--card-2);
  }
  .stat strong {
    font-size: 1.35rem;
    font-variant-numeric: tabular-nums;
  }
  .stat.alarm {
    border-color: var(--down-border);
  }
  .stat.alarm strong {
    color: var(--down-text-2);
  }
  .cap {
    font-size: 0.75rem;
    color: var(--muted);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  .cols {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
    gap: 16px;
    align-items: start;
  }
  .col {
    display: flex;
    flex-direction: column;
    gap: 8px;
    min-width: 0;
  }
  .col h3 {
    margin: 0 0 2px;
    font-size: 0.85rem;
    font-weight: 700;
    color: var(--text-2);
  }
  .node {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding: 10px 12px;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--card-2);
    color: inherit;
    text-decoration: none;
    min-width: 0;
  }
  a.node:hover {
    border-color: var(--border-hover);
  }
  a.node:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }
  .node.hub {
    border-color: var(--accent-border);
  }
  .node.hub.alarm {
    border-color: var(--down-border);
  }
  .row1 {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
  }
  .name {
    font-weight: 600;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    flex: 1;
    min-width: 0;
  }
  .row2 {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 4px 10px;
    font-size: 0.8rem;
    margin: 0;
  }
  .stackline {
    flex-direction: column;
    align-items: flex-start;
  }
  .kv {
    justify-content: flex-start;
  }
  .kv div {
    display: flex;
    gap: 4px;
  }
  .kv dd {
    margin: 0;
    color: var(--text-2);
    font-variant-numeric: tabular-nums;
  }
  .tag {
    font-size: 0.72rem;
    font-weight: 600;
    padding: 1px 7px;
    border-radius: 999px;
    border: 1px solid var(--border);
    color: var(--text-2);
    white-space: nowrap;
  }
  .tag.ok {
    color: var(--accent-text);
  }
  .tag.bad {
    color: var(--down-text-2);
    border-color: var(--down-border);
  }
  .tag.warn,
  .tag.pend {
    color: var(--warn-text);
  }
  .tag.maint {
    color: var(--maint-text);
  }
  .locs {
    display: inline-flex;
    gap: 3px;
  }
  .loc {
    width: 10px;
    height: 10px;
    border-radius: 3px;
    background: var(--paused);
  }
  .loc.ok {
    background: var(--up);
  }
  .loc.bad {
    background: var(--down);
  }
  .loc.pend {
    background: var(--pending);
  }
  .loc.warn {
    background: transparent;
    border: 2px dashed var(--warn);
  }
  .warn-text {
    color: var(--warn-text);
  }
  .err {
    color: var(--down-text-2);
    margin: 0 0 10px;
  }
  .decision {
    margin-top: 16px;
    border-color: var(--down-border);
  }
  .decision h2 {
    margin: 0 0 10px;
    font-size: 1.05rem;
  }
  .decision ul {
    list-style: none;
    margin: 0 0 12px;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .decision li {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 4px 10px;
  }
  .decision li a {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    color: inherit;
    font-weight: 600;
    min-width: 0;
  }
  .stat-sk {
    width: 150px;
    height: 58px;
  }
  .head-sk {
    height: 14px;
    width: 60%;
  }
  .node-sk {
    height: 54px;
  }
  @media (max-width: 640px) {
    .stat {
      min-width: 0;
      flex: 1 1 40%;
    }
    .stat-sk {
      flex: 1 1 40%;
    }
  }
</style>
