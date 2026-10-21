// Yeni sürüm şeridi uçtan uca, sahte GitHub sürüm sunucusuyla (bu süreçte açılır).
// Panel 1.5.0 sürümüyle ve UPDATE_CHECK_URL=http://127.0.0.1:FAKE_PORT ile başlamalı:
//   go build -ldflags "-X main.version=1.5.0" ... ; UPDATE_CHECK_URL=http://127.0.0.1:8099 uptime
//   BASE=http://127.0.0.1:8080 node guncelleme.mjs
// Akış: yeni sürüm yokken şerit yok -> 1.5.1 çıkar, şerit görünür -> kapat ->
// yenile, görünmez -> 1.6.0 çıkar, yeniden görünür; yönetici olmayan görmez.
import { chromium } from 'playwright';
import { createServer } from 'node:http';
import { mkdirSync } from 'node:fs';

const BASE = process.env.BASE || 'http://127.0.0.1:8080';
const FAKE_PORT = Number(process.env.FAKE_PORT || 8099);
const SHOTS = process.env.SHOTS || '';
const USER = 'e2e';
const PASS = 'e2e-parola-12345';
const H = { 'Content-Type': 'application/json', 'X-Uptime': '1' };

function fail(msg) {
  console.error('HATA:', msg);
  process.exit(1);
}

let tags = ['v1.4.0', 'v1.5.0', 'v1.5.0-postgres', 'v1.6.0-rc.1'];
let releases = [{ tag_name: 'v1.5.0', html_url: 'https://github.com/kadirsungurlu/bekci/releases/tag/v1.5.0', draft: false, prerelease: false }];
const fake = createServer((req, res) => {
  const path = req.url.split('?')[0];
  res.setHeader('Content-Type', 'application/json');
  if (path === '/tags') return res.end(JSON.stringify(tags.map((name) => ({ name }))));
  if (path === '/releases') return res.end(JSON.stringify(releases));
  res.statusCode = 404;
  res.end('{}');
});
await new Promise((r) => fake.listen(FAKE_PORT, '127.0.0.1', r));

const browser = await chromium.launch();
try {
  const ctx = await browser.newContext();
  const rq = ctx.request;
  await rq.post(BASE + '/api/auth/setup', { data: { username: USER, password: PASS }, headers: H });
  const login = await rq.post(BASE + '/api/auth/login', { data: { username: USER, password: PASS }, headers: H });
  if (!login.ok()) fail('giriş: ' + login.status());
  const check = async () => (await rq.post(BASE + '/api/update/check', { headers: H })).json();

  let st = await check();
  if (!st.enabled || st.current !== '1.5.0') fail('denetim kapalı ya da sürüm yanlış: ' + JSON.stringify(st));
  if (st.show) fail('yeni sürüm yokken şerit: ' + JSON.stringify(st));

  const banner = (p) => p.getByTestId('update-banner');
  const page = async (w) => {
    const p = await ctx.newPage();
    await p.setViewportSize({ width: w, height: w < 500 ? 800 : 900 });
    await p.goto(BASE + '/');
    await p.locator('main').waitFor();
    await p.waitForTimeout(800);
    return p;
  };
  let p = await page(1280);
  if (await banner(p).count()) fail('yeni sürüm yokken şerit görünüyor');
  await p.close();

  // 1.5.1 yalnız etiket olarak çıkar.
  tags = [...tags, 'v1.5.1', 'v1.5.1-postgres'];
  st = await check();
  if (!st.show || st.latest !== '1.5.1') fail('1.5.1: ' + JSON.stringify(st));
  if (SHOTS) mkdirSync(SHOTS, { recursive: true });
  for (const w of [375, 1280]) {
    p = await page(w);
    await banner(p).waitFor({ timeout: 10000 });
    if ((await banner(p).getAttribute('data-version')) !== '1.5.1') fail('şerit sürümü');
    await p.getByTestId('update-how').click();
    const sw = await p.evaluate(() => document.documentElement.scrollWidth);
    if (sw > w) fail(`${w}px: yatay taşma ${sw}`);
    if (SHOTS) await p.screenshot({ path: `${SHOTS}/guncelleme-${w}.jpg`, type: 'jpeg', quality: 60 });
    if (w === 1280) {
      await p.getByTestId('update-dismiss').click();
      await banner(p).waitFor({ state: 'detached', timeout: 5000 });
      await p.reload();
      await p.locator('main').waitFor();
      await p.waitForTimeout(1000);
      if (await banner(p).count()) fail('kapatılan sürüm yenileyince yeniden göründü');
    }
    await p.close();
  }
  console.log('şerit: göründü, kapatıldı, yenilemede görünmedi');

  // 1.6.0 yayın notuyla çıkar -> yeniden görünür.
  releases = [...releases, { tag_name: 'v1.6.0', html_url: 'https://example.test/notlar/1.6.0', draft: false, prerelease: false }];
  st = await check();
  if (!st.show || st.latest !== '1.6.0') fail('1.6.0: ' + JSON.stringify(st));
  p = await page(1280);
  await banner(p).waitFor({ timeout: 10000 });
  if ((await p.getByRole('link', { name: /Neler yeni|What's new/ }).getAttribute('href')) !== 'https://example.test/notlar/1.6.0') fail('Neler yeni bağlantısı');
  await p.close();
  console.log('daha yeni sürüm: şerit yeniden göründü');

  // Yönetici olmayan görmez.
  const vu = 'e2e-izleyici';
  const cu = await rq.post(BASE + '/api/users', { data: { username: vu, password: 'Gecici-Parola-123', role: 'viewer', all_monitors: true }, headers: H });
  if (!cu.ok() && cu.status() !== 409) fail('kullanıcı: ' + (await cu.text()));
  const vctx = await browser.newContext({ viewport: { width: 1280, height: 900 } });
  const vr = vctx.request;
  if (!(await vr.post(BASE + '/api/auth/login', { data: { username: vu, password: 'Gecici-Parola-123' }, headers: H })).ok()) fail('izleyici girişi');
  await vr.post(BASE + '/api/auth/password', { data: { current: 'Gecici-Parola-123', new: 'Yeni-Parola-45678' }, headers: H });
  if ((await vr.get(BASE + '/api/update')).status() !== 403) fail('izleyici /api/update erişebiliyor');
  const vp = await vctx.newPage();
  await vp.goto(BASE + '/');
  await vp.locator('main').waitFor();
  await vp.waitForTimeout(1500);
  if (await banner(vp).count()) fail('izleyici şeridi görüyor');
  await vctx.close();
  console.log('izleyici: şerit yok');
  console.log('TAMAM');
} finally {
  await browser.close();
  fake.close();
}
