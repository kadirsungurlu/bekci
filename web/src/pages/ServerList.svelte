<script lang="ts">
  import { onMount } from 'svelte';
  import type { ServerView } from '../lib/api';
  import { session } from '../lib/session.svelte';
  import { clock } from '../lib/ui.svelte';
  import { fmtDec, fmtRate, fmtRelative, fmtUptime } from '../lib/format';
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
  } from '../lib/servers.svelte';
  import UsageBar from '../components/UsageBar.svelte';
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
    <button class="btn primary" onclick={openAdd}><Icon name="plus" size={16} /> {t('servers.list.add')}</button>
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
        <span>{t('servers.list.cols.server')}</span>
        <span>{metricLabel('cpu')}</span>
        <span>{metricLabel('mem')}</span>
        <span>{metricLabel('disk')}</span>
        <span>{metricLabel('net')}</span>
        <span>{metricLabel('load')}</span>
        <span>{t('servers.list.cols.uptime')}</span>
      </div>
      {#each active as s (s.id)}
        {@const tone = serverTone(s)}
        {@const st = s.latest}
        {@const fl = firingLabels(s)}
        {@const hl = hostLine(s)}
        <a class="srow {tone}" href="#/servers/{s.id}">
          <div class="nm">
            <span class="sdot {tone}" title={STATE_LABELS[s.state]} aria-hidden="true"></span>
            <div class="nmt">
              <div class="n1">
                <span class="name">{s.name}</span>
                {#each fl as l (l)}<span class="badge pending fire" title={t('servers.list.firingTitle', { metric: l })}><Icon name="alert" size={11} /> {l}</span>{/each}
              </div>
              {#if s.state === 'offline'}
                <div class="sub c-down">{t('servers.list.offlineSince', { ago: fmtRelative(s.metrics_at, clock.now) })}</div>
              {:else}
                <div class="sub" title={hl.full !== hl.short ? hl.full : undefined}>{hl.short || '—'}</div>
              {/if}
            </div>
          </div>
          {#if st}
            <div class="m cpu"><span class="ml" aria-hidden="true">{metricLabel('cpu')}</span><UsageBar value={st.cpu} label="{s.name}: {metricLabel('cpu')}" inline /></div>
            <div class="m mem"><span class="ml" aria-hidden="true">{metricLabel('mem')}</span><UsageBar value={memPct(st)} label="{s.name}: {metricLabel('mem')}" inline /></div>
            {@const fm = fullestMount(st)}
            <div class="m disk" title={diskSummary(st) || undefined}>
              <span class="ml" aria-hidden="true">{metricLabel('disk')}</span>
              <UsageBar value={diskPct(st)} label="{s.name}: {metricLabel('disk')}{fm ? ` ${fm}` : ''}" inline />
              {#if fm}<span class="dm" aria-hidden="true">{fm}</span>{/if}
            </div>
            <div class="net">
              <span title={t('servers.list.netIn')}><Icon name="arrow-down" size={12} />{fmtRate(st.net_rx_bps)}</span>
              <span title={t('servers.list.netOut')}><Icon name="arrow-up" size={12} />{fmtRate(st.net_tx_bps)}</span>
            </div>
            <div class="load" title={t('servers.list.loadTitle', { a: fmtDec(st.load1, 2), b: fmtDec(st.load5, 2), c: fmtDec(st.load15, 2) })}>
              {fmtDec(st.load1, 2)}
            </div>
            <div class="up-t">{fmtUptime(st.uptime)}</div>
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
  }
  .sk-row {
    display: flex;
    gap: 16px;
    align-items: center;
    padding: 18px;
    border-bottom: 1px solid var(--border);
  }

  /* Masaüstü: sütunlu satırlar */
  .lhead,
  .srow {
    display: grid;
    grid-template-columns: minmax(200px, 1.7fr) repeat(3, minmax(92px, 1fr)) minmax(118px, 0.9fr) 56px 96px;
    grid-template-areas: 'nm cpu mem disk net load up';
    align-items: center;
    column-gap: 20px;
    padding: 12px 20px;
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
    grid-area: nm;
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
  .cpu {
    grid-area: cpu;
  }
  .mem {
    grid-area: mem;
  }
  .disk {
    grid-area: disk;
    min-width: 0;
    position: relative;
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
    grid-area: net;
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
    grid-area: load;
    font-size: 0.88rem;
    font-weight: 600;
    font-variant-numeric: tabular-nums;
  }
  .up-t {
    grid-area: up;
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
  .srow.down .up-t {
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

  /* Dar masaüstü: ağ ve çalışma süresi sütunları sığmıyorsa gizlenir. */
  @media (max-width: 1240px) and (min-width: 721px) {
    .lhead,
    .srow {
      grid-template-columns: minmax(180px, 1.6fr) repeat(3, minmax(80px, 1fr)) minmax(112px, 0.9fr) 50px;
      grid-template-areas: 'nm cpu mem disk net load';
      column-gap: 16px;
    }
    .lhead span:last-child,
    .up-t {
      display: none;
    }
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
