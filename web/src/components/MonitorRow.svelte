<script lang="ts">
  import './monitor-grid.css';
  import { PING_DOWN, type MonitorView } from '../lib/api';
  import { navigate } from '../lib/router.svelte';
  import {
    STATUS_LABELS,
    certDaysLeft,
    fmtDateSec,
    fmtDay,
    fmtDuration,
    fmtInterval,
    fmtMs,
    fmtPct,
    uptimeTone,
    fmtRelative,
    monitorKind,
  } from '../lib/format';
  import { session } from '../lib/session.svelte';
  import StatusIcon from './StatusIcon.svelte';
  import TypeBadge from './TypeBadge.svelte';
  import TagChip from './TagChip.svelte';
  import UptimeBars from './UptimeBars.svelte';
  import Sparkline from './Sparkline.svelte';
  import Icon from './Icon.svelte';
  import { shortTarget } from '../lib/monitorTypes';
  import { t, tOr } from '../lib/i18n';

  let {
    m,
    now,
    menuOpen,
    selected = false,
    selecting = false,
    onmenu,
    onselect,
  }: {
    m: MonitorView;
    now: number;
    menuOpen: boolean;
    selected?: boolean;
    /** Seçim modu: en az bir monitör seçili (dokunmatikte satıra dokunmak seçer). */
    selecting?: boolean;
    /** Satır menüsünü tetikleyici düğmenin yanında açar (null: kapat). */
    onmenu: (m: MonitorView | null, anchor?: HTMLElement) => void;
    /** Seçimi değiştirir; range: Shift ile aralık seçimi. */
    onselect?: (m: MonitorView, range: boolean) => void;
  } = $props();

  const kind = $derived(monitorKind(m));
  const host = $derived(shortTarget(m.type, m.target, m.config));
  // Satırda en fazla 3 etiket; fazlası "+N" olarak.
  const tags = $derived(m.tags ?? []);
  const shownTags = $derived(tags.slice(0, 2));
  // Süren olay: detay sayfasına kısayol (izleyici de görür; liste zaten kapsamla süzülü).
  const incident = $derived(kind === 'down' && m.open_incident_id ? m.open_incident_id : null);
  // Süren konum kesintisi: monitör çalışıyor ama bazı konumlar çalışmıyor (sarı işaret).
  const locOutage = $derived(incident === null && kind !== 'paused' && m.open_partial_incident_id ? m.open_partial_incident_id : null);

  // Alt satır: adres · durum (+ süre); durum metni kırpılmaz, yer daralınca adres
  // kısalır. Kontrol mesajı (neden) altında kendi satırındadır.
  const sub = $derived.by((): { status: string; msg?: string } => {
    switch (kind) {
      case 'paused':
        return { status: STATUS_LABELS.paused };
      case 'maintenance':
        return { status: t('monitors.row.maint') };
      case 'pending':
        // Yeni eklenen, düzenlenen veya yeniden başlatılan monitörde sunucu mesajı
        // temizler; mesaj varsa başarısız kontrol sonrası tekrar deneniyordur.
        return m.last_message ? { status: t('status.retrying'), msg: m.last_message } : { status: t('monitors.firstCheck') };
      case 'up':
        return { status: m.last_change_at ? `${STATUS_LABELS.up} · ${fmtDuration(now - m.last_change_at)}` : STATUS_LABELS.up };
      case 'down':
        return {
          status: m.last_change_at ? `${STATUS_LABELS.down} · ${fmtDuration(now - m.last_change_at)}` : STATUS_LABELS.down,
          msg: m.last_message || undefined,
        };
    }
  });
  const subText = $derived([host, sub.status, sub.msg].filter(Boolean).join(' · '));
  const subClass = $derived(`c-${kind === 'down' ? 'down' : kind === 'pending' ? 'pending' : kind === 'maintenance' ? 'maint' : 'muted'}`);

  // Geniş ekran sütunları ---------------------------------------------------------------
  // Yanıt süresi: son kontrollerin küçük grafiği + son ölçüm (çalışmıyorsa "—").
  const pings = $derived(m.pings ?? []);
  const lastMs = $derived.by(() => {
    if (kind === 'down' || kind === 'paused') return -1;
    if (pings.length) return pings[pings.length - 1];
    return m.last_ping_ms > 0 ? m.last_ping_ms : -1;
  });
  const sparkTone = $derived(kind === 'down' ? 'down' : kind === 'paused' ? 'muted' : 'accent');
  const respTitle = $derived(
    lastMs >= 0 ? t('overview.row.respTitle', { ms: fmtMs(lastMs), n: pings.length }) : t('overview.row.respNone'),
  );

  // SSL: yalnızca sertifika bilgisi olan HTTP(S) monitörlerinde.
  const certDays = $derived(m.type === 'http' && m.cert_expires_at ? certDaysLeft(m.cert_expires_at, now) : null);
  const certTone = $derived(certDays === null ? '' : certDays < 7 ? 'bad' : certDays < 14 ? 'warn' : '');
  const certTitle = $derived(
    certDays === null
      ? t('overview.row.sslNone')
      : m.cert_issuer
        ? t('overview.row.sslTitleIssuer', { date: fmtDay(m.cert_expires_at), issuer: m.cert_issuer })
        : t('overview.row.sslTitle', { date: fmtDay(m.cert_expires_at) }),
  );

  // Konumlar: çok konumlu monitörde her konumun canlı durumu; tek konumluda ana
  // sunucu (monitörün durumuyla). Push/grup monitörünün konumu yoktur.
  type Dot = { key: number; name: string; status: string; ping: number };
  const MAX_DOTS = 5;
  const dots = $derived.by((): Dot[] => {
    if (m.loc_states?.length) return m.loc_states.map((l) => ({ key: l.probe_id, name: l.name, status: l.status, ping: l.ping_ms }));
    if (m.type === 'push' || m.type === 'group' || kind === 'paused') return [];
    const status = kind === 'up' ? 'up' : kind === 'down' ? 'down' : kind === 'pending' ? 'retrying' : 'unknown';
    return [{ key: 0, name: t('overview.row.locMain'), status, ping: lastMs }];
  });
  const shownDots = $derived(dots.length > MAX_DOTS ? dots.slice(0, MAX_DOTS - 1) : dots);
  const dotClass = (s: string) => (s === 'up' ? 'up' : s === 'down' ? 'down' : s === 'retrying' ? 'warn' : 'muted');
  const dotTitle = (d: Dot) => {
    const status = tOr(`overview.row.locStatus.${d.status}`, d.status);
    return d.ping >= 0 && d.status === 'up'
      ? t('overview.row.locTitlePing', { name: d.name, status, ms: fmtMs(d.ping) })
      : t('overview.row.locTitle', { name: d.name, status });
  };

  const href = $derived(`#/monitors/${m.id}`);
  const narrow = () => window.matchMedia('(max-width: 640px)').matches;

  // Dokunmatikte uzun basış satırı seçer (seçim modunu başlatır).
  let pressTimer: ReturnType<typeof setTimeout> | undefined;
  let pressStart: { x: number; y: number } | null = null;
  let suppressClick = false;
  function onPointerDown(e: PointerEvent) {
    // Uzun basıştan sonra tarayıcı tıklama göndermediyse bayrak bir sonraki dokunuşu yutmasın.
    suppressClick = false;
    if (!onselect || e.pointerType === 'mouse') return;
    pressStart = { x: e.clientX, y: e.clientY };
    clearTimeout(pressTimer);
    pressTimer = setTimeout(() => {
      pressStart = null;
      suppressClick = true;
      navigator.vibrate?.(10);
      onselect?.(m, false);
    }, 480);
  }
  function onPointerMove(e: PointerEvent) {
    if (pressStart && Math.hypot(e.clientX - pressStart.x, e.clientY - pressStart.y) > 10) cancelPress();
  }
  function cancelPress() {
    clearTimeout(pressTimer);
    pressStart = null;
  }

  function onRowClick(e: MouseEvent) {
    if (suppressClick) {
      suppressClick = false;
      e.preventDefault();
      return;
    }
    // Dar ekranda seçim modundayken satıra dokunmak seçimi değiştirir.
    if (selecting && onselect && narrow()) {
      onselect(m, false);
      return;
    }
    navigate(`/monitors/${m.id}`);
  }

  function toggleMenu(e: MouseEvent) {
    e.stopPropagation();
    if (menuOpen) onmenu(null);
    else onmenu(m, e.currentTarget as HTMLElement);
  }
