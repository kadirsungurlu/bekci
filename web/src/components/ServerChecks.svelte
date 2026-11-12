<script lang="ts">
  // Sunucu ayrıntısı > Denetimler (730 §B.6): eklenti ve yerleşik denetimlerin
  // durumu, iletisi, son çalışması, metrik kıvılcımları; metriğe tıklanınca
  // LineChart. Yönetici eklenti yükler / düzenler (CheckEditModal).
  import { onDestroy, onMount } from 'svelte';
  import { api, type CheckMetric, type CheckSeries, type ServerCheck, type ServerChecks } from '../lib/api';
  import { fmtBytes, fmtDec, fmtInterval, fmtRelative, nowSec, type StatusKind } from '../lib/format';
  import { i18n, t, tOr } from '../lib/i18n';
  import { session } from '../lib/session.svelte';
  import CheckEditModal from './CheckEditModal.svelte';
  import Icon from './Icon.svelte';
  import LineChart from './LineChart.svelte';
  import Sparkline from './Sparkline.svelte';
  import StatusIcon from './StatusIcon.svelte';

  let { id }: { id: number } = $props();

  // Bir denetimde kıvılcım çizilen en çok metrik (her biri ayrı seri isteği).
  const SPARK_MAX = 8;
  const RANGES = ['24h', '7d', '30d'] as const;
  type Range = (typeof RANGES)[number];

  let data = $state<ServerChecks | null>(null);
  let loadError = $state('');
  let sparks = $state<Record<string, number[]>>({});
  let sel = $state<{ check: string; metric: string } | null>(null);
  let range = $state<Range>('24h');
  let series = $state<CheckSeries | null>(null);
  let editOpen = $state(false);
  let editing = $state<ServerCheck | null>(null);

  const key = (c: string, m: string) => `${c}/${m}`;

  async function load() {
    try {
      data = await api.getServerChecks(id);
      loadError = '';
      if (data.samples) loadSparks(data.checks);
    } catch (e) {
      if (!data) loadError = (e as Error).message;
    }
  }

  async function loadSparks(list: ServerCheck[]) {
    const jobs: Promise<void>[] = [];
    const next: Record<string, number[]> = {};
    for (const c of list)
      for (const m of (c.metrics ?? []).slice(0, SPARK_MAX))
        jobs.push(
          api
            .getCheckSeries(id, c.name, m.name, '24h')
            .then((s) => void (next[key(c.name, m.name)] = s.points.map((p) => p.avg)))
            .catch(() => {}),
        );
    await Promise.all(jobs);
    sparks = next;
  }

  async function loadSeries() {
    if (!sel) return;
    const want = { ...sel, range };
    try {
      const s = await api.getCheckSeries(id, want.check, want.metric, want.range);
      if (sel?.check === want.check && sel?.metric === want.metric && range === want.range) series = s;
    } catch {
      series = null;
    }
  }

  $effect(() => {
    void sel;
    void range;
    series = null;
    loadSeries();
  });

  let timer: ReturnType<typeof setInterval>;
  onMount(() => {
    load();
    timer = setInterval(() => {
      if (!document.hidden) {
        load();
        loadSeries();
      }
    }, 60000);
  });
  onDestroy(() => clearInterval(timer));

  const now = $derived(data?.now || nowSec());
  const checks = $derived(data?.checks ?? []);
  const docsUrl = $derived(i18n.locale === 'en' ? 'https://bekci.app/en/docs/plugins/' : 'https://bekci.app/docs/eklentiler/');

  const kindOf = (c: ServerCheck): StatusKind =>
    !c.enabled ? 'paused' : c.status === 'ok' ? 'up' : c.status === 'critical' ? 'down' : c.status === 'warning' ? 'pending' : 'paused';
  const badgeOf = (s: string) => (s === 'ok' ? 'up' : s === 'critical' ? 'down' : s === 'warning' ? 'pending' : 'paused');
  const statusText = (c: ServerCheck) => (!c.enabled ? t('servers.checks.disabled') : t(`servers.checks.status.${c.status || 'none'}`));
  const toneOf = (c: ServerCheck) => (c.status === 'critical' ? 'down' : c.status === 'warning' ? 'warn' : 'accent');
  const firing = (c: ServerCheck, m?: string) =>
    (data?.rules ?? []).some((r) => r.firing && r.mount === (m ? key(c.name, m) : c.name));

  // Değerler taban birimde (s, B, %, c ya da eklentinin kendi birimi).
  function fmtVal(v: number, unit = ''): string {
    if (unit === 'B') return fmtBytes(v);
    if (unit === 's') return v < 1 ? `${fmtDec(v * 1000, 0)} ms` : `${fmtDec(v, v < 10 ? 2 : 1)} s`;
    const n = Number.isInteger(v) ? v.toLocaleString(i18n.locale) : fmtDec(v, Math.abs(v) < 10 ? 2 : 1);
    if (unit === '%') return `${n}%`;
    return unit && unit !== 'c' ? `${n} ${unit}` : n;
  }

  const chart = $derived.by(() => {
    if (!series || series.points.length === 0) return null;
    const pts = series.points;
    const span = { '24h': 86400, '7d': 7 * 86400, '30d': 30 * 86400 }[range];
    let iv = series.hourly ? 3600 : 60;
    if (!series.hourly && pts.length > 1) {
      const d: number[] = [];
      for (let i = 1; i < pts.length; i++) d.push(pts[i].t - pts[i - 1].t);
      d.sort((a, b) => a - b);
      iv = Math.max(60, d[Math.floor(d.length / 2)]);
    }
    return {
      times: pts.map((p) => p.t),
      from: now - span,
      to: now,
      interval: iv,
      series: [{ label: series.metric, color: 'var(--chart-1)', values: pts.map((p) => p.avg), fill: true }],
      band: series.hourly
        ? { label: `${t('status.chart.min')} / ${t('status.chart.max')}`, color: 'var(--chart-1)', lo: pts.map((p) => p.min), hi: pts.map((p) => p.max) }
        : null,
    };
  });

  function pick(c: ServerCheck, m: CheckMetric) {
    sel = sel?.check === c.name && sel.metric === m.name ? null : { check: c.name, metric: m.name };
  }

  function openEdit(c: ServerCheck | null) {
    editing = c;
    editOpen = true;
  }
