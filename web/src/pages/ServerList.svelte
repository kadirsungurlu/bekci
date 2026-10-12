<script lang="ts">
  import { onMount } from 'svelte';
  import { api, errorMessage, type DiskInfo, type ServerView } from '../lib/api';
  import { session } from '../lib/session.svelte';
  import { clock, toast } from '../lib/ui.svelte';
  import { fmtDec, fmtPctInt, fmtRate, fmtRelative, fmtTemp, fmtUptime } from '../lib/format';
  import { t } from '../lib/i18n';
  import {
    STATE_LABELS,
    diskPct,
    diskSummary,
    fullestMount,
    hostLine,
    inactiveReason,
    isInactive,
    memPct,
    metricLabel,
    serverTone,
    servers,
    swapPct,
    usageLevel,
  } from '../lib/servers.svelte';
  import UsageBar from '../components/UsageBar.svelte';
  import Sparkline from '../components/Sparkline.svelte';
  import ServerSetupModal from '../components/ServerSetupModal.svelte';
  import Icon from '../components/Icon.svelte';

  onMount(() => servers.ensure());

  const active = $derived(servers.sorted.filter((s) => !isInactive(s)));
  const inactive = $derived(servers.sorted.filter(isInactive));
  const counts = $derived.by(() => {
    let online = 0,
      offline = 0,
      warn = 0;
    for (const s of servers.list) {
      if (s.state === 'offline') offline++;
      else if (s.state === 'online') {
        if (s.firing?.length) warn++;
        else online++;
      }
    }
    return { online, offline, warn };
  });

  // Ajan güncellemesi: panelin sürümüne geçebilecek ajanlar ve dikkat isteyen durumlar.
  const updatable = $derived(servers.list.filter((s) => s.update?.state === 'outdated' && !s.update.requested).length);
  const VER_WARN = new Set(['outdated', 'unsupported', 'failed', 'updating']);
  function verTitle(s: ServerView): string | undefined {
    const u = s.update;
    if (!u || !VER_WARN.has(u.state)) return undefined;
    const vars = { v: s.version || '—', panel: u.panel || '—', note: u.note || '' };
    switch (u.state) {
      case 'outdated':
        return `${t('probes.update.outdated')}: ${u.auto_effective || u.requested ? t('probes.update.outdatedAuto', vars) : t('probes.update.outdatedManual', vars)}`;
      case 'unsupported':
        return `${t('probes.update.unsupported')}: ${t('probes.update.unsupportedTitle', vars)}`;
      case 'failed':
        return `${t('probes.update.failed')}: ${t('probes.update.failedTitle', vars)}`;
      default:
        return t('probes.update.updatingTitle', vars);
    }
  }
  // Sürüm satırındaki kısa durum etiketi (güncel ve bilinmeyen durumda yok).
  function verTag(s: ServerView): string | undefined {
    switch (s.update?.state) {
      case 'outdated':
        return t('probes.update.outdated');
      case 'unsupported':
        return t('probes.update.unsupported');
      case 'failed':
        return t('probes.update.failed');
      case 'updating':
        return t('probes.update.updating');
      case 'unversioned':
        return t('probes.update.unversioned');
      case 'readonly':
        return t('probes.update.readonly');
      case 'unsigned':
        return t('probes.update.unsigned');
      case 'off':
        return t('probes.update.off');
      default:
        return undefined;
    }
  }
  let allBusy = $state(false);
  async function updateAll() {
    allBusy = true;
    try {
      const r = await api.updateAllAgents('server');
      toast.success(t('probes.update.allToast', { n: r.requested }));
      servers.load();
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      allBusy = false;
    }
  }

  let addOpen = $state(false);
  let renewFor = $state<ServerView | null>(null);
  let renewOpen = $state(false);

  function openAdd() {
    renewFor = null;
    addOpen = true;
  }
  function openRenew(s: ServerView) {
    renewFor = s;
    renewOpen = true;
  }

  // Disk bölüm başına ayrı kural olabildiği için aynı metrik birden fazla gelebilir ("disk", "disk"): rozet bir kez.
  const firingLabels = (s: ServerView) => [...new Set((s.firing ?? []).filter((m) => m !== 'offline'))].map(metricLabel);

  // Geniş ekran sütunları ---------------------------------------------------------------
  const dpct = (d: DiskInfo) => (d.total > 0 ? (100 * d.used) / d.total : 0);
  /** Birden çok bölümlü sunucuda bölümler (en dolu önce), en çok 3'ü gösterilir. */
  const MAX_DISKS = 3;
  const sortedDisks = (s: ServerView) => (s.latest?.disks ?? []).slice().sort((a, b) => dpct(b) - dpct(a));
  /** CPU grafiğinin ölçeği: 0'dan en yüksek değerin biraz üstüne (en az %25, en çok %100). */
  function cpuScale(h: number[]): { max: number; peak: number } {
    const peak = h.length ? Math.max(...h) : 0;
    return { peak, max: Math.min(100, Math.max(25, Math.ceil((peak * 1.15) / 5) * 5)) };
  }
  const tempLevel = (c: number | null) => (c === null ? 'ok' : c >= 90 ? 'danger' : c >= 80 ? 'warn' : 'ok');
