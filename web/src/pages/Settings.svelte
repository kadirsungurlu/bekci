<script lang="ts">
  import type { SettingsTab } from '../lib/router.svelte';
  import { session } from '../lib/session.svelte';
  import Icon, { type IconName } from '../components/Icon.svelte';
  import Account from './settings/Account.svelte';
  import Users from './settings/Users.svelte';
  import General from './settings/General.svelte';
  import Audit from './settings/Audit.svelte';
  import Tags from './settings/Tags.svelte';
  import Probes from './settings/Probes.svelte';
  import Backup from './settings/Backup.svelte';

  let { tab }: { tab: SettingsTab } = $props();

  // need: sekmeyi görebilecek en düşük rol (editör: etiketler; yönetici: diğerleri).
  const TABS: { key: SettingsTab; href: string; label: string; icon: IconName; need: 'viewer' | 'editor' | 'admin' }[] = [
    { key: 'account', href: '#/settings', label: 'Hesabım', icon: 'user', need: 'viewer' },
    { key: 'users', href: '#/settings/users', label: 'Kullanıcılar', icon: 'users', need: 'admin' },
    { key: 'general', href: '#/settings/general', label: 'Genel', icon: 'settings', need: 'admin' },
    { key: 'tags', href: '#/settings/tags', label: 'Etiketler', icon: 'tag', need: 'editor' },
    { key: 'probes', href: '#/settings/probes', label: 'Kontrol noktaları', icon: 'map-pin', need: 'admin' },
    { key: 'backup', href: '#/settings/backup', label: 'Yedekle / Geri yükle', icon: 'archive', need: 'admin' },
    { key: 'audit', href: '#/settings/audit', label: 'İşlem kaydı', icon: 'list', need: 'admin' },
  ];

  const can = (need: 'viewer' | 'editor' | 'admin') => need === 'viewer' || (need === 'editor' ? session.canEdit : session.isAdmin);
  const visible = $derived(TABS.filter((t) => can(t.need)));
  const allowed = $derived(visible.some((t) => t.key === tab));
</script>

<div class="page-head">
  <h1>Ayarlar<span class="dot">.</span></h1>
</div>

{#if visible.length > 1}
  <nav class="stabs" aria-label="Ayar bölümleri">
    {#each visible as t (t.key)}
      <a href={t.href} class:active={t.key === tab} aria-current={t.key === tab ? 'page' : undefined}>
        <Icon name={t.icon} size={16} />
        {t.label}
      </a>
    {/each}
  </nav>
{/if}

{#if !allowed}
  <div class="card empty">
    <h3>Bu bölüm için yetkiniz yok</h3>
    <p>Kullanıcıları, genel ayarları, kontrol noktalarını, yedekleri ve işlem kaydını yalnızca yönetici rolündeki hesaplar görebilir; etiketleri editörler de yönetebilir.</p>
    <a class="btn primary" href="#/settings">Hesabıma dön</a>
  </div>
{:else if tab === 'account'}
  <Account />
{:else if tab === 'users'}
  <Users />
{:else if tab === 'general'}
  <General />
{:else if tab === 'tags'}
  <Tags />
{:else if tab === 'probes'}
  <Probes />
{:else if tab === 'backup'}
  <Backup />
{:else if tab === 'audit'}
  <Audit />
{/if}

<style>
  .stabs {
    display: flex;
    gap: 4px;
    border-bottom: 1px solid var(--border);
    margin: -6px 0 22px;
    overflow-x: auto;
    scrollbar-width: none;
  }
  .stabs::-webkit-scrollbar {
    display: none;
  }
  .stabs a {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    padding: 10px 14px;
    color: var(--text-2);
    font-weight: 600;
    font-size: 0.92rem;
    border-bottom: 2px solid transparent;
    margin-bottom: -1px;
    white-space: nowrap;
    text-decoration: none;
  }
  .stabs a.active {
    color: var(--text);
    border-bottom-color: var(--accent);
  }
  .stabs a.active :global(svg) {
    color: var(--accent-text);
  }
  .stabs a:focus-visible {
    outline-offset: -2px;
    border-radius: 6px;
  }
  @media (hover: hover) {
    .stabs a:hover {
      color: var(--text);
    }
  }
  @media (max-width: 640px) {
    .stabs {
      margin-bottom: 16px;
      /* Sekmeler kaydırılabilir; kenara kadar uzansın. */
      margin-left: -16px;
      margin-right: -16px;
      padding: 0 12px;
    }
    .stabs a {
      padding: 10px 10px;
      font-size: 0.88rem;
    }
  }
</style>
