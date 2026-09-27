<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import {
    api,
    ApiError,
    errorMessage,
    STATUS_UP,
    type Incident,
    type MonitorDetail,
    type RawPoint,
    type Series,
    type SeriesRange,
  } from '../lib/api';
  import { live } from '../lib/live.svelte';
  import { navigate } from '../lib/router.svelte';
  import { clock, copyText, toast } from '../lib/ui.svelte';
  import {
    STATUS_LABELS,
    certDaysLeft,
    fmtDate,
    fmtDay,
    fmtDuration,
    fmtInterval,
    fmtMs,
    fmtPct,
    fmtRelative,
    nowSec,
    statusKind,
  } from '../lib/format';
  import { deleteMonitor, togglePause } from '../lib/actions';
  import StatusIcon from '../components/StatusIcon.svelte';
  import TypeBadge from '../components/TypeBadge.svelte';
  import PingChart from '../components/PingChart.svelte';
  import IncidentTable from '../components/IncidentTable.svelte';
  import Icon from '../components/Icon.svelte';

  let { id }: { id: number } = $props();

  let detail = $state.raw<MonitorDetail | null>(null);
  let notFound = $state(false);
  let loadError = $state('');
  let incidents = $state.raw<Incident[]>([]);
  let range = $state<SeriesRange>('24h');
  let series = $state.raw<Series | null>(null);
  let seriesLoading = $state(false);
  let chartTo = $state(nowSec());
  let busy = $state(false);

  const RANGES: { key: SeriesRange; label: string; sec: number }[] = [
    { key: '24h', label: '24 saat', sec: 86400 },
    { key: '7d', label: '7 gün', sec: 7 * 86400 },
    { key: '30d', label: '30 gün', sec: 30 * 86400 },
    { key: '90d', label: '90 gün', sec: 90 * 86400 },
  ];

  const monitor = $derived(live.byId(id) ?? detail?.monitor ?? null);
  const kind = $derived(monitor ? statusKind(monitor.status, monitor.active) : 'pending');
  const now = $derived(clock.now);
  const isHttps = $derived(monitor?.type === 'http' && /^https:/i.test(monitor.target));
  const pushUrl = $derived(monitor?.push_token ? `${location.origin}/api/push/${monitor.push_token}` : '');

  async function loadDetail() {
    try {
      const [d, inc] = await Promise.all([api.monitor(id), api.monitorIncidents(id)]);
      detail = d;
      incidents = inc;
      loadError = '';
    } catch (e) {
      if (e instanceof ApiError && e.status === 404) notFound = true;
      else if (!detail) loadError = errorMessage(e);
    }
  }

  let seriesReq = 0;
  async function loadSeries(r: SeriesRange) {
    const req = ++seriesReq;
    seriesLoading = true;
    try {
      const s = await api.series(id, r);
      if (req !== seriesReq) return;
      series = s;
      chartTo = nowSec();
    } catch (e) {
      if (req === seriesReq) toast.error(errorMessage(e));
    } finally {
      if (req === seriesReq) seriesLoading = false;
    }
  }

  $effect(() => {
    loadSeries(range);
  });

  let reloadTimer: ReturnType<typeof setTimeout> | undefined;
  let refreshTimer: ReturnType<typeof setInterval> | undefined;
  let unsub: (() => void) | undefined;
  let lastStatus: number | null = null;

  onMount(() => {
    loadDetail();
    refreshTimer = setInterval(loadDetail, 60_000);
    unsub = live.onBeat((b) => {
      if (b.monitor_id !== id) return;
      if (series && series.kind === 'raw') {
        const p: RawPoint = { t: b.time, s: b.status, p: b.ping };
        if (b.status !== STATUS_UP && b.message) p.m = b.message;
        const cutoff = b.time - 86400;
        const pts = series.points.filter((x) => x.t >= cutoff);
        pts.push(p);
        series = { ...series, points: pts };
        chartTo = Math.max(nowSec(), b.time);
      }
      const prev = lastStatus ?? monitor?.status ?? b.status;
      lastStatus = b.status;
      if (prev !== b.status) {
        clearTimeout(reloadTimer);
        reloadTimer = setTimeout(loadDetail, 500);
      }
    });
  });

  onDestroy(() => {
    unsub?.();
    clearTimeout(reloadTimer);
    clearInterval(refreshTimer);
  });

  async function onToggle() {
    if (!monitor) return;
    busy = true;
    const res = await togglePause(monitor);
    busy = false;
    if (res) {
      detail = detail ? { ...detail, monitor: res } : detail;
      loadDetail();
    }
  }

  async function onDelete() {
    if (!monitor) return;
    if (await deleteMonitor(monitor)) navigate('/');
  }

  async function copy(text: string) {
    if (await copyText(text)) toast.success('Panoya kopyalandı');
    else toast.error('Kopyalanamadı; metni elle seçip kopyalayın');
  }

  const statusSub = $derived.by(() => {
    if (!monitor) return '';
    switch (kind) {
      case 'paused':
        return 'Kontroller durduruldu';
      case 'pending':
        // Başlatma/düzenleme sonrası sunucu son mesajı temizler: henüz sonuç yok.
        return monitor.last_message ? 'Tekrar deneniyor' : 'İlk kontrol bekleniyor';
      case 'up':
        return monitor.last_change_at ? `${fmtDuration(now - monitor.last_change_at)} süredir çalışıyor` : '';
      case 'down':
        return monitor.last_change_at ? `${fmtDuration(now - monitor.last_change_at)} süredir çalışmıyor` : '';
    }
  });

  const cert = $derived.by(() => {
    if (!monitor || !monitor.cert_expires_at) return null;
    const days = certDaysLeft(monitor.cert_expires_at, now) ?? 0;
    const cls = days < 7 ? 'c-down' : days < 14 ? 'c-pending' : 'c-up';
    return { days, cls };
  });

  const selRange = $derived(RANGES.find((r) => r.key === range)!);
  const curlUp = $derived(pushUrl ? `curl -fsS -m 10 --retry 3 "${pushUrl}"` : '');
  const curlDown = $derived(pushUrl ? `curl -fsS -m 10 "${pushUrl}?status=down&msg=Yedekleme%20basarisiz"` : '');
