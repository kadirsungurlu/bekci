// Betikler bölümünün ortak yardımcıları: hata kodu, durum ve olay metinleri.
// Metin kaynağı i18n `scripts.*`; bilinmeyen kod ham gösterilir.

import { ApiError, errorMessage, type ScriptParam, type ScriptVersion } from './api';
import { t, tOr } from './i18n';

export const codeText = (code: string) => tOr(`scripts.codes.${code}`, code);
export const stateText = (state: string) => tOr(`scripts.state.${state}`, state);
export const eventText = (kind: string) => tOr(`scripts.event.${kind}`, kind);
export const reasonText = (reason: string) => tOr(`scripts.reason.${reason}`, reason);
export const approvalText = (a: string) => tOr(`scripts.approval.${a}`, a);

/** `code` varsa arayüz metni, yoksa sunucunun iletisi. */
export function scriptError(e: unknown): string {
  if (e instanceof ApiError && e.code) return codeText(e.code);
  return errorMessage(e);
}

export const isPostgresRequired = (e: unknown) => e instanceof ApiError && e.code === 'postgres_required';

const LIVE = new Set(['awaiting_approval', 'awaiting_ticket', 'queued', 'accepted', 'cancelling']);
export const isLive = (state: string) => LIVE.has(state);

export function stateClass(state: string): string {
  if (state === 'ok') return 'up';
  if (state === 'failed' || state === 'timeout' || state === 'agent_rejected' || state === 'lost') return 'down';
  if (LIVE.has(state)) return 'pending';
  return 'paused';
}

export const shortId = (id: string) => (id ? id.slice(0, 12) : '');

/** Bu sürümle başlatma panelde şimdi reddedilir mi? (Son karar yine ajanındır.) */
export function versionBlocked(v: ScriptVersion, now: number): 'revoked' | 'expired' | 'not_yet' | '' {
  if (v.revoked) return 'revoked';
  if (v.expired || now >= v.not_after) return 'expired';
  if (now < v.not_before) return 'not_yet';
  return '';
}

export function paramInitial(p: ScriptParam): string | boolean {
  if (p.type === 'bool') return false;
  if (p.type === 'enum') return p.values?.[0] ?? '';
  return '';
}

/** Alan değerini bildirimdeki türe çevirir; geçersizse undefined. */
export function paramValue(p: ScriptParam, v: string | boolean): string | number | boolean | undefined {
  if (p.type === 'bool') return v === true;
  const s = String(v).trim();
  if (!s) return undefined;
  if (p.type !== 'int') return s;
  const n = Number(s);
  if (!Number.isSafeInteger(n)) return undefined;
  if ((p.min !== undefined && n < p.min) || (p.max !== undefined && n > p.max)) return undefined;
  return n;
}

/** Dosya metnini JSON olarak okur; hata iletisi çevrilmiş olarak fırlatılır. */
export function parseJson(text: string): unknown {
  try {
    return JSON.parse(text);
  } catch {
    throw new Error(t('scripts.upload.errJson'));
  }
}

/** Yükleme dosyası: `{ "manifest": <zarf>, "body": "<base64>" }`. */
export function parseBundle(text: string): { manifest: unknown; body: string } {
  const o = parseJson(text) as { manifest?: unknown; body?: unknown } | null;
  if (!o || typeof o !== 'object' || !o.manifest || typeof o.body !== 'string' || !o.body) throw new Error(t('scripts.upload.errBundle'));
  return { manifest: o.manifest, body: o.body };
}

/** İptal dosyası: zarfın kendisi ya da `{ "revoke": <zarf> }`. */
export function parseRevoke(text: string): unknown {
  const o = parseJson(text) as { revoke?: unknown; payload?: unknown } | null;
  const env = o && typeof o === 'object' ? (o.revoke ?? o) : null;
  if (!env || typeof env !== 'object') throw new Error(t('scripts.upload.errBundle'));
  return env;
}

/** Olay verisinden zaman çizelgesi satırı için kısa ayrıntı. */
export function eventDetail(data: string): string {
  let d: Record<string, unknown>;
  try {
    d = JSON.parse(data);
  } catch {
    return '';
  }
  if (!d || typeof d !== 'object') return '';
  const out: string[] = [];
  if (typeof d.by === 'string' && d.by) out.push(d.by);
  if (typeof d.status === 'string' && d.status) out.push(stateText(d.status));
  if (typeof d.exit_code === 'number') out.push(t('scripts.detail.exitShort', { code: d.exit_code }));
  if (typeof d.code === 'string' && d.code) out.push(codeText(d.code));
  if (d.reason === 'emergency_stop') out.push(t('scripts.detail.emergency'));
  return out.join(' · ');
}
