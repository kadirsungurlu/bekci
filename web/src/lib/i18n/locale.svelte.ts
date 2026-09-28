// Geçerli arayüz dili (reaktif). t() ve biçimlendirme yardımcıları bu değeri
// okuduğu için dil değişince onları kullanan her yer kendiliğinden yeniden çizilir.

export type Locale = 'tr' | 'en';
export const LOCALES: readonly Locale[] = ['tr', 'en'];
export const DEFAULT_LOCALE: Locale = 'tr';

/** Giriş ekranında seçilen / son kullanılan dil (tarayıcıya özel). */
const STORAGE_KEY = 'uptime.lang';

export function isLocale(v: unknown): v is Locale {
  return v === 'tr' || v === 'en';
}

/** Tarayıcı dili: tr ile başlıyorsa tr, değilse en. */
export function browserLocale(): Locale {
  const langs = typeof navigator === 'undefined' ? [] : navigator.languages?.length ? navigator.languages : [navigator.language];
  const first = (langs[0] ?? '').toLowerCase();
  return first.startsWith('tr') ? 'tr' : 'en';
}

function stored(): Locale | null {
  try {
    const v = localStorage.getItem(STORAGE_KEY);
    return isLocale(v) ? v : null;
  } catch {
    return null;
  }
}

/**
 * Bu cihazın dili: giriş ekranında / hesap ayarında bu tarayıcıda açıkça
 * seçilen dil, yoksa tarayıcı dili. Hesapta dil tercihi yoksa bu kullanılır.
 */
export function deviceLocale(): Locale {
  return stored() ?? browserLocale();
}

class I18nState {
  /** Geçerli dil. Doğrudan değiştirmeyin; setLocale kullanın. */
  locale = $state<Locale>(deviceLocale());
}

export const i18n = new I18nState();

function applyHtmlLang(l: Locale) {
  if (typeof document !== 'undefined') document.documentElement.lang = l;
}
applyHtmlLang(i18n.locale);

/**
 * Dili değiştirir. persist: bu cihazın dili olarak tarayıcıda hatırlanır —
 * yalnızca kullanıcı bu cihazda açıkça seçtiğinde (giriş ekranı, Hesabım).
 * Hesap tercihinin girişte uygulanması ve herkese açık sayfanın kendi dili
 * persist=false ile yapılır.
 */
export function setLocale(l: Locale, persist = true) {
  i18n.locale = l;
  applyHtmlLang(l);
  if (!persist) return;
  try {
    localStorage.setItem(STORAGE_KEY, l);
  } catch {
    /* gizli pencere vb.: yalnızca bu oturumda geçerli */
  }
}

/** Tarayıcıda hatırlanan dili siler (kullanıcı "tarayıcı dili"ni seçti). */
export function forgetLocale() {
  try {
    localStorage.removeItem(STORAGE_KEY);
  } catch {
    /* yok say */
  }
  setLocale(browserLocale(), false);
}
