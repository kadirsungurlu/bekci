// İşlem kaydı eylem kodlarının Türkçe karşılıkları. Bilinmeyen kod olduğu gibi gösterilir.

export const AUDIT_LABELS: Record<string, string> = {
  'login.success': 'Giriş yaptı',
  'login.fail': 'Hatalı giriş denemesi',
  'login.2fa_fail': 'Hatalı 2FA kodu',
  'user.setup': 'İlk kurulum',
  'user.create': 'Kullanıcı eklendi',
  'user.update': 'Kullanıcı güncellendi',
  'user.delete': 'Kullanıcı silindi',
  'user.password_change': 'Şifresini değiştirdi',
  'user.password_reset': 'Şifre sıfırlandı',
  'user.2fa_enable': '2FA açıldı',
  'user.2fa_disable': '2FA kapatıldı',
  'user.2fa_recovery_codes': 'Kurtarma kodları yenilendi',
  'user.2fa_reset': '2FA sıfırlandı',
  'monitor.create': 'Monitör eklendi',
  'monitor.update': 'Monitör güncellendi',
  'monitor.delete': 'Monitör silindi',
  'monitor.pause': 'Monitör durduruldu',
  'monitor.resume': 'Monitör başlatıldı',
  'notification.create': 'Bildirim kanalı eklendi',
  'notification.update': 'Bildirim kanalı güncellendi',
  'notification.delete': 'Bildirim kanalı silindi',
  'settings.update': 'Ayarlar güncellendi',
  'apikey.create': 'API anahtarı oluşturuldu',
  'apikey.revoke': 'API anahtarı iptal edildi',
  'status_page.create': 'Durum sayfası oluşturuldu',
  'status_page.update': 'Durum sayfası güncellendi',
  'status_page.delete': 'Durum sayfası silindi',
  'status_page.logo': 'Logo yüklendi',
  'status_page.logo_delete': 'Logo kaldırıldı',
  'announcement.create': 'Duyuru eklendi',
  'announcement.update': 'Duyuru güncellendi',
  'announcement.delete': 'Duyuru silindi',
  'maintenance.create': 'Bakım eklendi',
  'maintenance.update': 'Bakım güncellendi',
  'maintenance.delete': 'Bakım silindi',
  'maintenance.pause': 'Bakım durduruldu',
  'maintenance.resume': 'Bakım başlatıldı',
  'monitor.tags': 'Monitör etiketleri değiştirildi',
  'monitor.locations': 'Monitör konumları değiştirildi',
  'tag.create': 'Etiket eklendi',
  'tag.update': 'Etiket güncellendi',
  'tag.delete': 'Etiket silindi',
  'probe.create': 'Kontrol noktası eklendi',
  'probe.update': 'Kontrol noktası güncellendi',
  'probe.delete': 'Kontrol noktası silindi',
  'probe.token': 'Kontrol noktası token’ı yenilendi',
  'backup.export': 'Yedek indirildi',
  'backup.import': 'İçe aktarma / geri yükleme yapıldı',
};

export const TARGET_LABELS: Record<string, string> = {
  user: 'Kullanıcı',
  monitor: 'Monitör',
  notification: 'Bildirim',
  settings: 'Ayarlar',
  apikey: 'API anahtarı',
  status_page: 'Durum sayfası',
  announcement: 'Duyuru',
  maintenance: 'Bakım',
  tag: 'Etiket',
  probe: 'Kontrol noktası',
  backup: 'Yedek',
};

export type AuditTone = 'bad' | 'warn' | 'good' | '';

export function auditTone(action: string): AuditTone {
  if (action === 'login.fail' || action === 'login.2fa_fail') return 'bad';
  if (
    action.endsWith('.delete') ||
    action.endsWith('.revoke') ||
    action.endsWith('_reset') ||
    action === 'user.password_reset' ||
    action === 'probe.token'
  )
    return 'warn';
  if (action === 'login.success') return 'good';
  return '';
}

export const auditLabel = (action: string) => AUDIT_LABELS[action] ?? action;

/** Hedefin bağlantısı (varsa). */
export function auditHref(type: string, id: number): string {
  if (!id) return '';
  switch (type) {
    case 'monitor':
      return `#/monitors/${id}`;
    case 'status_page':
      return `#/status-pages/${id}`;
    case 'maintenance':
      return `#/maintenance/${id}`;
    case 'probe':
      return '#/settings/probes';
    case 'tag':
      return '#/settings/tags';
    default:
      return '';
  }
}
