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

  let {
    m,
    now,
    menuOpen,
    onmenu,
    onpause,
    ondelete,
  }: {
    m: MonitorView;
    now: number;
    menuOpen: boolean;
    onmenu: (id: number | null) => void;
    onpause: (m: MonitorView) => void;
    ondelete: (m: MonitorView) => void;
  } = $props();

  const kind = $derived(monitorKind(m));
  const host = $derived(shortTarget(m.type, m.target));
  // Satırda en fazla 3 etiket; fazlası "+N" olarak.
  const tags = $derived(m.tags ?? []);
  const shownTags = $derived(tags.slice(0, 3));

  const sub = $derived.by(() => {
    switch (kind) {
      case 'paused':
        return STATUS_LABELS.paused;
      case 'maintenance':
        return 'Bakımda · bildirim gönderilmez';
      case 'pending':
        // Yeni eklenen, düzenlenen veya yeniden başlatılan monitörde sunucu mesajı
        // temizler; mesaj varsa başarısız kontrol sonrası tekrar deneniyordur.
        return m.last_message ? `Tekrar deneniyor · ${m.last_message}` : 'İlk kontrol bekleniyor';
      case 'up':
        return m.last_change_at ? `Çalışıyor · ${fmtDuration(now - m.last_change_at)}` : 'Çalışıyor';
      case 'down': {
        let s = 'Çalışmıyor';
        if (m.last_change_at) s += ` · ${fmtDuration(now - m.last_change_at)}`;
        if (m.last_message) s += ` · ${m.last_message}`;
        return s;
      }
    }
  });

  const href = $derived(`#/monitors/${m.id}`);

  // Menü ekrana sabitlenir (liste kendi içinde kaydığında kutunun kenarında
  // kesilmesin); ekranın altına sığmıyorsa yukarı açılır (mobilde sekme çubuğu
  // altında kalmasın).
  let menuStyle = $state('');
  let menuUp = $state(false);
  function toggleMenu(e: MouseEvent) {
    if (menuOpen) {
      onmenu(null);
      return;
    }
    const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
    const right = `right:${Math.round(window.innerWidth - r.right)}px`;
    menuUp = window.innerHeight - r.bottom < 190;
    menuStyle = menuUp
        ? `${right};bottom:${Math.round(window.innerHeight - r.top + 4)}px`
        : `${right};top:${Math.round(r.bottom + 4)}px`;
    onmenu(m.id);
  }
</script>

