<script lang="ts">
  // Mobil "Daha fazla" menüsü: sekme çubuğuna sığmayan bölümler.
  import { ROLE_LABELS, session } from '../lib/session.svelte';
  import Icon, { type IconName } from '../components/Icon.svelte';
  import { t } from '../lib/i18n';

  let { onLogout }: { onLogout: () => void } = $props();

  const items = $derived.by(() => {
    const out: { href: string; label: string; desc: string; icon: IconName }[] = [];
    if (session.canEdit)
      out.push({ href: '#/status-pages', label: t('nav.statusPages'), desc: t('nav.moreItems.statusPagesDesc'), icon: 'layout' });
    out.push({ href: '#/maintenance', label: t('nav.titles.maintenanceWindows'), desc: t('nav.moreItems.maintenanceDesc'), icon: 'wrench' });
    if (session.canEdit)
      out.push({ href: '#/notifications', label: t('nav.notifications'), desc: t('nav.moreItems.notificationsDesc'), icon: 'bell' });
    out.push({ href: '#/settings', label: t('nav.myAccount'), desc: t('nav.moreItems.accountDesc'), icon: 'user' });
    if (session.canEdit) out.push({ href: '#/settings/tags', label: t('nav.moreItems.tags'), desc: t('nav.moreItems.tagsDesc'), icon: 'tag' });
    if (session.isAdmin) {
      out.push({ href: '#/settings/users', label: t('nav.moreItems.users'), desc: t('nav.moreItems.usersDesc'), icon: 'users' });
      out.push({ href: '#/settings/general', label: t('nav.moreItems.general'), desc: t('nav.moreItems.generalDesc'), icon: 'settings' });
      out.push({ href: '#/settings/probes', label: t('nav.moreItems.probes'), desc: t('nav.moreItems.probesDesc'), icon: 'map-pin' });
      out.push({ href: '#/settings/backup', label: t('nav.moreItems.backup'), desc: t('nav.moreItems.backupDesc'), icon: 'archive' });
      out.push({ href: '#/settings/audit', label: t('nav.moreItems.audit'), desc: t('nav.moreItems.auditDesc'), icon: 'list' });
    }
    return out;
  });
</script>

<div class="page-head">
  <h1>{t('nav.more')}<span class="dot">.</span></h1>
</div>

<div class="card list">
  {#each items as it (it.href)}
    <a class="item" href={it.href}>
      <span class="ic"><Icon name={it.icon} size={18} /></span>
      <span class="t">
        <span class="l">{it.label}</span>
        <span class="d">{it.desc}</span>
      </span>
      <Icon name="chevron-right" size={16} />
    </a>
  {/each}
</div>

<div class="card me">
  <span class="ic"><Icon name="user" size={18} /></span>
  <span class="t">
    <span class="l">{session.displayName}</span>
    <span class="d">{session.user?.username} · {ROLE_LABELS[session.role]}</span>
  </span>
  <button class="btn sm" onclick={onLogout}><Icon name="logout" size={15} /> {t('common.logout')}</button>
</div>

<style>
  .list {
    padding: 0;
  }
  .item {
    display: flex;
    align-items: center;
    gap: 14px;
    padding: 14px 16px;
    border-bottom: 1px solid var(--border);
    color: var(--text);
    text-decoration: none;
  }
  .item:last-child {
    border-bottom: none;
  }
  .item:focus-visible {
    outline-offset: -2px;
    border-radius: var(--radius);
  }
  .item > :global(svg) {
    color: var(--muted);
    flex-shrink: 0;
  }
  .ic {
    width: 36px;
    height: 36px;
    border-radius: 10px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    background: var(--accent-soft);
    color: var(--accent-text);
    flex-shrink: 0;
  }
  .t {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
  }
  .l {
    font-weight: 700;
  }
  .d {
    font-size: 0.82rem;
    color: var(--muted);
  }
  .me {
    display: flex;
    align-items: center;
    gap: 14px;
    margin-top: 16px;
  }
  .me .ic {
    background: var(--card-2);
    color: var(--text-2);
  }
</style>
