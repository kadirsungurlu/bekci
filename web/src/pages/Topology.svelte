<script lang="ts">
  // Canlı topoloji: paneldeki gerçek monitörler, kontrol noktaları, sunucular
  // ve kanallar. Veri lib/topology.svelte.ts (tek uç, canlı akış + yedek yoklama).
  // Geniş ekranda tuval (components/TopoCanvas), dar ekranda dikey liste (TopoList).
  import { onDestroy, onMount } from 'svelte';
  import { TopologyStore, POLL_MS } from '../lib/topology.svelte';
  import { live } from '../lib/live.svelte';
  import { session } from '../lib/session.svelte';
  import { t } from '../lib/i18n';
  import { fmtTimeSec, fmtNum } from '../lib/format';
  import Icon from '../components/Icon.svelte';
  import TopoCanvas from '../components/TopoCanvas.svelte';
  import TopoList from '../components/TopoList.svelte';
  import TopoMatrix from '../components/TopoMatrix.svelte';
  import TopoDecision from '../components/TopoDecision.svelte';
  import { MIN_CANVAS_W } from '../lib/topolayout';

  const store = new TopologyStore();
  onMount(() => store.start());
  onDestroy(() => store.stop());

  // Göreli zamanlar her saniye ilerlesin (yeni veri gelmese de).
  let now = $state(Math.floor(Date.now() / 1000));
  const clock = setInterval(() => (now = Math.floor(Date.now() / 1000)), 1000);
  onDestroy(() => clearInterval(clock));

  const d = $derived(store.data);
  const isEmpty = $derived(!!d && d.monitors.length === 0 && d.probes.length === 0 && d.servers.length === 0);
  const rate = (v: number) => (v >= 10 ? fmtNum(Math.round(v)) : v.toFixed(1));
  // Kart içi genişlik (padding düşülmüş) tuval için yetmiyorsa liste görünümü.
  let boardW = $state(1200);
  const wide = $derived(boardW - 40 >= MIN_CANVAS_W);
  const showMatrix = $derived(!!d?.admin && !!d?.mesh && d.mesh.nodes.length >= 2);
  const rateStr = $derived(d?.stats.results_per_sec !== undefined ? rate(d.stats.results_per_sec) : null);
  const LEGEND = ['check', 'link', 'metric', 'mesh', 'notify', 'down', 'warn', 'off'] as const;
</script>

<div class="page-head">
  <div class="ph">
    <h1>{t('topology.title')}<span class="dot">.</span></h1>
    <p class="muted small sub">{t('topology.subtitle')}</p>
  </div>
  <div class="live-state" data-testid="topology-live">
    {#if live.connected}
      <span class="pill up" title={t('topology.live.sseTip')}>{t('topology.live.sse')}</span>
    {:else}
      <span class="pill pending" title={t('topology.live.pollTip', { s: POLL_MS / 1000 })}>{t('topology.live.poll')}</span>
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
  <section class="board card" data-testid="topology-board" bind:clientWidth={boardW}>
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

    {#if wide}
      <div class="bhead">
        <ul class="legend" aria-label={t('topology.legend.title')}>
          {#each LEGEND as k (k)}
            <li><svg width="26" height="6" aria-hidden="true"><line class={`lg ${k}`} x1="1" y1="3" x2="25" y2="3" /></svg>{t(`topology.legend.${k}`)}</li>
          {/each}
        </ul>
        <span class="muted small hint">{t('topology.legend.hint')}</span>
      </div>
      <TopoCanvas {d} {now} canEdit={session.canEdit} rate={rateStr} />
    {:else}
      <TopoList {d} {now} canEdit={session.canEdit} rate={rateStr} />
    {/if}
  </section>

  <div class="below" class:two={!!(showMatrix && d.incidents.length)}>
    {#if showMatrix && d.mesh}
      <TopoMatrix mesh={d.mesh} />
    {/if}
    {#if d.incidents.length}
      <TopoDecision {d} {now} />
    {/if}
  </div>
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
  .sd {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    flex: none;
    background: var(--muted);
  }
  .sd.ok {
    background: var(--up);
  }
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
  .err {
    color: var(--down-text-2);
    margin: 0 0 10px;
  }
  .bhead {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 8px 18px;
  }
  .legend {
    display: flex;
    flex-wrap: wrap;
    gap: 6px 16px;
    margin: 0;
    padding: 0;
    list-style: none;
    font-size: 0.78rem;
    color: var(--text-2);
  }
  .legend li {
    display: flex;
    align-items: center;
    gap: 6px;
    white-space: nowrap;
  }
  .lg {
    stroke-width: 2;
  }
  .lg.check {
    stroke: var(--spark);
    stroke-dasharray: 4 4;
  }
  .lg.link {
    stroke: var(--accent);
    stroke-dasharray: 6 5;
  }
  .lg.metric {
    stroke: var(--maint);
    stroke-dasharray: 2 5;
  }
  .lg.mesh {
    stroke: var(--text-2);
  }
  .lg.notify {
    stroke: var(--muted);
  }
  .lg.down {
    stroke: var(--down);
    stroke-dasharray: 2 5;
  }
  .lg.warn {
    stroke: var(--warn);
    stroke-dasharray: 8 4;
  }
  .lg.off {
    stroke: var(--paused);
    stroke-dasharray: 1 6;
  }
  .below {
    display: grid;
    gap: 16px;
    margin-top: 16px;
    min-width: 0;
  }
  @media (min-width: 1100px) {
    .below.two {
      grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
      align-items: start;
    }
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
  @media (max-width: 640px) {
    .btn {
      min-height: 44px;
    }
  }
</style>
