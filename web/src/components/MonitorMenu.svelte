<script lang="ts" module>
  export type MenuAction = 'select' | 'notify' | 'maint' | 'tags' | 'page' | 'clone' | 'pause' | 'reset' | 'delete';
</script>

<script lang="ts">
  // Monitör listesindeki satır menüsü. Liste kendi içinde kaydığı için menü ekrana
  // sabitlenir (position: fixed) ve tetikleyici düğmenin yanında açılır; aşağıya
  // sığmazsa yukarı, hiçbir yere sığmazsa ekran içinde kaydırılabilir olarak.
  // Dar ekranda alttan açılan sayfa (bottom sheet) olur. Klavye: ↑/↓, Home/End,
  // Escape (kapatıp düğmeye döner), Tab (kapatır).
  import { onMount, tick } from 'svelte';
  import type { MonitorView } from '../lib/api';
  import { monitorKind } from '../lib/format';
  import { lockScroll } from '../lib/ui.svelte';
  import Icon, { type IconName } from './Icon.svelte';
  import StatusIcon from './StatusIcon.svelte';

  let {
    m,
    anchor,
    selectable = false,
    onclose,
    onaction,
  }: {
    m: MonitorView;
    anchor: HTMLElement;
    /** Dokunmatik ekranda "Seç" ile çoklu seçim başlatılabilir. */
    selectable?: boolean;
    onclose: (refocus: boolean) => void;
    onaction: (a: MenuAction) => void;
  } = $props();

  interface Item {
    key: MenuAction | 'edit';
    label: string;
    icon: IconName;
    href?: string;
    danger?: boolean;
    /** Öncesine ayırıcı çizgi. */
    sep?: boolean;
  }

  const items = $derived.by((): Item[] => {
    const list: Item[] = [];
    if (selectable) list.push({ key: 'select', label: 'Seç', icon: 'check-square' });
    list.push(
      { key: 'edit', label: 'Düzenle', icon: 'edit', href: `#/monitors/${m.id}/edit`, sep: selectable },
      { key: 'notify', label: 'Bildirimler…', icon: 'bell' },
      { key: 'maint', label: 'Bakıma al…', icon: 'wrench' },
      { key: 'tags', label: 'Etiketler…', icon: 'tag' },
      { key: 'page', label: 'Durum sayfasına ekle…', icon: 'layout' },
      { key: 'clone', label: 'Kopyala', icon: 'copy' },
      { key: 'pause', label: m.active ? 'Durdur' : 'Başlat', icon: m.active ? 'pause' : 'play', sep: true },
      { key: 'reset', label: 'İstatistikleri sıfırla', icon: 'rotate-ccw' },
      { key: 'delete', label: 'Sil', icon: 'trash', danger: true, sep: true },
    );
    return list;
  });

  const sheet = typeof window !== 'undefined' && window.matchMedia('(max-width: 640px)').matches;

  let el: HTMLDivElement | undefined = $state();
  let style = $state('visibility:hidden');
  let up = $state(false);

  function place() {
    if (!el || sheet) return;
    const r = anchor.getBoundingClientRect();
    const h = el.scrollHeight;
    const vh = window.innerHeight;
    const right = `right:${Math.max(8, Math.round(window.innerWidth - r.right))}px`;
    const below = vh - r.bottom - 12;
    const above = r.top - 12;
    if (below >= h) {
      up = false;
      style = `${right};top:${Math.round(r.bottom + 4)}px`;
    } else if (above >= h) {
      up = true;
      style = `${right};bottom:${Math.round(vh - r.top + 4)}px`;
    } else {
      // Hiçbir yöne sığmıyor (çok kısa ekran): ekranın içinde, kaydırılabilir.
      up = false;
      style = `${right};top:8px;max-height:${vh - 16}px;overflow-y:auto`;
    }
  }

  const buttons = () => [...(el?.querySelectorAll<HTMLElement>('[role=menuitem]') ?? [])];

  onMount(() => {
    // Alttan açılan sayfa açıkken arkadaki liste kaymasın.
    const unlock = sheet ? lockScroll() : undefined;
    place();
    tick().then(() => buttons()[0]?.focus({ preventScroll: true }));
    return unlock;
  });

  function onKey(e: KeyboardEvent) {
    const list = buttons();
    const i = list.indexOf(document.activeElement as HTMLElement);
    switch (e.key) {
      case 'ArrowDown':
        e.preventDefault();
        list[(i + 1) % list.length]?.focus();
        break;
      case 'ArrowUp':
        e.preventDefault();
        list[(i - 1 + list.length) % list.length]?.focus();
        break;
      case 'Home':
        e.preventDefault();
        list[0]?.focus();
        break;
      case 'End':
        e.preventDefault();
        list[list.length - 1]?.focus();
        break;
      case 'Escape':
        e.preventDefault();
        e.stopPropagation();
        onclose(true);
        break;
      case 'Tab':
        onclose(false);
        break;
    }
  }

  function pick(it: Item) {
    if (it.key === 'edit') return;
    onaction(it.key);
  }

  const kind = $derived(monitorKind(m));
