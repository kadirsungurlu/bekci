<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import {
    api,
    ApiError,
    errorMessage,
    type AlertRule,
    type NotificationChannel,
    type ServerDetail,
    type ServerEvent,
    type ServerView,
    type StatsPoint,
    type StatsRange,
    type StatsSeries,
  } from '../lib/api';
  import { live } from '../lib/live.svelte';
  import { session } from '../lib/session.svelte';
  import { clock, toast } from '../lib/ui.svelte';
  import {
    collator,
    fmtBytes,
    fmtDate,
    fmtDec,
    fmtDuration,
    fmtPctInt,
    fmtRate,
    fmtRelative,
    fmtTemp,
    fmtUptime,
    nowSec,
  } from '../lib/format';
  import {
    STATE_LABELS,
    diskPct,
    fmtMetric,
    inactiveReason,
    memPct,
    metricLabel,
    pointFromStats,
    serverTone,
    servers,
    swapPct,
    usageLevel,
  } from '../lib/servers.svelte';
  import { NOTIFY_LABELS } from '../lib/notifyTypes';
  import LineChart, { type ChartSeries } from '../components/LineChart.svelte';
  import UsageBar from '../components/UsageBar.svelte';
  import ContainerTable from '../components/ContainerTable.svelte';
  import AlertRulesEditor from '../components/AlertRulesEditor.svelte';
  import ServerSetupModal from '../components/ServerSetupModal.svelte';
  import ServerSettingsModal from '../components/ServerSettingsModal.svelte';
  import Icon from '../components/Icon.svelte';

  let { id }: { id: number } = $props();

  let detail = $state.raw<ServerDetail | null>(null);
  let notFound = $state(false);
  let loadError = $state('');
  let events = $state.raw<ServerEvent[]>([]);
  let channels = $state.raw<NotificationChannel[] | null>(null);

  const RANGES: { key: StatsRange; label: string; sec: number }[] = [
    { key: '1h', label: '1 sa', sec: 3600 },
    { key: '24h', label: '24 sa', sec: 86400 },
    { key: '7d', label: '7 gün', sec: 7 * 86400 },
    { key: '30d', label: '30 gün', sec: 30 * 86400 },
  ];
  let range = $state<StatsRange>('24h');
  let stats = $state.raw<StatsSeries | null>(null);
  let statsLoading = $state(false);
  let statsError = $state('');

  // Liste biçimindeki canlı güncelleme detayın üstüne yazılır (containers/temps
  // listede boş geldiği için latest yalnızca tam yüklemede değişir).
  const view = $derived.by<ServerView | null>(() => {
    const d = detail;
    const l = servers.byId(id);
    if (!d) return l ?? null;
    if (!l || l.last_seen_at < d.last_seen_at) return d;
    return { ...d, ...l, latest: d.latest, host: l.host ?? d.host };
  });
  const tone = $derived(view ? serverTone(view) : 'muted');
  const st = $derived(view?.latest ?? null);
  const host = $derived(view?.host ?? null);
  const now = $derived(clock.now);

  async function loadDetail() {
    try {
      const d = await api.getServer(id);
      detail = d;
      loadError = '';
      appendLive(d);
    } catch (e) {
      if (e instanceof ApiError && e.status === 404) notFound = true;
      else if (!detail) loadError = errorMessage(e);
    }
  }

  async function loadEvents() {
    try {
      events = await api.getServerEvents(id);
    } catch {
      /* geçmiş zorunlu değil */
    }
  }

  async function loadChannels() {
    try {
      channels = (await api.notifications()).slice().sort((a, b) => collator.compare(a.name, b.name));
    } catch {
      channels = [];
    }
  }

  let statsReq = 0;
  async function loadStats(r: StatsRange) {
    const req = ++statsReq;
    statsLoading = true;
    try {
      const s = await api.getServerStats(id, r);
      if (req !== statsReq) return;
      stats = s;
      statsError = '';
    } catch (e) {
      if (req === statsReq) statsError = errorMessage(e);
    } finally {
      if (req === statsReq) statsLoading = false;
    }
  }

  $effect(() => {
    loadStats(range);
  });

  /** 1 sa / 24 sa grafiğine yeni örneği ekler (sayfayı yenilemeden akar). */
  function appendLive(d: ServerDetail) {
    const s = stats;
    if (!s || !d.latest || !d.metrics_at || (s.range !== '1h' && s.range !== '24h')) return;
    const pts = s.points ?? [];
    const last = pts.length ? pts[pts.length - 1].t : 0;
    if (d.metrics_at <= last) return;
    const sec = RANGES.find((r) => r.key === s.range)!.sec;
    const to = Math.max(s.to, d.metrics_at);
    const next = [...pts.filter((p) => p.t >= to - sec), pointFromStats(d.metrics_at, d.latest, d.temp_max)];
    stats = { ...s, points: next, to, from: to - sec };
  }

  let reloadTimer: ReturnType<typeof setTimeout> | undefined;
  let refreshTimer: ReturnType<typeof setInterval> | undefined;
  let unsub: (() => void) | undefined;
  let lastFiring = '';

  onMount(() => {
    servers.ensure();
    loadDetail();
    loadEvents();
    if (session.canEdit) loadChannels();
    // Yeni örnek canlı akıştan duyurulur; tam veri (konteynerler, sıcaklıklar) kısa gecikmeyle çekilir.
    unsub = live.onServer((v) => {
      if (v.id !== id) return;
      clearTimeout(reloadTimer);
      reloadTimer = setTimeout(loadDetail, 800);
      const f = (v.firing ?? []).join(',');
      if (f !== lastFiring) {
        lastFiring = f;
        loadEvents();
      }
    });
    refreshTimer = setInterval(() => {
      loadDetail();
      loadEvents();
    }, 60_000);
  });

  onDestroy(() => {
    unsub?.();
    clearTimeout(reloadTimer);
    clearInterval(refreshTimer);
  });

  // Grafik verisi -------------------------------------------------------------------------

  const pts = $derived<StatsPoint[]>(stats?.points ?? []);
  const times = $derived(pts.map((p) => p.t));
  const selRange = $derived(RANGES.find((r) => r.key === (stats?.range ?? range))!);
  const chartTo = $derived(stats?.to || nowSec());
  const chartFrom = $derived(stats?.from || chartTo - selRange.sec);
  const chartInterval = $derived(stats?.interval || (stats?.res ?? 1) * 60);
  const col = <K extends keyof StatsPoint>(k: K) => pts.map((p) => (p[k] ?? null) as number | null);

  const pctFmt = (v: number) => `%${fmtDec(v, 1)}`;
  const cpuBand = $derived(
    pts.some((p) => p.cpu_max != null)
      ? { label: 'Tepe', color: 'var(--chart-1)', lo: col('cpu'), hi: pts.map((p) => p.cpu_max ?? p.cpu) }
      : null,
  );
  const memTop = $derived(Math.max(0, ...pts.map((p) => p.mem_total)) || undefined);
  const hasSwap = $derived(pts.some((p) => p.swap_total > 0));
  const memSeries = $derived<ChartSeries[]>([
    { label: 'Kullanılan', color: 'var(--chart-1)', values: col('mem_used'), stack: true },
    { label: 'Önbellek', color: 'var(--chart-3)', values: col('mem_cache'), stack: true },
    ...(hasSwap ? [{ label: 'Swap', color: 'var(--chart-2)', values: col('swap_used'), dashed: true }] : []),
  ]);
  const hasTemp = $derived(pts.some((p) => p.temp != null));

  // En çok CPU kullanan 5 konteyner (aralık ortalamasına göre).
  const topContainers = $derived.by<ChartSeries[]>(() => {
    const sum = new Map<string, number>();
    for (const p of pts) for (const c of p.containers ?? []) sum.set(c.name, (sum.get(c.name) ?? 0) + c.cpu);
    const top = [...sum.entries()]
      .sort((a, b) => b[1] - a[1])
      .slice(0, 5)
      .map(([n]) => n);
    const colors = ['var(--chart-1)', 'var(--chart-2)', 'var(--chart-3)', 'var(--chart-4)', 'var(--chart-5)'];
    return top.map((name, i) => ({
      label: name,
      color: colors[i],
      values: pts.map((p) => (p.containers ? (p.containers.find((c) => c.name === name)?.cpu ?? 0) : null)),
    }));
  });

  // Disk doluluğu: birden fazla bölüm varsa (ör. / ve /home) her biri ayrı
  // çizgi; en dolu 5 bölüm gösterilir. Eski kayıtlarda yalnızca en dolu bölüm var.
  const diskSeries = $derived.by<ChartSeries[]>(() => {
    const peak = new Map<string, number>();
    for (const p of pts) for (const d of p.disks ?? []) peak.set(d.mount, Math.max(peak.get(d.mount) ?? 0, d.pct));
    if (peak.size <= 1) return [{ label: 'Doluluk', color: 'var(--chart-3)', values: col('disk_pct'), fill: true }];
    const colors = ['var(--chart-1)', 'var(--chart-2)', 'var(--chart-3)', 'var(--chart-4)', 'var(--chart-5)'];
    return [...peak.entries()]
      .sort((a, b) => b[1] - a[1])
      .slice(0, 5)
      .map(([mount], i) => ({
        label: mount,
        color: colors[i],
        values: pts.map((p) => (p.disks ? (p.disks.find((d) => d.mount === mount)?.pct ?? null) : null)),
      }));
  });

  // Anlık değerler ------------------------------------------------------------------------

  const threads = $derived(host?.threads || host?.cores || 0);
  const worstDisk = $derived.by(() => {
    if (!st?.disks?.length) return null;
    return st.disks.reduce((a, b) => (b.total && b.used / b.total > (a.total ? a.used / a.total : -1) ? b : a));
  });
  const temps = $derived((st?.temps ?? []).slice().sort((a, b) => b.c - a.c));
  const tempMax = $derived(view?.temp_max ?? (temps.length ? temps[0].c : null));
  const disks = $derived((st?.disks ?? []).slice().sort((a, b) => collator.compare(a.mount, b.mount)));
  const containers = $derived(st?.containers ?? []);
  const firingRules = $derived((detail?.alerts ?? []).filter((r) => r.firing));

  function onAlertsSaved(rules: AlertRule[]) {
    if (detail) detail = { ...detail, alerts: rules };
  }

  // Bildirim kanalları --------------------------------------------------------------------
  let selected = $state<number[]>([]);
  let chSaving = $state(false);
  let chSynced = -1;
  $effect(() => {
    // Detay ilk geldiğinde (ve kayıttan sonra) seçimi sunucudakiyle eşitle.
    if (detail && chSynced !== detail.id) {
      chSynced = detail.id;
      selected = (detail.notification_ids ?? []).slice();
    }
  });
  const chDirty = $derived(
    !!detail && [...selected].sort((a, b) => a - b).join(',') !== [...(detail.notification_ids ?? [])].sort((a, b) => a - b).join(','),
  );
  function toggleCh(cid: number, on: boolean) {
    selected = on ? [...selected, cid] : selected.filter((x) => x !== cid);
  }
  async function saveChannels() {
    if (!detail) return;
    chSaving = true;
    try {
      await api.putServerNotifications(id, selected);
      detail = { ...detail, notification_ids: selected.slice() };
      toast.success('Bildirim kanalları kaydedildi');
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      chSaving = false;
    }
  }

  let renewOpen = $state(false);
  let settingsOpen = $state(false);

  /** Doluluk değerinin rengi: ≥90 kırmızı, ≥80 turuncu. */
  const lvl = (v: number | null) => ({ ok: '', warn: 'c-pending', danger: 'c-down' })[usageLevel(v)];

  const TONE_ICON = { up: 'server', warn: 'alert', down: 'server', muted: 'server' } as const;
  const PILL = { up: 'up', warn: 'pending', down: 'down', muted: 'paused' } as const;
