// İşlem kaydı eylem kodlarının karşılıkları (lib/i18n/<dil>/audit.ts: "alan.eylem" →
// actions.alan.eylem). Bilinmeyen kod olduğu gibi gösterilir.

import { tOr } from './i18n';

export type AuditTone = 'bad' | 'warn' | 'good' | '';

export function auditTone(action: string): AuditTone {
  if (action === 'login.fail' || action === 'login.2fa_fail') return 'bad';
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
    case 'probe':
      return '#/settings/probes';
    case 'tag':
      return '#/settings/tags';
    default:
      return '';
  }
}