</script>

<div class="stack">
  <div class="head">
    {#if data}<span class="muted small">{t('servers.checks.limit', { n: checks.length, max: data.max })}</span>{/if}
    <a class="small" href={docsUrl} target="_blank" rel="noopener noreferrer">{t('servers.checks.docs')}</a>
    <div class="spacer"></div>
    {#if session.isAdmin}
      <button class="btn primary sm" onclick={() => openEdit(null)} disabled={!!data && checks.length >= data.max}><Icon name="plus" size={14} /> {t('servers.checks.add')}</button>
    {/if}
  </div>

  {#if loadError}
    <div class="alert error">{t('servers.checks.load')}: {loadError}</div>
  {:else if !data}
    <div class="skeleton" style="height:160px"></div>
  {:else if checks.length === 0}
    <div class="card empty">
      <h3>{t('servers.checks.emptyTitle')}</h3>
      <p class="muted">{t('servers.checks.emptyHint')}</p>
    </div>
  {:else}
    {#if !data.samples}<div class="alert info small">{t('servers.checks.noSamples')}</div>{/if}
    {#each checks as c (c.name)}
      <section class="card chk" class:off={!c.enabled}>
        <div class="top">
          <StatusIcon kind={kindOf(c)} size={30} />
          <div class="title">
            <div class="line">
              <code class="name">{c.name}</code>
              <span class="badge {c.enabled ? badgeOf(c.status) : 'paused'}">{statusText(c)}</span>
              <span class="badge paused">{t(`servers.checks.source.${c.source}`)}</span>
              {#if c.version}<span class="muted small">v{c.version}</span>{/if}
              {#if firing(c)}<span class="badge down"><Icon name="alert" size={12} /> {t('servers.checks.ruleFiring')}</span>{/if}
            </div>
            {#if c.message}<div class="msg">{c.message}</div>{/if}
            <div class="meta muted small">
              {#if c.last_run}
                <span title={new Date(c.last_run * 1000).toLocaleString(i18n.locale)}>{t('servers.checks.lastRun', { ago: fmtRelative(c.last_run, now) })}</span>
                <span>{t('servers.checks.duration', { ms: c.duration_ms })}</span>
              {:else}
                <span>{t('servers.checks.neverRan')}</span>
              {/if}
              {#if c.interval}<span>{t('servers.checks.every', { iv: fmtInterval(c.interval) })}</span>{/if}
              {#if c.status_since && c.status}<span>{t('servers.checks.since', { ago: fmtRelative(c.status_since, now) })}</span>{/if}
              {#if c.unknown_streak >= 3}<span>{t('servers.checks.unknownStreak', { n: c.unknown_streak })}</span>{/if}
            </div>
          </div>
          {#if session.isAdmin}
            <button class="btn sm" onclick={() => openEdit(c)}><Icon name="edit" size={14} /> {t('common.edit')}</button>
          {/if}
        </div>

        {#if c.error}<div class="alert warn small">{tOr(`servers.checks.errors.${c.error}`, c.error)}</div>{/if}
        {#if c.dropped}<div class="muted small">{t('servers.checks.dropped', { n: c.dropped })}</div>{/if}
        {#if c.truncated}<div class="muted small">{t('servers.checks.truncated')}</div>{/if}
        {#if c.stderr}
          <details class="small"><summary>{t('servers.checks.stderr')}</summary><pre>{c.stderr}</pre></details>
        {/if}

        {#if c.metrics?.length}
          <div class="metrics">
            {#each c.metrics as m (m.name)}
              {@const on = sel?.check === c.name && sel.metric === m.name}
              <button class="metric" class:on class:fire={firing(c, m.name)} aria-pressed={on} onclick={() => pick(c, m)}>
                <span class="mname">{m.name}</span>
                <span class="mval">{fmtVal(m.value, m.unit)}</span>
                {#if sparks[key(c.name, m.name)]?.length > 1}
                  <Sparkline values={sparks[key(c.name, m.name)]} w={96} h={22} tone={toneOf(c)} label={m.name} />
                {/if}
              </button>
            {/each}
          </div>
          {#if sel?.check === c.name}
            <div class="chartbox">
              <div class="tabs" role="tablist" aria-label={t('servers.checks.chartRange')}>
                {#each RANGES as r (r)}
                  <button role="tab" aria-selected={range === r} class:active={range === r} onclick={() => (range = r)}>{t(`servers.checks.ranges.${r}`)}</button>
                {/each}
              </div>
              {#if chart}
                {@const unit = series?.unit ?? ''}
                <LineChart label="{c.name} / {sel.metric}" times={chart.times} from={chart.from} to={chart.to} interval={chart.interval} series={chart.series} band={chart.band} bytes={unit === 'B'} format={(v) => fmtVal(v, unit)} />
              {:else if series}
                <p class="muted small">{t('servers.checks.chartEmpty')}</p>
              {:else}
                <div class="skeleton" style="height:180px"></div>
              {/if}
            </div>
          {/if}
        {:else if c.last_run}
          <p class="muted small">{t('servers.checks.noMetrics')}</p>
        {/if}
      </section>
    {/each}
  {/if}
</div>

{#if session.isAdmin}
  <CheckEditModal bind:open={editOpen} serverId={id} check={editing} rules={data?.rules ?? []} onsaved={load} />
{/if}

<style>
  .head {
    display: flex;
    align-items: center;
    gap: 12px;
    flex-wrap: wrap;
  }
  .empty {
    padding: 28px 24px;
    text-align: center;
  }
  .empty h3 {
    margin: 0 0 6px;
  }
  .empty p {
    max-width: 560px;
    margin: 0 auto;
  }
  .chk {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .chk.off {
    opacity: 0.75;
  }
  .top {
    display: flex;
    gap: 12px;
    align-items: flex-start;
  }
  .title {
    flex: 1;
    min-width: 0;
  }
  .line {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px 8px;
  }
  .name {
    font-size: 15px;
    font-weight: 600;
  }
  .msg {
    margin-top: 4px;
    overflow-wrap: anywhere;
  }
  .meta {
    display: flex;
    flex-wrap: wrap;
    gap: 2px 14px;
    margin-top: 4px;
  }
  .metrics {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
    gap: 8px;
  }
  .metric {
    display: grid;
    grid-template-columns: 1fr auto;
    gap: 2px 8px;
    align-items: center;
    text-align: left;
    padding: 8px 10px;
    border: 1px solid var(--border);
    border-radius: 8px;
    background: var(--surface-2, transparent);
    color: inherit;
    font: inherit;
    cursor: pointer;
  }
  .metric:hover {
    border-color: var(--accent);
  }
  .metric.on {
    border-color: var(--accent);
    box-shadow: inset 0 0 0 1px var(--accent);
  }
  .metric.fire {
    border-color: var(--down);
  }
  .metric:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }
  .mname {
    font-size: 12px;
    color: var(--muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .mval {
    grid-row: 2;
    font-weight: 600;
    font-variant-numeric: tabular-nums;
  }
  .metric :global(svg) {
    grid-column: 2;
    grid-row: 1 / span 2;
  }
  .chartbox {
    border-top: 1px solid var(--border);
    padding-top: 10px;
  }
  .tabs {
    display: inline-flex;
    gap: 2px;
    margin-bottom: 8px;
    padding: 2px;
    border: 1px solid var(--border);
    border-radius: 8px;
  }
  .tabs button {
    border: 0;
    background: none;
    color: var(--muted);
    font: inherit;
    font-size: 13px;
    padding: 4px 10px;
    border-radius: 6px;
    cursor: pointer;
  }
  .tabs button.active {
    background: var(--accent-soft, var(--border));
    color: var(--text);
  }
  pre {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    margin: 6px 0 0;
    font-size: 12px;
  }
  @media (max-width: 560px) {
    .top {
      flex-wrap: wrap;
    }
    .metrics {
      grid-template-columns: 1fr 1fr;
    }
  }
</style>