</script>

<div class="page-head">
  <div class="ph">
    <h1>{t('servers.list.title')}<span class="dot">.</span></h1>
    {#if servers.loaded && servers.list.length}
      <div class="sum">
        <span><i class="sd up"></i>{t('servers.list.online', { count: counts.online })}</span>
        {#if counts.warn}<span class="c-pending"><i class="sd warn"></i>{t('servers.list.warn', { count: counts.warn })}</span>{/if}
        {#if counts.offline}<span class="c-down"><i class="sd down"></i>{t('servers.list.offline', { count: counts.offline })}</span>{/if}
      </div>
    {/if}
  </div>
  {#if session.isAdmin && servers.list.length > 0}
    <div class="head-actions">
      {#if updatable > 0}
        <button class="btn" onclick={updateAll} disabled={allBusy} title={t('probes.update.allTitle', { n: updatable, panel: session.version })}>
          {#if allBusy}<span class="spinner"></span>{:else}<Icon name="refresh" size={16} />{/if}
          {t('probes.update.all')} ({updatable})
        </button>
      {/if}
      <button class="btn primary" onclick={openAdd}><Icon name="plus" size={16} /> {t('servers.list.add')}</button>
    </div>
  {/if}
</div>

{#if !servers.loaded}
  {#if servers.loadError}
    <div class="card empty">
      <h3>{t('servers.list.loadFailed')}</h3>
      <p>{servers.loadError}</p>
      <button class="btn primary" onclick={() => servers.load()}>{t('common.retry')}</button>
    </div>
  {:else}
    <div class="card list">
      {#each Array(4) as _, i (i)}
        <div class="sk-row"><div class="skeleton" style="width:10px;height:10px;border-radius:50%"></div><div class="skeleton" style="flex:1;height:34px"></div></div>
      {/each}
    </div>
  {/if}
{:else if servers.list.length === 0}
  <div class="card empty first">
    <div class="hero-ic"><Icon name="server" size={30} /></div>
    <h3>{t('servers.list.emptyTitle')}</h3>
    <p>{t('servers.list.emptyText')}</p>
    {#if session.isAdmin}
      <button class="btn primary" onclick={openAdd}><Icon name="plus" size={16} /> {t('servers.list.add')}</button>
      <p class="help hint">{t('servers.list.emptyHintAdmin')}</p>
    {:else}
      <p class="help hint">{t('servers.list.emptyHintViewer')}</p>
    {/if}
  </div>
{:else}
  {#if active.length}
    <div class="card list">
      <div class="lhead" aria-hidden="true">
        <span class="a-nm">{t('servers.list.cols.server')}</span>
        <span class="a-cpu">{metricLabel('cpu')}</span>
        <span class="a-trend">{t('overview.servers.cols.trend')}</span>
        <span class="a-mem">{metricLabel('mem')}</span>
        <span class="a-swap r">{metricLabel('swap')}</span>
        <span class="a-disk">{metricLabel('disk')}</span>
        <span class="a-temp r" title={metricLabel('temp')}>{t('overview.servers.cols.temp')}</span>
        <span class="a-dock r">{t('overview.servers.cols.docker')}</span>
        <span class="a-net">{metricLabel('net')}</span>
        <span class="a-load">{metricLabel('load')}</span>
        <span class="a-up">{t('servers.list.cols.uptime')}</span>
        <span class="a-agent">{t('overview.servers.cols.agent')}</span>
      </div>
      {#each active as s (s.id)}
        {@const tone = serverTone(s)}
        {@const st = s.latest}
        {@const fl = firingLabels(s)}
        {@const hl = hostLine(s)}
        <a class="srow {tone}" href="#/servers/{s.id}">
          <div class="nm a-nm">
            <span class="sdot {tone}" title={STATE_LABELS[s.state]} aria-hidden="true"></span>
            <div class="nmt">
              <div class="n1">
                <span class="name">{s.name}</span>
                {#each fl as l (l)}<span class="badge pending fire" title={t('servers.list.firingTitle', { metric: l })}><Icon name="alert" size={11} /> {l}</span>{/each}
                {#if s.in_maintenance}<span class="badge maintenance fire" title={t('servers.inMaintenanceHint')}><Icon name="wrench" size={11} /> {t('servers.inMaintenance')}</span>{/if}
              </div>
              {#if s.state === 'offline'}
                <div class="sub c-down">{t('servers.list.offlineSince', { ago: fmtRelative(s.metrics_at, clock.now) })}</div>
              {:else}
                <div class="sub" title={hl.full !== hl.short ? hl.full : undefined}>
                  {hl.short || '—'}{#if s.ip}<span class="ip" title={t('overview.servers.ipTitle')}><span class="sep" aria-hidden="true">·</span>{s.ip}</span>{/if}
                </div>
              {/if}
              {#if s.version}
                {@const vt = verTitle(s)}
                {@const tag = verTag(s)}
                <div class="sv" title={vt}>
                  <span class="ver" class:old={!!vt} class:bad={s.update?.state === 'failed'}>{s.version}</span>{#if tag}<span class="vtag" class:old={!!vt} class:bad={s.update?.state === 'failed'}>{tag}</span>{/if}
                </div>
              {/if}
            </div>
          </div>
          {#if st}
            {@const hist = s.cpu_hist ?? []}
            {@const sc = cpuScale(hist)}
            {@const disks = sortedDisks(s)}
            {@const sw = st.swap_total > 0 ? swapPct(st) : null}
            <div class="m cpu a-cpu"><span class="ml" aria-hidden="true">{metricLabel('cpu')}</span><UsageBar value={st.cpu} label="{s.name}: {metricLabel('cpu')}" inline /></div>
            <div class="trend a-trend" title={hist.length > 1 ? t('overview.servers.trendTitle', { max: fmtPctInt(sc.peak) }) : t('overview.servers.trendNone')}>
              <Sparkline values={hist} min={0} max={sc.max} w={104} h={26} tone={s.state === 'offline' ? 'muted' : st.cpu >= 90 ? 'down' : st.cpu >= 80 ? 'warn' : 'accent'} />
            </div>
            <div class="m mem a-mem"><span class="ml" aria-hidden="true">{metricLabel('mem')}</span><UsageBar value={memPct(st)} label="{s.name}: {metricLabel('mem')}" inline /></div>
            <div class="val a-swap {usageLevel(sw)}" title={sw === null ? t('overview.servers.swapNone') : metricLabel('swap')}>{sw === null ? '—' : fmtPctInt(sw)}</div>
            {@const fm = fullestMount(st)}
            <div class="m disk a-disk" class:multi={disks.length > 1} title={diskSummary(st) || undefined}>
              <span class="ml" aria-hidden="true">{metricLabel('disk')}</span>
              <div class="d1">
                <UsageBar value={diskPct(st)} label="{s.name}: {metricLabel('disk')}{fm ? ` ${fm}` : ''}" inline />
                <!-- Tek bölümlü sunucuda telefonda yalnızca "/" yazan satır gösterilmez. -->
                {#if fm}<span class="dm" class:root={fm === '/'} aria-hidden="true">{fm}</span>{/if}
              </div>
              {#if disks.length > 1}
                <div class="dlist">
                  {#each disks.slice(0, disks.length > MAX_DISKS ? MAX_DISKS - 1 : MAX_DISKS) as d (d.mount)}
                    {@const v = dpct(d)}
                    <span class="dmnt">{d.mount}</span><span class="dtrack {usageLevel(v)}"><i style="width:{Math.min(100, v)}%"></i></span><span class="dpct {usageLevel(v)}">{fmtPctInt(v)}</span>
                  {/each}
                  {#if disks.length > MAX_DISKS}
                    <span
                      class="dmore"
                      title={t('overview.servers.disksMoreTitle', {
                        list: disks
                          .slice(MAX_DISKS - 1)
                          .map((d) => `${d.mount} ${fmtPctInt(dpct(d))}`)
                          .join(' · '),
                      })}>{t('overview.servers.disksMore', { n: disks.length - MAX_DISKS + 1 })}</span
                    >
                  {/if}
                </div>
              {/if}
            </div>
            <div class="val a-temp {tempLevel(s.temp_max)}" title={s.temp_max === null ? t('overview.servers.noTemp') : t('overview.servers.tempTitle')}>
              {s.temp_max === null ? '—' : fmtTemp(s.temp_max)}
            </div>
            <div
              class="val a-dock"
              title={s.host?.docker || s.container_count ? t('overview.servers.containersTitle', { n: s.container_count, count: s.container_count }) : t('overview.servers.noDocker')}
            >
              {#if s.host?.docker || s.container_count}<Icon name="box" size={13} />{s.container_count}{:else}—{/if}
            </div>
            <div class="net a-net">
              <span title={t('servers.list.netIn')}><Icon name="arrow-down" size={12} />{fmtRate(st.net_rx_bps)}</span>
              <span title={t('servers.list.netOut')}><Icon name="arrow-up" size={12} />{fmtRate(st.net_tx_bps)}</span>
            </div>
            <div class="load a-load" title={t('servers.list.loadTitle', { a: fmtDec(st.load1, 2), b: fmtDec(st.load5, 2), c: fmtDec(st.load15, 2) })}>
              {fmtDec(st.load1, 2)}
            </div>
            <div class="up-t a-up">{fmtUptime(st.uptime)}</div>
            <div class="agent a-agent">
              {#if s.version}
                {@const vt = verTitle(s)}
                <span class="ver" class:old={!!vt} class:bad={s.update?.state === 'failed'} title={vt}>{#if vt}<Icon name="arrow-up" size={11} />{/if}{s.version}</span>
                {#if verTag(s)}<span class="vtag" class:old={!!vt} class:bad={s.update?.state === 'failed'}>{verTag(s)}</span>{/if}
              {/if}
              <span class="ago" class:c-down={s.state === 'offline'}>{fmtRelative(s.metrics_at, clock.now)}</span>
            </div>
            <!-- Mobil kartın alt satırı: ağ, yük ve çalışma süresi tek satırda. -->
            <div class="foot">
              <span><Icon name="arrow-down" size={12} />{fmtRate(st.net_rx_bps)}</span>
              <span><Icon name="arrow-up" size={12} />{fmtRate(st.net_tx_bps)}</span>
              <span>{metricLabel('load')} <b>{fmtDec(st.load1, 2)}</b></span>
              <span><Icon name="clock" size={12} />{fmtUptime(st.uptime)}</span>
            </div>
          {:else}
            <div class="nodata">{t('servers.list.noData')}</div>
          {/if}
        </a>
      {/each}
    </div>
  {/if}

  {#if inactive.length}
    <h2 class="sect">{t('servers.list.inactiveTitle')}</h2>
    <div class="card list dimlist">
      {#each inactive as s (s.id)}
        <div class="irow">
          <span class="sdot muted" aria-hidden="true"></span>
          <div class="it">
            <div class="n1">
              <a class="name" href="#/servers/{s.id}">{s.name}</a>
              <span class="badge paused">{STATE_LABELS[s.state]}</span>
            </div>
            <div class="sub">{inactiveReason(s)}</div>
          </div>
          {#if session.isAdmin}
            {#if s.state === 'disabled'}
              <a class="btn sm" href="#/servers/{s.id}">{t('servers.list.openSettings')}</a>
            {:else}
              <button type="button" class="btn sm" onclick={() => openRenew(s)}><Icon name="terminal" size={14} /> {t('servers.list.showCommand')}</button>
            {/if}
          {/if}
        </div>
      {/each}
    </div>
  {/if}
{/if}

{#if session.isAdmin}
  <ServerSetupModal bind:open={addOpen} />
  <ServerSetupModal bind:open={renewOpen} renew={renewFor} />
{/if}

<style>
  .ph {
    display: flex;
    align-items: baseline;
    gap: 6px 18px;
    flex-wrap: wrap;
    min-width: 0;
  }
  .sum {
    display: flex;
    gap: 14px;
    flex-wrap: wrap;
    font-size: 0.88rem;
    color: var(--text-2);
  }
  .sum span {
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }
  .sd {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--paused);
  }
  .sd.up {
    background: var(--up);
  }
  .sd.warn {
    background: var(--pending);
  }
  .sd.down {
    background: var(--down);
  }

  .list {
    padding: 0;
    overflow: hidden;
    /* Sütunlar listenin genişliğine göre açılır (aşağıdaki @container slist). */
    container: slist / inline-size;
  }
  .sk-row {
    display: flex;
    gap: 16px;
    align-items: center;
    padding: 18px;
    border-bottom: 1px solid var(--border);
  }

  /* Masaüstü: sütunlu satırlar. Temel düzen dar liste içindir; liste
     genişledikçe CPU grafiği, sıcaklık, Docker, bölümler, swap, çalışma süresi
     ve ajan sütunları eklenir. Izgaraya girmeyen hücre gizlidir. */
  .lhead,
  .srow {
    display: grid;
    grid-template-columns: minmax(180px, 1.6fr) repeat(3, minmax(80px, 1fr)) minmax(112px, 0.9fr) 50px;
    grid-template-areas: 'nm cpu mem disk net load';
    align-items: center;
    column-gap: 16px;
    padding: 12px 20px;
  }
  .a-nm {
    grid-area: nm;
  }
  .a-cpu {
    grid-area: cpu;
  }
  .a-trend {
    grid-area: trend;
  }
  .a-mem {
    grid-area: mem;
  }
  .a-swap {
    grid-area: swap;
  }
  .a-disk {
    grid-area: disk;
  }
  .a-temp {
    grid-area: temp;
  }
  .a-dock {
    grid-area: dock;
  }
  .a-net {
    grid-area: net;
  }
  .a-load {
    grid-area: load;
  }
  .a-up {
    grid-area: up;
  }
  .a-agent {
    grid-area: agent;
  }
  .a-trend,
  .a-swap,
  .a-temp,
  .a-dock,
  .a-up,
  .a-agent,
  .ip,
  .dlist {
    display: none;
  }
  @container slist (min-width: 1000px) {
    .lhead,
    .srow {
      grid-template-columns: minmax(190px, 1.5fr) minmax(84px, 1fr) 104px minmax(84px, 1fr) minmax(100px, 1fr) 118px 52px;
      grid-template-areas: 'nm cpu trend mem disk net load';
      column-gap: 18px;
    }
    .a-trend {
      display: block;
    }
  }
  @container slist (min-width: 1100px) {
    .lhead,
    .srow {
      grid-template-columns: minmax(200px, 1.5fr) minmax(80px, 0.9fr) 104px minmax(80px, 0.9fr) minmax(150px, 1.5fr) 58px 52px 118px 52px;
      grid-template-areas: 'nm cpu trend mem disk temp dock net load';
    }
    .a-temp,
    .a-dock {
      display: flex;
    }
    /* Birden çok bölüm: her biri ayrı küçük çubuk (en çok 3). */
    .disk.multi .d1 {
      display: none;
    }
    .disk.multi .dlist {
      display: grid;
    }
  }
  @container slist (min-width: 1300px) {
    .lhead,
    .srow {
      grid-template-columns:
        minmax(210px, 1.5fr) minmax(84px, 1fr) 104px minmax(84px, 1fr) 50px minmax(160px, 1.4fr) 58px 52px 118px 52px
        96px;
      grid-template-areas: 'nm cpu trend mem swap disk temp dock net load up';
    }
    .a-swap {
      display: block;
    }
    .a-up {
      display: block;
    }
  }
  @container slist (min-width: 1500px) {
    .lhead,
    .srow {
      grid-template-columns:
        minmax(300px, 2.2fr) minmax(84px, 1fr) 110px minmax(84px, 1fr) 50px minmax(170px, 1.4fr) 64px 52px 118px 52px
        96px 100px;
      grid-template-areas: 'nm cpu trend mem swap disk temp dock net load up agent';
      column-gap: 20px;
    }
    .a-agent {
      display: flex;
    }
    .sv {
      display: none;
    }
    .ip {
      display: inline;
    }
  }
  .lhead .r {
    text-align: right;
  }
  .lhead > span {
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .lhead {
    padding-top: 10px;
    padding-bottom: 10px;
    border-bottom: 1px solid var(--border);
    font-size: 0.72rem;
    font-weight: 700;
    letter-spacing: 0.05em;
    text-transform: uppercase;
    color: var(--muted);
  }
  .srow {
    border-bottom: 1px solid var(--border);
    color: var(--text);
    text-decoration: none;
    transition: background 0.12s;
    min-height: 64px;
  }
  .srow:last-child {
    border-bottom: none;
  }
  .srow:focus-visible {
    outline-offset: -2px;
  }
  @media (hover: hover) {
    .srow:hover {
      background: var(--card-hover);
      text-decoration: none;
    }
    .srow:hover .name {
      color: var(--accent-text);
    }
  }
  .nm {
    display: flex;
    align-items: center;
    gap: 12px;
    min-width: 0;
  }
  .sdot {
    width: 10px;
    height: 10px;
    border-radius: 50%;
    flex-shrink: 0;
    background: var(--paused);
  }
  .sdot.up {
    background: var(--up);
    box-shadow: 0 0 0 3px var(--up-ring);
  }
  .sdot.warn {
    background: var(--pending);
    box-shadow: 0 0 0 3px var(--pending-soft);
  }
  .sdot.down {
    background: var(--down);
    box-shadow: 0 0 0 3px var(--down-soft);
  }
  .nmt,
  .it {
    min-width: 0;
    flex: 1;
  }
  .n1 {
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
    flex-wrap: wrap;
  }
  .name {
    font-weight: 700;
    font-size: 0.97rem;
    color: var(--text);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    min-width: 0;
    max-width: 100%;
  }
  .fire {
    gap: 3px;
    letter-spacing: 0.02em;
  }
  .sub {
    font-size: 0.8rem;
    color: var(--muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    margin-top: 1px;
  }
  .m {
    min-width: 0;
  }
  .disk {
    min-width: 0;
    position: relative;
  }
  .d1 {
    position: relative;
  }
  /* Bölüm listesi: "/data ▬▬▬ %82" satırları */
  .dlist {
    grid-template-columns: minmax(0, 0.9fr) minmax(40px, 1fr) 32px;
    column-gap: 6px;
    row-gap: 2px;
    align-items: center;
    font-size: 0.72rem;
    line-height: 1.3;
  }
  .dmnt {
    font-family: var(--mono);
    color: var(--muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    max-width: 110px;
  }
  .dtrack {
    height: 5px;
    border-radius: 3px;
    background: var(--meter-track);
    overflow: hidden;
  }
  .dtrack i {
    display: block;
    height: 100%;
    border-radius: 3px;
    background: var(--accent);
  }
  .dtrack.warn i {
    background: var(--pending);
  }
  .dtrack.danger i {
    background: var(--down);
  }
  .dpct {
    text-align: right;
    font-weight: 700;
    color: var(--text);
    font-variant-numeric: tabular-nums;
  }
  .dpct.warn {
    color: var(--pending);
  }
  .dpct.danger {
    color: var(--down-text-2);
  }
  .dmore {
    grid-column: 1 / -1;
    color: var(--muted);
    font-weight: 600;
  }
  .trend {
    min-width: 0;
  }
  /* Tek değerli sütunlar (swap, sıcaklık, Docker) */
  .val {
    justify-content: flex-end;
    align-items: center;
    gap: 4px;
    text-align: right;
    font-size: 0.86rem;
    font-weight: 600;
    color: var(--text-2);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }
  .val :global(svg) {
    color: var(--muted);
  }
  .val.warn {
    color: var(--pending);
  }
  .val.danger {
    color: var(--down-text-2);
  }
  .agent {
    flex-direction: column;
    min-width: 0;
    line-height: 1.35;
  }
  .ver {
    font-family: var(--mono);
    font-size: 0.76rem;
    color: var(--text-2);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .sv {
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
    margin-top: 1px;
  }
  .sv .ver {
    flex: 0 1 auto;
    min-width: 0;
  }
  .vtag {
    flex: 0 1 auto;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 0.7rem;
    line-height: 1.5;
    padding: 0 6px;
    border-radius: 999px;
    background: var(--surface-2, rgba(128, 128, 128, 0.14));
    color: var(--text-2);
  }
  .agent .vtag {
    align-self: flex-start;
  }
  .vtag.old {
    background: var(--pending-soft);
    color: var(--pending);
  }
  .vtag.bad {
    color: var(--down-text);
  }
  .ver.old {
    color: var(--pending);
  }
  .ver.bad {
    color: var(--down-text);
  }
  .head-actions {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
  }
  .ago {
    font-size: 0.76rem;
    color: var(--muted);
    white-space: nowrap;
  }
  .sep {
    margin: 0 6px;
  }
  /* Gösterilen bölüm ("/", "C:", "/var/lib/pgsql") her satırda çubuğun altında:
     masaüstünde çubuklar aynı hizada kalsın diye akış dışında. */
  .dm {
    position: absolute;
    left: 0;
    right: 0;
    top: 100%;
    display: block;
    margin-top: 1px;
    font-family: var(--mono);
    font-size: 0.7rem;
    color: var(--muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .ml {
    display: none;
  }
  .net {
    display: flex;
    flex-direction: column;
    font-size: 0.8rem;
    color: var(--text-2);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
    line-height: 1.45;
  }
  .net span {
    display: inline-flex;
    align-items: center;
    gap: 4px;
  }
  .net :global(svg) {
    color: var(--muted);
  }
  .load {
    font-size: 0.88rem;
    font-weight: 600;
    font-variant-numeric: tabular-nums;
  }
  .up-t {
    font-size: 0.84rem;
    color: var(--text-2);
    white-space: nowrap;
  }
  .foot {
    display: none;
  }
  .nodata {
    grid-column: 2 / -1;
    color: var(--muted);
    font-size: 0.85rem;
  }
  /* Çevrimdışı: son değerler soluk (eski veri). */
  .srow.down .m,
  .srow.down .net,
  .srow.down .load,
  .srow.down .up-t,
  .srow.down .trend,
  .srow.down .val {
    opacity: 0.45;
  }

  .sect {
    font-size: 0.8rem;
    font-weight: 700;
    letter-spacing: 0.05em;
    text-transform: uppercase;
    color: var(--muted);
    margin: 26px 0 10px 4px;
  }
  .dimlist {
    background: transparent;
    border-style: dashed;
  }
  .irow {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 12px 16px 12px 20px;
    border-bottom: 1px dashed var(--border);
  }
  .irow:last-child {
    border-bottom: none;
  }
  .irow .name {
    color: var(--text-2);
  }
  .irow .sub {
    white-space: normal;
  }

  .first {
    padding: 56px 20px;
  }
  .hero-ic {
    width: 60px;
    height: 60px;
    margin: 0 auto;
    border-radius: 16px;
    display: flex;
    align-items: center;
    justify-content: center;
    background: var(--accent-soft);
    color: var(--accent-text);
  }
  .first p {
    max-width: 520px;
  }
  .hint {
    margin: 14px auto 0 !important;
  }

  /* Mobil: kart görünümü */
  @media (max-width: 720px) {
    .lhead {
      display: none;
    }
    .srow {
      grid-template-columns: repeat(3, minmax(0, 1fr));
      grid-template-areas:
        'nm nm nm'
        'cpu mem disk'
        'foot foot foot';
      row-gap: 10px;
      column-gap: 14px;
      padding: 14px 16px;
    }
    .ml {
      display: block;
      font-size: 0.72rem;
      font-weight: 600;
      color: var(--muted);
      margin-bottom: 2px;
    }
    .m :global(.inline .v) {
      min-width: 30px;
    }
    /* Mobil: CPU / RAM / Disk başlıkları ve çubukları aynı hizada; bölüm adı altta. */
    .m {
      align-self: start;
    }
    .dm {
      position: static;
      margin-top: 2px;
    }
    .dm.root {
      display: none;
    }
    .net,
    .load,
    .up-t {
      display: none;
    }
    .foot {
      grid-area: foot;
      display: flex;
      flex-wrap: wrap;
      gap: 2px 14px;
      font-size: 0.79rem;
      color: var(--text-2);
      font-variant-numeric: tabular-nums;
    }
    .foot span {
      display: inline-flex;
      align-items: center;
      gap: 4px;
      white-space: nowrap;
    }
    .foot :global(svg) {
      color: var(--muted);
    }
    .foot b {
      font-weight: 600;
      color: var(--text);
    }
    .srow.down .foot {
      opacity: 0.45;
    }
    .nodata {
      grid-column: 1 / -1;
    }
    .irow {
      flex-wrap: wrap;
      padding: 12px 16px;
    }
    .irow {
      align-items: flex-start;
    }
    .irow .sdot {
      margin-top: 6px;
    }
    .irow .it {
      flex: 1 1 calc(100% - 30px);
    }
    .irow .btn {
      margin-left: 22px;
    }
  }
</style>
