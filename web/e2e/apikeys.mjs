// Salt okunur API anahtarı uçtan uca: modalde varsayılan kapsam, oluşturma,
// listedeki rozet, okuma ucu 200, yazma/ayar 403 (code read_only_key), son
// kullanım, işlem kaydı, iptal sonrası 401; 390 px taşma denetimi.
// Çalışan bir panele karşı:  BASE=http://127.0.0.1:8080 node apikeys.mjs
// SHOTS=<klasör> verilirse 1920 ve 390 görüntüleri kaydedilir.
import { chromium } from 'playwright';
import { mkdirSync } from 'node:fs';

const BASE = process.env.BASE || 'http://127.0.0.1:8080';
const SHOTS = process.env.SHOTS || '';
const USER = 'e2e';
const PASS = 'e2e-parola-12345';
const H = { 'Content-Type': 'application/json', 'X-Uptime': '1' };
const NAME = 'okuma-' + Date.now();

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
  if (SHOTS) mkdirSync(SHOTS, { recursive: true });
  const shot = async (p, ad) => {
    if (SHOTS) await p.screenshot({ path: `${SHOTS}/${ad}.jpg`, type: 'jpeg', quality: 60 });
  };

  const page = await ctx.newPage();
  const errors = [];
  page.on('pageerror', (e) => errors.push(e.message));
  await page.goto(BASE + '/#/settings');
  await page.getByRole('button', { name: /Yeni anahtar|New key/ }).click();

  // Varsayılan kapsam salt okunur, rol alanı görünmez; tam erişimde görünür.
  const scope = page.locator('#ak-scope');
  await scope.waitFor();
  if ((await scope.inputValue()) !== 'read') fail('varsayılan kapsam read değil');
  if (await page.locator('#ak-role').count()) fail('salt okunurda rol alanı görünüyor');
  await scope.selectOption('full');
  await page.locator('#ak-role').waitFor();
  await scope.selectOption('read');
  await page.locator('#ak-name').fill(NAME);
  await shot(page, 'modal-1920');
  await page.locator('button[type=submit][form=akf]').click();

  // Tek seferlik anahtar yalnız bu ekranda görünür.
  const code = page.locator('.box .copybox code').filter({ hasText: /^upk_/ }).first();
  await code.waitFor();
  const secret = (await code.textContent()).trim();
  if (!secret.startsWith('upk_')) fail('anahtar görünmedi');
  await page.getByRole('button', { name: /Kopyaladım|copied/i }).click();

  const row = page.locator('tr', { hasText: NAME });
  await row.waitFor();
  const rowText = await row.textContent();
  if (!/Salt okunur|Read-only/.test(rowText)) fail('listede rozet yok: ' + rowText);
  if (rowText.includes(secret)) fail('anahtar listede göründü');
  await shot(page, 'liste-1920');

  // Çerezsiz ayrı istemci: yalnız anahtarla.
  const k = await browser.newContext();
  const bearer = { ...H, Authorization: 'Bearer ' + secret };
  const get = (u) => k.request.get(BASE + u, { headers: bearer });
  for (const u of ['/api/servers', '/api/monitors', '/api/incidents', '/api/certs', '/api/server-events']) {
    const r = await get(u);
    if (r.status() !== 200) fail(`${u}: ${r.status()}`);
  }
  for (const u of ['/api/settings', '/api/users', '/api/api-keys', '/api/export', '/api/audit']) {
    const r = await get(u);
    if (r.status() !== 403) fail(`${u}: ${r.status()} (403 beklenirdi)`);
    if ((await r.json()).code !== 'read_only_key') fail(`${u}: code yok`);
  }
  const w = await k.request.post(BASE + '/api/monitors', { headers: bearer, data: { name: 'x', type: 'http' } });
  if (w.status() !== 403) fail('yazma: ' + w.status());

  // Son kullanım ve işlem kaydı.
  const keys = await (await rq.get(BASE + '/api/api-keys')).json();
  const key = keys.find((x) => x.name === NAME);
  if (!key || key.scope !== 'read' || key.role !== 'viewer') fail('kayıt: ' + JSON.stringify(key));
  if (!key.last_used_at) fail('son kullanım yazılmadı');
  const audit = await (await rq.get(BASE + '/api/audit?limit=20')).json();
  const list = Array.isArray(audit) ? audit : audit.items || [];
  if (!list.some((a) => a.action === 'apikey.create' && a.target_name === NAME)) fail('işlem kaydı yok');

  // Dar ekran: yatay taşma yok.
  await page.setViewportSize({ width: 390, height: 844 });
  await page.reload();
  await page.locator('[data-label]', { hasText: NAME }).first().waitFor();
  const over = await page.evaluate(() => document.documentElement.scrollWidth - innerWidth);
  if (over > 1) fail('390 px yatay taşma: ' + over);
  await page.locator('[data-label]', { hasText: NAME }).first().scrollIntoViewIfNeeded();
  await shot(page, 'liste-390');

  // İptal: arayüzden, sonra anahtar 401.
  await page.locator('tr', { hasText: NAME }).getByRole('button', { name: /İptal et|Revoke/ }).click();
  await page.locator('.box button.danger').click();
  await page.locator('tr', { hasText: NAME }).getByText(/İptal edildi|Revoked/).waitFor();
  const gone = await get('/api/servers');
  if (gone.status() !== 401) fail('iptal sonrası: ' + gone.status());
  if (errors.length) fail('sayfa hatası: ' + errors.join(' | '));
  console.log('apikeys e2e tamam');
} finally {
  await browser.close();
}
