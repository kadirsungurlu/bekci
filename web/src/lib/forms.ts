// Formlar için ortak yardımcılar: kaydedilmemiş değişiklik koruması, hatalı alanın
// erişilebilir işaretlenmesi ve gizli alanların hedef değişince yeniden istenmesi.

import { setLeaveGuard } from './router.svelte';
import { confirmDialog } from './ui.svelte';

/**
 * Kaydedilmemiş değişiklik koruması: sekme kapatılırken/yenilenirken tarayıcının
 * kendi uyarısı, uygulama içi geçişte onay penceresi. Bileşenin onMount'unda
 * çağrılır; dönen fonksiyon korumayı kaldırır.
 */
export function guardUnsaved(dirty: () => boolean): () => void {
  const onBeforeUnload = (e: BeforeUnloadEvent) => {
    if (!dirty()) return;
    e.preventDefault();
    e.returnValue = '';
  };
  window.addEventListener('beforeunload', onBeforeUnload);
  const off = setLeaveGuard({
    dirty,
    confirm: () =>
      confirmDialog({
        title: 'Kaydedilmemiş değişiklikler',
        message: 'Yaptığınız değişiklikler kaydedilmedi. Sayfadan ayrılırsanız kaybolacak.',
        confirmText: 'Kaydetmeden ayrıl',
        cancelText: 'Sayfada kal',
        danger: true,
      }),
  });
  return () => {
    window.removeEventListener('beforeunload', onBeforeUnload);
    off();
  };
}

/** Karşılaştırma için kararlı JSON (anahtar sırası önemsiz). */
export function snapshot(v: unknown): string {
  return JSON.stringify(v, (_k, x) =>
    x && typeof x === 'object' && !Array.isArray(x)
      ? Object.fromEntries(Object.entries(x as Record<string, unknown>).sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0)))
      : x,
  );
}

let invalidEl: HTMLElement | null = null;

/**
 * Doğrulama hatasını ilgili alana bağlar: aria-invalid ve hata kutusuna
 * aria-describedby. `fieldId` boşsa önceki işaret kaldırılır.
 */
export function markInvalid(fieldId: string | null | undefined, errorId: string) {
  if (invalidEl) {
    invalidEl.removeAttribute('aria-invalid');
    const d = (invalidEl.getAttribute('aria-describedby') ?? '')
      .split(' ')
      .filter((x) => x && x !== errorId)
      .join(' ');
    if (d) invalidEl.setAttribute('aria-describedby', d);
    else invalidEl.removeAttribute('aria-describedby');
    invalidEl = null;
  }
  if (!fieldId) return;
  const el = document.getElementById(fieldId);
  if (!el || !(el instanceof HTMLInputElement || el instanceof HTMLSelectElement || el instanceof HTMLTextAreaElement)) return;
  el.setAttribute('aria-invalid', 'true');
  const d = (el.getAttribute('aria-describedby') ?? '').split(' ').filter(Boolean);
  if (!d.includes(errorId)) el.setAttribute('aria-describedby', [...d, errorId].join(' '));
  invalidEl = el;
}

// Gizli alanlar ve hedef adres ------------------------------------------------------------

/**
 * Gizli bilginin gönderildiği yeri belirleyen ayar anahtarları (sunucudaki
 * notify.destinationKeys ile aynı). Bunlardan biri değişirse kayıtlı gizli değer
 * maskeli hâliyle yeni hedefe taşınamaz; sunucu yeniden girilmesini ister.
 */
export const DESTINATION_KEYS = [
  'url',
  'host',
  'port',
  'server',
  'endpoint',
  'webhook_url',
  'homeserver_url',
  'broker_url',
  'target',
  'proxy_url',
  'oauth_token_url',
  'token_url',
];

const DEST_LABELS: Record<string, string> = {
  url: 'adres',
  host: 'sunucu',
  port: 'port',
  server: 'sunucu',
  endpoint: 'uç nokta',
  webhook_url: 'webhook adresi',
  homeserver_url: 'sunucu adresi',
  broker_url: 'broker adresi',
  target: 'hedef',
  proxy_url: 'proxy adresi',
  oauth_token_url: 'token adresi',
  token_url: 'token adresi',
};

const norm = (v: unknown) => (v === undefined || v === null ? '' : String(v).trim().toLowerCase());

/** Değişen hedef alanları (sunucunun karşılaştırmasıyla aynı kural). */
export function changedDestinations(next: Record<string, unknown>, prev: Record<string, unknown>): string[] {
  return DESTINATION_KEYS.filter((k) => norm(next[k]) !== norm(prev[k]));
}

/** "Adres ve port değiştiği için" gibi kısa açıklama. */
export function destinationPhrase(keys: string[]): string {
  const l = [...new Set(keys.map((k) => DEST_LABELS[k] ?? k))];
  const s = l.length > 1 ? `${l.slice(0, -1).join(', ')} ve ${l[l.length - 1]}` : (l[0] ?? 'hedef');
  return s.charAt(0).toLocaleUpperCase('tr') + s.slice(1);
}
