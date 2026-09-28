<script lang="ts">
  import type { MonitorView } from '../lib/api';
  import { navigate } from '../lib/router.svelte';
  import { STATUS_LABELS, fmtDuration, fmtInterval, fmtPct, monitorKind } from '../lib/format';
  import { session } from '../lib/session.svelte';
  import StatusIcon from './StatusIcon.svelte';
  import TypeBadge from './TypeBadge.svelte';
  import TagChip from './TagChip.svelte';
  import UptimeBars from './UptimeBars.svelte';
  import Icon from './Icon.svelte';
  import { shortTarget } from '../lib/monitorTypes';
  import { t } from '../lib/i18n';

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
  const shownTags = $derived(tags.slice(0, 3));
  // Süren olay: detay sayfasına kısayol (izleyici de görür; liste zaten kapsamla süzülü).
  const incident = $derived(kind === 'down' && m.open_incident_id ? m.open_incident_id : null);

  // Alt satır: durum (+ süre) her zaman görünür; yer daralınca önce kontrol mesajı,
  // sonra adres kısalır (durum metni kırpılmasın).
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
  class="row"
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
    <label class="sel check" onclick={(e) => e.stopPropagation()}>
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
  <div class="ic"><StatusIcon {kind} size={32} /></div>
  <div class="info">
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
        {#if tags.length > 3}<span class="more-tags" title={tags.slice(3).map((tg) => (tg.value ? `${tg.name}: ${tg.value}` : tg.name)).join(', ')}>+{tags.length - 3}</span>{/if}
      {/if}
    </div>
    <div class="sub" title={subText}>
      {#if host}<span class="host">{host}</span>{/if}<span class="st {subClass}"
        >{#if host}<span class="sep" aria-hidden="true">·</span>{/if}{sub.status}</span
      >{#if sub.msg}<span class="msg {subClass}"><span class="sep" aria-hidden="true">·</span>{sub.msg}</span>{/if}
    </div>
  </div>
  {#if incident !== null}
    <a
      class="inc"
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
  <div class="interval" title={t('monitors.row.interval')}>
    <Icon name="refresh" size={13} />
    {fmtInterval(m.interval)}
  </div>
  <div class="uptime">
    <UptimeBars bars={m.bars} />
    <div class="pct" class:c-down={m.uptime_24h !== null && m.uptime_24h < 99}>{fmtPct(m.uptime_24h)}</div>
  </div>
  {#if session.canEdit}
    <div class="menu">
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
  .row {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr) auto auto auto auto;
    grid-template-areas: 'ic info inc interval uptime menu';
    align-items: center;
    column-gap: 16px;
    padding: 14px 10px 14px 18px;
    border-bottom: 1px solid var(--border);
    cursor: pointer;
    transition: background 0.12s;
    /* Uzun basış seçim başlatır: iOS'ta metin seçimi ve büyüteç açılmasın. */
    -webkit-touch-callout: none;
    -webkit-user-select: none;
    user-select: none;
  }
  .row.selectable {
    grid-template-columns: auto auto minmax(0, 1fr) auto auto auto auto;
    grid-template-areas: 'sel ic info inc interval uptime menu';
    column-gap: 14px;
    padding-left: 14px;
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
  .row.dim .uptime {
    opacity: 0.6;
  }
  /* Seçim kutusu: masaüstünde satırın üzerine gelince veya seçim varken görünür. */
  .sel {
    grid-area: sel;
    align-items: center;
    padding: 8px 2px;
    margin: -8px -2px;
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
    grid-area: ic;
    display: flex;
  }
  .info {
    grid-area: info;
    min-width: 0;
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
  /* Adres · durum · mesaj. Durum metni hiç kırpılmaz; yer daralınca önce kontrol
     mesajı, o bitince adres kısalır (ızgara izleri bu önceliği verir). */
  .sub {
    display: grid;
    grid-template-columns: minmax(0, max-content) max-content minmax(0, 1fr);
    align-items: baseline;
    min-width: 0;
    font-size: 0.83rem;
    margin-top: 2px;
    overflow: hidden;
    white-space: nowrap;
  }
  .sub > span {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .host {
    grid-column: 1;
    color: var(--text-2);
    font-weight: 500;
  }
  .st {
    grid-column: 2;
  }
  .msg {
    grid-column: 3;
  }
  .sep {
    color: var(--muted);
    margin: 0 6px;
  }
  .c-muted {
    color: var(--muted);
  }
  .inc {
    grid-area: inc;
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
    grid-area: interval;
    display: flex;
    align-items: center;
    gap: 5px;
    color: var(--muted);
    font-size: 0.83rem;
    white-space: nowrap;
  }
  .uptime {
    grid-area: uptime;
    width: 246px;
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
  .menu {
    grid-area: menu;
  }
  .menu .btn.open {
    background: var(--card-2);
    color: var(--text);
  }

  /* Liste daraldıkça (kenar çubuğu + yan panel, tablet) ada ve duruma yer bırak:
     liste kutusu (MonitorList .list) "mlist" adlı kapsayıcıdır. */
  @container mlist (max-width: 900px) {
    .uptime {
      width: 180px;
    }
    /* Olay düğmesi varken aralık sütunu gizlenir (ad sıkışmasın). */
    .row.has-inc .interval {
      display: none;
    }
  }
  @media (min-width: 641px) {
    /* Olay düğmesi önce kısa etikete ("Olay"), sonra yalnızca simgeye iner. */
    @container mlist (max-width: 720px) {
      .inc-l {
        display: none;
      }
      .inc-s {
        display: inline;
      }
    }
    @container mlist (max-width: 600px) {
      .interval {
        display: none;
      }
      .inc {
        width: 28px;
        padding: 0;
        justify-content: center;
      }
      .inc-s {
        display: none;
      }
    }
  }
  @media (max-width: 640px) {
    .row,
    .row.selectable {
      grid-template-columns: auto minmax(0, 1fr) auto auto;
      grid-template-areas:
        'ic info inc menu'
        '. uptime uptime uptime';
      column-gap: 10px;
      row-gap: 8px;
      padding: 12px 6px 12px 14px;
    }
    /* Seçim kutusu yalnızca seçim modunda, durum ikonunun yerinde. */
    .row.selectable .sel {
      display: none;
      grid-area: ic;
      justify-self: center;
    }
    .row.selecting .sel {
      display: flex;
      opacity: 1;
      padding: 7px;
      margin: 0;
    }
    .row.selecting .ic {
      display: none;
    }
    .interval {
      display: none;
    }
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
      width: auto;
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