</script>

<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
<div
  class="row mrow"
  class:dim={kind === 'paused'}
  class:selectable={!!onselect}
  class:selecting
  class:selected
  class:has-inc={incident !== null}
  onclick={onRowClick}
  onpointerdown={onPointerDown}
  onpointermove={onPointerMove}
  onpointerup={cancelPress}
  onpointercancel={cancelPress}
  onpointerleave={cancelPress}
  oncontextmenu={(e) => {
    // Android uzun basışta bağlam menüsü açar; seçimle çakışmasın.
    if (onselect && (pressStart || suppressClick)) e.preventDefault();
  }}
>
  {#if onselect}
    <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_noninteractive_element_interactions -->
    <label class="sel check mc-sel" onclick={(e) => e.stopPropagation()}>
      <input
        type="checkbox"
        checked={selected}
        onclick={(e) => {
          e.stopPropagation();
          onselect?.(m, e.shiftKey);
        }}
        aria-label={t('monitors.row.select', { name: m.name })}
      />
    </label>
  {/if}
  <div class="ic mc-ic"><StatusIcon {kind} size={32} /></div>
  <div class="info mc-info">
    <div class="name">
      <a
        {href}
        onclick={(e) => {
          e.stopPropagation();
          // Dar ekranda seçim modunda ada dokunmak da seçer (satırın geri kalanı gibi).
          if (suppressClick || (selecting && onselect && narrow())) {
            e.preventDefault();
            if (suppressClick) suppressClick = false;
            else onselect?.(m, false);
          }
        }}>{m.name}</a
      >
      <TypeBadge type={m.type} />
      {#if tags.length}
        <span class="tags">
          {#each shownTags as tg (tg.id + ':' + tg.value)}<TagChip name={tg.name} color={tg.color} value={tg.value} size="sm" />{/each}
        </span>
        {#if tags.length > 2}<span class="more-tags" title={tags.slice(2).map((tg) => (tg.value ? `${tg.name}: ${tg.value}` : tg.name)).join(', ')}>+{tags.length - 2}</span>{/if}
      {/if}
    </div>
    <div class="sub" title={subText}>
      {#if host}<span class="host">{host}</span>{/if}<span class="st {subClass}"
        >{#if host}<span class="sep" aria-hidden="true">·</span>{/if}{sub.status}</span
      >
    </div>
    <!-- Neden (kontrol mesajı) kendi satırında: dar listede de okunur kalsın
         (en fazla iki satır; tamamı title'da). -->
    {#if sub.msg}<div class="why {subClass}" title={sub.msg}>{sub.msg}</div>{/if}
  </div>
  {#if locOutage !== null}
    <a
      class="inc loc-out mc-inc"
      href="#/incidents/{locOutage}"
      onclick={(e) => e.stopPropagation()}
      title={t('monitors.row.locOutageTitle')}
      aria-label={t('incidents.kind.partialLong')}
    >
      <Icon name="map-pin" size={13} />
      <span class="inc-l" aria-hidden="true">{t('incidents.kind.partialLong')}</span><span class="inc-s" aria-hidden="true"
        >{t('incidents.kind.partialLong')}</span
      >
    </a>
  {/if}
  {#if incident !== null}
    <a
      class="inc mc-inc"
      href="#/incidents/{incident}"
      onclick={(e) => e.stopPropagation()}
      title={t('monitors.row.incidentTitle')}
      aria-label={t('monitors.row.viewIncident')}
    >
      <Icon name="zap" size={13} />
      <span class="inc-l" aria-hidden="true">{t('monitors.row.viewIncident')}</span><span class="inc-s" aria-hidden="true"
        >{t('monitors.row.incident')}</span
      >
    </a>
  {/if}
  <div class="locs mc-locs" role="list" aria-label={t('overview.row.locAria')}>
    {#each shownDots as d (d.key)}<span class="ldot {dotClass(d.status)}" role="listitem" title={dotTitle(d)} aria-label={dotTitle(d)}></span>{/each}
    {#if dots.length > shownDots.length}
      <span class="lmore" title={dots.slice(shownDots.length).map(dotTitle).join(', ')}>+{dots.length - shownDots.length}</span>
    {/if}
    {#if dots.length === 0}<span class="none">—</span>{/if}
  </div>
  <div class="resp mc-resp" title={respTitle}>
    <Sparkline values={pings} fail={PING_DOWN} tone={sparkTone} w={64} h={22} />
    <span class="ms" class:slow={lastMs >= 1000}>{lastMs >= 0 ? fmtMs(lastMs) : '—'}</span>
  </div>
  <div class="ssl mc-ssl {certTone}" title={certTitle}>
    {#if certDays === null}
      <span class="none">—</span>
    {:else}
      <Icon name="lock" size={12} />{certDays < 0 ? t('overview.row.sslExpired') : t('overview.row.sslDays', { n: certDays })}
    {/if}
  </div>
  <div class="u mc-u7" class:c-warn={uptimeTone(m.uptime_7d) === 'warn'} class:c-down={uptimeTone(m.uptime_7d) === 'bad'} title={t('overview.row.u7Title')}>{fmtPct(m.uptime_7d)}</div>
  <div class="u mc-u30" class:c-warn={uptimeTone(m.uptime_30d) === 'warn'} class:c-down={uptimeTone(m.uptime_30d) === 'bad'} title={t('overview.row.u30Title')}>{fmtPct(m.uptime_30d)}</div>
  <div class="last mc-last" title={m.last_check_at ? t('overview.row.lastTitle', { date: fmtDateSec(m.last_check_at) }) : t('overview.row.never')}>
    {m.last_check_at ? fmtRelative(m.last_check_at, now) : '—'}
  </div>
  <div class="interval mc-int" title={t('monitors.row.interval')}>
    <Icon name="refresh" size={13} />
    {fmtInterval(m.interval)}
  </div>
  <div class="uptime mc-bars">
    <UptimeBars bars={m.bars} />
    <div class="pct" class:c-warn={uptimeTone(m.uptime_24h) === 'warn'} class:c-down={uptimeTone(m.uptime_24h) === 'bad'}>{fmtPct(m.uptime_24h)}</div>
  </div>
  {#if session.canEdit}
    <div class="menu mc-menu">
      <button
        type="button"
        class="btn ghost icon"
        class:open={menuOpen}
        aria-label={t('monitors.row.actionsFor', { name: m.name })}
        aria-haspopup="menu"
        aria-expanded={menuOpen}
        onclick={toggleMenu}
      >
        <Icon name="more" />
      </button>
    </div>
  {/if}
</div>

<style>
  /* Izgara ve sütunlar: monitor-grid.css (başlıkla ortak). */
  .row {
    border-bottom: 1px solid var(--border);
    cursor: pointer;
    transition: background 0.12s;
    /* Uzun basış seçim başlatır: iOS'ta metin seçimi ve büyüteç açılmasın. */
    -webkit-touch-callout: none;
    -webkit-user-select: none;
    user-select: none;
  }
  /* Dokunmatik ekranda dokunulan satır "hover" rengiyle takılı kalmasın. */
  @media (hover: hover) {
    .row:hover {
      background: var(--card-hover);
    }
  }
  .row.selected {
    background: color-mix(in srgb, var(--accent) 7%, var(--card));
  }
  @media (hover: hover) {
    .row.selected:hover {
      background: color-mix(in srgb, var(--accent) 10%, var(--card));
    }
  }
  .row.dim .info,
  .row.dim .uptime,
  .row.dim .resp,
  .row.dim .ssl,
  .row.dim .u,
  .row.dim .last {
    opacity: 0.6;
  }
  /* Seçim kutusu: masaüstünde satırın üzerine gelince veya seçim varken görünür. */
  .sel {
    align-items: center;
    opacity: 0;
    transition: opacity 0.12s;
  }
  .sel input {
    margin: 0;
  }
  .row:hover .sel,
  .row.selecting .sel,
  .sel:focus-within {
    opacity: 1;
  }
  .ic {
    display: flex;
  }
  .name {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
  }
  .name a {
    flex: 0 1 auto;
    color: var(--text);
    font-weight: 700;
    font-size: 0.97rem;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    min-width: 0;
  }
  /* Etiketler addan çok daha önce daralır; sığmayan rozet kırpılır (ad önceliklidir). */
  .tags {
    display: flex;
    align-items: center;
    gap: 4px;
    min-width: 0;
    overflow: hidden;
    flex: 0 1000 auto;
  }
  .tags :global(.tag-chip) {
    flex-shrink: 1;
    min-width: 3.6em;
    max-width: 150px;
  }
  .more-tags {
    font-size: 0.72rem;
    font-weight: 600;
    color: var(--muted);
    flex-shrink: 0;
  }
  /* Adres · durum. Durum metni hiç kırpılmaz; yer daralınca adres kısalır. */
  .sub {
    display: flex;
    align-items: baseline;
    min-width: 0;
    font-size: 0.83rem;
    margin-top: 2px;
    white-space: nowrap;
  }
  .host {
    flex: 0 1 auto;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    color: var(--text-2);
    font-weight: 500;
  }
  .st {
    flex: 0 0 auto;
  }
  /* Neden: kendi satırında, en fazla iki satır. */
  .why {
    margin-top: 2px;
    font-size: 0.8rem;
    line-height: 1.35;
    overflow: hidden;
    display: -webkit-box;
    -webkit-box-orient: vertical;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    overflow-wrap: anywhere;
  }
  .sep {
    color: var(--muted);
    margin: 0 6px;
  }
  .c-muted {
    color: var(--muted);
  }
  .row.selected .c-muted {
    color: var(--text-2);
  }
  .inc {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    height: 28px;
    padding: 0 12px 0 10px;
    border-radius: 999px;
    border: 1px solid var(--down-border);
    background: var(--down-soft);
    color: var(--down-text);
    font-size: 0.8rem;
    font-weight: 600;
    white-space: nowrap;
    text-decoration: none;
    transition:
      background 0.12s,
      border-color 0.12s;
  }
  .inc-s {
    display: none;
  }
  /* Konum kesintisi: turuncu (durum yine "Çalışıyor"). */
  .inc.loc-out {
    border-color: var(--pending-border);
    background: var(--pending-soft);
    color: var(--pending-text);
  }
  @media (hover: hover) {
    .inc.loc-out:hover {
      background: color-mix(in srgb, var(--pending) 24%, transparent);
      border-color: var(--pending);
    }
  }
  /* Dokunmatikte küçük "Olay" çipine 44 px'lik görünmez dokunma alanı. */
  @media (max-width: 900px), (pointer: coarse) {
    .inc {
      position: relative;
    }
    .inc::after {
      content: '';
      position: absolute;
      inset: -9px -6px;
    }
  }
  @media (hover: hover) {
    .inc:hover {
      background: color-mix(in srgb, var(--down) 24%, transparent);
      border-color: var(--down);
      text-decoration: none;
    }
  }
  .interval {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 5px;
    color: var(--muted);
    font-size: 0.83rem;
    white-space: nowrap;
  }
  /* Konum noktaları */
  .locs {
    display: flex;
    align-items: center;
    gap: 5px;
    min-width: 0;
  }
  .ldot {
    width: 9px;
    height: 9px;
    border-radius: 50%;
    background: var(--paused);
    flex-shrink: 0;
  }
  .ldot.up {
    background: var(--up);
  }
  .ldot.down {
    background: var(--down);
    box-shadow: 0 0 0 3px var(--down-soft);
  }
  .ldot.warn {
    background: var(--pending);
  }
  .lmore {
    font-size: 0.72rem;
    font-weight: 600;
    color: var(--muted);
  }
  .none {
    color: var(--muted);
    opacity: 0.7;
  }
  /* Yanıt süresi: küçük grafik + son değer */
  .resp {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 8px;
  }
  .ms {
    min-width: 54px;
    text-align: right;
    font-size: 0.85rem;
    font-weight: 600;
    color: var(--text-2);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }
  .ms.slow {
    color: var(--pending);
  }
  .ssl {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 4px;
    font-size: 0.83rem;
    font-weight: 600;
    color: var(--text-2);
    white-space: nowrap;
    font-variant-numeric: tabular-nums;
  }
  .ssl :global(svg) {
    color: var(--muted);
  }
  .ssl.warn,
  .ssl.warn :global(svg) {
    color: var(--pending);
  }
  .ssl.bad,
  .ssl.bad :global(svg) {
    color: var(--down-text-2);
  }
  .u {
    text-align: right;
    font-size: 0.85rem;
    font-weight: 600;
    color: var(--text-2);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }
  .u.c-down {
    color: var(--down-text-2);
  }
  .u.c-warn {
    color: var(--warn-text);
  }
  .last {
    text-align: right;
    font-size: 0.8rem;
    color: var(--muted);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .pct {
    text-align: right;
    font-size: 0.8rem;
    font-weight: 600;
    margin-top: 4px;
    color: var(--text-2);
  }
  .pct.c-down {
    color: var(--down-text-2);
  }
  .pct.c-warn {
    color: var(--warn-text);
  }
  .menu .btn.open {
    background: var(--card-2);
    color: var(--text);
  }

  /* Dar listede yalnızca son değer (grafik gizlenir): liste kutusu (MonitorList
     .list) "mlist" adlı kapsayıcıdır; sütunların kendisi monitor-grid.css'te. */
  @container mlist (max-width: 1119px) {
    .resp :global(.spark) {
      display: none;
    }
  }
  @media (min-width: 641px) {
    /* Masaüstünde olay düğmesi ad sütununa yer bırakmak için yalnızca simge;
       çok geniş listede kısa etiketle ("Olay"). Tam metin title'da. */
    .inc {
      width: 28px;
      padding: 0;
      justify-content: center;
    }
    .inc-l,
    .inc-s {
      display: none;
    }
    @container mlist (min-width: 1440px) {
      .inc {
        width: auto;
        padding: 0 12px 0 10px;
      }
      .inc-s {
        display: inline;
      }
    }
  }
  @media (max-width: 640px) {
    .inc {
      height: 26px;
      padding: 0 9px 0 8px;
      font-size: 0.76rem;
      gap: 4px;
    }
    .inc-l {
      display: none;
    }
    .inc-s {
      display: inline;
    }
    .name {
      flex-wrap: wrap;
      row-gap: 4px;
    }
    /* Telefonda adres ve durum sığmazsa durum alt satıra iner (adres iki harfe
       inmesin); ayraç yerine boşluk kullanılır, satır başında "·" kalmasın. */
    .sub {
      flex-wrap: wrap;
      column-gap: 8px;
      row-gap: 1px;
    }
    .host {
      max-width: 100%;
    }
    .sub .sep {
      display: none;
    }
    .tags {
      order: 3;
      flex: 0 1 auto;
      max-width: calc(100% - 34px);
      flex-wrap: wrap;
      row-gap: 4px;
    }
    .more-tags {
      order: 4;
    }
    /* Etiketler ad satırının altına iner. */
    .name::after {
      content: '';
      order: 2;
      flex-basis: 100%;
    }
    .uptime {
      display: flex;
      align-items: center;
      gap: 10px;
      padding-right: 10px;
    }
    .uptime :global(.bars) {
      flex: 1;
      height: 20px;
    }
    .pct {
      margin: 0;
      min-width: 58px;
    }
  }
</style>
