<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import {
    api,
    ApiError,
    errorMessage,
    type CheckDetail,
    type HttpHeader,
    type IncidentCapture,
    type IncidentDetail,
    type IncidentEvent,
    type NotificationType,
    type ServerIncidentData,
    type ServerMetric,
    isServerIncident,
  } from '../lib/api';
  import { fmtMetric, metricLabel } from '../lib/servers.svelte';
  import { live } from '../lib/live.svelte';
  import { fmtDateSec, fmtDuration, fmtDurationLong, fmtTimeSec, fmtDay, fmtSize, nowSec } from '../lib/format';
  import { t, tOr, tParts } from '../lib/i18n';
  import { displayTarget, isWebTarget, typeName } from '../lib/monitorTypes';
  import { NOTIFY_LABELS, NOTIFY_STYLE } from '../lib/notifyTypes';
  import StatusIcon from '../components/StatusIcon.svelte';
  import TypeBadge from '../components/TypeBadge.svelte';
  import Icon, { type IconName } from '../components/Icon.svelte';
  import CopyButton from '../components/CopyButton.svelte';
  import ConnectionDetails from '../components/ConnectionDetails.svelte';

  let { id }: { id: number } = $props();

  let data = $state.raw<IncidentDetail | null>(null);
  let notFound = $state(false);
  let loadError = $state('');
  // Süre sürerken saniye saniye artsın (genel saat 5 sn'de bir ilerler).
  let now = $state(nowSec());

  async function load() {
    try {
      data = await api.incident(id);
      loadError = '';
    } catch (e) {
      if (e instanceof ApiError && e.status === 404) notFound = true;
      else if (!data) loadError = errorMessage(e);
    }
  }

  let tick: ReturnType<typeof setInterval> | undefined;
  let reloadTimer: ReturnType<typeof setTimeout> | undefined;
  let poll: ReturnType<typeof setInterval> | undefined;
  let unsub: (() => void) | undefined;
  let unsubLoc: (() => void) | undefined;
  let unsubSrv: (() => void) | undefined;
  let unsubResume: (() => void) | undefined;

  onMount(() => {
    load();
    // Bağlantı yeniden kuruldu / uygulamaya geri dönüldü: kaçan değişiklikler için yenile.
    unsubResume = live.onResume(load);
    tick = setInterval(() => (now = nowSec()), 1000);
    // Monitörün yeni sonucu olay geçmişini değiştirebilir (çözülme, hata değişimi).
    unsub = live.onBeat((b) => {
      if (!data || !data.incident.monitor_id || b.monitor_id !== data.incident.monitor_id || data.incident.resolved_at) return;
      clearTimeout(reloadTimer);
      reloadTimer = setTimeout(load, 1000);
    });
    // Kısmi kesinti konum değişimiyle, sunucu olayı sunucunun durumuyla değişir.
    unsubLoc = live.onLocations((mid) => {
      if (!data || mid !== data.incident.monitor_id || data.incident.resolved_at) return;
      clearTimeout(reloadTimer);
      reloadTimer = setTimeout(load, 1500);
    });
    unsubSrv = live.onServer((v) => {
      if (!data?.server || v.id !== data.server.id || data.incident.resolved_at) return;
      clearTimeout(reloadTimer);
      reloadTimer = setTimeout(load, 1000);
    });
    // Bildirim gönderim sonuçları arka planda gelir; süren olayda ara ara yenilenir.
    poll = setInterval(() => {
      if (data && !data.incident.resolved_at && !document.hidden) load();
    }, 30000);
  });
  onDestroy(() => {
    clearInterval(tick);
    clearInterval(poll);
    clearTimeout(reloadTimer);
    unsub?.();
    unsubLoc?.();
    unsubSrv?.();
    unsubResume?.();
  });

  const inc = $derived(data?.incident);
  const ongoing = $derived(!!inc && inc.resolved_at === 0);
  // Olay türü: kısmi kesinti (sarı) ve sunucu olayları ayrı gösterilir.
  const partial = $derived(inc?.kind === 'partial');
  const server = $derived(isServerIncident(inc?.kind));
  const offline = $derived(inc?.kind === 'server_offline');
  const sdata = $derived<ServerIncidentData | null>(server ? ((inc?.data ?? null) as ServerIncidentData | null) : null);
  const smetric = $derived((sdata?.metric ?? '') as ServerMetric);
  const affected = $derived.by<string[]>(() => {
    const l = partial ? inc?.data?.locations : null;
    return Array.isArray(l) ? l.filter((x): x is string => typeof x === 'string') : [];
  });
  const tone = $derived(ongoing ? (partial ? 'pending' : 'down') : 'up');
  const duration = $derived(inc ? (ongoing ? Math.max(now, inc.started_at) : inc.resolved_at) - inc.started_at : 0);
  const isHttp = $derived(data?.monitor.type === 'http');
  // Yakalanan konumlar: çok konumlu olayda çalışmayan her konumun kaydı (ilki
  // öncelikli olan); eski sunucu yalnızca capture döner.
  const captures = $derived<IncidentCapture[]>(data?.captures ?? (data?.capture ? [data.capture] : []));
  let capIdx = $state(0);
  const capture = $derived<IncidentCapture | null>(captures[capIdx] ?? captures[0] ?? null);
  const detail = $derived<CheckDetail | null>(capture?.detail ?? null);
  // HTTP isteği/yanıtı mı, bağlantı tanısı mı? Eski HTTP kayıtlarında kind yok.
  const isDiag = $derived(!!detail && (!!detail.diag || (!detail.method && !isHttp)));
  const showSide = $derived(!!data && data.details && (isHttp || captures.length > 0));

  // Kök neden: çok konumluda birleşik mesaj ("A: …; B: …") yerine ilk çalışmayan
  // konumun kendi hatası; konum adı altta ayrıca yazılır.
  const rootCause = $derived.by(() => {
    if (!data) return '';
    const down = data.locations.find((l) => l.status === 'down' && l.message);
    return data.locations.length > 1 && down ? down.message : data.incident.cause;
  });

  /** "HTTP 400 Bad Request" → "400 Bad Request" (UptimeRobot gibi). */
  function causeTitle(c: string): string {
    const m = c.match(/^HTTP (\d{3}\b.*)$/);
    return m ? m[1] : c || t('incidents.detail.noCause');
  }

  // İşlem geçmişi --------------------------------------------------------------------

  type Tone = 'down' | 'up' | 'pending' | 'maint' | 'accent' | 'muted';
  interface Row {
    ev: IncidentEvent;
    icon: IconName;
    tone: Tone;
    color?: string;
    title: string;
    sub: string;
    note: string;
    /** Bağlı olay (kısmi ↔ tam kesinti). */
    href?: string;
  }

  // Kısmi olayın kapanış mesajı başlıkla aynıysa tekrar yazılmaz.
  const PARTIAL_RESOLVED_MSG = ['Tüm konumlar çalışıyor', 'All locations are up'];

  const num = (v: unknown) => (typeof v === 'number' ? v : 0);
  const str = (v: unknown) => (typeof v === 'string' ? v : '');

  const NOTIFY_EVENT: Record<string, 'notifyDown' | 'notifyUp' | 'notifyReminder' | 'notifyServerAlert'> = {
    down: 'notifyDown',
    up: 'notifyUp',
    reminder: 'notifyReminder',
    server_alert: 'notifyServerAlert',
    server_resolved: 'notifyUp',
  };
  const LOC_TONE: Record<string, Tone> = { up: 'up', down: 'down', retrying: 'pending', unknown: 'muted' };

  // Sunucu konum adlarını Türkçe saklar: sabit "Ana sunucu" ve "…, X ve 3 diğer" eki
  // dile göre yazılır (kullanıcının verdiği kontrol noktası adları olduğu gibi kalır).
  const LOCAL_NAME = 'Ana sunucu';
  function locName(s: string): string {
    if (!s) return s;
    const m = s.match(/^(.*) ve (\d+) diğer$/);
    const names = (m ? m[1] : s)
      .split(', ')
      .map((n) => (n === LOCAL_NAME ? t('incidents.localName') : n))
      .join(', ');
    return m ? t('incidents.andOthers', { names, n: Number(m[2]) }) : names;
  }

  // Eski kayıtlarda (data'sız) düzenleme kaydının olayı kapatıp kapatmadığı saklı metinden anlaşılır.
  const EDITED_CLOSED_TR = 'Monitörün hedefi değiştirildi; olay kapatıldı';

  function row(ev: IncidentEvent): Row {
    const d = ev.data ?? {};
    const r: Row = { ev, icon: 'info', tone: 'muted', title: ev.message, sub: '', note: '' };
    switch (ev.kind) {
      case 'retry': {
        const att = num(d.attempt);
        r.icon = 'refresh';
        r.tone = 'pending';
        r.title = att ? t('incidents.ev.retry', { attempt: att, max: num(d.max) }) : t('incidents.ev.retryLocations');
        r.sub = ev.message;
        r.note = locName(ev.location);
        break;
      }
      case 'down':
        r.icon = server ? (offline ? 'wifi-off' : 'alert') : 'zap';
        r.tone = partial ? 'pending' : 'down';
        r.title = partial
          ? t('incidents.ev.partialStarted')
          : server
            ? t(offline ? 'incidents.ev.serverOffline' : 'incidents.ev.serverAlert')
            : t('incidents.ev.started');
        r.sub = offline ? '' : ev.message;
        r.note = ev.location ? t('incidents.ev.confirmedBy', { where: locName(ev.location) }) : '';
        break;
      case 'escalated':
      case 'from_partial': {
        r.icon = ev.kind === 'escalated' ? 'zap' : 'map-pin';
        r.tone = ev.kind === 'escalated' ? 'down' : 'pending';
        r.title = t(ev.kind === 'escalated' ? 'incidents.ev.escalated' : 'incidents.ev.fromPartial');
        const other = num(d.incident_id);
        if (other) r.href = `#/incidents/${other}`;
        break;
      }
      case 'change':
        r.icon = 'alert';
        r.tone = 'down';
        r.title = t('incidents.ev.changed');
        r.sub = ev.message;
        break;
      case 'location': {
        r.icon = 'map-pin';
        const status = str(d.status);
        r.tone = LOC_TONE[status] ?? 'muted';
        // data.message (ham kontrol mesajı) yalnızca yeni kayıtlarda var; yoksa
        // çalışmayan konumun saklı özeti olduğu gibi gösterilir.
        let what = ev.message;
        if (status === 'up') what = t('incidents.ev.locUp');
        else if (status === 'unknown') what = t('incidents.ev.locUnknown');
        else if (status === 'down' && typeof d.message === 'string')
          what = d.message ? t('incidents.ev.locDownMsg', { msg: d.message }) : t('incidents.ev.locDown');
        r.title = `${locName(ev.location)}: ${what}`;
        break;
      }
      case 'reminder':
        r.icon = 'bell-ring';
        r.tone = 'pending';
        r.title = t('incidents.ev.reminder');
        r.sub = d.downtime !== undefined ? t('incidents.ev.reminderSub', { d: fmtDuration(num(d.downtime)) }) : '';
        break;
      case 'maint_start':
      case 'maint_end':
        r.icon = 'wrench';
        r.tone = 'maint';
        r.title = t(ev.kind === 'maint_start' ? 'incidents.ev.maintStart' : 'incidents.ev.maintEnd');
        break;
      case 'notify': {
        const type = str(d.type) as NotificationType;
        const st = NOTIFY_STYLE[type];
        const ek = NOTIFY_EVENT[str(d.event)];
        const what = ek ? t(`incidents.ev.${ek}`) : t('incidents.ev.notifyOther');
        if (d.none) {
          r.icon = 'bell';
          r.tone = 'muted';
          r.title = t('incidents.ev.notSent', { what });
          r.sub = t(server ? 'incidents.ev.noChannelsServer' : 'incidents.ev.noChannels');
          break;
        }
        r.icon = st?.icon ?? 'bell';
        r.color = st?.color;
        r.tone = d.ok ? 'accent' : 'down';
        const ch = [str(d.channel), NOTIFY_LABELS[type] ?? str(d.type)].filter(Boolean).join(' · ');
        r.title = t(d.ok ? 'incidents.ev.sent' : 'incidents.ev.failed', { ch, what });
        r.sub = d.ok ? '' : str(d.error);
        break;
      }
      case 'edited':
      case 'paused': {
        r.icon = ev.kind === 'paused' ? 'pause' : 'edit';
        r.tone = 'muted';
        r.note = str(d.user);
        const closed = typeof d.closed === 'boolean' ? d.closed : ev.message === EDITED_CLOSED_TR;
        r.title =
          ev.kind === 'paused'
            ? t('incidents.ev.paused')
            : t(closed ? 'incidents.ev.editedClosed' : 'incidents.ev.edited');
        break;
      }
      case 'up':
        r.icon = 'check';
        r.tone = 'up';
        r.title = partial
          ? t('incidents.ev.partialResolved')
          : server
            ? t(offline ? 'incidents.ev.serverOnline' : 'incidents.ev.serverResolved')
            : t('incidents.ev.resolved');
        r.sub = [
          partial && PARTIAL_RESOLVED_MSG.includes(ev.message) ? '' : ev.message,
          d.downtime !== undefined ? t('incidents.ev.downtime', { d: fmtDurationLong(num(d.downtime)) }) : '',
        ]
          .filter(Boolean)
          .join(' · ');
        break;
      case 'limit':
        r.icon = 'info';
        r.tone = 'muted';
        r.title = t('incidents.ev.limit');
        break;
    }
    return r;
  }

  const rows = $derived((data?.events ?? []).map(row));

  // İstek / yanıt -----------------------------------------------------------------------

  let reqTab = $state<'url' | 'headers'>('url');
  let resTab = $state<'body' | 'headers'>('body');

  const headersText = (hs: HttpHeader[] | undefined) => (hs ?? []).map((h) => `${h.name}: ${h.value}`).join('\n');

  /** JSON gövde okunaklı girintilenir (kırpılmamışsa). */
  const prettyBody = $derived.by(() => {
    const b = detail?.body ?? '';
    if (!b || detail?.body_binary || detail?.body_truncated) return b;
    const ct = detail?.content_type ?? '';
    if (/json/i.test(ct) || /^\s*[[{]/.test(b)) {
      try {
        return JSON.stringify(JSON.parse(b), null, 2);
      } catch {
        /* geçerli JSON değil: olduğu gibi */
      }
    }
    return b;
  });

  function fmtBytes(n: number): string {
    return n < 1024 ? t('incidents.detail.bytes', { n, count: n }) : fmtSize(n);
  }

  const bodyMeta = $derived.by(() => {
    if (!detail) return '';
    const parts: string[] = [];
    if (detail.content_type) parts.push(detail.content_type);
    if (detail.body_size) parts.push(fmtBytes(detail.body_size));
    if (detail.body_truncated) parts.push(t('incidents.detail.truncated'));
    return parts.join(' · ');
  });

  function download() {
    if (!data || !capture) return;
    const d = capture.detail;
    if (isDiag) {
      const all = captures.map((c) => ({ location: c.location, captured_at: fmtDateSec(c.time), kind: c.detail.kind ?? data!.monitor.type, diag: c.detail.diag ?? null }));
      save(
        {
          incident: { id: data.incident.id, started_at: fmtDateSec(data.incident.started_at), cause: data.incident.cause },
          monitor: { id: data.monitor.id, name: data.monitor.name, type: data.monitor.type },
          captures: all,
        },
        t('incidents.conn.downloadFile', { id: data.incident.id }),
      );
      return;
    }
    const out = {
      incident: { id: data.incident.id, started_at: fmtDateSec(data.incident.started_at), cause: data.incident.cause },
      monitor: { id: data.monitor.id, name: data.monitor.name, type: data.monitor.type },
      captured_at: fmtDateSec(capture.time),
      location: capture.location,
      request: { method: d.method, url: d.url, headers: d.request_headers ?? [] },
      response: d.status
        ? {
            status: d.status,
            status_text: d.status_text ?? '',
            proto: d.proto ?? '',
            final_url: d.final_url ?? '',
            headers: d.response_headers ?? [],
            content_type: d.content_type ?? '',
            body: d.body ?? '',
            body_size: d.body_size ?? 0,
            body_truncated: !!d.body_truncated,
            body_binary: !!d.body_binary,
          }
        : null,
      error: d.error ?? '',
    };
    save(out, t('incidents.detail.downloadFile', { id: data.incident.id }));
  }

  function save(out: unknown, name: string) {
    const blob = new Blob([JSON.stringify(out, null, 2)], { type: 'application/json' });
    const a = document.createElement('a');
    a.href = URL.createObjectURL(blob);
    a.download = name;
    document.body.appendChild(a);
    a.click();
    a.remove();
    setTimeout(() => URL.revokeObjectURL(a.href), 1000);
  }

  const locLabel = (s: string) => tOr(`incidents.detail.loc.${s}`, s);
</script>

{#if notFound}
  <div class="card empty">
    <h3>{t('incidents.detail.notFound')}</h3>
    <p>{t('incidents.detail.notFoundText')}</p>
    <a class="btn primary" href="#/incidents">{t('incidents.detail.backToList')}</a>
  </div>
{:else if !data || !inc}
  {#if loadError}
    <div class="card empty">
      <h3>{t('incidents.detail.loadFailed')}</h3>
      <p>{loadError}</p>
      <button class="btn primary" onclick={load}>{t('common.retry')}</button>
    </div>
  {:else}
    <div class="skeleton" style="height:80px;margin-bottom:20px"></div>
    <div class="skeleton" style="height:320px"></div>
  {/if}
{:else}
  <div class="page" class:narrow={!showSide}>
    <a class="back" href="#/incidents"><Icon name="chevron-left" size={16} /> {t('incidents.detail.back')}</a>

    <div class="head">
      <div class="title">
        <StatusIcon kind={tone} size={40} pulse={ongoing} />
        <div class="tt">
          <h1>
            <span
              class="pre"
              class:c-down={ongoing && !partial}
              class:c-pending={ongoing && partial}
              class:c-up={!ongoing}
              title={partial ? t('incidents.kind.partialHint') : undefined}
              >{partial ? t('incidents.detail.partialPre') : ongoing ? t('incidents.detail.ongoingPre') : t('incidents.detail.resolvedPre')}</span
            >
            {data.server ? data.server.name : data.monitor.name}
          </h1>
          <div class="target">
            {#if data.server}
              <span class="badge accent">{t(offline ? 'incidents.kind.serverOffline' : 'incidents.kind.serverAlert')}</span>
              {#if data.server.hostname}<span class="text-2"
                  >{#each tParts('incidents.detail.hostname') as p, i (i)}{#if p.slot === 'name'}<span class="mono">{data.server.hostname}</span
                      >{:else}{p.text}{/if}{/each}</span
                >{/if}
            {:else}
            <!-- Konum kesintisi başlıkta yazıyor; ayrıca rozet gösterilmez. -->
            <TypeBadge type={data.monitor.type} />
            {#if !data.monitor.target}
              <span class="muted">{t('incidents.detail.typeMonitor', { type: typeName(data.monitor.type) })}</span>
            {:else if data.monitor.type === 'group'}
              <span class="text-2">{data.monitor.target}</span>
            {:else if isWebTarget(data.monitor.type) && /^https?:\/\//i.test(data.monitor.target)}
              <a href={data.monitor.target} target="_blank" rel="noopener noreferrer">{data.monitor.target}<Icon name="external" size={13} /></a>
            {:else}
              <span class="text-2 mono">{displayTarget(data.monitor.target)}</span>
            {/if}
            {/if}
          </div>
          {#if data.monitor?.changed}
            <!-- Tip/hedef olay anındaki değerlerdir; monitör sonradan değiştirildi. -->
            <div class="changed">
              <Icon name="info" size={13} />
              {t('incidents.detail.changedSince', {
                now: [data.monitor.current_type ? typeName(data.monitor.current_type) : '', data.monitor.current_target ?? '']
                  .filter(Boolean)
                  .join(' · '),
              })}
            </div>
          {/if}
        </div>
      </div>
      <div class="actions">
        {#if data.server}
          <a class="btn" href="#/servers/{data.server.id}"><Icon name="server" size={15} /> {t('incidents.detail.goToServer')}</a>
        {:else}
          <a class="btn" href="#/monitors/{data.monitor.id}"><Icon name="activity" size={15} /> {t('incidents.detail.goToMonitor')}</a>
        {/if}
        {#if capture}
          <button class="btn" onclick={download}>
            <Icon name="download" size={15} />
            {t('incidents.detail.downloadDetails')}
          </button>
        {/if}
      </div>
    </div>

    <div class="layout" class:single={!showSide}>
      <div class="col">
        <div class="card cause" class:resolved={!ongoing} class:partial={ongoing && partial}>
          <div class="label">{t('incidents.detail.rootCause')}</div>
          <div class="cause-t">{causeTitle(rootCause)}</div>
          {#if data.location}
            <div class="cause-w"><Icon name="map-pin" size={14} /> {locName(data.location)}</div>
          {/if}
        </div>

        <div class="pair">
          <div class="card stat">
            <div class="label">{t('incidents.detail.status')}</div>
            <div class="value">
              <span class="pill {tone}">{ongoing ? t('incidents.detail.ongoing') : t('incidents.detail.resolved')}</span>
            </div>
            <div class="sub">{t('incidents.detail.started', { date: fmtDateSec(inc.started_at) })}</div>
          </div>
          <div class="card stat">
            <div class="label">{t('incidents.detail.duration')}</div>
            <div class="value" class:c-down={ongoing && !partial} class:c-pending={ongoing && partial}>{fmtDurationLong(duration)}</div>
            <div class="sub">{ongoing ? t('incidents.detail.stillOngoing') : t('incidents.detail.resolvedAt', { date: fmtDateSec(inc.resolved_at) })}</div>
          </div>
        </div>

        {#if partial}
          <div class="card">
            <div class="card-head">
              <h2 class="card-title">{t('incidents.detail.affected')}<span class="dot">.</span></h2>
              <span class="muted small">{t('incidents.detail.affectedHint')}</span>
            </div>
            {#if affected.length}
              <div class="aff">
                {#each affected as name (name)}
                  {@const at = data.locations.find((l) => l.name === name && l.status !== 'up')}
                  <span class="aff-i"
                    ><Icon name="map-pin" size={13} /> {locName(name)}{#if at?.message}<span class="aff-m">· {at.message}</span>{/if}</span
                  >
                {/each}
              </div>
            {/if}
            <p class="note"><Icon name="info" size={14} /> {t('incidents.detail.partialNote')}</p>
          </div>
        {/if}

        {#if sdata}
          <div class="card">
            <div class="card-head">
              <h2 class="card-title">{offline ? t('incidents.detail.offlineTitle') : t('incidents.detail.alertTitle')}<span class="dot">.</span></h2>
            </div>
            <dl class="facts">
              {#if offline}
                {#if sdata.last_seen}
                  <dt>{t('incidents.detail.lastSeen')}</dt>
                  <dd>{fmtDateSec(sdata.last_seen)}</dd>
                {/if}
                <dt>{t('incidents.detail.offlineAfter')}</dt>
                <dd>{t('incidents.detail.minutes', { n: sdata.minutes })}</dd>
              {:else}
                <dt>{t('incidents.detail.metric')}</dt>
                <dd>{metricLabel(smetric)}{sdata.mount ? ` · ${sdata.mount}` : ''}</dd>
                <dt>{t('incidents.detail.threshold')}</dt>
                <dd>≥ {fmtMetric(smetric, sdata.threshold ?? 0)}</dd>
                <dt>{t('incidents.detail.window')}</dt>
                <dd>{t('incidents.detail.minutes', { n: sdata.minutes })}</dd>
                <dt>{t('incidents.detail.valueAtStart')}</dt>
                <dd class="num">{fmtMetric(smetric, sdata.value)}</dd>
                <dt>{t('incidents.detail.peak')}</dt>
                <dd class="num c-down">{fmtMetric(smetric, sdata.peak)}</dd>
                <dt>{ongoing ? t('incidents.detail.last') : t('incidents.detail.lastAtEnd')}</dt>
                <dd class="num">{fmtMetric(smetric, sdata.last)}</dd>
              {/if}
            </dl>
          </div>
        {/if}

        <!-- Grup ve push monitörlerinin konumu yoktur ("Ana sunucu" anlamsız). -->
        <!-- Konum kesintisinde etkilenen konumlar yukarıdaki kartta (aynı bilgi iki kez yazılmaz). -->
        {#if data.locations.length && !partial && data.monitor.type !== 'group' && data.monitor.type !== 'push'}
          <div class="card">
            <div class="card-head">
              <h2 class="card-title">{t('incidents.detail.locations')}<span class="dot">.</span></h2>
              <span class="muted small">{t('incidents.detail.atStart')}</span>
            </div>
            <ul class="locs">
              {#each data.locations as l (l.probe_id)}
                <li class="loc {l.status}">
                  <span class="li" aria-hidden="true">
                    <Icon name={l.status === 'up' ? 'check' : l.status === 'down' ? 'x' : l.status === 'retrying' ? 'refresh' : 'clock'} size={13} />
                  </span>
                  <span class="lt">
                    <span class="ln">{locName(l.name)}</span>
                    <span class="ls">{locLabel(l.status)}{l.message && l.status !== 'up' ? ` · ${l.message}` : ''}</span>
                  </span>
                </li>
              {/each}
            </ul>
          </div>
        {/if}

        <div class="card log">
          <div class="card-head">
            <h2 class="card-title">{t('incidents.detail.timeline')}<span class="dot">.</span></h2>
            <span class="muted small">{t('incidents.detail.newestFirst')}</span>
          </div>
          <ol class="timeline">
            {#each rows as r, i (r.ev.id + ':' + i)}
              <li class="ev {r.tone}">
                <span class="ei" style={r.color ? `--ch:${r.color}` : undefined} class:ch={!!r.color} aria-hidden="true">
                  <Icon name={r.icon} size={14} />
                </span>
                <div class="eb">
                  <div class="et">{r.title}</div>
                  {#if r.sub}<div class="es">{r.sub}</div>{/if}
                  <div class="em">
                    <time datetime={new Date(r.ev.time * 1000).toISOString()} title={fmtDateSec(r.ev.time)}>
                      {fmtDay(r.ev.time) === fmtDay(inc.started_at) ? fmtTimeSec(r.ev.time) : fmtDateSec(r.ev.time)}
                    </time>
                    {#if r.note}<span class="en">· {r.note}</span>{/if}
                    {#if r.href}<a class="en" href={r.href}>· {t('incidents.ev.openLinked')}</a>{/if}
                  </div>
                </div>
              </li>
            {/each}
          </ol>
        </div>
      </div>

      {#if showSide}
        <div class="col side">
          {#if captures.length > 1}
            <div class="cap-locs">
              <span class="label">{t('incidents.detail.captureLocations')}</span>
              <div class="tabs" role="tablist" aria-label={t('incidents.detail.captureLocations')}>
                {#each captures as c, i (i)}
                  <button role="tab" aria-selected={capIdx === i} class:active={capIdx === i} onclick={() => (capIdx = i)}>
                    <Icon name="map-pin" size={13} />
                    {locName(c.location)}
                  </button>
                {/each}
              </div>
            </div>
          {/if}
          {#if detail && isDiag}
            <ConnectionDetails
              {detail}
              type={data.monitor.type}
              fallbackError={detail.error || ''}
              meta={`${capture?.location ? `${locName(capture.location)} · ` : ''}${capture ? fmtDateSec(capture.time) : ''}`}
            />
          {:else if detail}
            <div class="card">
              <div class="card-head">
                <h2 class="card-title">{t('incidents.detail.request')}<span class="dot">.</span></h2>
                <div class="tools">
                  <div class="tabs" role="tablist" aria-label={t('incidents.detail.request')}>
                    <button role="tab" aria-selected={reqTab === 'url'} class:active={reqTab === 'url'} onclick={() => (reqTab = 'url')}>URL</button>
                    <button role="tab" aria-selected={reqTab === 'headers'} class:active={reqTab === 'headers'} onclick={() => (reqTab = 'headers')}>
                      {t('incidents.detail.headers')} <span class="cnt">{detail.request_headers?.length ?? 0}</span>
                    </button>
                  </div>
                  <CopyButton
                    class="btn sm icon"
                    iconOnly
                    text={() => (reqTab === 'url' ? `${detail.method} ${detail.url}` : headersText(detail.request_headers))}
                  />
                </div>
              </div>
              {#if reqTab === 'url'}
                <div class="code url"><span class="method">{detail.method}</span> {detail.url}</div>
                {#if detail.final_url}
                  <div class="redirect small text-2"><Icon name="arrow-right" size={13} /> {t('incidents.detail.afterRedirect')} <span class="mono">{detail.final_url}</span></div>
                {/if}
              {:else}
                {@render headerList(detail.request_headers)}
              {/if}
              <div class="cap-meta muted small">
                {capture?.location ? `${locName(capture.location)} · ` : ''}{capture ? fmtDateSec(capture.time) : ''}
              </div>
            </div>

            <div class="card">
              <div class="card-head">
                <div class="ttl">
                  <h2 class="card-title">{t('incidents.detail.response')}<span class="dot">.</span></h2>
                  {#if detail.status}
                    <span class="status" class:ok={detail.status < 400}>{detail.status} {detail.status_text ?? ''}</span>
                  {/if}
                </div>
                {#if detail.status}
                  <div class="tools">
                    <div class="tabs" role="tablist" aria-label={t('incidents.detail.response')}>
                      <button role="tab" aria-selected={resTab === 'body'} class:active={resTab === 'body'} onclick={() => (resTab = 'body')}>{t('incidents.detail.body')}</button>
                      <button role="tab" aria-selected={resTab === 'headers'} class:active={resTab === 'headers'} onclick={() => (resTab = 'headers')}>
                        {t('incidents.detail.headers')} <span class="cnt">{detail.response_headers?.length ?? 0}</span>
                      </button>
                    </div>
                    <CopyButton
                      class="btn sm icon"
                      iconOnly
                      text={() => (resTab === 'body' ? (detail.body ?? '') : headersText(detail.response_headers))}
                    />
                  </div>
                {/if}
              </div>
              {#if !detail.status}
                <div class="noresp">
                  <Icon name="wifi-off" size={18} />
                  <div>
                    <div class="nr-t">{t('incidents.detail.noResponse')}</div>
                    <div class="nr-s">{detail.error || inc.cause}</div>
                  </div>
                </div>
              {:else if resTab === 'body'}
                {#if detail.body_binary}
                  <div class="empty-body">{detail.body}</div>
                {:else if !detail.body}
                  <div class="empty-body">{t('incidents.detail.emptyBody')}</div>
                {:else}
                  <pre class="code body">{prettyBody}</pre>
                {/if}
                {#if bodyMeta}<div class="cap-meta muted small">{bodyMeta}</div>{/if}
              {:else}
                {@render headerList(detail.response_headers)}
              {/if}
            </div>
          {:else}
            <div class="card nocap">
              <Icon name="inbox" size={22} />
              <div>
                <div class="nr-t">{isHttp ? t('incidents.detail.noCapture') : t('incidents.conn.noCapture')}</div>
                <div class="nr-s">
                  {isHttp ? t('incidents.detail.noCaptureText') : t('incidents.conn.noCaptureText')}
                </div>
              </div>
            </div>
          {/if}
        </div>
      {/if}
    </div>
  </div>
{/if}

{#snippet headerList(hs: HttpHeader[] | undefined)}
  {#if !hs?.length}
    <div class="empty-body">{t('incidents.detail.noHeaders')}</div>
  {:else}
    <dl class="hdrs">
      {#each hs as h, i (i)}
        <dt>{h.name}</dt>
        <dd class:masked={h.value === '••••••'}>{h.value}</dd>
      {/each}
    </dl>
  {/if}
{/snippet}

<style>
  .changed {
    display: flex;
    align-items: center;
    gap: 6px;
    margin-top: 6px;
    font-size: 0.82rem;
    color: var(--pending-text);
  }
  .aff-m {
    margin-left: 5px;
    color: var(--text-2);
    font-weight: 400;
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
    gap: 14px;
    min-width: 0;
    flex: 1 1 420px;
  }
  .tt {
    min-width: 0;
  }
  h1 {
    word-break: break-word;
    line-height: 1.25;
  }
  .pre {
    font-weight: 600;
  }
  .target {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
    margin-top: 6px;
    font-size: 0.9rem;
    word-break: break-all;
  }
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

  .layout {
    display: grid;
    grid-template-columns: minmax(0, 1.08fr) minmax(0, 1fr);
    gap: 16px;
    align-items: start;
  }
  .layout.single {
    grid-template-columns: minmax(0, 1fr);
  }
  /* Yan sütunu olmayan olay (ör. grup) dar ve ortada: sola yaslı dar sayfa
     diğer olay sayfalarıyla hizasız duruyordu. */
  .page.narrow {
    max-width: 880px;
    margin-left: auto;
    margin-right: auto;
  }
  .col {
    display: flex;
    flex-direction: column;
    gap: 16px;
    min-width: 0;
  }

  .label {
    font-size: 0.78rem;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--muted);
  }
  .cause {
    border-left: 3px solid var(--down);
  }
  .cause.resolved {
    border-left-color: var(--border-strong);
  }
  .cause.partial {
    border-left-color: var(--pending);
  }
  .cause.partial .cause-t {
    color: var(--pending);
  }
  .aff {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }
  .aff-i {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    padding: 5px 10px;
    border-radius: 999px;
    font-size: 0.86rem;
    font-weight: 600;
    background: var(--pending-soft);
    color: var(--pending);
  }
  .note {
    display: flex;
    gap: 7px;
    align-items: flex-start;
    margin: 12px 0 0;
    font-size: 0.85rem;
    color: var(--text-2);
    line-height: 1.45;
  }
  .note :global(svg) {
    flex: none;
    margin-top: 2px;
  }
  .facts {
    display: grid;
    grid-template-columns: max-content minmax(0, 1fr);
    gap: 9px 18px;
    margin: 0;
    font-size: 0.92rem;
  }
  .facts dt {
    color: var(--muted);
  }
  .facts dd {
    margin: 0;
    font-weight: 600;
    word-break: break-word;
  }
  .facts .num {
    font-variant-numeric: tabular-nums;
  }
  .cause-t {
    margin-top: 8px;
    font-size: 1.35rem;
    font-weight: 700;
    line-height: 1.3;
    color: var(--down-text);
    word-break: break-word;
  }
  .cause.resolved .cause-t {
    color: var(--text);
  }
  .cause-w {
    display: flex;
    align-items: center;
    gap: 5px;
    margin-top: 6px;
    color: var(--text-2);
    font-size: 0.88rem;
  }

  .pair {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 16px;
  }
  .stat {
    display: flex;
    flex-direction: column;
    gap: 6px;
    min-width: 0;
  }
  .value {
    font-size: 1.3rem;
    font-weight: 700;
    line-height: 1.3;
    font-variant-numeric: tabular-nums;
  }
  .sub {
    font-size: 0.84rem;
    color: var(--text-2);
    font-variant-numeric: tabular-nums;
  }

  .card-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px 12px;
    flex-wrap: wrap;
    margin-bottom: 14px;
  }
  .card-head .card-title {
    margin: 0;
  }
  .ttl {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
  }

  /* Konumlar */
  .locs {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(210px, 1fr));
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
  .li {
    width: 22px;
    height: 22px;
    border-radius: 50%;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    background: var(--paused);
    color: var(--on-paused);
  }
  .li :global(svg) {
    stroke-width: 3;
  }
  .loc.up .li {
    background: var(--up);
    color: var(--on-up);
  }
  .loc.down .li {
    background: var(--down);
    color: var(--on-down);
  }
  .loc.retrying .li {
    background: var(--pending);
    color: var(--on-pending);
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
    font-size: 0.8rem;
    color: var(--text-2);
    word-break: break-word;
  }
  .loc.down .ls {
    color: var(--down-text);
  }

  /* İşlem geçmişi */
  .timeline {
    list-style: none;
    margin: 0;
    padding: 0;
    position: relative;
  }
  .ev {
    display: flex;
    gap: 12px;
    position: relative;
    padding-bottom: 16px;
  }
  .ev:last-child {
    padding-bottom: 0;
  }
  /* Simgeleri birleştiren dikey çizgi */
  .ev:not(:last-child)::before {
    content: '';
    position: absolute;
    left: 13px;
    top: 30px;
    bottom: 2px;
    width: 2px;
    background: var(--border);
    border-radius: 1px;
  }
  .ei {
    width: 28px;
    height: 28px;
    border-radius: 50%;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    background: var(--card-2);
    color: var(--text-2);
    border: 1px solid var(--border-strong);
  }
  .ev.down .ei {
    background: var(--down-soft);
    border-color: var(--down-border);
    color: var(--down-text-2);
  }
  .ev.up .ei {
    background: var(--up-soft);
    border-color: var(--up-border);
    color: var(--up);
  }
  .ev.pending .ei {
    background: var(--pending-soft);
    border-color: var(--pending-border);
    color: var(--pending);
  }
  .ev.maint .ei {
    background: var(--maint-soft);
    border-color: var(--maint-border);
    color: var(--maint);
  }
  .ev.accent .ei {
    background: var(--accent-soft);
    border-color: var(--accent-border);
    color: var(--accent-text);
  }
  /* Bildirim kanalı simgesi kanalın kendi renginde */
  .ev.accent .ei.ch {
    color: var(--ch);
  }
  .eb {
    min-width: 0;
    padding-top: 4px;
    flex: 1;
  }
  .et {
    font-weight: 600;
    font-size: 0.92rem;
    line-height: 1.35;
    word-break: break-word;
  }
  .ev.down .et {
    color: var(--down-text);
  }
  .ev.up .et {
    color: var(--up-text);
  }
  .es {
    margin-top: 2px;
    font-size: 0.85rem;
    color: var(--text-2);
    word-break: break-word;
  }
  .em {
    margin-top: 3px;
    font-size: 0.78rem;
    color: var(--muted);
    font-variant-numeric: tabular-nums;
  }

  /* İstek / yanıt */
  .tools {
    display: flex;
    align-items: center;
    gap: 8px;
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
  /* Yakalanan konumlar (çok konumlu olay) */
  .cap-locs {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px 12px;
    flex-wrap: wrap;
  }
  .cap-locs .tabs {
    flex-wrap: wrap;
    max-width: 100%;
  }
  .cap-locs .tabs button {
    display: inline-flex;
    align-items: center;
    gap: 5px;
  }
  .cnt {
    color: var(--muted);
    font-weight: 500;
    margin-left: 2px;
  }
  .code {
    font-family: var(--mono);
    font-size: 0.82rem;
    background: var(--input);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 12px 14px;
    color: var(--text);
    margin: 0;
  }
  .url {
    word-break: break-all;
    line-height: 1.5;
  }
  .method {
    color: var(--accent-text);
    font-weight: 700;
  }
  .redirect {
    display: flex;
    align-items: center;
    gap: 5px;
    margin-top: 8px;
    word-break: break-all;
  }
  .body {
    max-height: 460px;
    overflow: auto;
    white-space: pre-wrap;
    word-break: break-word;
    line-height: 1.5;
  }
  .status {
    font-size: 0.78rem;
    font-weight: 700;
    padding: 3px 9px;
    border-radius: 999px;
    background: var(--down-soft);
    color: var(--down-text);
    border: 1px solid var(--down-border);
    white-space: nowrap;
  }
  .status.ok {
    background: var(--pending-soft);
    color: var(--pending-text);
    border-color: var(--pending-border);
  }
  .hdrs {
    margin: 0;
    display: grid;
    grid-template-columns: minmax(110px, max-content) minmax(0, 1fr);
    font-family: var(--mono);
    font-size: 0.8rem;
    background: var(--input);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    max-height: 460px;
    overflow: auto;
  }
  .hdrs dt,
  .hdrs dd {
    margin: 0;
    padding: 7px 12px;
    border-bottom: 1px solid var(--border);
    min-width: 0;
  }
  .hdrs dt:nth-last-of-type(1),
  .hdrs dd:last-of-type {
    border-bottom: none;
  }
  .hdrs dt {
    color: var(--text-2);
    font-weight: 600;
    white-space: nowrap;
  }
  .hdrs dd {
    color: var(--text);
    word-break: break-all;
  }
  .hdrs dd.masked {
    color: var(--muted);
    letter-spacing: 0.1em;
  }
  .empty-body {
    background: var(--input);
    border: 1px dashed var(--border-strong);
    border-radius: var(--radius-sm);
    padding: 18px 14px;
    color: var(--muted);
    font-size: 0.88rem;
    text-align: center;
  }
  .cap-meta {
    margin-top: 10px;
    word-break: break-word;
  }
  .noresp,
  .nocap {
    display: flex;
    align-items: flex-start;
    gap: 12px;
  }
  .noresp {
    background: var(--down-soft);
    border: 1px solid var(--down-border);
    border-radius: var(--radius-sm);
    padding: 12px 14px;
    color: var(--down-text);
  }
  .noresp :global(svg),
  .nocap :global(svg) {
    flex-shrink: 0;
    margin-top: 1px;
  }
  .nocap {
    color: var(--muted);
  }
  .nr-t {
    font-weight: 700;
    color: var(--text);
  }
  .nr-s {
    font-size: 0.86rem;
    margin-top: 2px;
    word-break: break-word;
  }
  .noresp .nr-s {
    color: var(--down-text);
  }

  /* Tek sütun: istek/yanıt işlem geçmişinden önce gelir. */
  @media (max-width: 960px) {
    .layout,
    .layout.single {
      display: flex;
      flex-direction: column;
      align-items: stretch;
    }
    .col {
      display: contents;
    }
    .log {
      order: 10;
    }
  }
  @media (max-width: 520px) {
    .cap-locs {
      flex-direction: column;
      align-items: stretch;
    }
    .cap-locs .tabs button {
      flex: 1;
      justify-content: center;
    }
    .pair {
      gap: 10px;
    }
    .value {
      font-size: 1.1rem;
    }
    .cause-t {
      font-size: 1.15rem;
    }
    .actions {
      width: 100%;
    }
    .actions > :global(*) {
      flex: 1;
    }
    .card-head .tools {
      width: 100%;
      justify-content: space-between;
    }
    .hdrs {
      grid-template-columns: minmax(0, 1fr);
    }
    .hdrs dt {
      border-bottom: none;
      padding-bottom: 0;
    }
  }
</style>
