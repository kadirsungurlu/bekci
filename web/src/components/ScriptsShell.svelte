<script lang="ts">
  // Betikler bölümünün ortak çerçevesi: başlık, sekmeler, 2FA ve Postgres uyarısı.
  import type { Snippet } from 'svelte';
  import Icon, { type IconName } from './Icon.svelte';
  import { session } from '../lib/session.svelte';
  import { t } from '../lib/i18n';

  let {
    active,
    pg = false,
    actions,
    children,
  }: { active: 'library' | 'runs' | 'settings'; pg?: boolean; actions?: Snippet; children: Snippet } = $props();

  const TABS: { key: 'library' | 'runs' | 'settings'; href: string; icon: IconName }[] = [
    { key: 'library', href: '#/scripts', icon: 'file' },
    { key: 'runs', href: '#/scripts/runs', icon: 'list' },
    { key: 'settings', href: '#/scripts/settings', icon: 'shield' },
  ];
</script>

<div class="page-head shead">
  <h1>{t('scripts.title')}<span class="dot">.</span></h1>
  {#if actions && !pg}<div class="actions">{@render actions()}</div>{/if}
</div>

<nav class="stabs" aria-label={t('scripts.tabsLabel')}>
  {#each TABS as tb (tb.key)}
    <a href={tb.href} class:active={tb.key === active} aria-current={tb.key === active ? 'page' : undefined}>
      <Icon name={tb.icon} size={16} />
      {t(`scripts.tabs.${tb.key}`)}
    </a>
  {/each}
</nav>

{#if pg}
  <div class="empty">
    <Icon name="database" size={28} />
    <h3>{t('scripts.pg.title')}</h3>
    <p>{t('scripts.pg.text')}</p>
  </div>
{:else}
  {#if !session.user?.two_factor_enabled}
    <div class="alert warning tfa" data-testid="scripts-2fa">
      <Icon name="shield" size={18} />
      <span>{t('scripts.tfa.text')}</span>
      <a class="btn sm" href="#/settings">{t('scripts.tfa.link')}</a>
    </div>
  {/if}
  <div class="sbody">{@render children()}</div>
{/if}

<style>
  /* Düğmesi olan ve olmayan sekmelerde başlık aynı yerde kalsın. */
  .shead {
    min-height: 38px;
  }
  .actions {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
  }
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
    min-height: 44px;
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
  .tfa {
    display: flex;
    align-items: center;
    gap: 10px;
    margin-bottom: 18px;
  }
  .tfa span {
    flex: 1;
  }
  @media (max-width: 640px) {
    .tfa {
      flex-wrap: wrap;
    }
    .tfa span {
      flex-basis: calc(100% - 30px);
    }
  }
</style>