</script>

{#if notFound}
  <div class="card empty">
    <h3>Monitör bulunamadı</h3>
    <p>Bu monitör silinmiş olabilir.</p>
    <a class="btn primary" href="#/">Monitörlere dön</a>
  </div>
{:else if !monitor}
  {#if loadError}
    <div class="card empty">
      <h3>Yüklenemedi</h3>
      <p>{loadError}</p>
      <button class="btn primary" onclick={loadDetail}>Tekrar dene</button>
    </div>
  {:else}
    <div class="skeleton" style="height:90px;margin-bottom:20px"></div>
    <div class="skeleton" style="height:300px"></div>
  {/if}
{:else}
  <a class="back" href="#/"><Icon name="chevron-left" size={16} /> Monitörler</a>

  <div class="head">
    <div class="title">
      <StatusIcon {kind} size={44} pulse={kind === 'up' || kind === 'down'} />
      <div class="tt">
        <div class="name-row">
          <h1>{monitor.name}</h1>
          <TypeBadge type={monitor.type} />
        </div>
        <div class="target">
          {#if monitor.type === 'http'}
            <a href={monitor.target} target="_blank" rel="noopener noreferrer">{monitor.target}<Icon name="external" size={13} /></a>
          {:else if monitor.type === 'push'}
            <span class="muted">Push monitörü · beklenen aralık {fmtInterval(monitor.interval)}</span>
          {:else}
            <span class="text-2 mono">{monitor.target}</span>
          {/if}
        </div>
      </div>
    </div>
    <div class="actions">
      <button class="btn" onclick={onToggle} disabled={busy}>
        <Icon name={monitor.active ? 'pause' : 'play'} size={15} />
        {monitor.active ? 'Durdur' : 'Başlat'}
      </button>
      <a class="btn" href="#/monitors/{monitor.id}/edit"><Icon name="edit" size={15} /> Düzenle</a>
      <button class="btn danger" onclick={onDelete}><Icon name="trash" size={15} /> Sil</button>
    </div>
  </div>

  {#if monitor.description}
    <p class="desc">{monitor.description}</p>
  {/if}

  <div class="stats">
    <div class="card stat">
      <div class="label">Mevcut durum</div>
      <div class="value"><span class="pill {kind}">{STATUS_LABELS[kind]}</span></div>
      <div class="sub">{statusSub}</div>
    </div>
    <div class="card stat">
      <div class="label">Son kontrol</div>
      <div class="value">{monitor.last_check_at ? fmtRelative(monitor.last_check_at, now) : '—'}</div>
      <div class="sub" title={monitor.last_message}>
        {#if monitor.last_message}
          <span class:c-down={kind === 'down'} class:c-pending={kind === 'pending'}>{monitor.last_message}</span> ·
        {/if}
        her {fmtInterval(monitor.interval)}
      </div>
    </div>
    <div class="card stat">
      <div class="label">Ortalama yanıt (24 saat)</div>
      <div class="value">{fmtMs(detail?.avg_ping_24h)}</div>
      <div class="sub">Son ölçüm: {monitor.last_check_at && kind === 'up' ? fmtMs(monitor.last_ping_ms) : '—'}</div>
    </div>
    {#if isHttps}
      <div class="card stat">
        <div class="label"><Icon name="lock" size={13} /> SSL sertifikası</div>
        {#if cert}
          <div class="value {cert.cls}">{cert.days < 0 ? 'Süresi doldu' : `${cert.days} gün kaldı`}</div>
          <div class="sub">{fmtDay(monitor.cert_expires_at)} bitiyor{monitor.cert_issuer ? ` · ${monitor.cert_issuer}` : ''}</div>
        {:else}
          <div class="value muted">—</div>
          <div class="sub">Henüz bilgi yok</div>
        {/if}
      </div>
    {/if}
  </div>

  <div class="card uptimes">
    {#each RANGES as r (r.key)}
      {@const v = detail?.uptime[r.key] ?? null}
      <div>
        <div class="label">Son {r.label}</div>
        <div class="big {v === null ? 'muted' : v >= 99.9 ? 'c-up' : v >= 99 ? 'c-pending' : 'c-down'}">{fmtPct(v)}</div>
      </div>
    {/each}
  </div>

  {#if monitor.type === 'push' && pushUrl}
    <div class="card block">
      <h2 class="card-title">Push adresi<span class="dot">.</span></h2>
      <p class="text-2 small intro">
        Zamanlanmış işiniz (cron, yedekleme betiği vb.) her çalıştığında bu adrese istek göndermeli.
        {fmtInterval(monitor.interval)} içinde istek gelmezse monitör <b>çalışmıyor</b> sayılır.
      </p>
      <div class="copybox">
        <code>{pushUrl}</code>
        <button class="btn sm" onclick={() => copy(pushUrl)}><Icon name="copy" size={14} /> Kopyala</button>
      </div>
      <div class="label mt">Örnek (işiniz başarıyla bittiğinde)</div>
      <div class="copybox">
        <code>{curlUp}</code>
        <button class="btn sm" onclick={() => copy(curlUp)}><Icon name="copy" size={14} /> Kopyala</button>
      </div>
      <div class="label mt">Hata bildirmek için</div>
      <div class="copybox">
        <code>{curlDown}</code>
        <button class="btn sm" onclick={() => copy(curlDown)}><Icon name="copy" size={14} /> Kopyala</button>
      </div>
      <p class="help mt">
        İsteğe bağlı parametreler: <code>status=up|down</code>, <code>msg=</code> (mesaj), <code>ping=</code> (ms cinsinden süre).
        GET veya POST kullanılabilir.
      </p>
    </div>
  {/if}

  <div class="card block">
    <div class="chart-head">
      <h2 class="card-title">Yanıt süresi<span class="dot">.</span></h2>
      <div class="tabs" role="tablist">
        {#each RANGES as r (r.key)}
          <button role="tab" aria-selected={range === r.key} class:active={range === r.key} onclick={() => (range = r.key)}>
            {r.label}
          </button>
        {/each}
      </div>
    </div>
    <div class="chart-wrap" class:loading={seriesLoading}>
      {#if series}
        <PingChart {series} from={chartTo - selRange.sec} to={chartTo} interval={monitor.interval} />
      {:else}
        <div class="skeleton" style="height:230px"></div>
      {/if}
    </div>
  </div>

  <div class="card block">
    <h2 class="card-title">Olaylar<span class="dot">.</span></h2>
    {#if detail?.open_incident_since}
      <div class="alert error ongoing">
        {fmtDuration(now - detail.open_incident_since)} süredir devam eden bir kesinti var ({fmtDate(detail.open_incident_since)} başladı).
      </div>
    {/if}
    <IncidentTable {incidents} {now} emptyText="Bu monitörde henüz olay kaydı yok. Harika!" />
  </div>
{/if}

<style>
  .back {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    color: var(--text-2);
    font-size: 0.88rem;
    margin-bottom: 14px;
  }
  .head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 16px;
    flex-wrap: wrap;
    margin-bottom: 20px;
  }
  .title {
    display: flex;
    align-items: flex-start;
    gap: 16px;
    min-width: 0;
    flex: 1 1 460px;
  }
  .title :global(.si) {
    margin-top: 1px;
  }
  .tt {
    min-width: 0;
  }
  .name-row {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
  }
  h1 {
    word-break: break-word;
  }
  .target {
    margin-top: 3px;
    font-size: 0.9rem;
    word-break: break-all;
  }
  /* Uzun adres satır kırsa da dış bağlantı simgesi metnin sonuna yapışık kalsın. */
  .target a :global(svg) {
    display: inline-block;
    vertical-align: -2px;
    margin-left: 4px;
  }
  .actions {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
  }
  .desc {
    margin: -6px 0 18px;
    color: var(--text-2);
    white-space: pre-line;
  }
  .stats {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(210px, 1fr));
    gap: 16px;
    margin-bottom: 16px;
  }
  .stat {
    display: flex;
    flex-direction: column;
    gap: 6px;
    min-width: 0;
  }
  .label {
    display: flex;
    align-items: center;
    gap: 5px;
    font-size: 0.78rem;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--muted);
  }
  .value {
    font-size: 1.3rem;
    font-weight: 700;
    line-height: 1.3;
  }
  .sub {
    font-size: 0.84rem;
    color: var(--text-2);
    overflow: hidden;
    text-overflow: ellipsis;
    display: -webkit-box;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    -webkit-box-orient: vertical;
    word-break: break-word;
  }
  .uptimes {
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    gap: 16px;
    margin-bottom: 16px;
  }
  .uptimes .big {
    font-size: 1.45rem;
    font-weight: 700;
    margin-top: 4px;
  }
  .block {
    margin-bottom: 16px;
  }
  .chart-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    flex-wrap: wrap;
    margin-bottom: 14px;
  }
  .chart-head .card-title {
    margin: 0;
  }
  .tabs {
    display: inline-flex;
    background: var(--input);
    border: 1px solid var(--border);
    border-radius: 9px;
    padding: 3px;
  }
  .tabs button {
    background: none;
    border: none;
    color: var(--text-2);
    font: inherit;
    font-size: 0.83rem;
    font-weight: 600;
    padding: 5px 12px;
    border-radius: 6px;
    cursor: pointer;
    white-space: nowrap;
  }
  .tabs button.active {
    background: var(--card-2);
    color: var(--text);
  }
  .tabs button:focus-visible {
    outline-offset: -2px;
  }
  .chart-wrap {
    transition: opacity 0.15s;
  }
  .chart-wrap.loading {
    opacity: 0.5;
  }
  .intro {
    margin: -4px 0 14px;
  }
  .copybox {
    display: flex;
    align-items: center;
    gap: 10px;
    background: var(--input);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-sm);
    padding: 8px 8px 8px 12px;
  }
  .copybox code {
    flex: 1;
    min-width: 0;
    word-break: break-all;
    color: var(--text);
  }
  .mt {
    margin-top: 14px;
    margin-bottom: 6px;
  }
  .help code {
    color: var(--text-2);
  }
  .ongoing {
    margin-bottom: 12px;
  }

  @media (max-width: 900px) {
    .stats {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
  }
  @media (max-width: 640px) {
    .title {
      gap: 12px;
    }
    .title :global(.si) {
      width: 36px !important;
      height: 36px !important;
    }
    .actions {
      width: 100%;
    }
    .actions > :global(*) {
      flex: 1;
    }
    .stats {
      gap: 10px;
    }
    .stat {
      padding: 14px;
    }
    .value {
      font-size: 1.1rem;
    }
    .uptimes {
      grid-template-columns: repeat(2, 1fr);
    }
    .uptimes .big {
      font-size: 1.2rem;
    }
    .tabs {
      width: 100%;
    }
    .tabs button {
      flex: 1;
      padding: 5px 4px;
    }
    .copybox {
      flex-direction: column;
      align-items: stretch;
    }
  }
</style>
