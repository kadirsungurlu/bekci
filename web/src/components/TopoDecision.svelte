<script lang="ts">
  // Canlı topoloji: "Panelin kararı" (yalnız gerçek açık olay varken). En yeni olayın gerçek
  // nedeni, mesh bulguları ve giden bildirim kayıtları (olay ayrıntısından); diğer açık olaylar altta.
  import { t } from '../lib/i18n';
  import { api, type IncidentDetail, type Topology } from '../lib/api';
  import { fmtRelative, fmtTimeSec } from '../lib/format';
  import { notifyLabel } from '../lib/topoview';

  let { d, now }: { d: Topology; now: number } = $props();
  const MORE = 4;

  const inc = $derived(d.incidents[0]);
  let detail = $state<IncidentDetail | null>(null);
  let loadedFor = '';
  $effect(() => {
    const id = inc.id;
    // Olay, onay, açık olay sayısı değişince ya da 30 sn'de bir (yeni bildirim kaydı) yeniden alınır.
    const key = `${id}:${inc.acked}:${d.stats.open_incidents}:${Math.floor(d.now / 30)}`;
    if (key === loadedFor) return;
    loadedFor = key;
    api.incident(id).then(
      (r) => {
        if (inc.id === id) detail = r;
      },
      () => {
        if (inc.id === id) detail = null;
      },
    );
  });
  const det = $derived(detail && detail.incident.id === inc.id ? detail : null);
  const findings = $derived(det?.mesh?.findings ?? (inc.kind === 'mesh' ? (d.mesh?.findings ?? []) : []));

  const name = (i: Topology['incidents'][number]) => i.monitor_name || i.server_name || (i.kind === 'mesh' ? t('topology.decision.mesh') : i.cause);
  const str = (v: unknown) => (typeof v === 'string' ? v : '');

  interface Send {
    id: number;
    time: number;
    channel: string;
    res: 'ok' | 'fail' | 'deferred' | 'skipped' | 'none';
  }
  const sends = $derived.by<Send[]>(() => {
    if (!det) return [];
    return det.events
      .filter((e) => e.kind === 'notify')
      .slice(-4)
      .reverse()
      .map((e) => {
        const x = (e.data ?? {}) as Record<string, unknown>;
        const res: Send['res'] = x.none ? 'none' : x.deferred ? 'deferred' : str(x.skipped) ? 'skipped' : x.ok ? 'ok' : 'fail';
        const ch = [str(x.channel), x.type ? notifyLabel(str(x.type)) : ''].filter(Boolean).join(' · ');
        return { id: e.id, time: e.time, channel: ch, res };
      });
  });
  const level = $derived(inc.kind === 'mesh' ? 'warn' : 'bad');
</script>

<section class="card decision" aria-live="polite" data-testid="topology-decision">
  <h2>{t('topology.decision.title')} <span class="muted small">{t('topology.decision.count', { count: d.stats.open_incidents })}</span></h2>
  <div class="head">
    <span class={`lvl ${level}`}>{t(`topology.decision.kind.${inc.kind === 'mesh' ? 'mesh' : inc.server_id ? 'server' : 'monitor'}`)}</span>
    {#if inc.acked}<span class="lvl ok">{t('topology.decision.acked')}</span>{/if}
    <span class="muted small">{t('topology.decision.started', { ago: fmtRelative(inc.started_at, now) })}</span>
  </div>
  <a class="title" href={`#/incidents/${inc.id}`} data-testid="decision-title">{name(inc)}</a>
  <p class="cause">{inc.cause}</p>
  {#if findings.length}
    <ul class="findings">
      {#each findings as f, i (i)}<li>{f}</li>{/each}
    </ul>
  {/if}
  <h3 class="cap">{t('topology.decision.outgoing')}</h3>
  {#if sends.length}
    <ul class="sends" data-testid="decision-sends">
      {#each sends as s (s.id)}
        <li>
          <span class={`res ${s.res}`}>{t(`topology.decision.send.${s.res}`)}</span>
          {#if s.channel}<span class="ch">{s.channel}</span>{/if}
          <span class="muted small tm">{fmtTimeSec(s.time)}</span>
        </li>
      {/each}
    </ul>
  {:else}
    <p class="muted small">{det ? t('topology.decision.noSend') : t('topology.decision.loading')}</p>
  {/if}

  {#if d.incidents.length > 1}
    <h3 class="cap">{t('topology.decision.others')}</h3>
    <ul class="others">
      {#each d.incidents.slice(1, 1 + MORE) as o (o.id)}
        <li>
          <a href={`#/incidents/${o.id}`}><i class="sd"></i><span class="nm">{name(o)}</span></a>
          <span class="muted small">{fmtRelative(o.started_at, now)}</span>
        </li>
      {/each}
    </ul>
    {#if d.stats.open_incidents > 1 + MORE}
      <p class="muted small">{t('topology.decision.more', { count: d.stats.open_incidents - 1 - MORE })}</p>
    {/if}
  {/if}
  <a class="btn" href="#/incidents">{t('topology.decision.all')}</a>
</section>

<style>
  .decision {
    border-color: var(--down-border);
    min-width: 0;
  }
  h2 {
    margin: 0 0 10px;
    font-size: 1.05rem;
  }
  .head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px 10px;
  }
  .lvl {
    padding: 2px 8px;
    border-radius: 999px;
    font-size: 0.75rem;
    font-weight: 600;
  }
  .lvl.bad {
    background: var(--down-soft);
    color: var(--down-text);
  }
  .lvl.warn {
    background: var(--pending-soft);
    color: var(--pending-text);
  }
  .lvl.ok {
    background: var(--up-soft);
    color: var(--up-text);
  }
  .title {
    display: inline-flex;
    align-items: center;
    min-height: 44px;
    font-weight: 600;
    font-size: 1rem;
    color: var(--text);
    overflow-wrap: anywhere;
  }
  .cause {
    margin: 0 0 8px;
    overflow-wrap: anywhere;
  }
  .findings {
    margin: 0 0 8px;
    padding-left: 18px;
    font-size: 0.88rem;
  }
  .cap {
    margin: 14px 0 6px;
    font-size: 0.75rem;
    font-weight: 600;
    color: var(--muted);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  ul.sends,
  ul.others {
    list-style: none;
    margin: 0 0 8px;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .sends li {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 4px 10px;
    padding: 6px 10px;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--card-2);
    font-size: 0.86rem;
  }
  .ch {
    flex: 1;
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .res {
    font-weight: 600;
  }
  .res.ok {
    color: var(--up-text);
  }
  .res.fail {
    color: var(--down-text);
  }
  .res.deferred,
  .res.skipped,
  .res.none {
    color: var(--muted);
  }
  .tm {
    font-variant-numeric: tabular-nums;
  }
  .others li {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    min-width: 0;
  }
  .others a {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    min-height: 44px;
    min-width: 0;
    color: inherit;
    font-weight: 600;
  }
  .nm {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .sd {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    flex: none;
    background: var(--down);
  }
  @media (max-width: 640px) {
    .btn {
      min-height: 44px;
    }
  }
</style>
