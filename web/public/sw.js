// Uptime servis çalışanı (yalnızca yönetim paneli; bkz. src/lib/pwa.svelte.ts).
//
// - index.html (sayfa gezintisi): önce ağ, ağ yoksa önbellekteki kopya.
// - /assets/* (dosya adında içerik özeti var): önce önbellek.
// - Simgeler, manifest, favicon: önce ağ, yoksa önbellek.
// - /api/* (canlı olay akışı /api/events dahil) ve herkese açık durum sayfaları
//   (/durum/*) HİÇ yakalanmaz; tarayıcı doğrudan sunucuya gider.
//
// BUILD ve PRECACHE derlemede (vite.config.ts → pwaBuild) doldurulur; her yeni
// sürümde bu dosya değiştiği için tarayıcı yeni çalışanı kurar ve uygulama
// "Yeni sürüm hazır" der. Kullanıcı onaylayınca SKIP_WAITING ile devreye girer.

const BUILD = '__BUILD__';
const PRECACHE = /* __PRECACHE__ */ [];
const CACHE = `uptime-${BUILD}`;
const INDEX = '/';

self.addEventListener('install', (event) => {
  event.waitUntil(
    (async () => {
      const cache = await caches.open(CACHE);
      // Kabuk: index ve giriş dosyaları. Biri alınamazsa kurulum yine de sürsün.
      await Promise.all(
        [INDEX, ...PRECACHE].map((u) =>
          fetch(u, { cache: 'no-cache' })
            .then((r) => (r.ok ? cache.put(u, r) : undefined))
            .catch(() => undefined),
        ),
      );
    })(),
  );
});

self.addEventListener('activate', (event) => {
  event.waitUntil(
    (async () => {
      const keys = await caches.keys();
      await Promise.all(keys.filter((k) => k.startsWith('uptime-') && k !== CACHE).map((k) => caches.delete(k)));
      // İlk kurulumda açık sayfa da hemen bu çalışanla çalışsın (çevrimdışı açılış).
      // Sayfa bu ilk sahiplenmede YENİLENMEZ (bkz. pwa.svelte.ts controllerchange).
      await self.clients.claim();
    })(),
  );
});

self.addEventListener('message', (event) => {
  if (event.data && event.data.type === 'SKIP_WAITING') self.skipWaiting();
});

// Web Push: sunucu şifreli JSON gönderir ({title, body, url, tag, kind});
// bildirim gösterilir, tıklanınca açık sekme odaklanıp adrese gider, yoksa yeni açılır.
self.addEventListener('push', (event) => {
  let data = {};
  try {
    data = event.data ? event.data.json() : {};
  } catch {
    data = { title: event.data ? event.data.text() : '' };
  }
  const title = data.title || 'Bekci';
  const opts = {
    body: data.body || '',
    icon: '/icons/icon-192.png',
    badge: '/icons/icon-192.png',
    tag: data.tag || undefined,
    renotify: !!data.tag,
    data: { url: data.url || '/#/' },
  };
  event.waitUntil(self.registration.showNotification(title, opts));
});

self.addEventListener('notificationclick', (event) => {
  event.notification.close();
  const url = new URL((event.notification.data && event.notification.data.url) || '/#/', self.location.origin).href;
  event.waitUntil(
    (async () => {
      const all = await self.clients.matchAll({ type: 'window', includeUncontrolled: true });
      for (const c of all) {
        if (new URL(c.url).origin === self.location.origin && 'focus' in c) {
          await c.focus();
          if ('navigate' in c) {
            try {
              await c.navigate(url);
            } catch {
              /* bazı tarayıcılar navigate'i desteklemez */
            }
          }
          return;
        }
      }
      await self.clients.openWindow(url);
    })(),
  );
});

// Push servisi aboneliği yenilediğinde yeni abonelik sunucuya yazılır.
self.addEventListener('pushsubscriptionchange', (event) => {
  event.waitUntil(
    (async () => {
      const key = event.oldSubscription && event.oldSubscription.options ? event.oldSubscription.options.applicationServerKey : null;
      const sub = await self.registration.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: key });
      await fetch('/api/webpush/subscriptions', {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json', 'X-Uptime': '1' },
        body: JSON.stringify(sub.toJSON()),
      });
    })(),
  );
});

/** Bu istek hiç yakalanmamalı mı? */
function bypass(url, req) {
  if (req.method !== 'GET') return true;
  if (url.origin !== self.location.origin) return true;
  const p = url.pathname;
  return p === '/api' || p.startsWith('/api/') || p === '/durum' || p.startsWith('/durum/') || p === '/sw.js';
}

async function networkFirst(req, key, html = false) {
  const cache = await caches.open(CACHE);
  try {
    const res = await fetch(req);
    const okType = !html || (res.headers.get('content-type') || '').includes('text/html');
    if (res.ok && res.type === 'basic' && okType) cache.put(key, res.clone());
    return res;
  } catch (err) {
    const hit = await cache.match(key);
    if (hit) return hit;
    throw err;
  }
}

async function cacheFirst(req) {
  const cache = await caches.open(CACHE);
  const hit = await cache.match(req);
  if (hit) return hit;
  const res = await fetch(req);
  // Yalnızca gerçek dosyayı sakla (eksik dosyada sunucu 404 döner, HTML değil).
  if (res.ok && res.type === 'basic') cache.put(req, res.clone());
  return res;
}

self.addEventListener('fetch', (event) => {
  const req = event.request;
  const url = new URL(req.url);
  if (bypass(url, req)) return;

  if (req.mode === 'navigate') {
    // Doğrudan açılan dosyalar (ör. bir simge) sayfa değildir; dokunma.
    if (/\.[a-z0-9]+$/i.test(url.pathname) && !url.pathname.endsWith('.html')) return;
    // Hash yönlendirici: tüm yönetim sayfaları aynı index.html'dir.
    event.respondWith(networkFirst(req, INDEX, true));
    return;
  }
  if (url.pathname.startsWith('/assets/')) {
    event.respondWith(cacheFirst(req));
    return;
  }
  if (url.pathname.startsWith('/icons/') || url.pathname === '/manifest.webmanifest' || url.pathname === '/favicon.svg') {
    event.respondWith(networkFirst(req, req));
  }
});