</script>

<!-- Mobilde adres çubuğu gizlenirken de resize gelir; alttan açılan sayfa kapanmasın. -->
<svelte:window onresize={() => !sheet && onclose(false)} />

{#if sheet}
  <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
  <div class="backdrop" onclick={(e) => { e.stopPropagation(); onclose(false); }}></div>
{/if}
<!-- svelte-ignore a11y_click_events_have_key_events -->
<div
  bind:this={el}
  class="menu"
  class:sheet
  class:up
  style={sheet ? '' : style}
  role="menu"
  tabindex="-1"
  aria-label="{m.name} için işlemler"
  onkeydown={onKey}
  onclick={(e) => e.stopPropagation()}
>
  {#if sheet}
    <div class="sheet-head">
      <span class="grip" aria-hidden="true"></span>
      <div class="sh-title">
        <StatusIcon {kind} size={22} />
        <b>{m.name}</b>
      </div>
    </div>
  {/if}
  {#each items as it (it.key)}
    {#if it.sep}<div class="sep" role="separator"></div>{/if}
    {#if it.href}
      <a role="menuitem" tabindex="-1" href={it.href} onclick={() => onclose(false)}><Icon name={it.icon} size={16} /> {it.label}</a>
    {:else}
      <button role="menuitem" tabindex="-1" type="button" class:danger={it.danger} onclick={() => pick(it)}>
        <Icon name={it.icon} size={16} />
        {it.label}
      </button>
    {/if}
  {/each}
</div>

<style>
  .menu {
    position: fixed;
    z-index: 30;
    min-width: 218px;
    background: var(--bg-elev);
    border: 1px solid var(--border-strong);
    border-radius: 10px;
    box-shadow: var(--shadow);
    padding: 5px;
    display: flex;
    flex-direction: column;
    animation: pop 0.12s ease-out;
    outline: none;
    overscroll-behavior: contain;
  }
  .menu.up {
    animation-name: pop-up;
  }
  .menu a,
  .menu button {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 7px 10px;
    border-radius: 7px;
    background: none;
    border: none;
    color: var(--text);
    font: inherit;
    font-size: 0.9rem;
    cursor: pointer;
    text-align: left;
    text-decoration: none;
    white-space: nowrap;
  }
  .menu a :global(svg),
  .menu button :global(svg) {
    color: var(--muted);
  }
  .menu a:focus-visible,
  .menu button:focus-visible {
    background: var(--card-2);
    outline: none;
  }
  @media (hover: hover) {
    .menu a:hover,
    .menu button:hover {
      background: var(--card-2);
    }
    .menu a:hover :global(svg),
    .menu button:hover :global(svg) {
      color: var(--text-2);
    }
  }
  .menu .danger,
  .menu .danger :global(svg) {
    color: var(--down-text-2);
  }
  .sep {
    height: 1px;
    margin: 5px 4px;
    background: var(--border);
  }
  @keyframes pop {
    from {
      opacity: 0;
      transform: translateY(-4px);
    }
  }
  @keyframes pop-up {
    from {
      opacity: 0;
      transform: translateY(4px);
    }
  }

  /* Dar ekran: alttan açılan sayfa (sekme çubuğunun da üstünde). */
  .backdrop {
    position: fixed;
    inset: 0;
    z-index: 40;
    background: var(--overlay);
    animation: fade 0.15s ease-out;
  }
  .menu.sheet {
    left: 0;
    right: 0;
    bottom: 0;
    z-index: 41;
    min-width: 0;
    max-height: calc(100dvh - 48px);
    overflow-y: auto;
    border-radius: 16px 16px 0 0;
    border-bottom: none;
    padding: 4px 10px calc(12px + env(safe-area-inset-bottom));
    animation: slide 0.2s ease-out;
  }
  .menu.sheet a,
  .menu.sheet button {
    min-height: 44px;
    padding: 11px 12px;
    font-size: 0.97rem;
    gap: 14px;
  }
  .menu.sheet .sep {
    margin: 4px 8px;
  }
  .sheet-head {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 10px;
    padding: 8px 4px 10px;
    margin-bottom: 4px;
    border-bottom: 1px solid var(--border);
  }
  .grip {
    width: 36px;
    height: 4px;
    border-radius: 2px;
    background: var(--border-strong);
  }
  .sh-title {
    align-self: stretch;
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 0 8px;
    min-width: 0;
  }
  .sh-title b {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  @keyframes slide {
    from {
      transform: translateY(100%);
    }
  }
  @keyframes fade {
    from {
      opacity: 0;
    }
  }
</style>
