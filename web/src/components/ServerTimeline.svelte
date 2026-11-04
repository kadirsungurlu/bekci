<script lang="ts">
  import { api, type ServerMetric, type TimelineEvent } from '../lib/api';
  import { fmtDate, fmtDay } from '../lib/format';
  import { t, tOr, type TKey } from '../lib/i18n';
  import { fmtMetric, metricLabel } from '../lib/servers.svelte';
  import Icon from './Icon.svelte';
  import StateMark from './StateMark.svelte';

  let { id }: { id: number } = $props();

  // Sorunlar süzgeci: yalnızca dikkat isteyen olay türleri.
  const PROBLEMS = ['oom', 'reboot', 'service_failed', 'service_stopped', 'container_stopped', 'container_restarted', 'alert'];
  const KNOWN = new Set([
    ...PROBLEMS,
    'service_recovered',
    'container_started',
    'container_added',
    'container_removed',
    'container_updated',
    'port_opened',
    'port_closed',
    'alert_resolved',
  ]);
  const PAGE = 50;

  let filter = $state<'all' | 'problems'>('all');
  let events = $state.raw<TimelineEvent[] | null>(null);
  let more = $state(false);
  let busy = $state(false);
  let loadError = $state('');

  async function load(append = false) {
    busy = true;
    loadError = '';
    try {
      const before = append && events?.length ? events[events.length - 1].time : 0;
      const r = await api.getServerTimeline(id, { before, limit: PAGE, kinds: filter === 'problems' ? PROBLEMS : [] });
      events = append ? [...(events ?? []), ...r.events] : r.events;
      more = r.more;
    } catch (e) {
      loadError = (e as Error).message;
    } finally {
      busy = false;
    }
  }

  $effect(() => {
    void [id, filter];
    load();
  });

  function title(e: TimelineEvent): string {
    const d = e.data ?? {};
    if (e.kind === 'alert' || e.kind === 'alert_resolved') {
      const name = metricLabel(d.metric as ServerMetric) + (d.mount ? ` (${d.mount})` : '');
      return t(e.kind === 'alert' ? 'servers.timeline.kinds.alert' : 'servers.timeline.kinds.alert_resolved', { name });
    }
    if (e.kind === 'oom' && !e.subject) return t('servers.timeline.kinds.oomUnknown');
    return KNOWN.has(e.kind) ? t(`servers.timeline.kinds.${e.kind}` as TKey, { subject: e.subject }) : e.kind;
  }

  function detail(e: TimelineEvent): string {
    const d = e.data ?? {};
    switch (e.kind) {
      case 'alert':
      case 'alert_resolved': {
        const v = fmtMetric(d.metric as ServerMetric, Number(d.value ?? 0));
        return v ? t('servers.timeline.value', { v }) : '';
      }
      case 'oom': {
        const n = Number(d.count ?? 1);
        const parts = [];
        if (d.pid) parts.push(t('servers.timeline.pid', { n: Number(d.pid) }));
        if (n > 1) parts.push(t('servers.timeline.times', { n }));
        return parts.join(' · ');
      }
      case 'container_updated':
        return `${d.from ?? ''} > ${d.image ?? ''}`;
      case 'container_added':
        return String(d.image ?? '');
      case 'container_stopped':
        return tOr(`servers.inventory.st.${d.state}`, String(d.state ?? ''));
      case 'container_restarted':
        return t('servers.timeline.restarts', { n: Number(d.count ?? 0) });
      case 'port_opened':
      case 'port_closed':
        return String(d.process ?? '');
    }
    return '';
  }

  // Güne göre gruplar: en yeni gün üstte.
  const days = $derived.by(() => {
    const out: { day: string; list: TimelineEvent[] }[] = [];
    for (const e of events ?? []) {
      const day = fmtDay(e.time);
      if (out.at(-1)?.day === day) out.at(-1)!.list.push(e);
      else out.push({ day, list: [e] });
    }
    return out;
  });
</script>

