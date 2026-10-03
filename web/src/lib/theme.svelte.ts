// Panel teması: sistem (varsayılan) | açık | koyu. Tercih bu cihazda
// (localStorage) ve hesapta (users.theme) saklanır; girişte hesaptaki tercih
// uygulanır. Herkese açık durum sayfası kendi (sistem) temasını kullanır:
// o sayfadayken kök öğeye data-theme yazılmaz (bkz. suspend).
//
// Renk belişkenleri app.css'tedir: koyu :root'ta, açık :root[data-theme="light"]
// altında (herkese açık sayfanın .pub-light paletiyle ortak).

export type ThemePref = '' | 'light' | 'dark';
export type ThemeName = 'light' | 'dark';

const STORAGE_KEY = 'uptime.theme';
const DARK_BG = '#0c1017';
const LIGHT_BG = '#f4f6fa';

export function isThemePref(v: unknown): v is ThemePref {
  return v === '' || v === 'light' || v === 'dark';
}

function stored(): ThemePref {
  try {
    const v = localStorage.getItem(STORAGE_KEY);
    return isThemePref(v) ? v : '';
  } catch {
    return '';
  }
}

class Theme {
  /** Tercih: '' = sistem. */
  pref = $state<ThemePref>(stored());
  private systemLight = $state(false);
  private suspended = $state(false);
  /** Uygulanan tema. */
  current = $derived<ThemeName>(this.pref ? this.pref : this.systemLight ? 'light' : 'dark');

  constructor() {
    if (typeof window === 'undefined') return;
    const mq = matchMedia('(prefers-color-scheme: light)');
    this.systemLight = mq.matches;
    mq.addEventListener?.('change', (e) => {
      this.systemLight = e.matches;
      this.apply();
    });
    this.apply();
  }

  /** Kök öğeye data-theme ve tarayıcı çubuğu rengini yazar. */
  apply() {
    if (typeof document === 'undefined') return;
    const root = document.documentElement;
    if (this.suspended) {
      delete root.dataset.theme;
      return;
    }
    const name = this.current;
    if (name === 'light') root.dataset.theme = 'light';
    else delete root.dataset.theme;
    const scheme = document.querySelector<HTMLMetaElement>('meta[name="color-scheme"]');
    if (scheme) scheme.content = name;
    const color = document.querySelector<HTMLMetaElement>('meta[name="theme-color"]');
    if (color) color.content = name === 'light' ? LIGHT_BG : DARK_BG;
  }

  /**
   * Tercihi değiştirir. persist: bu cihazda hatırlanır (kullanıcı seçti);
   * hesaptaki tercihin girişte uygulanması persist=false ile yapılır.
   */
  set(p: ThemePref, persist = true) {
    this.pref = p;
    if (persist) {
      try {
        if (p) localStorage.setItem(STORAGE_KEY, p);
        else localStorage.removeItem(STORAGE_KEY);
      } catch {
        /* gizli pencere vb. */
      }
    }
    this.apply();
  }

  /** Hızlı geçiş: açık ↔ koyu (sistemdeyken o anki görünümün tersi). */
  toggle(): ThemeName {
    const next: ThemeName = this.current === 'light' ? 'dark' : 'light';
    this.set(next);
    return next;
  }

  /** Herkese açık sayfa: panel teması askıya alınır / geri gelir. */
  suspend(on: boolean) {
    this.suspended = on;
    this.apply();
  }
}

export const theme = new Theme();
