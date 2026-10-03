// Küçük hash yönlendirici: #/, #/monitors/new, #/monitors/:id, #/monitors/:id/edit,
// #/incidents[/:id], #/notifications, #/status-pages[/new|/:id|/:id/preview],
// #/maintenance[/new|/:id], #/servers[/:id], #/settings[/:tab], #/more
//
// Herkese açık durum sayfası hash değil gerçek yol kullanır (/durum/<kısa-ad>);
// bkz. publicSlugFromPath.

export type SettingsTab = 'account' | 'users' | 'general' | 'login' | 'tags' | 'probes' | 'backup' | 'audit';

export type Route =
  | { name: 'list' }
  | { name: 'new' }
  | { name: 'detail'; id: number }
  | { name: 'edit'; id: number }
  | { name: 'incidents' }
  | { name: 'incident'; id: number }
  | { name: 'notifications' }
  | { name: 'pages' }
  | { name: 'page-new' }
  | { name: 'page-edit'; id: number }
  | { name: 'page-preview'; id: number }
  | { name: 'maintenance' }
  | { name: 'maint-new' }
  | { name: 'maint-edit'; id: number }
  | { name: 'settings'; tab: SettingsTab }
  | { name: 'servers' }
  | { name: 'server'; id: number }
  | { name: 'more' }
  | { name: 'notfound' };

const SETTINGS_TABS: Record<string, SettingsTab> = {
  '': 'account',
  account: 'account',
  users: 'users',
  general: 'general',
  tags: 'tags',
  probes: 'probes',
  backup: 'backup',
  audit: 'audit',
  login: 'login',
};

function currentPath(): string {
  const h = location.hash.replace(/^#/, '');
  return h === '' ? '/' : h;
}

export function parse(path: string): Route {
  const clean = path.split('?')[0].replace(/\/+$/, '') || '/';
  if (clean === '/') return { name: 'list' };
  if (clean === '/monitors/new') return { name: 'new' };
  if (clean === '/incidents') return { name: 'incidents' };
  if (clean === '/notifications') return { name: 'notifications' };
  if (clean === '/status-pages') return { name: 'pages' };
  if (clean === '/status-pages/new') return { name: 'page-new' };
  if (clean === '/maintenance') return { name: 'maintenance' };
  if (clean === '/maintenance/new') return { name: 'maint-new' };
  if (clean === '/more') return { name: 'more' };
  if (clean === '/servers') return { name: 'servers' };
  let m = clean.match(/^\/settings(?:\/([a-z]+))?$/);
  if (m) {
    const tab = SETTINGS_TABS[m[1] ?? ''];
    return tab ? { name: 'settings', tab } : { name: 'notfound' };
  }
  m = clean.match(/^\/monitors\/(\d+)$/);
  if (m) return { name: 'detail', id: Number(m[1]) };
  m = clean.match(/^\/monitors\/(\d+)\/edit$/);
  if (m) return { name: 'edit', id: Number(m[1]) };
  m = clean.match(/^\/status-pages\/(\d+)$/);
  if (m) return { name: 'page-edit', id: Number(m[1]) };
  m = clean.match(/^\/status-pages\/(\d+)\/preview$/);
  if (m) return { name: 'page-preview', id: Number(m[1]) };
  m = clean.match(/^\/incidents\/(\d+)$/);
  if (m) return { name: 'incident', id: Number(m[1]) };
  m = clean.match(/^\/servers\/(\d+)$/);
  if (m) return { name: 'server', id: Number(m[1]) };
  m = clean.match(/^\/maintenance\/(\d+)$/);
  if (m) return { name: 'maint-edit', id: Number(m[1]) };
  return { name: 'notfound' };
}

// Sayfadan ayrılma koruması: kaydedilmemiş değişikliği olan form bir kontrol
// fonksiyonu kaydeder; uygulama içi geçişte onay sorulur (bkz. lib/forms).
type LeaveGuard = { dirty: () => boolean; confirm: () => Promise<boolean> };
// Aynı sayfada birden çok bağımsız form olabilir (ör. sunucu ayrıntısında uyarı
// kuralları ve bildirim kanalları): biri kaydedilmemişse sorulur.
const leaveGuards = new Set<LeaveGuard>();

/** Sayfadan ayrılma korumasını kurar; kaldıran fonksiyon döner. */
export function setLeaveGuard(g: LeaveGuard): () => void {
  leaveGuards.add(g);
  return () => {
    leaveGuards.delete(g);
  };
}

function dirtyGuard(): LeaveGuard | null {
  for (const g of leaveGuards) if (g.dirty()) return g;
  return null;
}

class Router {
  path = $state(currentPath());
  route = $derived(parse(this.path));
  private asking = false;

  constructor() {
    window.addEventListener('hashchange', () => {
      const next = currentPath();
      if (next === this.path) return;
      const g = dirtyGuard();
      if (g) {
        // Adres çubuğunu eski sayfaya geri al (hashchange tetiklemeden) ve sor.
        history.replaceState(null, '', '#' + this.path);
        if (this.asking) return;
        this.asking = true;
        g.confirm().then((ok) => {
          this.asking = false;
          if (!ok) return;
          leaveGuards.clear();
          location.hash = '#' + next;
        });
        return;
      }
      this.path = next;
      window.scrollTo(0, 0);
    });
  }
}

export const router = new Router();

/**
 * Uygulama içinde başka sayfaya geçer. `force`: kaydedilmemiş değişiklik korumasını
 * atla (ör. kaydettikten hemen sonra).
 */
export function navigate(path: string, replace = false, force = false) {
  const hash = '#' + path;
  if (force) leaveGuards.clear();
  if (replace) {
    history.replaceState(null, '', hash);
    router.path = currentPath();
  } else if (location.hash !== hash) {
    location.hash = hash;
  }
}

/** /durum/<kısa-ad> yolundaysak kısa adı döner (geçersizse sunucu 404 verir). */
export function publicSlugFromPath(pathname = location.pathname): string | null {
  const m = pathname.match(/^\/durum\/([^/]*)\/?$/);
  if (!m) return null;
  try {
    return decodeURIComponent(m[1]).toLowerCase();
  } catch {
    return m[1].toLowerCase();
  }
}