<section class="card block tl">
  <div class="head">
    <h2 class="card-title">{t('servers.timeline.title')}<span class="dot">.</span></h2>
    <div class="seg" role="group" aria-label={t('servers.timeline.filter')}>
      <button aria-pressed={filter === 'all'} class:active={filter === 'all'} onclick={() => (filter = 'all')}>{t('servers.timeline.all')}</button>
      <button aria-pressed={filter === 'problems'} class:active={filter === 'problems'} onclick={() => (filter = 'problems')}>{t('servers.timeline.problems')}</button>
    </div>
  </div>
  {#if loadError}
    <div class="empty">
      <h3>{t('servers.timeline.loadFailed')}</h3>
      <p>{loadError}</p>
      <button class="btn primary" onclick={() => load()}>{t('common.retry')}</button>
    </div>
  {:else if events === null}
    <div class="skeleton" style="height:240px"></div>
  {:else if events.length === 0}
    <div class="empty">
      <div class="eicon"><Icon name="activity" size={26} /></div>
      <h3>{t(filter === 'problems' ? 'servers.timeline.noProblems' : 'servers.timeline.emptyTitle')}</h3>
      <p>{t('servers.timeline.emptyHint')}</p>
      {#if filter === 'problems'}
        <button class="btn" onclick={() => (filter = 'all')}>{t('servers.timeline.showAll')}</button>
      {/if}
    </div>
  {:else}
    {#each days as g (g.day)}
      <h3 class="day">{g.day}</h3>
      <ul class="evs">
        {#each g.list as e (`${e.time}-${e.kind}-${e.subject}`)}
          {@const sub = detail(e)}
          <li class={e.level}>
            <span class="emark">
              {#if e.level === 'error' || e.level === 'warning'}
                <StateMark cls={e.level === 'error' ? 'bad' : 'warn'} label={t(`servers.timeline.level.${e.level}`)} />
              {:else}
                <span class="edot" aria-hidden="true"></span>
              {/if}
            </span>
            <span class="time" title={fmtDate(e.time)}>{fmtDate(e.time).slice(-5)}</span>
            <span class="txt">
              <span class="t1">{title(e)}</span>
              {#if sub}<span class="t2">{sub}</span>{/if}
            </span>
          </li>
        {/each}
      </ul>
    {/each}
    {#if more}
      <button class="btn morebtn" disabled={busy} onclick={() => load(true)}>{t('servers.timeline.more')}</button>
    {/if}
  {/if}
</section>

<style>
  .head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    margin-bottom: 12px;
  }
  .head .card-title {
    margin: 0;
  }
  .seg {
    display: inline-flex;
    gap: 2px;
    padding: 2px;
    border: 1px solid var(--border);
    border-radius: 8px;
  }
  .seg button {
    min-height: 32px;
    padding: 0 12px;
    border: none;
    border-radius: 6px;
    background: none;
    color: var(--text-2);
    font: inherit;
    font-size: 0.82rem;
    font-weight: 600;
    cursor: pointer;
  }
  .seg button.active {
    background: var(--bg-2, var(--border));
    color: var(--text);
  }
  .day {
    margin: 16px 0 6px;
    font-size: 0.78rem;
    font-weight: 600;
    color: var(--text-2);
  }
  .evs {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .evs li {
    display: grid;
    grid-template-columns: 14px 48px minmax(0, 1fr);
    gap: 10px;
    align-items: start;
    line-height: 1.4;
    padding: 7px 0;
    border-top: 1px solid var(--border);
  }
  /* İşaret ilk satırla hizalı: metin satır atlasa da dikey ortaya kaymaz. */
  .emark {
    display: flex;
    align-items: center;
    justify-content: center;
    height: 1.4em;
    font-size: 0.88rem;
  }
  .edot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--text-3, var(--text-2));
  }
  .time {
    color: var(--text-2);
    font-size: 0.8rem;
    font-variant-numeric: tabular-nums;
  }
  .txt {
    display: flex;
    flex-wrap: wrap;
    gap: 2px 10px;
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .t1 {
    font-size: 0.88rem;
  }
  .t2 {
    color: var(--text-2);
    font-size: 0.8rem;
  }
  .morebtn {
    margin-top: 12px;
    width: 100%;
  }
  .empty {
    padding: 28px 12px;
    text-align: center;
  }
  .empty h3 {
    margin: 6px 0 4px;
    font-size: 0.95rem;
  }
  .empty p {
    margin: 0 auto 10px;
    max-width: 44ch;
    color: var(--text-2);
    font-size: 0.85rem;
  }
  .eicon {
    color: var(--text-2);
  }
  @media (max-width: 640px) {
    .seg button {
      min-height: 44px;
    }
    .morebtn {
      min-height: 44px;
    }
  }
</style>
