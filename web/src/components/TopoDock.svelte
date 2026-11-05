<script lang="ts">
  // Seçili öğenin ayrıntı şeridi; dar ekranda alt sayfa. Seçim yokken ipucu ve çizgi anahtarı.
  import Icon from './Icon.svelte';
  import { fmtDurationShort, fmtRelative } from '../lib/format';
  import { t } from '../lib/i18n';
  import { typeLabel } from '../lib/monitorTypes';
  import { NOTIFY_LABELS, NOTIFY_STYLE } from '../lib/notifyTypes';
  import type { NotificationType } from '../lib/api';
  import { cellClass, MARK, RANK, sparkPoints, type ChaNode, type Graph, type Kind, type MonNode, type Rel, type TopoNode } from '../lib/topomodel';
  import { ek, offCount, pct, serverSymptom, sinceText, stateText } from '../lib/topotext';

  let { n, g, now, admin }: { n: TopoNode | null; g: Graph; now: number; admin: boolean } = $props();

  const rels = $derived(n ? (g.rel.get(n.id) ?? []) : []);
  const ofKind = (k: Kind) => rels.filter((r) => g.nodes.get(r.id)?.kind === k).sort((a, b) => RANK[a.c] - RANK[b.c]);
  const openHref = $derived.by(() => {
    if (!n) return '';
    if (n.kind === 'mon') return `#/monitors/${n.m.id}`;
    if (n.kind === 'srv') return `#/servers/${n.s.id}`;
    if (n.kind === 'cha') return '#/notifications';
    return admin && !n.local ? '#/settings/probes' : '';
  });
  const sub = $derived.by(() => {
    if (!n) return '';
    const a: (string | undefined)[] =
      n.kind === 'mon'
        ? [typeLabel(n.m.type), n.tag, n.m.server_id ? g.nodes.get(`s${n.m.server_id}`)?.name : t('topology.dock.noServer')]
        : n.kind === 'srv'
          ? [n.s.role, n.s.region, n.customer]
          : n.kind === 'pro'
            ? [n.p.region, n.p.provider, n.code]
            : [NOTIFY_LABELS[n.c.type as NotificationType] ?? n.c.type];
    return a.filter(Boolean).join(', ');
  });
  const durExtra = $derived.by(() => {
    if (n?.kind === 'mon' && (n.st === 'bad' || n.st === 'warn')) return [n.m.message, sinceText(n.m.since, now)].filter(Boolean).join(', ');
    if (n?.kind === 'srv' && (n.st === 'bad' || n.st === 'warn')) return serverSymptom(n);
    return '';
  });
  const spark = (series: number[] | undefined) => {
    const v = sparkPoints(series);
    if (v.length < 2) return '';
    const hi = Math.max(...v);
    const lo = Math.min(...v);
    const span = hi - lo || 1;
    return v.map((x, i) => `${((i / (v.length - 1)) * 132).toFixed(1)},${(20 - ((x - lo) / span) * 18).toFixed(1)}`).join(' ');
  };
  const lastEvent = (m: MonNode) => {
    const i = m.m.last_incident;
    if (!i) return t('topology.dock.noIncident');
    if (i.resolved_at) return t('topology.dock.lastDown', { ago: fmtRelative(i.resolved_at, now), dur: fmtDurationShort(i.resolved_at - i.started_at) });
    return `${t('topology.dock.startedAgo', { ago: fmtRelative(i.started_at, now), cause: i.cause })}, ${t(i.acked ? 'topology.dock.acked' : 'topology.dock.notAcked')}`;
  };
  const chipClass = (r: Rel) => `cip ${r.c === 'bad' ? 'bad' : r.c === 'warn' ? 'w' : ''}`;
  const short = (id: string) => {
    const x = g.nodes.get(id);
    return x?.kind === 'pro' ? x.code : (x?.name ?? '');
  };
</script>

