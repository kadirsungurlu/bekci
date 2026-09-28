<script lang="ts">
  import type { SettingsTab } from '../lib/router.svelte';
  import { session } from '../lib/session.svelte';
  import Icon, { type IconName } from '../components/Icon.svelte';
  import { t } from '../lib/i18n';
  import Account from './settings/Account.svelte';
  import Users from './settings/Users.svelte';
  import General from './settings/General.svelte';
  import Audit from './settings/Audit.svelte';
  import Tags from './settings/Tags.svelte';
  import Probes from './settings/Probes.svelte';
  import Backup from './settings/Backup.svelte';

  let { tab }: { tab: SettingsTab } = $props();

  // need: sekmeyi görebilecek en düşük rol (editör: etiketler; yönetici: diğerleri).
  // Sekme adı: t(`settings.tabs.${key}`).
  const TABS: { key: SettingsTab; href: string; icon: IconName; need: 'viewer' | 'editor' | 'admin' }[] = [
    { key: 'account', href: '#/settings', icon: 'user', need: 'viewer' },
    { key: 'users', href: '#/settings/users', icon: 'users', need: 'admin' },
    { key: 'general', href: '#/settings/general', icon: 'settings', need: 'admin' },
    { key: 'tags', href: '#/settings/tags', icon: 'tag', need: 'editor' },
    { key: 'probes', href: '#/settings/probes', icon: 'map-pin', need: 'admin' },
    { key: 'backup', href: '#/settings/backup', icon: 'archive', need: 'admin' },
    { key: 'audit', href: '#/settings/audit', icon: 'list', need: 'admin' },
  ];

  const can = (need: 'viewer' | 'editor' | 'admin') => need === 'viewer' || (need === 'editor' ? session.canEdit : session.isAdmin);
  const visible = $derived(TABS.filter((x) => can(x.need)));
  const allowed = $derived(visible.some((x) => x.key === tab));

  // Dar ekranda sekme şeridi yatay kayar: seçili sekme görünür alanın ortasına gelsin.
  let navEl: HTMLElement | undefined = $state();
  let firstScroll = true;
  $effect(() => {
    void tab;
    const a = navEl?.querySelector<HTMLElement>('a.active');
    if (!a || !navEl || navEl.scrollWidth <= navEl.clientWidth) return;
    a.scrollIntoView({ inline: 'center', block: 'nearest', behavior: firstScroll ? 'instant' : 'smooth' });
    firstScroll = false;
  });
</script>

<div class="page-head">
  <h1>{t('settings.title')}<span class="dot">.</span></h1>
</div>

{#if visible.length > 1}
  <nav class="stabs" aria-label={t('settings.sectionsLabel')} bind:this={navEl}>
    {#each visible as tb (tb.key)}
      <a href={tb.href} class:active={tb.key === tab} aria-current={tb.key === tab ? 'page' : undefined}>
        <Icon name={tb.icon} size={16} />
        {t(`settings.tabs.${tb.key}`)}
      </a>
    {/each}
  </nav>
{/if}

{#if !allowed}
  <div class="card empty">
    <h3>{t('settings.forbiddenTitle')}</h3>
    <p>{t('settings.forbiddenText')}</p>
    <a class="btn primary" href="#/settings">{t('settings.backToAccount')}</a>
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
      min-height: 44px;
      padding: 10px 10px;
      font-size: 0.88rem;
    }
  }
</style>
