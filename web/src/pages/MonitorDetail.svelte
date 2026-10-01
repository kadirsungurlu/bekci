<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import {
    api,
    ApiError,
    errorMessage,
    STATUS_UP,
    type Incident,
    type LocationState,
    type MonitorDetail,
    type MonitorLocations,
    type RawPoint,
    type Series,
    type SeriesRange,
  } from '../lib/api';
  import { live } from '../lib/live.svelte';
  import { navigate } from '../lib/router.svelte';
  import { clock, toast } from '../lib/ui.svelte';
  import {
    STATUS_LABELS,
    certDaysLeft,
    fmtDate,
    fmtDay,
    fmtDuration,
    fmtInterval,
    fmtMs,
    fmtPct,
    uptimeTone,
    fmtRelative,
    monitorKind,
    nowSec,
  } from '../lib/format';
  import { session } from '../lib/session.svelte';
  import { GROUP_MODES, displayTarget, groupTarget, isWebTarget, typeName } from '../lib/monitorTypes';
  import { t, tParts, type TKey } from '../lib/i18n';
  import { deleteMonitor, togglePause } from '../lib/actions';
  import StatusIcon from '../components/StatusIcon.svelte';
  import TypeBadge from '../components/TypeBadge.svelte';
  import PingChart from '../components/PingChart.svelte';
  import IncidentTable from '../components/IncidentTable.svelte';
  import BadgeBuilder from '../components/BadgeBuilder.svelte';
  import TagChip from '../components/TagChip.svelte';
  import Icon from '../components/Icon.svelte';
  import CopyButton from '../components/CopyButton.svelte';

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

  const RANGES: { key: SeriesRange; label: TKey; sec: number }[] = [
    { key: '24h', label: 'monitors.detail.r24h', sec: 86400 },
    { key: '7d', label: 'monitors.detail.r7d', sec: 7 * 86400 },
    { key: '30d', label: 'monitors.detail.r30d', sec: 30 * 86400 },
    { key: '90d', label: 'monitors.detail.r90d', sec: 90 * 86400 },
  ];

  const monitor = $derived(live.byId(id) ?? detail?.monitor ?? null);
  const kind = $derived(monitor ? monitorKind(monitor) : 'pending');
  let badgesOpen = $state(false);

  // Grup monitörünün alt monitörleri (izleyicide ayarlar gizli olduğundan boş kalır).
  const children = $derived.by(() => {
    if (monitor?.type !== 'group') return [];
    const ids = Array.isArray(monitor.config?.monitor_ids) ? (monitor.config.monitor_ids as number[]) : [];
    return ids.map((cid) => ({ id: cid, m: live.byId(cid) }));
  });
  const groupMode = $derived(
    monitor?.type === 'group' ? GROUP_MODES.find((g) => g.v === monitor.config?.mode)?.l ?? GROUP_MODES[0].l : '',
  );
  const now = $derived(clock.now);
  const isHttps = $derived(monitor?.type === 'http' && /^https:/i.test(monitor.target));
  // Sertifika bilgisi HTTPS dışında da gelebilir (TLS sertifikası, gRPC/SMTP/WebSocket TLS, tarayıcı).
  const showCert = $derived(
    !!monitor && (isHttps || monitor.type === 'tlscert' || !!monitor.cert_expires_at),
  );

  // Konumlar: yalnızca çok konumlu monitörde dolu gelir.
  let locations = $state.raw<MonitorLocations | null>(null);
  const LOC_STATE: Record<LocationState, { l: TKey; c: string }> = {
    up: { l: 'status.up', c: 'up' },
    down: { l: 'status.down', c: 'down' },
    retrying: { l: 'status.retrying', c: 'pending' },
    waiting: { l: 'monitors.detail.waitingFirst', c: 'waiting' },
    unknown: { l: 'monitors.detail.noResult', c: 'paused' },
  };
  async function loadLocations() {
    if (!ready || notFound) return;
    try {
      locations = await api.monitorLocations(id);
    } catch {
      /* konum bilgisi zorunlu değil */
    }
  }
  let locTimer: ReturnType<typeof setTimeout> | undefined;
  const loadLocationsSoon = () => {
    clearTimeout(locTimer);
    locTimer = setTimeout(loadLocations, 800);
  };
  const pushUrl = $derived(monitor?.push_token ? `${location.origin}/api/push/${monitor.push_token}` : '');

  // İlk yüklemede önce monitörün kendisi istenir: yoksa (404) grafik, konum ve
  // olay istekleri hiç gönderilmez ve "bulunamadı" kartının yanında ayrıca hata
  // bildirimi çıkmaz.
  let ready = $state(false);
  async function loadDetail() {
    if (notFound) return;
    try {
      if (!detail) {
        detail = await api.monitor(id);
        if (!ready) {
          ready = true;
          loadLocations();
        }
        incidents = await api.monitorIncidents(id);
      } else {
        const [d, inc] = await Promise.all([api.monitor(id), api.monitorIncidents(id)]);
        detail = d;
        incidents = inc;
      }
      loadError = '';
    } catch (e) {
      if (e instanceof ApiError && e.status === 404) notFound = true;
      else if (!detail) loadError = errorMessage(e);
    }
  }

  let seriesReq = 0;
  async function loadSeries(r: SeriesRange) {
    if (!ready || notFound) return;
    const req = ++seriesReq;
    seriesLoading = true;
    try {
      const s = await api.series(id, r);
      if (req !== seriesReq) return;
      series = s;
      chartTo = nowSec();
    } catch (e) {
      if (req !== seriesReq) return;
      if (e instanceof ApiError && e.status === 404) notFound = true;
      else toast.error(errorMessage(e));
    } finally {
      if (req === seriesReq) seriesLoading = false;
    }
  }

  $effect(() => {
    if (ready && !notFound) loadSeries(range);
  });

  let reloadTimer: ReturnType<typeof setTimeout> | undefined;
  let refreshTimer: ReturnType<typeof setInterval> | undefined;
  let unsub: (() => void) | undefined;
  let lastStatus: number | null = null;

  let unsubProbe: (() => void) | undefined;
  let unsubLocations: (() => void) | undefined;
  let unsubReset: (() => void) | undefined;
  let unsubResume: (() => void) | undefined;

  onMount(() => {
    loadDetail();
    refreshTimer = setInterval(() => {
      loadDetail();
      loadLocations();
    }, 60_000);
    unsubProbe = live.onProbe(() => {
      if (locations?.locations.length) loadLocationsSoon();
    });
    // Konum durumu değişti (ör. yeni konumun ilk sonucu geldi): kayıt yazılmasa da yenile.
    unsubLocations = live.onLocations((mid) => {
      if (mid === id) loadLocationsSoon();
    });
    // İstatistikler sıfırlandı (listeden veya başka sekmeden): uptime, grafik ve olaylar yeniden yüklenir.
    unsubReset = live.onStatsReset((mid) => {
      if (mid !== id) return;
      loadDetail();
      loadSeries(range);
    });
    // Bağlantı yeniden kuruldu / uygulamaya geri dönüldü: arada kaçan kontroller için
    // grafik ve ayrıntılar baştan yüklenir.
    unsubResume = live.onResume(() => {
      loadDetail();
      loadLocations();
      loadSeries(range);
    });
    unsub = live.onBeat((b) => {
      if (b.monitor_id !== id) return;
      if (locations?.locations.length) loadLocationsSoon();
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
    unsubProbe?.();
    unsubLocations?.();
    unsubReset?.();
    unsubResume?.();
    clearTimeout(locTimer);
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


  const statusSub = $derived.by(() => {
    if (!monitor) return '';
    switch (kind) {
      case 'paused':
        return t('monitors.detail.pausedSub');
      case 'maintenance':
        return t('monitors.detail.maintSub');
      case 'pending':
        // Başlatma/düzenleme sonrası sunucu son mesajı temizler: henüz sonuç yok.
        return monitor.last_message ? t('status.retrying') : t('monitors.firstCheck');
      case 'up':
        return monitor.last_change_at ? t('monitors.detail.upFor', { d: fmtDuration(now - monitor.last_change_at) }) : '';
      case 'down':
        return monitor.last_change_at ? t('monitors.detail.downFor', { d: fmtDuration(now - monitor.last_change_at) }) : '';
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
  const curlDown = $derived(pushUrl ? `curl -fsS -m 10 "${pushUrl}?status=down&msg=${t('monitors.detail.pushFailMsg')}"` : '');
  // Grup hedefi ("3 monitör") geçerli dilde.
  const groupTargetText = $derived(monitor?.type === 'group' ? groupTarget(monitor.config, monitor.target) : '');
</script>

{#if notFound}
  <div class="card empty">
    <h3>{t('monitors.detail.notFound')}</h3>
    <p>{t('monitors.detail.notFoundText')}</p>
    <a class="btn primary" href="#/">{t('nav.backToMonitors')}</a>
  </div>
{:else if !monitor}
  {#if loadError}
    <div class="card empty">
      <h3>{t('monitors.detail.loadFailed')}</h3>
      <p>{loadError}</p>
      <button class="btn primary" onclick={loadDetail}>{t('common.retry')}</button>
    </div>
  {:else}
    <div class="skeleton" style="height:90px;margin-bottom:20px"></div>
    <div class="skeleton" style="height:300px"></div>
  {/if}
{:else}
  <a class="back" href="#/"><Icon name="chevron-left" size={16} /> {t('nav.monitors')}</a>

  <div class="head">
    <div class="title">
      <StatusIcon {kind} size={44} pulse={kind === 'up' || kind === 'down'} />
      <div class="tt">
        <div class="name-row">
          <h1>{monitor.name}</h1>
          <TypeBadge type={monitor.type} />
          {#if monitor.open_partial_incident_id && kind !== 'down' && kind !== 'paused'}
            <!-- Konum kesintisi: durum "Çalışıyor" kalır, uptime etkilenmez. -->
            <a class="badge pending loc-out" href="#/incidents/{monitor.open_partial_incident_id}" title={t('monitors.row.locOutageTitle')}
              ><Icon name="map-pin" size={12} /> {t('incidents.kind.partialLong')}</a
            >
          {/if}
        </div>
        <div class="target">
          {#if monitor.type === 'push'}
            {#if typeof monitor.config?.grace_sec === 'number' && monitor.config.grace_sec > 0}
              <span class="muted">{t('monitors.detail.pushTargetGrace', { interval: fmtInterval(monitor.interval), grace: fmtInterval(monitor.config.grace_sec) })}</span>
            {:else}
              <span class="muted">{t('monitors.detail.pushTarget', { interval: fmtInterval(monitor.interval) })}</span>
            {/if}
          {:else if !monitor.target}
            <span class="muted">{t('monitors.detail.typeMonitor', { type: typeName(monitor.type) })}</span>
          {:else if isWebTarget(monitor.type) && /^https?:\/\//i.test(monitor.target)}
            <a href={monitor.target} target="_blank" rel="noopener noreferrer">{monitor.target}<Icon name="external" size={13} /></a>
          {:else if monitor.type === 'group'}
            <span class="muted">{t('monitors.detail.group')} · {groupTargetText}{groupMode ? ` · ${groupMode}` : ''}</span>
          {:else}
            <span class="text-2 mono">{displayTarget(monitor.target)}</span>
          {/if}
        </div>
        {#if monitor.tags?.length}
          <div class="dtags" aria-label={t('monitors.tags')}>
            {#each monitor.tags as tg (tg.id)}<TagChip name={tg.name} color={tg.color} value={tg.value} />{/each}
          </div>
        {/if}
      </div>
    </div>
    {#if session.canEdit}
      <div class="actions">
        <button class="btn" onclick={onToggle} disabled={busy}>
          <Icon name={monitor.active ? 'pause' : 'play'} size={15} />
          {monitor.active ? t('monitors.pause') : t('monitors.resume')}
        </button>
        <a class="btn" href="#/monitors/{monitor.id}/edit"><Icon name="edit" size={15} /> {t('common.edit')}</a>
        <button class="btn danger" onclick={onDelete}><Icon name="trash" size={15} /> {t('common.delete')}</button>
      </div>
    {/if}
  </div>

  {#if kind === 'maintenance'}
    <div class="alert maint maint-note">
      <Icon name="wrench" size={16} />
      <span>{t('monitors.detail.maintNote')}
        <a href="#/maintenance">{t('nav.titles.maintenanceWindows')}</a></span>
    </div>
  {/if}

  {#if monitor.description}
    <p class="desc">{monitor.description}</p>
  {/if}

  <div class="stats">
    <div class="card stat">
      <div class="label">{t('monitors.list.currentStatus')}</div>
      <div class="value"><span class="pill {kind}">{STATUS_LABELS[kind]}</span></div>
      <div class="sub">{statusSub}</div>
    </div>
    <div class="card stat">
      <div class="label">{t('monitors.detail.lastCheck')}</div>
      <div class="value">{monitor.last_check_at ? fmtRelative(monitor.last_check_at, now) : '—'}</div>
      <div class="sub" title={monitor.last_message}>
        {#if monitor.last_message}
          <span class:c-down={kind === 'down'} class:c-pending={kind === 'pending'}>{monitor.last_message}</span> ·
        {/if}
        {t('monitors.detail.every', { interval: fmtInterval(monitor.interval) })}
      </div>
    </div>
    {#if monitor.type === 'group'}
      <div class="card stat">
        <div class="label">{t('monitors.detail.children')}</div>
        <div class="value">{groupTargetText}</div>
        <div class="sub">{groupMode || t('monitors.detail.childrenNote')}</div>
      </div>
    {:else}
      <div class="card stat">
        <div class="label">{t('monitors.detail.avgResponse')}</div>
        <div class="value">{fmtMs(detail?.avg_ping_24h)}</div>
        <div class="sub">
          {t('monitors.detail.lastMeasure', { v: monitor.last_check_at && kind === 'up' ? fmtMs(monitor.last_ping_ms) : '—' })}
        </div>
      </div>
    {/if}
    {#if showCert}
      <div class="card stat">
        <div class="label"><Icon name="lock" size={13} /> {t('monitors.detail.sslCert')}</div>
        {#if cert}
          <div class="value {cert.cls}">{cert.days < 0 ? t('monitors.detail.expired') : t('monitors.detail.daysLeft', { count: cert.days })}</div>
          <div class="sub">
            {t('monitors.detail.expiresOn', { date: fmtDay(monitor.cert_expires_at) })}{monitor.cert_issuer ? ` · ${monitor.cert_issuer}` : ''}
          </div>
        {:else}
          <div class="value muted">—</div>
          <div class="sub">{t('monitors.detail.noInfo')}</div>
        {/if}
      </div>
    {/if}
  </div>

  <div class="card uptimes">
    {#each RANGES as r (r.key)}
      {@const v = detail?.uptime[r.key] ?? null}
      <div>
        <div class="label">{t('monitors.detail.lastRange', { range: t(r.label) })}</div>
        <div class="big {({ good: 'c-up', warn: 'c-warn', bad: 'c-down', none: 'muted' })[uptimeTone(v)]}">{fmtPct(v)}</div>
      </div>
    {/each}
  </div>

  {#if monitor.type === 'push' && pushUrl}
    <div class="card block">
      <h2 class="card-title">{t('monitors.detail.pushTitle')}<span class="dot">.</span></h2>
      <p class="text-2 small intro">
        {#each tParts('monitors.detail.pushIntro', { interval: fmtInterval(monitor.interval) }) as p, i (i)}{#if p.slot === 'down'}<b
              >{t('monitors.detail.pushDown')}</b
            >{:else}{p.text}{/if}{/each}
      </p>
      <div class="copybox">
        <code>{pushUrl}</code>
        <CopyButton text={pushUrl} />
      </div>
      <div class="label mt">{t('monitors.detail.pushExample')}</div>
      <div class="copybox">
        <code>{curlUp}</code>
        <CopyButton text={curlUp} />
      </div>
      <div class="label mt">{t('monitors.detail.pushFail')}</div>
      <div class="copybox">
        <code>{curlDown}</code>
        <CopyButton text={curlDown} />
      </div>
      <p class="help mt">
        {#each tParts('monitors.detail.pushParams') as p, i (i)}{#if p.slot === 'status'}<code>status=up|down</code>{:else if p.slot === 'msg'}<code
              >msg=</code
            >{:else if p.slot === 'ping'}<code>ping=</code>{:else}{p.text}{/if}{/each}
      </p>
    </div>
  {/if}

  {#if locations && locations.locations.length > 0}
    <div class="card block">
      <div class="loc-head">
        <h2 class="card-title">{t('monitors.detail.locations')}<span class="dot">.</span></h2>
        <span class="muted small">
          {locations.down_when === 'all'
            ? t('monitors.detail.downAll')
            : locations.down_when === 'majority'
              ? t('monitors.detail.downMajority')
              : t('monitors.detail.downAny')}
        </span>
      </div>
      <ul class="locs">
        {#each locations.locations as l (l.probe_id)}
          {@const st = LOC_STATE[l.status] ?? LOC_STATE.unknown}
          <li class="loc {st.c}" title={l.status === 'down' || l.status === 'retrying' ? l.message || t(st.l) : t(st.l)}>
            <span class="ldot" aria-hidden="true"></span>
            <span class="lt">
              <span class="ln">{l.name}</span>
              <span class="ls">
                {[t(st.l), l.status === 'up' && l.ping_ms >= 0 ? fmtMs(l.ping_ms) : '', l.last_check_at ? fmtRelative(l.last_check_at, now) : '']
                  .filter(Boolean)
                  .join(' · ')}
              </span>
              {#if l.message && (l.status === 'down' || l.status === 'retrying')}<span class="lm">{l.message}</span>{/if}
            </span>
          </li>
        {/each}
      </ul>
    </div>
  {/if}

  <!-- Grup monitörünün yanıt süresi yoktur (alt monitörlerin durumundan hesaplanır):
       boş bir yanıt süresi grafiği yerine alt monitörler listelenir. -->
  {#if monitor.type !== 'group'}
  <div class="card block">
    <div class="chart-head">
      <h2 class="card-title">{t('monitors.detail.responseTime')}<span class="dot">.</span></h2>
      <div class="tabs" role="tablist">
        {#each RANGES as r (r.key)}
          <button role="tab" aria-selected={range === r.key} class:active={range === r.key} onclick={() => (range = r.key)}>
            {t(r.label)}
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
  {/if}

  {#if monitor.type === 'group' && children.length}
    <div class="card block">
      <h2 class="card-title">{t('monitors.detail.children')}<span class="dot">.</span></h2>
      <ul class="kids">
        {#each children as c (c.id)}
          <li>
            {#if c.m}
              <StatusIcon kind={monitorKind(c.m)} size={22} />
              <span class="kid">
                <a href="#/monitors/{c.id}">{c.m.name}</a>
                <span class="muted small kid-t">{c.m.type === 'group' ? groupTarget(c.m.config, c.m.target) : c.m.target}</span>
              </span>
            {:else}
              <span class="muted small">{t('monitors.detail.childDeleted', { id: c.id })}</span>
            {/if}
          </li>
        {/each}
      </ul>
      <p class="help kids-help">{t('monitors.childrenHelp')}</p>
    </div>
  {/if}

  <div class="card block">
    <h2 class="card-title">{t('nav.incidents')}<span class="dot">.</span></h2>
    {#if detail?.open_incident_since}
      <div class="alert error ongoing">
        {t('monitors.detail.ongoing', { d: fmtDuration(now - detail.open_incident_since), date: fmtDate(detail.open_incident_since) })}
        {#if detail.open_incident_id}
          <a class="inc-link" href="#/incidents/{detail.open_incident_id}"
            >{t('monitors.detail.incidentDetails')} <Icon name="chevron-right" size={14} /></a
          >
        {/if}
      </div>
    {/if}
    <IncidentTable {incidents} {now} emptyText={t('monitors.detail.noIncidents')} />
  </div>

  <div class="card block badges">
    <button type="button" class="bb-toggle" aria-expanded={badgesOpen} onclick={() => (badgesOpen = !badgesOpen)}>
      <span class="bb-t"><Icon name="award" size={17} /> {t('monitors.detail.badges')}</span>
      <span class="chev" class:open={badgesOpen}><Icon name="chevron-down" /></span>
    </button>
    {#if badgesOpen}
      <div class="bb-body"><BadgeBuilder id={monitor.id} https={showCert} /></div>
    {/if}
  </div>
{/if}

<style>
  .loc-out {
    gap: 4px;
    text-decoration: none;
  }
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
  .dtags {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    margin-top: 8px;
  }
  .loc-head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 6px 12px;
    flex-wrap: wrap;
    margin-bottom: 12px;
  }
  .loc-head .card-title {
    margin: 0;
  }
  .locs {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    /* "Çalışıyor · 259 ms · 59 dk 59 sn önce" tek satıra sığsın. */
    grid-template-columns: repeat(auto-fill, minmax(min(300px, 100%), 1fr));
    gap: 10px;
  }
  .loc {
    display: flex;
    align-items: flex-start;
    gap: 10px;
    padding: 10px 12px;
    border-radius: var(--radius-sm);
    border: 1px solid var(--border);
    background: var(--input);
    min-width: 0;
  }
  .loc.down {
    border-color: var(--down-border);
    background: var(--down-soft);
  }
  .loc.pending {
    border-color: var(--pending-border);
  }
  .ldot {
    width: 10px;
    height: 10px;
    border-radius: 50%;
    margin-top: 5px;
    flex-shrink: 0;
    background: var(--paused);
  }
  .loc.up .ldot {
    background: var(--up);
    box-shadow: 0 0 0 3px var(--up-ring);
  }
  .loc.down .ldot {
    background: var(--down);
  }
  .loc.pending .ldot {
    background: var(--pending);
  }
  /* İlk sonuç bekleniyor: nötr (gri) nokta, hafif nabız. */
  .loc.waiting .ldot {
    animation: loc-wait 1.6s ease-in-out infinite;
  }
  @keyframes loc-wait {
    50% {
      opacity: 0.35;
    }
  }
  .lt {
    display: flex;
    flex-direction: column;
    min-width: 0;
    line-height: 1.35;
  }
  .ln {
    font-weight: 700;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .ls {
    font-size: 0.82rem;
    color: var(--text-2);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .loc.down .ls {
    color: var(--down-text);
  }
  .lm {
    font-size: 0.78rem;
    color: var(--muted);
    overflow: hidden;
    text-overflow: ellipsis;
    display: -webkit-box;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    -webkit-box-orient: vertical;
    word-break: break-word;
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
  .inc-link {
    display: inline-flex;
    align-items: center;
    gap: 2px;
    color: inherit;
    font-weight: 700;
    text-decoration: underline;
    white-space: nowrap;
  }
  .ongoing {
    margin-bottom: 12px;
  }
  .maint-note {
    display: flex;
    align-items: flex-start;
    gap: 8px;
    margin: -6px 0 16px;
  }
  .maint-note :global(svg) {
    flex-shrink: 0;
    margin-top: 2px;
  }
  .maint-note a {
    color: inherit;
    text-decoration: underline;
  }
  .kids {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
    gap: 8px 16px;
  }
  .kids li {
    display: flex;
    align-items: center;
    gap: 10px;
    min-width: 0;
  }
  .kid {
    display: flex;
    flex-direction: column;
    min-width: 0;
    line-height: 1.35;
  }
  .kids a {
    color: var(--text);
    font-weight: 600;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .kid-t {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    min-width: 0;
  }
  .kids-help {
    margin: 12px 0 0;
  }
  .badges {
    padding: 0;
  }
  .bb-toggle {
    width: 100%;
    display: flex;
    align-items: center;
    justify-content: space-between;
    background: none;
    border: none;
    color: var(--text);
    font: inherit;
    font-weight: 700;
    font-size: 1.05rem;
    padding: 18px 20px;
    cursor: pointer;
    border-radius: var(--radius);
  }
  .bb-t {
    display: inline-flex;
    align-items: center;
    gap: 8px;
  }
  .bb-t :global(svg) {
    color: var(--accent-text);
  }
  .chev {
    display: inline-flex;
    color: var(--muted);
    transition: transform 0.2s;
  }
  .chev.open {
    transform: rotate(180deg);
  }
  .bb-body {
    padding: 0 20px 20px;
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
    .bb-toggle {
      padding: 16px;
    }
    .bb-body {
      padding: 0 16px 16px;
    }
  }
</style>