{#snippet chan(c: ChaNode)}
  <button type="button" class="cip" class:w={c.st === 'warn'} data-id={c.id}>
    <span style:color={NOTIFY_STYLE[c.c.type as NotificationType]?.color}><Icon name={NOTIFY_STYLE[c.c.type as NotificationType]?.icon ?? 'send'} size={12} /></span>{c.name}
  </button>
{/snippet}
{#snippet chans()}
  {@const l = ofKind('cha')}
  <div class="d-c ek">
    <span class="d-e">{t('topology.dock.channels')}</span>
    <div class="d-v">
      {#if l.length}
        <div class="cipler">{#each l.slice(0, 6) as r (r.id)}{@render chan(g.nodes.get(r.id) as ChaNode)}{/each}</div>
      {:else}{t('topology.dock.noChannels')}{/if}
    </div>
  </div>
{/snippet}
{#snippet part(label: string, kind: Kind, limit: number, tab: string)}
  {@const l = ofKind(kind)}
  {#if l.length}
    <div class="satir">
      <span class="d-e">{label}</span>
      <div class="cipler">
        {#each l.slice(0, limit) as r (r.id)}
          <button type="button" class={chipClass(r)} data-id={r.id}>{short(r.id)}</button>
        {/each}
        {#if l.length > limit}<button type="button" class="cip" data-tab={tab}>+{l.length - limit}</button>{/if}
      </div>
    </div>
  {/if}
{/snippet}
{#snippet gauge(name: string, v: number | undefined)}
  {#if v !== undefined}
    <div class="olc" class:yuk={v >= 90}><span>{name}</span><i><b style:width="{Math.min(100, v)}%"></b></i><span>{pct(v)}</span></div>
  {/if}
{/snippet}

<section class="dock" class:acik={!!n} aria-label={t('topology.dock.label')} data-testid="topology-dock">
  {#if n}
    <div class="d-c">
      <div class="d-ad">
        {#if n.kind === 'cha'}
          <span style:color={NOTIFY_STYLE[n.c.type as NotificationType]?.color}><Icon name={NOTIFY_STYLE[n.c.type as NotificationType]?.icon ?? 'send'} size={15} /></span>
        {:else}{@html MARK[n.cls]}{/if}
        <span>{n.name}</span>
        {#if openHref}<a class="d-ac" href={openHref} data-testid="topology-open">{t('topology.dock.open')}<Icon name="chevron-right" size={13} /></a>{/if}
      </div>
      <div class="d-dur {n.st}">{stateText(n)}{durExtra ? `, ${durExtra}` : ''}</div>
      <div class="d-sub">{sub}</div>
    </div>
    {#if n.kind === 'mon'}
      {@const points = spark(n.m.series)}
      <div class="d-c ek">
        <span class="d-e">{t('topology.dock.latency')}</span>
        <div class="d-v">
          {#if n.st === 'bad'}<div class="d-ms">{t('topology.dock.noResponse')}</div>
          {:else if n.m.ping_ms > 0}<div class="d-ms">{Math.round(n.m.ping_ms)}<small>ms</small></div>{/if}
          {#if points}
            <svg width="100%" height="22" viewBox="0 0 132 22" preserveAspectRatio="none" aria-hidden="true"><polyline {points} fill="none" stroke="var(--spark)" stroke-width="1.4" vector-effect="non-scaling-stroke" /></svg>
          {/if}
        </div>
      </div>
      <div class="d-c"><span class="d-e">{t('topology.dock.lastEvent')}</span><div class="d-v">{lastEvent(n)}</div></div>
      <div class="d-c">
        <span class="d-e">{t('topology.dock.locations')}</span>
        <div class="d-v">
          {#if n.m.state === 'paused'}{t('topology.dock.pausedNoVote')}
          {:else}
            {@const off = offCount(g, n)}
            {#if n.bad.length}{t('topology.dock.ofSee', { n: n.pro.length })}<span class="bad-t">{t('topology.dock.ofBad', { k: ek(n.bad.length) })}</span>
            {:else}{t('topology.dock.ofClean', { n: n.pro.length })}{/if}{off ? t('topology.dock.offSuffix', { k: ek(off) }) : ''}
          {/if}
          <span class="mat ek">{#each g.proFixed as p (p.id)}<span class={cellClass(n, p)} title={p.name}></span>{/each}</span>
        </div>
      </div>
      {@render chans()}
    {:else if n.kind === 'srv'}
      {@const mons = ofKind('mon')}
      {@const bad = mons.filter((r) => g.nodes.get(r.id)?.st === 'bad').length}
      <div class="d-c ek">
        <span class="d-e">{t('topology.dock.load')}</span>
        <div class="d-v">
          {@render gauge('CPU', n.s.cpu)}{@render gauge('RAM', n.s.mem)}{@render gauge('Disk', n.s.disk)}
          {#if n.s.cpu === undefined}{t('topology.dock.noMetrics')}{/if}
        </div>
      </div>
      <div class="d-c"><span class="d-e">{t('topology.dock.lastEvent')}</span><div class="d-v">{n.s.alert || t('topology.dock.noAlert')}</div></div>
      <div class="d-c">
        <span class="d-e">{t('topology.dock.onServer')}</span>
        <div class="d-v">
          {#if mons.length}{t('topology.dock.monN', { count: mons.length })}{#if bad}, <span class="bad-t">{t('topology.dock.monBad', { k: ek(bad) })}</span>{/if}
          {:else}{t('topology.dock.noMon')}{/if}
          <br /><small>{t('topology.dock.metricsAt', { ago: fmtRelative(n.s.metrics_at, now), v: n.s.version })}</small>
        </div>
      </div>
      {@render chans()}
    {:else if n.kind === 'pro'}
      {@const bad = rels.filter((r) => r.c === 'bad')}
      <div class="d-c ek">
        <span class="d-e">{t('topology.dock.panelRtt')}</span>
        <div class="d-v">
          {#if n.st === 'off'}<div class="d-ms">{t('topology.dock.noVote')}</div><small>{t('topology.dock.seenAgo', { ago: fmtRelative(n.p.last_seen_at, now) })}</small>
          {:else if n.p.panel_rtt_ms !== undefined}<div class="d-ms">{Math.round(n.p.panel_rtt_ms)}<small>ms</small></div>{/if}
        </div>
      </div>
      <div class="d-c">
        <span class="d-e">{t('topology.dock.lastEvent')}</span>
        <div class="d-v">
          {#if n.local}{t('topology.dock.localMain')}
          {:else if n.st === 'off'}{t('topology.dock.hbLost', { ago: fmtRelative(n.p.last_seen_at, now) })}
          {:else if n.p.uplink === 'oob'}{t('topology.dock.onOob', { name: n.p.oob_name || '' })}
          {:else if n.p.late}{t('topology.dock.hbLate')}
          {:else}{t('topology.dock.connected', { ago: fmtRelative(n.p.last_seen_at, now) })}{/if}
        </div>
      </div>
      <div class="d-c">
        <span class="d-e">{t('topology.dock.inspects')}</span>
        <div class="d-v">{t('topology.dock.monN', { count: rels.length })}{#if n.p.peers}, {t('topology.dock.peers', { up: n.p.peers_up ?? 0, n: n.p.peers })}{/if}</div>
      </div>
      <div class="d-c ek">
        <span class="d-e">{t('topology.dock.sees')}</span>
        <div class="d-v">
          {#if bad.length}<span class="bad-t">{bad.slice(0, 6).map((r) => short(r.id)).join(', ')}</span>
          {:else}{n.st === 'off' ? t('topology.dock.offNoVote') : t('topology.dock.seesNone')}{/if}
        </div>
      </div>
    {:else}
      {@const c = n.c}
      <div class="d-c ek">
        <span class="d-e">{t('topology.dock.lastSend')}</span>
        <div class="d-v"><div class="d-ms kucuk">{c.last_send ? fmtRelative(c.last_send.at, now) : t('topology.dock.never')}</div></div>
      </div>
      <div class="d-c">
        <span class="d-e">{t('topology.dock.lastEvent')}</span>
        <div class="d-v">
          {#if c.last_send?.error}<span class="warn-t">{c.last_send.error}</span>
          {:else if c.last_send}{t('topology.dock.sendOk')}
          {:else}{t('topology.dock.never')}{/if}
        </div>
      </div>
      <div class="d-c"><span class="d-e">{t('topology.dock.linked')}</span><div class="d-v">{t('topology.dock.linkedN', { m: ofKind('mon').length, s: ofKind('srv').length })}</div></div>
      <div class="d-c ek">
        <span class="d-e">{t('topology.dock.last24')}</span>
        <div class="d-v">
          {t('topology.dock.sent', { n: c.sent_24h ?? 0 })}{#if c.failed_24h}<span class="warn-t">{t('topology.dock.failed', { n: c.failed_24h })}</span>{:else}{t('topology.dock.noFail')}{/if}
        </div>
      </div>
    {/if}
    <div class="ilg-l">
      {@render part(t('topology.dock.relatedSrv', { n: ofKind('srv').length }), 'srv', 8, 'srv')}
      {@render part(t('topology.dock.relatedPro', { n: ofKind('pro').length }), 'pro', 10, 'pro')}
      {@render part(t('topology.dock.relatedCha', { n: ofKind('cha').length }), 'cha', 8, 'pro')}
      {@render part(t('topology.dock.relatedMon', { n: ofKind('mon').length }), 'mon', 8, 'mon')}
    </div>
    <button type="button" class="ikon-b d-x" data-close aria-label={t('topology.dock.close')}><Icon name="x" size={15} /></button>
  {:else}
    <div class="bosd">
      <span class="y"><Icon name="mouse" size={15} />{t('topology.dock.hintClick')}</span>
      <span class="y"><kbd>{t('topology.dock.keys')}</kbd> {t('topology.dock.keysMove')} <kbd>Enter</kbd> {t('topology.dock.keysSelect')} <kbd>Esc</kbd> {t('topology.dock.keysClear')} <kbd>/</kbd> {t('topology.dock.keysSearch')}</span>
      <span class="y son">
        <svg width="26" height="6" aria-hidden="true"><path d="M0 3h26" stroke="var(--up)" stroke-width="1.5" /></svg>{t('topology.dock.lineClean')}
        <svg width="26" height="6" aria-hidden="true"><path d="M0 3h26" stroke="var(--down)" stroke-width="2" stroke-dasharray="7 4" /></svg>{t('topology.dock.lineBad')}
        <svg width="26" height="6" aria-hidden="true"><path d="M0 3h26" stroke="var(--warn)" stroke-width="1.75" /></svg>{t('topology.dock.lineWarn')}
        <svg width="26" height="6" aria-hidden="true"><path d="M0 3h26" stroke="var(--paused)" stroke-width="1.5" stroke-dasharray="2 5" /></svg>{t('topology.dock.lineOff')}
      </span>
    </div>
  {/if}
</section>
