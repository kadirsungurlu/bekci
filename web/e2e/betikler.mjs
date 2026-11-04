// Betikler bölümü: dört sayfa açılıyor, 2FA'sız yönetici uyarı görüyor, yazma düğmesi
// kapalı. SQLite panelde PostgreSQL açıklaması beklenir.
// Çalışan bir panele karşı:  BASE=http://127.0.0.1:8080 node betikler.mjs
import { chromium } from 'playwright';

const BASE = process.env.BASE || 'http://127.0.0.1:8080';
const USER = 'e2e';
const PASS = 'e2e-parola-12345';
const H = { 'Content-Type': 'application/json', 'X-Uptime': '1' };

function fail(msg) {
  console.error('HATA:', msg);
  process.exit(1);
}

const browser = await chromium.launch();
try {
  const ctx = await browser.newContext({ viewport: { width: 1920, height: 1080 } });
  const rq = ctx.request;
  await rq.post(BASE + '/api/auth/setup', { data: { username: USER, password: PASS }, headers: H });
  const login = await rq.post(BASE + '/api/auth/login', { data: { username: USER, password: PASS }, headers: H });
  if (!login.ok()) fail('giriş: ' + login.status());
  const probe = await rq.get(BASE + '/api/scripts');
  const sqlite = probe.status() === 409;

  const page = await ctx.newPage();
  const errors = [];
  page.on('pageerror', (e) => errors.push(e.message));
  for (const path of ['/scripts', '/scripts/runs', '/scripts/settings']) {
    await page.goto(`${BASE}/#${path}`);
    await page.locator('h1', { hasText: /Betikler|Scripts/ }).waitFor();
    if (sqlite) await page.getByRole('heading', { name: /PostgreSQL/ }).waitFor();
    else await page.getByTestId('scripts-2fa').waitFor();
  }
  if (!sqlite) {
    await page.goto(`${BASE}/#/scripts`);
    await page.getByTestId('scripts-2fa').waitFor();
    if (!(await page.getByRole('button', { name: /Betik yükle|Upload script/ }).first().isDisabled())) fail('2FA yokken yükleme açık');
  }
  if (errors.length) fail('sayfa hatası: ' + errors.join('; '));
  console.log('tamam');
} finally {
  await browser.close();
}