</script>

{#if notFound}
  <div class="card empty">
    <h3>Sunucu bulunamadı</h3>
    <p>Bu ajan silinmiş olabilir.</p>
    <a class="btn primary" href="#/servers">Sunuculara dön</a>
  </div>
{:else if !view}
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
  <a class="back" href="#/servers"><Icon name="chevron-left" size={16} /> Sunucular</a>

  <div class="head">
    <div class="title">
      <span class="sicon {tone}" aria-hidden="true"><Icon name={TONE_ICON[tone]} size={22} /></span>
      <div class="tt">
        <h1>{view.name}</h1>
        <div class="hsub">
          <span class="pill {PILL[tone]}">{view.state === 'online' && view.firing?.length ? 'Uyarı var' : STATE_LABELS[view.state]}</span>
          {#if view.metrics_at}
            <span class="muted small" title={fmtDate(view.metrics_at)}>Son ölçüm {fmtRelative(view.metrics_at, now)}</span>
          {/if}
        </div>
        {#if host}
          <div class="hostline">{[host.hostname, host.platform, host.arch].filter(Boolean).join(' · ')}</div>
        {/if}
      </div>
    </div>
    {#if session.isAdmin}
      <div class="actions">
        <button class="btn" onclick={() => (renewOpen = true)}><Icon name="terminal" size={15} /> Kurulum komutu</button>
        <button class="btn" onclick={() => (settingsOpen = true)}><Icon name="settings" size={15} /> Ayarlar</button>
      </div>
    {/if}
  </div>

  {#if view.state === 'offline'}
    <div class="alert error note">
      <Icon name="wifi-off" size={16} />
      <span>
        Sunucudan {view.metrics_at ? `${fmtDuration(now - view.metrics_at)} süredir` : 'uzun süredir'} veri gelmiyor. Sunucu kapalı, ağ
        bağlantısı kopmuş veya ajan durmuş olabilir. Aşağıdaki değerler son ölçüme aittir.
      </span>
    </div>
  {:else if view.state !== 'online'}
    <div class="alert warning note">
      <Icon name="info" size={16} />
      <span>{inactiveReason(view)}{#if session.isAdmin && view.state !== 'disabled'} <button class="linkbtn inl" onclick={() => (renewOpen = true)}>Kurulum komutunu göster</button>{/if}</span>
    </div>
  {/if}

  {#if firingRules.length}
    <div class="alert warning note">
      <Icon name="alert" size={16} />
      <span>
        {#each firingRules as r, i (r.id)}
          {#if i > 0}<br />{/if}
          <b>{metricLabel(r.metric)}{r.mount ? ` (${r.mount})` : ''}</b>
          {#if r.metric === 'offline'}
            uyarısı sürüyor
          {:else}
            eşiği aşıldı (eşik {fmtMetric(r.metric, r.threshold)}{r.minutes ? `, ${r.minutes} dk ortalama` : ''})
          {/if}
          {#if r.fired_at}<span class="muted"> · {fmtRelative(r.fired_at, now)}</span>{/if}
        {/each}
      </span>
    </div>
  {/if}

  {#if st}
    <div class="tiles" class:dimmed={view.state === 'offline'}>
      <div class="card tile" class:fire={view.firing?.includes('cpu')}>
        <div class="label"><Icon name="cpu" size={13} /> CPU</div>
        <div class="value {lvl(st.cpu)}">{fmtPctInt(st.cpu)}</div>
        <UsageBar value={st.cpu} bare />
        <div class="sub">{threads ? `${threads} iş parçacığı` : ' '}</div>
      </div>
      <div class="card tile" class:fire={view.firing?.includes('mem')}>
        <div class="label"><Icon name="memory" size={13} /> RAM</div>
        <div class="value {lvl(memPct(st))}">{fmtPctInt(memPct(st))}</div>
        <UsageBar value={memPct(st)} bare />
        <div class="sub">{fmtBytes(st.mem_used)} / {fmtBytes(st.mem_total)}{st.swap_total ? ` · swap ${fmtPctInt(swapPct(st))}` : ''}</div>
      </div>
      <div class="card tile" class:fire={view.firing?.includes('disk')}>
        <div class="label"><Icon name="database" size={13} /> Disk</div>
        <div class="value {lvl(diskPct(st))}">{fmtPctInt(diskPct(st))}</div>
        <UsageBar value={diskPct(st)} bare />
        <div class="sub">{worstDisk ? `${worstDisk.mount} · ${fmtBytes(worstDisk.total - worstDisk.used)} boş` : '—'}</div>
      </div>
      <div class="card tile" class:fire={view.firing?.includes('load')}>
        <div class="label"><Icon name="activity" size={13} /> Yük</div>
        <div class="value">{fmtDec(st.load1, 2)}</div>
        <div class="sub">5 dk {fmtDec(st.load5, 2)} · 15 dk {fmtDec(st.load15, 2)}</div>
        <div class="sub">{threads ? `Çekirdek başına ${fmtDec(st.load1 / threads, 2)}` : ''}</div>
      </div>
      <div class="card tile">
        <div class="label"><Icon name="arrows-lr" size={13} /> Ağ</div>
        <div class="value net"><span><Icon name="arrow-down" size={14} />{fmtRate(st.net_rx_bps)}</span></div>
        <div class="sub"><Icon name="arrow-up" size={12} /> {fmtRate(st.net_tx_bps)} giden</div>
      </div>
      {#if tempMax !== null}
        <div class="card tile" class:fire={view.firing?.includes('temp')}>
          <div class="label"><Icon name="zap" size={13} /> Sıcaklık</div>
          <div class="value" class:c-pending={tempMax >= 80} class:c-down={tempMax >= 90}>{fmtTemp(tempMax)}</div>
          <div class="sub">{temps.length ? `En sıcak: ${temps[0].name}` : 'En sıcak sensör'}</div>
          {#if temps.length > 1}<div class="sub">{temps.length} sensör</div>{/if}
        </div>
      {/if}
    </div>
  {/if}

  {#if host}
    <section class="card facts-card">
      <dl class="facts">
        <div><dt>Host adı</dt><dd class="mono">{host.hostname || '—'}</dd></div>
        <div><dt>İşletim sistemi</dt><dd>{host.platform || host.os || '—'}</dd></div>
        <div><dt>Çekirdek sürümü</dt><dd class="mono">{host.kernel || '—'}</dd></div>
        <div><dt>Mimari</dt><dd>{host.arch || '—'}</dd></div>
        <div class="wide"><dt>İşlemci</dt><dd>{host.cpu_model || '—'}</dd></div>
        <div><dt>Çekirdek / iş parçacığı</dt><dd>{host.cores || '—'} / {host.threads || '—'}</dd></div>
        <div><dt>RAM</dt><dd>{fmtBytes(host.mem_total)}</dd></div>
        <div><dt>Çalışma süresi</dt><dd>{fmtUptime(st?.uptime ?? (host.boot_time ? now - host.boot_time : 0))}</dd></div>
        <div><dt>Ajan sürümü</dt><dd>{view.version || '—'}</dd></div>
        <div><dt>Son ölçüm</dt><dd title={view.metrics_at ? fmtDate(view.metrics_at) : ''}>{fmtRelative(view.metrics_at, now)}{view.interval ? ` · her ${view.interval} sn` : ''}</dd></div>
      </dl>
    </section>
  {/if}

  <!-- Grafikler -->
  <div class="chart-head">
    <h2 class="sect-title">Geçmiş<span class="dot">.</span></h2>
    <div class="tabs" role="tablist" aria-label="Zaman aralığı">
      {#each RANGES as r (r.key)}
        <button role="tab" aria-selected={range === r.key} class:active={range === r.key} onclick={() => (range = r.key)}>{r.label}</button>
      {/each}
    </div>
  </div>

  {#if statsError && !stats}
    <div class="alert error block">{statsError} <button class="linkbtn" onclick={() => loadStats(range)}>Tekrar dene</button></div>
  {:else if !stats}
    <div class="charts">
      {#each Array(4) as _, i (i)}<div class="skeleton" style="height:250px"></div>{/each}
    </div>
  {:else if pts.length === 0}
    <div class="card nopts" class:loading={statsLoading}>
      <Icon name="activity" size={22} />
      <span>{view.metrics_at ? 'Bu aralıkta ölçüm yok.' : 'Henüz ölçüm yok. Ajan metrik göndermeye başlayınca grafikler burada görünür.'}</span>
    </div>
  {:else}
    <div class="charts" class:loading={statsLoading}>
      <section class="card ch">
        <div class="ch-h"><h3>CPU</h3><span class="ch-u">%</span></div>
        <LineChart
          label="CPU kullanımı"
          {times}
          from={chartFrom}
          to={chartTo}
          interval={chartInterval}
          series={[{ label: cpuBand ? 'Ortalama' : 'CPU', color: 'var(--chart-1)', values: col('cpu'), fill: true }]}
          band={cpuBand}
          max={100}
          format={pctFmt}
          axisFormat={fmtPctInt}
        />
      </section>
      <section class="card ch">
        <div class="ch-h"><h3>Bellek</h3><span class="ch-u">{memTop ? `toplam ${fmtBytes(memTop)}` : ''}</span></div>
        <LineChart label="Bellek kullanımı" {times} from={chartFrom} to={chartTo} interval={chartInterval} series={memSeries} max={memTop} bytes format={fmtBytes} />
      </section>
      <section class="card ch">
        <div class="ch-h"><h3>Ağ</h3><span class="ch-u">bayt/sn</span></div>
        <LineChart
          label="Ağ trafiği"
          {times}
          from={chartFrom}
          to={chartTo}
          interval={chartInterval}
          series={[
            { label: 'Gelen', color: 'var(--chart-1)', values: col('net_rx_bps'), fill: true },
            { label: 'Giden', color: 'var(--chart-3)', values: col('net_tx_bps') },
          ]}
          bytes
          format={fmtRate}
          axisFormat={fmtBytes}
        />
      </section>
      <section class="card ch">
        <div class="ch-h"><h3>Disk G/Ç</h3><span class="ch-u">bayt/sn</span></div>
        <LineChart
          label="Disk okuma ve yazma"
          {times}
          from={chartFrom}
          to={chartTo}
          interval={chartInterval}
          series={[
            { label: 'Okuma', color: 'var(--chart-1)', values: col('disk_read_bps'), fill: true },
            { label: 'Yazma', color: 'var(--chart-2)', values: col('disk_write_bps') },
          ]}
          bytes
          format={fmtRate}
          axisFormat={fmtBytes}
        />
      </section>
      <section class="card ch">
        <div class="ch-h"><h3>Yük</h3><span class="ch-u">{threads ? `${threads} iş parçacığı` : ''}</span></div>
        <LineChart
          label="Sistem yükü"
          {times}
          from={chartFrom}
          to={chartTo}
          interval={chartInterval}
          series={[
            { label: '1 dk', color: 'var(--chart-1)', values: col('load1') },
            { label: '5 dk', color: 'var(--chart-3)', values: col('load5') },
            { label: '15 dk', color: 'var(--chart-4)', values: col('load15') },
          ]}
          minTop={1}
          format={(v) => fmtDec(v, 2)}
          axisFormat={(v) => fmtDec(v, v % 1 === 0 ? 0 : 1)}
        />
      </section>
      <section class="card ch">
        <div class="ch-h"><h3>Disk doluluğu</h3><span class="ch-u">{diskSeries.length > 1 ? 'bölüm başına' : 'en dolu bölüm'}</span></div>
        <LineChart
          label="Disk doluluğu"
          {times}
          from={chartFrom}
          to={chartTo}
          interval={chartInterval}
          series={diskSeries}
          max={100}
          format={pctFmt}
          axisFormat={fmtPctInt}
        />
      </section>
      {#if hasTemp}
        <section class="card ch">
          <div class="ch-h"><h3>Sıcaklık</h3><span class="ch-u">en sıcak sensör</span></div>
          <LineChart
            label="Sıcaklık"
            {times}
            from={chartFrom}
            to={chartTo}
            interval={chartInterval}
            series={[{ label: 'Sıcaklık', color: 'var(--chart-2)', values: col('temp') }]}
            minTop={50}
            format={fmtTemp}
            axisFormat={(v) => `${v}°`}
          />
        </section>
      {/if}
    </div>
  {/if}

  {#if disks.length}
    <section class="card block">
      <h2 class="card-title">Diskler<span class="dot">.</span></h2>
      <table class="table responsive disks">
        <thead>
          <tr><th>Bölüm</th><th>Aygıt</th><th class="w-bar">Doluluk</th><th>Boş</th></tr>
        </thead>
        <tbody>
          {#each disks as d (d.mount)}
            <tr>
              <td data-label="Bölüm" class="mono strong">{d.mount}</td>
              <td data-label="Aygıt" class="muted small">{d.device}{d.fs ? ` · ${d.fs}` : ''}</td>
              <td data-label="Doluluk" class="w-bar">
                <UsageBar value={d.total ? (100 * d.used) / d.total : null} detail="{fmtBytes(d.used)} / {fmtBytes(d.total)}" />
              </td>
              <td data-label="Boş" class="nowrap">{fmtBytes(d.total - d.used)}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    </section>
  {/if}

  {#if containers.length || view.container_count}
    <section class="card block">
      <div class="sec-h">
        <h2 class="card-title">Konteynerler<span class="dot">.</span></h2>
        <span class="muted small">{containers.length || view.container_count} çalışıyor</span>
      </div>
      {#if topContainers.length}
        <div class="top5">
          <div class="ch-h"><h3>En çok CPU kullanan 5 konteyner</h3><span class="ch-u">%</span></div>
          <LineChart
            label="En çok CPU kullanan konteynerler"
            {times}
            from={chartFrom}
            to={chartTo}
            interval={chartInterval}
            series={topContainers}
            minTop={5}
            format={pctFmt}
            axisFormat={fmtPctInt}
            height={180}
          />
        </div>
      {/if}
      {#if containers.length}
        <ContainerTable {containers} />
      {/if}
    </section>
  {:else if host && !host.docker && view.state === 'online'}
    <p class="help block">Docker bulunamadı veya ajan Docker soketine erişemiyor; konteyner bilgisi toplanmıyor.</p>
  {/if}

  <div class="two">
    <section class="card block">
      <h2 class="card-title">Uyarı kuralları<span class="dot">.</span></h2>
      {#if detail}
        <AlertRulesEditor
          serverId={id}
          rules={detail.alerts ?? []}
          canEdit={session.canEdit}
          mounts={(detail.latest?.disks ?? []).map((d) => d.mount)}
          onsaved={onAlertsSaved}
        />
      {:else}
        <div class="skeleton" style="height:120px"></div>
      {/if}
    </section>

    {#if session.canEdit}
      <section class="card block">
        <h2 class="card-title">Bildirim kanalları<span class="dot">.</span></h2>
        <p class="help intro">Bu sunucunun uyarıları seçili kanallara gönderilir.</p>
        {#if channels === null}
          <div class="skeleton" style="height:80px"></div>
        {:else if channels.length === 0}
          <p class="muted small nomargin">Henüz bildirim kanalı yok. <a href="#/notifications">Kanal ekleyin</a></p>
        {:else}
          <div class="chs">
            {#each channels as c (c.id)}
              <label class="check ch-item">
                <input type="checkbox" checked={selected.includes(c.id)} onchange={(e) => toggleCh(c.id, (e.currentTarget as HTMLInputElement).checked)} />
                <span>
                  {c.name}
                  <small>{NOTIFY_LABELS[c.type] ?? c.type}{c.active ? '' : ' · kapalı'}</small>
                </span>
              </label>
            {/each}
          </div>
          <div class="bar">
            <a class="small" href="#/notifications">Kanalları yönet</a>
            <div class="spacer"></div>
            <button type="button" class="btn sm primary" onclick={saveChannels} disabled={!chDirty || chSaving}>
              {#if chSaving}<span class="spinner"></span>{/if} Kaydet
            </button>
          </div>
        {/if}
      </section>
    {/if}
  </div>

  <section class="card block">
    <h2 class="card-title">Uyarı geçmişi<span class="dot">.</span></h2>
    {#if events.length === 0}
      <p class="muted nomargin">Son 90 günde uyarı yok.</p>
    {:else}
      <ul class="evs">
        {#each events as ev (ev.id)}
          <li class:open={!ev.ended_at}>
            <span class="edot" aria-hidden="true"></span>
            <span class="e1">
              <b>{metricLabel(ev.metric)}{ev.mount ? ` (${ev.mount})` : ''}</b>
              {#if ev.metric === 'offline'}
                <span class="text-2">veri gelmedi</span>
              {:else}
                {fmtMetric(ev.metric, ev.value)} <span class="muted">(eşik {fmtMetric(ev.metric, ev.threshold)})</span>
              {/if}
            </span>
            <span class="e2">
              <span title="Başladı">{fmtDate(ev.started_at)}</span>
              {#if ev.ended_at}
                <span class="muted">· {fmtDuration(ev.ended_at - ev.started_at)} sürdü</span>
              {:else}
                <span class="c-pending">· Sürüyor, {fmtDuration(now - ev.started_at)}</span>
              {/if}
            </span>
          </li>
        {/each}
      </ul>
    {/if}
  </section>


  {#if session.isAdmin}
    <ServerSetupModal bind:open={renewOpen} renew={view} />
    <ServerSettingsModal bind:open={settingsOpen} server={view} onsaved={loadDetail} />
  {/if}
{/if}

<style>
  .head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 16px;
    flex-wrap: wrap;
    margin-bottom: 18px;
  }
  .title {
    display: flex;
    align-items: center;
    gap: 14px;
    min-width: 0;
    flex: 1 1 360px;
  }
  .tt {
    min-width: 0;
  }
  h1 {
    word-break: break-word;
  }
  .hsub {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
    margin-top: 6px;
  }
  .hsub .pill {
    height: 24px;
    font-size: 0.8rem;
  }
  .hostline {
    margin-top: 4px;
    font-size: 0.85rem;
    color: var(--text-2);
    word-break: break-word;
  }
  .sicon {
    width: 46px;
    height: 46px;
    border-radius: 13px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    background: var(--paused-soft);
    color: var(--paused-text);
  }
  .sicon.up {
    background: var(--up-soft);
    color: var(--up);
  }
  .sicon.warn {
    background: var(--pending-soft);
    color: var(--pending);
  }
  .sicon.down {
    background: var(--down-soft);
    color: var(--down-text-2);
  }
  .actions {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
  }
  .note {
    display: flex;
    align-items: flex-start;
    gap: 9px;
    margin-bottom: 16px;
  }
  .note :global(svg) {
    flex-shrink: 0;
    margin-top: 2px;
  }
  .inl {
    color: inherit;
    text-decoration: underline;
    font-weight: 600;
  }

  .tiles {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
    gap: 14px;
    margin-bottom: 26px;
  }
  .tiles.dimmed .tile {
    opacity: 0.6;
  }
  .tile {
    display: flex;
    flex-direction: column;
    gap: 6px;
    min-width: 0;
    padding: 16px;
  }
  .tile.fire {
    border-color: var(--pending-border);
    box-shadow: inset 0 0 0 1px var(--pending-border);
  }
  .label {
    display: flex;
    align-items: center;
    gap: 5px;
    font-size: 0.75rem;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--muted);
  }
  .value {
    font-size: 1.45rem;
    font-weight: 700;
    line-height: 1.2;
    font-variant-numeric: tabular-nums;
  }
  .value.net {
    font-size: 1.15rem;
    padding: 3px 0 1px;
  }
  .value.net span {
    display: inline-flex;
    align-items: center;
    gap: 4px;
  }
  .value.net :global(svg) {
    color: var(--muted);
  }
  .sub {
    font-size: 0.8rem;
    color: var(--text-2);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    min-height: 1.2em;
  }
  .sub :global(svg) {
    vertical-align: -1px;
    color: var(--muted);
  }

  .chart-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    flex-wrap: wrap;
    margin-bottom: 12px;
  }
  .sect-title {
    font-size: 1.15rem;
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
    padding: 5px 14px;
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
  .charts {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 14px;
    margin-bottom: 16px;
    transition: opacity 0.15s;
  }
  .charts.loading {
    opacity: 0.55;
  }
  .nopts {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 10px;
    padding: 36px 20px;
    margin-bottom: 16px;
    color: var(--muted);
    text-align: center;
  }
  .nopts.loading {
    opacity: 0.55;
  }
  .ch {
    padding: 16px 16px 12px;
    min-width: 0;
  }
  .ch-h {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 8px;
    margin-bottom: 8px;
  }
  .ch-h h3 {
    font-size: 0.95rem;
  }
  .ch-u {
    font-size: 0.76rem;
    color: var(--muted);
    white-space: nowrap;
  }
  .block {
    margin-bottom: 16px;
  }
  .sec-h {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 10px;
    margin-bottom: 12px;
  }
  .sec-h .card-title {
    margin: 0;
  }
  .top5 {
    padding: 12px 14px 10px;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    margin-bottom: 16px;
  }
  .disks td {
    vertical-align: middle;
  }
  .w-bar {
    width: 38%;
  }
  .strong {
    font-weight: 600;
  }
  .two {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: 0;
  }
  .two > .block {
    min-width: 0;
  }
  .intro {
    margin: -8px 0 10px;
  }
  .nomargin {
    margin: 0;
  }
  .chs {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .ch-item span {
    min-width: 0;
    word-break: break-word;
  }
  .bar {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-top: 14px;
  }
  .spacer {
    flex: 1;
  }
  .evs {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .evs li {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr) auto;
    align-items: center;
    gap: 4px 12px;
    padding: 10px 0;
    border-bottom: 1px solid var(--border);
    font-size: 0.9rem;
  }
  .evs li:last-child {
    border-bottom: none;
  }
  .evs li:first-child {
    padding-top: 0;
  }
  .e1 b {
    margin-right: 6px;
  }
  .e2 {
    color: var(--text-2);
    font-size: 0.85rem;
    white-space: nowrap;
  }
  .edot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--paused);
  }
  .evs li.open .edot {
    background: var(--pending);
    box-shadow: 0 0 0 3px var(--pending-soft);
  }
  .facts-card {
    padding: 14px 18px;
    margin: -12px 0 26px;
  }
  .facts {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(160px, 1fr));
    gap: 10px 22px;
    margin: 0;
    font-size: 0.88rem;
  }
  .facts div {
    min-width: 0;
  }
  .facts .wide {
    grid-column: span 2;
  }
  .facts dt {
    font-size: 0.7rem;
    font-weight: 600;
    color: var(--muted);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  .facts dd {
    margin: 2px 0 0;
    word-break: break-word;
  }
  @media (max-width: 900px) {
    .charts {
      grid-template-columns: minmax(0, 1fr);
    }
  }
  @media (max-width: 640px) {
    .sicon {
      width: 40px;
      height: 40px;
    }
    .actions {
      width: 100%;
    }
    .actions > :global(*) {
      flex: 1;
    }
    .tiles {
      grid-template-columns: repeat(2, minmax(0, 1fr));
      gap: 10px;
    }
    .tile {
      padding: 12px;
    }
    .value {
      font-size: 1.2rem;
    }
    .value.net {
      font-size: 1rem;
    }
    .tabs {
      width: 100%;
    }
    .tabs button {
      flex: 1;
      padding: 5px 4px;
    }
    .ch {
      padding: 14px 12px 10px;
    }
    .top5 {
      padding: 10px 8px 8px;
    }
    .facts {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
    .w-bar {
      width: auto;
    }
    .disks :global(.ub) {
      flex: 1;
    }
    .evs li {
      grid-template-columns: auto minmax(0, 1fr);
    }
    .e2 {
      grid-column: 2;
      white-space: normal;
    }
  }
</style>
