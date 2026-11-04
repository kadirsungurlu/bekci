// Sunucu olayları uçtan uca: ajandan gelen OOM olayı zaman çizelgesinde, sorun
// süzgeci, boş durum, disk tahmin sütunu, 390 px yatay taşma.
// Çalışan bir panele karşı:  BASE=http://127.0.0.1:8080 node sunucu-olaylar.mjs
// SHOTS=<klasör> ve FC_ID=<sunucu> verilirse dolan diskli sunucunun görüntüleri alınır.
import { chromium } from 'playwright';
import { mkdirSync } from 'node:fs';

const BASE = process.env.BASE || 'http://127.0.0.1:8080';
const SHOTS = process.env.SHOTS || '';
const FC_ID = process.env.FC_ID || '';
const USER = 'e2e';
const PASS = 'e2e-parola-12345';
const H = { 'Content-Type': 'application/json', 'X-Uptime': '1' };

function fail(msg) {
  console.error('HATA:', msg);
  process.exit(1);
}

const browser = await chromium.launch();
try {
  const ctx = await browser.newContext({ viewport: { width: 1920, height: 1000 }, locale: 'tr-TR' });
  const rq = ctx.request;
  await rq.post(BASE + '/api/auth/setup', { data: { username: USER, password: PASS }, headers: H });
  const login = await rq.post(BASE + '/api/auth/login', { data: { username: USER, password: PASS }, headers: H });
  if (!login.ok()) fail('giriş: ' + login.status());
  const mk = async (name) => (await rq.post(BASE + '/api/servers', { data: { name }, headers: H })).json();
  const a = await mk('olay-' + Date.now());
  const b = await mk('bos-' + Date.now());
  const now = Math.floor(Date.now() / 1000);
  const r = await rq.post(BASE + '/api/probe/metrics', {
    headers: { ...H, Authorization: 'Bearer ' + a.token },
    data: {
      time: Date.now(),
      host: { hostname: 'web-1', platform: 'debian', arch: 'amd64', cores: 4 },
      stats: {
        cpu: 12, mem_total: 2 ** 33, mem_used: 2 ** 32, uptime: 5000,
        disks: [{ mount: '/', device: '/dev/sda1', fs: 'ext4', total: 100 * 2 ** 30, used: 40 * 2 ** 30 }],
      },
      events: [
        { kind: 'oom', time: now - 60, subject: 'java', pid: 4242, count: 2 },
        { kind: 'oom', time: now - 30, count: 1 },
      ],
    },
  });
  if (!r.ok()) fail('metrik: ' + r.status() + ' ' + (await r.text()));

  const page = await ctx.newPage();
  const errors = [];
  page.on('pageerror', (e) => errors.push(String(e)));
  const shot = async (p, name) => {
    if (SHOTS) await p.screenshot({ path: `${SHOTS}/${name}.jpg`, type: 'jpeg', quality: 55 });
  };
  if (SHOTS) mkdirSync(SHOTS, { recursive: true });
  const tab = (p) => p.getByRole('tab', { name: /^(Olaylar|Events)$/ }).click();

  await page.goto(`${BASE}/#/servers/${a.probe.id}`);
  await page.locator('th', { hasText: /^(Tahmin|Forecast)$/ }).waitFor();
  if (!(await page.locator('td.fc').first().innerText()).trim()) fail('tahmin hücresi boş');
  await tab(page);
  await page.locator('.evs li').first().waitFor();
  const list = await page.locator('.tl').innerText();
  if (!/java/.test(list) || !/PID 4242/.test(list)) fail('OOM kaydı yok: ' + list);
  if (!/(okunamad|bir süreç|a process)/i.test(list)) fail('adsız OOM metni yok: ' + list);
  await page.locator('.seg button').nth(1).click();
  await page.waitForTimeout(400);
  if ((await page.locator('.evs li').count()) < 2) fail('sorun süzgeci OOM kayıtlarını göstermeli');

  await page.goto(`${BASE}/#/servers/${b.probe.id}`);
  await tab(page);
  await page.locator('.tl .empty h3').waitFor();

  // Telefon: yatay taşma yok, süzgeç düğmesi dokunmaya uygun.
  const m = await browser.newContext({ viewport: { width: 390, height: 800 }, storageState: await ctx.storageState(), locale: 'tr-TR' });
  const pm = await m.newPage();
  await pm.goto(`${BASE}/#/servers/${a.probe.id}`);
  await tab(pm);
  await pm.locator('.evs li').first().waitFor();
  const over = await pm.evaluate(() => document.documentElement.scrollWidth - innerWidth);
  if (over > 0) fail('390 px yatay taşma: ' + over);
  const bh = (await pm.locator('.seg button').first().boundingBox()).height;
  if (bh < 44) fail('süzgeç düğmesi kısa: ' + bh);

  if (SHOTS && FC_ID) {
    for (const [p, w] of [[page, '1920'], [pm, '390']]) {
      await p.goto(`${BASE}/#/servers`);
      await p.waitForTimeout(1200);
      await shot(p, `liste-${w}`);
      await p.goto(`${BASE}/#/servers/${FC_ID}`);
      await p.locator('td.fc').first().waitFor();
      await p.locator('td.fc').first().scrollIntoViewIfNeeded();
      await p.waitForTimeout(500);
      await shot(p, `tahmin-${w}`);
      await tab(p);
      await p.locator('.evs li').first().waitFor();
      await shot(p, `cizelge-${w}`);
    }
  }
  if (errors.length) fail('sayfa hatası: ' + errors[0]);
  console.log('tamam');
} finally {
  await browser.close();
}