<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
<div class="row" class:dim={kind === 'paused'} onclick={() => navigate(`/monitors/${m.id}`)}>
  <div class="ic"><StatusIcon {kind} size={32} /></div>
  <div class="info">
    <div class="name">
      <a {href} onclick={(e) => e.stopPropagation()}>{m.name}</a>
      <TypeBadge type={m.type} />
      {#if tags.length}
        <span class="tags">
          {#each shownTags as t (t.id)}<TagChip name={t.name} color={t.color} value={t.value} size="sm" />{/each}
        </span>
        {#if tags.length > 3}<span class="more-tags" title={tags.slice(3).map((t) => (t.value ? `${t.name}: ${t.value}` : t.name)).join(', ')}>+{tags.length - 3}</span>{/if}
      {/if}
    </div>
    <div class="sub" title={host ? `${host} · ${sub}` : sub}>
      {#if host}<span class="host">{host}</span><span class="sep" aria-hidden="true">·</span>{/if}<span
        class="c-{kind === 'down' ? 'down' : kind === 'pending' ? 'pending' : kind === 'maintenance' ? 'maint' : 'muted'}">{sub}</span
      >
    </div>
  </div>
  <div class="interval" title="Kontrol aralığı">
    <Icon name="refresh" size={13} />
    {fmtInterval(m.interval)}
  </div>
  <div class="uptime">
    <UptimeBars bars={m.bars} />
    <div class="pct" class:c-down={m.uptime_24h !== null && m.uptime_24h < 99}>{fmtPct(m.uptime_24h)}</div>
  </div>
  {#if session.canEdit}
  <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
  <div class="menu" onclick={(e) => e.stopPropagation()}>
    <button
      type="button"
      class="btn ghost icon"
      aria-label="İşlemler"
      aria-expanded={menuOpen}
      onclick={toggleMenu}
    >
      <Icon name="more" />
    </button>
    {#if menuOpen}
      <div class="dropdown" class:up={menuUp} style={menuStyle} role="menu">
        <a role="menuitem" href="#/monitors/{m.id}/edit" onclick={() => onmenu(null)}><Icon name="edit" size={15} /> Düzenle</a>
        <button
          role="menuitem"
          type="button"
          onclick={() => {
            onmenu(null);
            onpause(m);
          }}
        >
          <Icon name={m.active ? 'pause' : 'play'} size={15} />
          {m.active ? 'Durdur' : 'Başlat'}
        </button>
        <button
          role="menuitem"
          type="button"
          class="danger"
          onclick={() => {
            onmenu(null);
            ondelete(m);
          }}
        >
          <Icon name="trash" size={15} /> Sil
        </button>
      </div>
    {/if}
  </div>
  {/if}
</div>

<style>
  .row {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr) auto auto auto;
    grid-template-areas: 'ic info interval uptime menu';
    align-items: center;
    column-gap: 16px;
    padding: 14px 10px 14px 18px;
    border-bottom: 1px solid var(--border);
    cursor: pointer;
    transition: background 0.12s;
  }
  /* Dokunmatik ekranda dokunulan satır "hover" rengiyle takılı kalmasın. */
  @media (hover: hover) {
    .row:hover {
      background: var(--card-hover);
    }
  }
  .row.dim .info,
  .row.dim .uptime {
    opacity: 0.6;
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
    min-width: 4em;
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
  .sub {
    font-size: 0.83rem;
    margin-top: 2px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .host {
    color: var(--text-2);
    font-weight: 500;
  }
  .sep {
    color: var(--muted);
    margin: 0 6px;
  }
  .c-muted {
    color: var(--muted);
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
    position: relative;
  }
  .dropdown {
    position: fixed;
    animation: pop 0.12s ease-out;
    z-index: 30;
    min-width: 160px;
    background: var(--bg-elev);
    border: 1px solid var(--border-strong);
    border-radius: 10px;
    box-shadow: var(--shadow);
    padding: 5px;
    display: flex;
    flex-direction: column;
  }
  .dropdown a,
  .dropdown button {
    display: flex;
    align-items: center;
    gap: 9px;
    padding: 8px 10px;
    border-radius: 7px;
    background: none;
    border: none;
    color: var(--text);
    font: inherit;
    font-size: 0.9rem;
    cursor: pointer;
    text-align: left;
    text-decoration: none;
  }
  .dropdown a:hover,
  .dropdown button:hover,
  .dropdown a:focus-visible,
  .dropdown button:focus-visible {
    background: var(--card-2);
    outline: none;
  }
  .dropdown .danger {
    color: var(--down-text-2);
  }
  @keyframes pop {
    from {
      opacity: 0;
      transform: translateY(-4px);
    }
  }
  .dropdown.up {
    animation-name: pop-up;
  }
  @keyframes pop-up {
    from {
      opacity: 0;
      transform: translateY(4px);
    }
  }

  /* Dar masaüstünde (kenar çubuğu + yan panel varken) ada daha çok yer bırak. */
  @media (max-width: 1400px) and (min-width: 901px), (max-width: 760px) {
    .uptime {
      width: 180px;
    }
  }
  @media (max-width: 640px) {
    .row {
      grid-template-columns: auto minmax(0, 1fr) auto;
      grid-template-areas:
        'ic info menu'
        '. uptime uptime';
      column-gap: 12px;
      row-gap: 8px;
      padding: 12px 6px 12px 14px;
    }
    .interval {
      display: none;
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
