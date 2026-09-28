<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import {
    api,
    ApiError,
    errorMessage,
    type CheckDetail,
    type HttpHeader,
    type IncidentDetail,
    type IncidentEvent,
    type NotificationType,
  } from '../lib/api';
  import { live } from '../lib/live.svelte';
  import { fmtDateSec, fmtDuration, fmtDurationLong, fmtTimeSec, fmtDay, nowSec } from '../lib/format';
  import { displayTarget, isWebTarget, typeName } from '../lib/monitorTypes';
  import { NOTIFY_LABELS, NOTIFY_STYLE } from '../lib/notifyTypes';
  import StatusIcon from '../components/StatusIcon.svelte';
  import TypeBadge from '../components/TypeBadge.svelte';
  import Icon, { type IconName } from '../components/Icon.svelte';
  import CopyButton from '../components/CopyButton.svelte';

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
  let unsubResume: (() => void) | undefined;

  onMount(() => {
    load();
    // Bağlantı yeniden kuruldu / uygulamaya geri dönüldü: kaçan değişiklikler için yenile.
    unsubResume = live.onResume(load);
    tick = setInterval(() => (now = nowSec()), 1000);
    // Monitörün yeni sonucu olay geçmişini değiştirebilir (çözülme, hata değişimi).
    unsub = live.onBeat((b) => {
      if (!data || b.monitor_id !== data.incident.monitor_id || data.incident.resolved_at) return;
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
    unsubResume?.();
  });

  const inc = $derived(data?.incident);
  const ongoing = $derived(!!inc && inc.resolved_at === 0);
  const duration = $derived(inc ? (ongoing ? Math.max(now, inc.started_at) : inc.resolved_at) - inc.started_at : 0);
  const isHttp = $derived(data?.monitor.type === 'http');
  const capture = $derived(data?.capture ?? null);
  const detail = $derived<CheckDetail | null>(capture?.detail ?? null);
  const showSide = $derived(!!data && data.details && isHttp);

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
    return m ? m[1] : c || 'Neden kaydedilmemiş';
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
  }

  const num = (v: unknown) => (typeof v === 'number' ? v : 0);
  const str = (v: unknown) => (typeof v === 'string' ? v : '');

  const NOTIFY_EVENT: Record<string, string> = {
    down: 'kesinti bildirimi',
    up: 'düzelme bildirimi',
    reminder: 'hatırlatma',
  };
  const LOC_TONE: Record<string, Tone> = { up: 'up', down: 'down', retrying: 'pending', unknown: 'muted' };

  function row(ev: IncidentEvent): Row {
    const d = ev.data ?? {};
    const r: Row = { ev, icon: 'info', tone: 'muted', title: ev.message, sub: '', note: '' };
    switch (ev.kind) {
      case 'retry': {
        const att = num(d.attempt);
        r.icon = 'refresh';
        r.tone = 'pending';
        r.title = att ? `Kontrol başarısız, tekrar denenecek (${att}/${num(d.max)})` : 'Konumlar tekrar deneniyor';
        r.sub = ev.message;
        r.note = ev.location;
        break;
      }
      case 'down':
        r.icon = 'zap';
        r.tone = 'down';
        r.title = 'Olay başladı';
        r.sub = ev.message;
        r.note = ev.location ? `${ev.location} doğruladı` : '';
        break;
      case 'change':
        r.icon = 'alert';
        r.tone = 'down';
        r.title = 'Hata değişti';
        r.sub = ev.message;
        break;
      case 'location':
        r.icon = 'map-pin';
        r.tone = LOC_TONE[str(d.status)] ?? 'muted';
        r.title = `${ev.location}: ${ev.message}`;
        break;
      case 'reminder':
        r.icon = 'bell-ring';
        r.tone = 'pending';
        r.title = 'Hatırlatma bildirimi tetiklendi';
        r.sub = d.downtime !== undefined ? `Kesinti ${fmtDuration(num(d.downtime))} sürüyordu` : '';
        break;
      case 'maint_start':
      case 'maint_end':
        r.icon = 'wrench';
        r.tone = 'maint';
        break;
      case 'notify': {
        const type = str(d.type) as NotificationType;
        const st = NOTIFY_STYLE[type];
        const what = NOTIFY_EVENT[str(d.event)] ?? 'bildirim';
        if (d.none) {
          r.icon = 'bell';
          r.tone = 'muted';
          r.title = `Bildirim gönderilmedi (${what})`;
          r.sub = 'Monitöre bağlı etkin bildirim kanalı yok';
          break;
        }
        r.icon = st?.icon ?? 'bell';
        r.color = st?.color;
        r.tone = d.ok ? 'accent' : 'down';
        const ch = [str(d.channel), NOTIFY_LABELS[type] ?? str(d.type)].filter(Boolean).join(' · ');
        r.title = d.ok ? `${ch}: ${what} gönderildi` : `${ch}: ${what} gönderilemedi`;
        r.sub = d.ok ? '' : str(d.error);
        break;
      }
      case 'edited':
      case 'paused':
        r.icon = ev.kind === 'paused' ? 'pause' : 'edit';
        r.tone = 'muted';
        r.note = str(d.user);
        break;
      case 'up':
        r.icon = 'check';
        r.tone = 'up';
        r.title = 'Monitör tekrar çalışıyor (çözüldü)';
        r.sub = [ev.message, d.downtime !== undefined ? `kesinti ${fmtDurationLong(num(d.downtime))}` : '']
          .filter(Boolean)
          .join(' · ');
        break;
      case 'limit':
        r.icon = 'info';
        r.tone = 'muted';
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
    if (n < 1024) return `${n} bayt`;
    if (n < 1024 * 1024) return `${(n / 1024).toFixed(1).replace('.', ',')} KB`;
    return `${(n / 1024 / 1024).toFixed(1).replace('.', ',')} MB`;
  }

  const bodyMeta = $derived.by(() => {
    if (!detail) return '';
    const parts: string[] = [];
    if (detail.content_type) parts.push(detail.content_type);
    if (detail.body_size) parts.push(fmtBytes(detail.body_size));
    if (detail.body_truncated) parts.push('ilk 16 KB gösteriliyor');
    return parts.join(' · ');
  });

  function download() {
    if (!data || !capture) return;
    const d = capture.detail;
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
    const blob = new Blob([JSON.stringify(out, null, 2)], { type: 'application/json' });
    const a = document.createElement('a');
    a.href = URL.createObjectURL(blob);
    a.download = `olay-${data.incident.id}-yanit.json`;
    document.body.appendChild(a);
    a.click();
    a.remove();
    setTimeout(() => URL.revokeObjectURL(a.href), 1000);
  }

  const LOC_LABEL: Record<string, string> = {
    up: 'Çalışıyordu',
    down: 'Çalışmıyordu',
    retrying: 'Tekrar deniyordu',
    unknown: 'Sonuç yoktu',
  };
</script>

{#if notFound}
  <div class="card empty">
    <h3>Olay bulunamadı</h3>
    <p>Bu olay silinmiş olabilir ya da görüntüleme yetkiniz yok.</p>
    <a class="btn primary" href="#/incidents">Olaylara dön</a>
  </div>
{:else if !data || !inc}
  {#if loadError}
    <div class="card empty">
      <h3>Yüklenemedi</h3>
      <p>{loadError}</p>
      <button class="btn primary" onclick={load}>Tekrar dene</button>
    </div>
  {:else}
    <div class="skeleton" style="height:80px;margin-bottom:20px"></div>
    <div class="skeleton" style="height:320px"></div>
  {/if}
{:else}
  <div class="page" class:narrow={!showSide}>
    <a class="back" href="#/incidents"><Icon name="chevron-left" size={16} /> Olaylar</a>

    <div class="head">
      <div class="title">
        <StatusIcon kind={ongoing ? 'down' : 'up'} size={40} pulse={ongoing} />
        <div class="tt">
          <h1><span class="pre" class:c-down={ongoing} class:c-up={!ongoing}>{ongoing ? 'Süren olay:' : 'Çözülen olay:'}</span> {data.monitor.name}</h1>
          <div class="target">
            <TypeBadge type={data.monitor.type} />
            {#if !data.monitor.target}
              <span class="muted">{typeName(data.monitor.type)} monitörü</span>
            {:else if isWebTarget(data.monitor.type) && /^https?:\/\//i.test(data.monitor.target)}
              <a href={data.monitor.target} target="_blank" rel="noopener noreferrer">{data.monitor.target}<Icon name="external" size={13} /></a>
            {:else}
              <span class="text-2 mono">{displayTarget(data.monitor.target)}</span>
            {/if}
          </div>
        </div>
      </div>
      <div class="actions">
        <a class="btn" href="#/monitors/{data.monitor.id}"><Icon name="activity" size={15} /> Monitöre git</a>
        {#if capture}
          <button class="btn" onclick={download}><Icon name="download" size={15} /> Yanıtı indir</button>
        {/if}
      </div>
    </div>

    <div class="layout" class:single={!showSide}>
      <div class="col">
        <div class="card cause" class:resolved={!ongoing}>
          <div class="label">Kök neden</div>
          <div class="cause-t">{causeTitle(rootCause)}</div>
          {#if data.location}
            <div class="cause-w"><Icon name="map-pin" size={14} /> {data.location}</div>
          {/if}
        </div>

        <div class="pair">
          <div class="card stat">
            <div class="label">Durum</div>
            <div class="value">
              <span class="pill {ongoing ? 'down' : 'up'}">{ongoing ? 'Sürüyor' : 'Çözüldü'}</span>
            </div>
            <div class="sub">Başladı: {fmtDateSec(inc.started_at)}</div>
          </div>
          <div class="card stat">
            <div class="label">Süre</div>
            <div class="value" class:c-down={ongoing}>{fmtDurationLong(duration)}</div>
            <div class="sub">{ongoing ? 'Hâlâ sürüyor' : `Çözüldü: ${fmtDateSec(inc.resolved_at)}`}</div>
          </div>
        </div>

        {#if data.locations.length}
          <div class="card">
            <div class="card-head">
              <h2 class="card-title">Konumlar<span class="dot">.</span></h2>
              <span class="muted small">Olay başladığında</span>
            </div>
            <ul class="locs">
              {#each data.locations as l (l.probe_id)}
                <li class="loc {l.status}">
                  <span class="li" aria-hidden="true">
                    <Icon name={l.status === 'up' ? 'check' : l.status === 'down' ? 'x' : l.status === 'retrying' ? 'refresh' : 'clock'} size={13} />
                  </span>
                  <span class="lt">
                    <span class="ln">{l.name}</span>
                    <span class="ls">{LOC_LABEL[l.status] ?? l.status}{l.message && l.status !== 'up' ? ` · ${l.message}` : ''}</span>
                  </span>
                </li>
              {/each}
            </ul>
          </div>
        {/if}

        <div class="card log">
          <div class="card-head">
            <h2 class="card-title">İşlem geçmişi<span class="dot">.</span></h2>
            <span class="muted small">Yeniden eskiye</span>
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
                  </div>
                </div>
              </li>
            {/each}
          </ol>
        </div>
      </div>

      {#if showSide}
        <div class="col side">
          {#if detail}
            <div class="card">
              <div class="card-head">
                <h2 class="card-title">İstek<span class="dot">.</span></h2>
                <div class="tools">
                  <div class="tabs" role="tablist" aria-label="İstek">
                    <button role="tab" aria-selected={reqTab === 'url'} class:active={reqTab === 'url'} onclick={() => (reqTab = 'url')}>URL</button>
                    <button role="tab" aria-selected={reqTab === 'headers'} class:active={reqTab === 'headers'} onclick={() => (reqTab = 'headers')}>
                      Başlıklar <span class="cnt">{detail.request_headers?.length ?? 0}</span>
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
                  <div class="redirect small text-2"><Icon name="arrow-right" size={13} /> Yönlendirme sonrası: <span class="mono">{detail.final_url}</span></div>
                {/if}
              {:else}
                {@render headerList(detail.request_headers)}
              {/if}
              <div class="cap-meta muted small">
                {capture?.location ? `${capture.location} · ` : ''}{capture ? fmtDateSec(capture.time) : ''}
              </div>
            </div>

            <div class="card">
              <div class="card-head">
                <div class="ttl">
                  <h2 class="card-title">Yanıt<span class="dot">.</span></h2>
                  {#if detail.status}
                    <span class="status" class:ok={detail.status < 400}>{detail.status} {detail.status_text ?? ''}</span>
                  {/if}
                </div>
                {#if detail.status}
                  <div class="tools">
                    <div class="tabs" role="tablist" aria-label="Yanıt">
                      <button role="tab" aria-selected={resTab === 'body'} class:active={resTab === 'body'} onclick={() => (resTab = 'body')}>Gövde</button>
                      <button role="tab" aria-selected={resTab === 'headers'} class:active={resTab === 'headers'} onclick={() => (resTab = 'headers')}>
                        Başlıklar <span class="cnt">{detail.response_headers?.length ?? 0}</span>
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
                    <div class="nr-t">Yanıt alınamadı</div>
                    <div class="nr-s">{detail.error || inc.cause}</div>
                  </div>
                </div>
              {:else if resTab === 'body'}
                {#if detail.body_binary}
                  <div class="empty-body">{detail.body}</div>
                {:else if !detail.body}
                  <div class="empty-body">Yanıt gövdesi boş</div>
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
                <div class="nr-t">İstek ve yanıt kaydı yok</div>
                <div class="nr-s">
                  Bu olay kayıt tutulmaya başlanmadan önce açılmış ya da kaydın 90 günlük saklama süresi dolmuş.
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
    <div class="empty-body">Başlık yok</div>
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
  .page.narrow {
    max-width: 880px;
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
