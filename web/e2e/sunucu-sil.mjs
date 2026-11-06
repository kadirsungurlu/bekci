// Bağlanmamış sunucunun listeden silinmesi: vazgeç satırı korur, onay siler,
// editör düğmeyi görmez, 390 px yatay taşma yok.
// Çalışan bir panele karşı:  BASE=http://127.0.0.1:8080 node sunucu-sil.mjs
// SHOTS=<klasör> verilirse ekran görüntüleri alınır.
import { chromium } from 'playwright';

const BASE = process.env.BASE || 'http://127.0.0.1:8080';
const SHOTS = process.env.SHOTS || '';
const USER = 'e2e';
const PASS = 'e2e-parola-12345';
const H = { 'Content-Type': 'application/json', 'X-Uptime': '1' };

function fail(msg) {
  console.error('HATA:', msg);
  process.exit(1);
}

const browser = await chromium.launch({ executablePath: process.env.CHROME || undefined });
try {
  const ctx = await browser.newContext({ viewport: { width: 1920, height: 1000 }, locale: 'tr-TR' });
  const rq = ctx.request;
  await rq.post(BASE + '/api/auth/setup', { data: { username: USER, password: PASS }, headers: H });
  const login = await rq.post(BASE + '/api/auth/login', { data: { username: USER, password: PASS }, headers: H });
  if (!login.ok()) fail('giriş: ' + login.status());
  const ad = 'sil-' + Date.now();
  const uzun = 'uzun-adli-bekleyen-sunucu-' + 'x'.repeat(40) + Date.now();
  await rq.post(BASE + '/api/servers', { data: { name: ad }, headers: H });
  await rq.post(BASE + '/api/servers', { data: { name: uzun }, headers: H });

  const page = await ctx.newPage();
  await page.goto(BASE + '/#/servers');
  const row = page.locator('.irow', { hasText: ad });
  const sil = row.getByRole('button', { name: 'Sil', exact: true });
  await sil.waitFor();
  if (SHOTS) await page.screenshot({ path: SHOTS + '/liste-1920.png' });

  await sil.click();
  const dlg = page.getByRole('dialog');
  await dlg.waitFor();
  if (SHOTS) await page.screenshot({ path: SHOTS + '/onay-1920.png' });
  await dlg.getByRole('button', { name: 'Vazgeç' }).click();
  await dlg.waitFor({ state: 'hidden' });
  if ((await row.count()) !== 1) fail('vazgeçince satır kalmalı');

  await page.setViewportSize({ width: 390, height: 844 });
  await sil.waitFor();
  const tasma = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  if (tasma > 0) fail('390 px yatay taşma: ' + tasma);
  if ((await sil.boundingBox()).height < 44) fail('390 px dokunma alanı 44 px altında');
  if (SHOTS) await page.screenshot({ path: SHOTS + '/liste-390.png' });
  await page.setViewportSize({ width: 1920, height: 1000 });

  await sil.click();
  await dlg.waitFor();
  await dlg.getByRole('button', { name: 'Sil', exact: true }).click();
  await row.waitFor({ state: 'detached' });
  const list = await (await rq.get(BASE + '/api/servers')).json();
  if (JSON.stringify(list).includes(ad)) fail('API listesinde hâlâ var');
  await page.reload();
  await page.locator('.irow', { hasText: uzun }).waitFor();
  if ((await row.count()) !== 0) fail('yenileyince geri geldi');

  const editor = 'editor-' + Date.now();
  const u = await rq.post(BASE + '/api/users', { data: { username: editor, password: PASS, role: 'editor', all_monitors: true }, headers: H });
  if (!u.ok()) fail('editör: ' + u.status());
  const ctx2 = await browser.newContext({ viewport: { width: 1920, height: 1000 }, locale: 'tr-TR' });
  await ctx2.request.post(BASE + '/api/auth/login', { data: { username: editor, password: PASS }, headers: H });
  // Yeni hesap ilk girişte parola değiştirir, yoksa uçlar reddeder.
  const yeni = 'degisti-parola-98765';
  const pw = await ctx2.request.post(BASE + '/api/auth/password', {
    data: { current: PASS, new: yeni },
    headers: H,
  });
  if (!pw.ok()) fail('parola: ' + pw.status() + ' ' + (await pw.text()));
  const p2 = await ctx2.newPage();
  await p2.goto(BASE + '/#/servers');
  const r2 = p2.locator('.irow', { hasText: uzun });
  await r2.waitFor();
  if ((await r2.getByRole('button', { name: 'Sil', exact: true }).count()) !== 0) fail('editör Sil görmemeli');
  console.log('TAMAM: sunucu-sil');
} finally {
  await browser.close();
}
