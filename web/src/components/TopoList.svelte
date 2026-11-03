<script lang="ts">
  // Canlı topoloji, dar ekran: tuval yerine dikey gruplu liste (yatay kaydırma yok, satırlar >= 48 px).
  // Sıra tuvaldeki gibi: panel, monitörler, kanallar, kontrol noktaları, sunucular; sorunlular önce.
  import { t } from '../lib/i18n';
  import type { Topology } from '../lib/api';
  import type { GroupKey } from '../lib/topolayout';
  import {
    channelClass,
    channelSub,
    counts,
    groupLevel,
    monClass,
    monitorSub,
    probeClass,
    probeSub,
    serverClass,
    serverSub,
    sortChannels,
    sortMonitors,
    sortProbes,
    sortServers,
    type Cls,
  } from '../lib/topoview';
  import Icon, { type IconName } from './Icon.svelte';

  let { d, now, canEdit, rate }: { d: Topology; now: number; canEdit: boolean; rate: string | null } = $props();

  interface Row {
    id: number;
    name: string;
    sub: string;
    cls: Cls;
    href: string | null;
  }
  const LIMIT = 8;
  let open = $state<Record<GroupKey, boolean>>({ monitors: false, channels: false, probes: false, servers: false });

  const groups = $derived.by(() => {
    const g: { key: GroupKey; rows: Row[]; link: string | null; icon: IconName }[] = [
      {
        key: 'monitors',
        icon: 'activity',
        link: canEdit ? '#/monitors/new' : null,
        rows: sortMonitors(d.monitors).map((m) => ({ id: m.id, name: m.name, sub: monitorSub(m), cls: monClass(m), href: `#/monitors/${m.id}` })),
      },
    ];
    if (d.channels)
      g.push({
        key: 'channels',
        icon: 'bell',
        link: '#/notifications',
        rows: sortChannels(d.channels).map((c) => ({ id: c.id, name: c.name, sub: channelSub(c, now), cls: channelClass(c), href: '#/notifications' })),
      });
    g.push(
      {
        key: 'probes',
        icon: 'network',
        link: d.admin ? '#/settings/probes' : null,
        rows: sortProbes(d.probes).map((p) => ({
          id: p.id,
          name: p.name,
          sub: probeSub(p, d.admin),
          cls: probeClass(p),
          href: d.admin ? '#/settings/probes' : null,
        })),
      },
      {
        key: 'servers',
        icon: 'server',
        link: canEdit ? '#/servers' : null,
        rows: sortServers(d.servers).map((s) => ({ id: s.id, name: s.name, sub: serverSub(s), cls: serverClass(s), href: `#/servers/${s.id}` })),
      },
    );
    return g;
  });
</script>

<div class="tl" data-testid="topology-list">
  <div class="hub row" class:bad={d.stats.open_incidents > 0}>
    <Icon name="server" size={18} />
    <span class="txt">
      <span class="nm">{t('topology.col.panel')}</span>
      <span class="sub">
        {[t('topology.panel.online', { n: d.stats.probes_online, total: d.stats.probes_total }), rate !== null ? t('topology.panel.rate', { n: rate }) : '']
          .filter(Boolean)
          .join(' · ')}
      </span>
      {#if d.stats.open_incidents}
        <a class="sub inc" href="#/incidents" data-testid="topology-list-incidents">{t('topology.decision.count', { count: d.stats.open_incidents })}</a>
      {:else}
        <span class="sub">{t('topology.panel.noIncident')}</span>
      {/if}
    </span>
  </div>
  {#each groups as g (g.key)}
    {@const c = counts(g.rows.map((r) => r.cls))}
    {@const shown = open[g.key] ? g.rows : g.rows.slice(0, LIMIT)}
    <section class={`grp lv-${groupLevel(c)}`} data-testid={`lgrp-${g.key}`}>
      <header>
        <h3>{t(`topology.col.${g.key}`)}</h3>
        {#if c.total}
          <span class="cnt" aria-label={t('topology.countsAria', { ok: c.ok, warn: c.warn, bad: c.bad })}>
            {#if c.bad}<span class="n bad">{c.bad}</span>{/if}
            {#if c.warn}<span class="n warn">{c.warn}</span>{/if}
            <span class="n ok">{c.ok}</span>
            <span class="tot">/ {c.total}</span>
          </span>
        {/if}
      </header>
      {#each shown as r (r.id)}
        <svelte:element this={r.href ? 'a' : 'div'} href={r.href} class={`row c-${r.cls}`}>
          <i class={`sd ${r.cls}`}></i>
          <span class="txt"><span class="nm">{r.name}</span><span class="sub">{r.sub}</span></span>
          {#if r.href}<Icon name="chevron-right" size={16} />{/if}
        </svelte:element>
      {:else}
        <div class="gempty">
          <Icon name={g.icon} size={20} />
          <p>{t(`topology.groupEmpty.${g.key}`)}</p>
          {#if g.link}<a class="lnk" href={g.link}>{t(`topology.groupEmptyLink.${g.key}`)}</a>{/if}
        </div>
      {/each}
      {#if g.rows.length > LIMIT}
        <button type="button" class="moreb" aria-expanded={open[g.key]} onclick={() => (open[g.key] = !open[g.key])}>
          {open[g.key] ? t('topology.less') : t('topology.more', { count: g.rows.length - LIMIT })}
        </button>
      {/if}
    </section>
  {/each}
</div>

<style>
  .tl {
    display: flex;
    flex-direction: column;
    gap: 12px;
    min-width: 0;
  }
  .grp {
    border: 1px solid var(--border);
    border-top-width: 3px;
    border-radius: var(--radius-sm);
    background: var(--card-2);
    overflow: hidden;
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
  header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    padding: 10px 12px;
  }
  h3 {
    margin: 0;
    font-size: 0.8rem;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--text-2);
  }
  .cnt {
    display: flex;
    gap: 6px;
    align-items: center;
    font-size: 0.78rem;
    font-variant-numeric: tabular-nums;
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
  .row {
    display: flex;
    align-items: center;
    gap: 10px;
    min-height: 52px;
    padding: 6px 12px;
    border-top: 1px solid var(--border);
    color: var(--text);
    text-decoration: none;
    min-width: 0;
  }
  a.row:active,
  a.row:hover {
    background: var(--card-hover);
  }
  .hub {
    border: 1px solid var(--border);
    border-top: 3px solid var(--up);
    border-radius: var(--radius-sm);
    background: var(--card);
  }
  .sub.inc {
    color: var(--down-text);
    font-weight: 600;
  }
  .hub.bad {
    border-top-color: var(--down);
  }
  .txt {
    display: flex;
    flex-direction: column;
    gap: 2px;
    flex: 1;
    min-width: 0;
  }
  .nm {
    font-weight: 600;
    font-size: 0.92rem;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .sub {
    font-size: 0.78rem;
    color: var(--muted);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .row :global(svg) {
    flex: none;
    color: var(--muted);
  }
  .sd {
    width: 10px;
    height: 10px;
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
  .gempty {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 6px;
    padding: 14px 12px 16px;
    border-top: 1px solid var(--border);
    color: var(--muted);
    text-align: center;
  }
  .gempty p {
    margin: 0;
    font-size: 0.85rem;
  }
  .lnk {
    display: inline-flex;
    align-items: center;
    min-height: 44px;
    padding: 0 12px;
  }
  .moreb {
    width: 100%;
    min-height: 48px;
    border: 0;
    border-top: 1px solid var(--border);
    background: transparent;
    color: var(--text-2);
    font: inherit;
    cursor: pointer;
  }
</style>
