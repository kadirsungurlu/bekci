// Küçük hash yönlendirici: #/, #/monitors/new, #/monitors/:id, #/monitors/:id/edit,
// #/incidents, #/notifications, #/settings

export type Route =
  | { name: 'list' }
  | { name: 'new' }
  | { name: 'detail'; id: number }
  | { name: 'edit'; id: number }
  | { name: 'incidents' }
  | { name: 'notifications' }
  | { name: 'settings' }
  | { name: 'notfound' };

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
  if (clean === '/settings') return { name: 'settings' };
  let m = clean.match(/^\/monitors\/(\d+)$/);
  if (m) return { name: 'detail', id: Number(m[1]) };
  m = clean.match(/^\/monitors\/(\d+)\/edit$/);
  if (m) return { name: 'edit', id: Number(m[1]) };
  return { name: 'notfound' };
}

class Router {
  path = $state(currentPath());
  route = $derived(parse(this.path));

  constructor() {
    window.addEventListener('hashchange', () => {
      this.path = currentPath();
      window.scrollTo(0, 0);
    });
  }
}

export const router = new Router();

export function navigate(path: string, replace = false) {
  const hash = '#' + path;
  if (replace) {
    history.replaceState(null, '', hash);
    router.path = currentPath();
  } else if (location.hash !== hash) {
    location.hash = hash;
  }
}

