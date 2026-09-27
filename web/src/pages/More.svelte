<script lang="ts">
  // Mobil "Daha fazla" menüsü: sekme çubuğuna sığmayan bölümler.
  import { ROLE_LABELS, session } from '../lib/session.svelte';
  import Icon, { type IconName } from '../components/Icon.svelte';

  let { onLogout }: { onLogout: () => void } = $props();

  const items = $derived.by(() => {
    const out: { href: string; label: string; desc: string; icon: IconName }[] = [
      { href: '#/maintenance', label: 'Bakım pencereleri', desc: 'Planlı bakımlarda bildirimleri sustur', icon: 'wrench' },
    ];
    if (session.canEdit)
      out.push({ href: '#/notifications', label: 'Bildirimler', desc: 'WhatsApp, Telegram, e-posta ve diğer kanallar', icon: 'bell' });
    out.push({ href: '#/settings', label: 'Hesabım', desc: 'Şifre, iki adımlı doğrulama, API anahtarları', icon: 'user' });
    if (session.canEdit) out.push({ href: '#/settings/tags', label: 'Etiketler', desc: 'Monitörleri ortam, müşteri veya ekibe göre grupla', icon: 'tag' });
    if (session.isAdmin) {
      out.push({ href: '#/settings/users', label: 'Kullanıcılar', desc: 'Hesaplar, roller ve müşteri erişimi', icon: 'users' });
      out.push({ href: '#/settings/general', label: 'Genel ayarlar', desc: 'Veri saklama, SSL uyarıları, yedekler', icon: 'settings' });
      out.push({ href: '#/settings/probes', label: 'Kontrol noktaları', desc: 'Farklı konumlardan kontrol', icon: 'map-pin' });
      out.push({
        href: '#/settings/backup',
        label: 'Yedekle / Geri yükle',
        desc: 'Yedek al, geri yükle; UptimeRobot veya Uptime Kuma’dan taşı',
        icon: 'archive',
      });
      out.push({ href: '#/settings/audit', label: 'İşlem kaydı', desc: 'Kim ne zaman ne yaptı', icon: 'list' });
    }
    return out;
  });
</script>

<div class="page-head">
  <h1>Daha fazla<span class="dot">.</span></h1>
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
  <button class="btn sm" onclick={onLogout}><Icon name="logout" size={15} /> Çıkış</button>
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
