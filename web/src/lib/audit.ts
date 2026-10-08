// İşlem kaydı eylem kodlarının karşılıkları (lib/i18n/<dil>/audit.ts: "alan.eylem" →
// actions.alan.eylem). Bilinmeyen kod olduğu gibi gösterilir.

import { i18n, tOr } from './i18n';

export type AuditTone = 'bad' | 'warn' | 'good' | '';

export function auditTone(action: string): AuditTone {
  if (action === 'login.fail' || action === 'login.2fa_fail' || action === 'agent.update_failed') return 'bad';
  if (
    action.endsWith('.delete') ||
    action.endsWith('.revoke') ||
    action.endsWith('_reset') ||
    action === 'user.password_reset' ||
    action === 'monitor.reset_stats' ||
    action === 'probe.token'
  )
    return 'warn';
  if (action === 'login.success') return 'good';
  return '';
}

/** Eylem kodunun geçerli dildeki adı ("monitor.create" → "Monitör eklendi"). */
export const auditLabel = (action: string) =>
  /^[a-z_]+\.[a-z0-9_]+$/.test(action) ? tOr(`audit.actions.${action}`, action) : action;

/** Hedef türünün geçerli dildeki adı ("status_page" → "Durum sayfası"). */
export const auditTargetLabel = (type: string) => (/^[a-z_]+$/.test(type) ? tOr(`audit.targets.${type}`, type) : type);

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
    case 'incident':
      return `#/incidents/${id}`;
    case 'probe':
      return '#/settings/probes';
    case 'server':
      return `#/servers/${id}`;
    case 'tag':
      return '#/settings/tags';
    default:
      return '';
  }
}

// İşlem kaydı ayrıntıları sunucuda Türkçe saklanır. İngilizce arayüzde bilinen
// kalıplar çevrilir; tanınmayan metin olduğu gibi kalır (adlar, sayılar, kodlar).
// Kalıcı çözüm: ayrıntıyı kod+parametre olarak saklamak (ayrı iş).
const DETAIL_EN: [RegExp, string][] = [
  [/IP kilidi açıldı/g, 'IP lock enabled'],
  [/IP kilidi kapatıldı/g, 'IP lock disabled'],
  [/IP kilidi sıfırlandı/g, 'IP lock reset'],
  [/çevrimdışı bildirimi açıldı/g, 'offline notification enabled'],
  [/çevrimdışı bildirimi kapatıldı/g, 'offline notification disabled'],
  [/metrik toplama açıldı/g, 'metrics collection enabled'],
  [/metrik toplama kapatıldı/g, 'metrics collection disabled'],
  [/bildirim kanalları değişti/g, 'notification channels changed'],
  [/otomatik güncelleme: genel ayar/g, 'auto-update: global setting'],
  [/otomatik güncelleme açıldı/g, 'auto-update enabled'],
  [/otomatik güncelleme kapatıldı/g, 'auto-update disabled'],
  [/\(otomatik güncelleme\)/g, '(auto-update)'],
  [/(^|, )ad: /g, '$1name: '],
  [/(^|, )etkinleştirildi/g, '$1enabled'],
  [/(^|, )devre dışı bırakıldı/g, '$1disabled'],
  [/değişiklik yok/g, 'no changes'],
  [/değişen: /g, 'changed: '],
  [/ham kayıt saklama/g, 'raw data retention'],
  [/saatlik özet saklama/g, 'hourly summary retention'],
  [/SSL eşikleri/g, 'SSL thresholds'],
  [/yedek sayısı/g, 'backup count'],
  [/bildirim dili/g, 'notification language'],
  [/olay saklama/g, 'incident retention'],
  [/istek\/yanıt saklama/g, 'request/response retention'],
  [/işlem kaydı saklama/g, 'audit log retention'],
  [/sistem e-postası/g, 'system email'],
  [/ajan otomatik güncelleme/g, 'agent auto-update'],
  [/^kural yok$/, 'no rules'],
  [/^kanal yok$/, 'no channels'],
  [/^kanallar: /, 'channels: '],
  [/\(devre dışı\)/g, '(disabled)'],
  [/(\d+) dk\b/g, '$1 min'],
  [/^(\d+) monitörden çıkarıldı/, 'removed from $1 monitor(s)'],
  [/^(\d+) ajan → /, '$1 agent(s) → '],
  [/^(\d+) kanal$/, '$1 channel(s)'],
  [/^(\d+) oturum$/, '$1 session(s)'],
  [/^(\d+) etiket$/, '$1 tag(s)'],
  [/^eşleşen hesap yok$/, 'no matching account'],
];

/** İşlem kaydı ayrıntısı geçerli dilde (İngilizcede bilinen kalıplar çevrilir). */
export function auditDetail(detail: string): string {
  if (i18n.locale !== 'en' || !detail) return detail;
  return DETAIL_EN.reduce((s, [re, to]) => s.replace(re, to), detail);
}
