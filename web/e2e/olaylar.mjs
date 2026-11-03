// Olaylar listesi sayfalama uçtan uca: varsayılan 25, sayfa adreste (geri tuşu),
// sayfa boyu 50, arama ve sekme sayfalamayla birlikte, CSV tüm süzülmüş sonuç;
// sayfada "null" yazan görünür öğe yok. Çalışan bir panele karşı:
//   BASE=http://127.0.0.1:8080 node olaylar.mjs
// SHOTS=<klasör> verilirse koyu/açık 1280 ve 375 görüntüleri kaydedilir.
import { chromium } from 'playwright';
import { mkdirSync } from 'node:fs';

const BASE = process.env.BASE || 'http://127.0.0.1:8080';
const SHOTS = process.env.SHOTS || '';
const USER = 'e2e';
const PASS = 'e2e-parola-12345';
const H = { 'Content-Type': 'application/json', 'X-Uptime': '1' };
const N = 60;

function fail(msg) {
  console.error('HATA:', msg);
  process.exit(1);
}

const browser = await chromium.launch();
try {
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 900 } });
  const rq = ctx.request;
  await rq.post(BASE + '/api/auth/setup', { data: { username: USER, password: PASS }, headers: H });
  const login = await rq.post(BASE + '/api/auth/login', { data: { username: USER, password: PASS }, headers: H });
  if (!login.ok()) fail('giriş: ' + login.status());

  const pr = await rq.post(BASE + '/api/status-pages', { data: { slug: 'e2e-olaylar', title: 'E2E Olaylar', published: true, sections: [] }, headers: H });
  let pageId = pr.ok() ? (await pr.json()).id : 0;
  if (!pageId) pageId = ((await (await rq.get(BASE + '/api/status-pages', { headers: H })).json()).find((x) => x.slug === 'e2e-olaylar') || {}).id;
  if (!pageId) fail('sayfa: ' + pr.status());
  const now = Math.floor(Date.now() / 1000);
  for (let i = 0; i < N; i++) {
    const r = await rq.post(BASE + '/api/incidents', {
      data: { page_id: pageId, title: `E2E sayfalama ${String(i + 1).padStart(2, '0')}`, severity: 'minor', started_at: now - (N - i) * 600, body: 'deneme' },
      headers: H,
    });
    if (!r.ok()) fail('olay: ' + (await r.text()));
  }
  // Önceki betiklerin olayları da sayılır: toplam API'den.
  const all = await rq.get(BASE + '/api/incidents?offset=0&limit=25');
  const TOTAL = Number(all.headers()['x-total-count']);
  if (!(TOTAL >= N)) fail('toplam ' + TOTAL);

  const p = await ctx.newPage();
  const rows = () => p.locator('table tbody tr').count();
  const hash = () => p.evaluate(() => location.hash);
  await p.goto(BASE + '/#/incidents');
  await p.getByTestId('pager').waitFor({ timeout: 15000 });
  if ((await rows()) !== 25) fail('1. sayfa satır ' + (await rows()));
  if (!(await p.getByTestId('pager-total').textContent()).includes(String(TOTAL))) fail('toplam metni');

  await p.getByTestId('pager-next').click();
  await p.waitForFunction(() => location.hash.includes('sayfa=2')).catch(() => {});
  if (!(await hash()).includes('sayfa=2')) fail('adreste sayfa yok: ' + (await hash()));
  const p2first = await p.locator('table tbody tr').first().textContent();
  await p.getByRole('button', { name: /^(Sayfa|Page) 3$/ }).click();
  await p.waitForTimeout(600);
  if (!(await hash()).includes('sayfa=3')) fail('3. sayfa adresi');
  await p.goBack();
  await p.waitForTimeout(800);
  if (!(await hash()).includes('sayfa=2')) fail('geri tuşu: ' + (await hash()));
  if ((await p.locator('table tbody tr').first().textContent()) !== p2first) fail('geri tuşunda içerik 2. sayfa değil');

  await p.getByTestId('pager-size').selectOption('50');
  await p.waitForTimeout(800);
  if ((await rows()) !== 50 || !(await hash()).includes('adet=50') || (await hash()).includes('sayfa=')) fail('sayfa boyu 50: ' + (await hash()));

  // Arama + sekme sayfalamayla birlikte: toplam daralır, ilk sayfaya döner.
  await p.getByRole('radio', { name: /Elle|Manual/ }).click();
  await p.locator('input[type="search"]').fill('sayfalama 0');
  await p.waitForTimeout(1200);
  const tot = await p.getByTestId('pager-total').textContent();
  if (!/\b9\b/.test(tot) || (await rows()) !== 9) fail('arama sonucu: ' + tot + ' / ' + (await rows()));
  if (!(await hash()).includes('tur=manual') || !(await hash()).includes('q=')) fail('süzgeç adreste değil: ' + (await hash()));
  const csv = await p.locator('a[download]').first().getAttribute('href');
  if (/offset|limit/.test(csv) || !csv.includes('kind=manual')) fail('CSV adresi: ' + csv);

  // Doğrudan adres: süzgeçli 2. sayfa açılır.
  await p.goto(BASE + '/#/incidents?tur=manual&sayfa=2');
  await p.getByTestId('pager').waitFor();
  await p.waitForTimeout(600);
  if ((await rows()) !== 25 || !(await p.getByRole('button', { name: /^(Sayfa|Page) 2$/ }).getAttribute('aria-current'))) fail('doğrudan adres 2. sayfa');

  // "null" yazan görünür öğe yok.
  const nulls = await p.evaluate(() =>
    [...document.querySelectorAll('body *')]
      .filter((e) => e.childElementCount === 0 && e.textContent.trim() === 'null' && e.getClientRects().length > 0)
      .map((e) => e.outerHTML.slice(0, 120)),
  );
  if (nulls.length) fail('"null" öğesi: ' + nulls.join(' | '));
  console.log('olaylar: sayfalama, adres, geri tuşu, süzgeç, CSV tamam');

  if (SHOTS) {
    mkdirSync(SHOTS, { recursive: true });
    for (const scheme of ['dark', 'light']) {
      for (const w of [1280, 375]) {
        const c = await browser.newContext({ viewport: { width: w, height: w < 500 ? 800 : 900 }, colorScheme: scheme });
        await c.addCookies((await ctx.cookies()).map((k) => ({ ...k })));
        const q = await c.newPage();
        await q.goto(BASE + '/#/incidents?sayfa=2');
        await q.getByTestId('pager').waitFor();
        await q.getByTestId('pager').scrollIntoViewIfNeeded();
        await q.waitForTimeout(400);
        await q.screenshot({ path: `${SHOTS}/olaylar-${scheme}-${w}.jpg`, type: 'jpeg', quality: 60 });
        await c.close();
      }
    }
  }
  console.log('TAMAM');
} finally {
  await browser.close();
}
