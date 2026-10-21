// Durum sayfası uçtan uca: ızgara sütun ayarı (1, 2, 3, 6) üç ekran genişliğinde
// taşma denetimi ve canlı güncelleme (monitör düşünce sayfa yenilenmeden değişir;
// canlı akış yokken eski "60 sn" metni). Çalışan bir panele karşı:
//   BASE=http://127.0.0.1:8080 node durum.mjs
// SHOTS=<klasör> verilirse her sütun/genişlik için görünüm alanı görüntüsü kaydedilir.
import { chromium } from 'playwright';
import { mkdirSync } from 'node:fs';

const BASE = process.env.BASE || 'http://127.0.0.1:8080';
const SHOTS = process.env.SHOTS || '';
const USER = 'e2edurum';
const PASS = 'e2e-durum-sifre-123';
const H = { 'Content-Type': 'application/json', 'X-Uptime': '1' };

function fail(msg) {
  console.error('HATA:', msg);
  process.exit(1);
}

const browser = await chromium.launch();
try {
  const ctx = await browser.newContext();
  const rq = ctx.request;
  await rq.post(BASE + '/api/auth/setup', { data: { username: USER, password: PASS }, headers: H });
  const login = await rq.post(BASE + '/api/auth/login', { data: { username: USER, password: PASS }, headers: H });
  if (!login.ok()) fail('giriş: ' + login.status());

  // Altı grup, her birinde iki push monitör (biri uzun adlı: taşma denetimi).
  const groups = [];
  const mons = [];
  for (let g = 0; g < 6; g++) {
    const ids = [];
    for (const name of [`Servis ${g + 1}`, `Ödeme ve faturalandırma API'si birincil bölge ${g + 1}`]) {
      const r = await rq.post(BASE + '/api/monitors', { data: { name, type: 'push', interval: 60, config: {} }, headers: H });
      if (!r.ok()) fail('monitör: ' + (await r.text()));
      const m = await r.json();
      mons.push(m);
      ids.push({ id: m.id });
      await rq.get(BASE + '/api/push/' + m.push_token + '?status=up&msg=ok');
    }
    groups.push({ title: `Grup ${g + 1} uzun bir grup başlığı örneği`, monitors: ids });
  }
  const pageBody = (columns) => ({
    slug: 'e2e-durum', title: 'E2E Durum', published: true, lang: 'tr', sections: groups,
    bar_range: 'recent', layout: { style: 'grid', width: 'wide', columns, show_uptime: true },
  });
  const cr = await rq.post(BASE + '/api/status-pages', { data: pageBody(0), headers: H });
  if (!cr.ok()) fail('sayfa: ' + (await cr.text()));
  const pageId = (await cr.json()).id;
  const url = BASE + '/durum/e2e-durum';

  // Izgara ---------------------------------------------------------------------------
  if (SHOTS) mkdirSync(SHOTS, { recursive: true });
  const viewports = [375, 1280, 1920];
  for (const cols of [1, 2, 3, 6]) {
    const up = await rq.put(BASE + `/api/status-pages/${pageId}`, { data: pageBody(cols), headers: H });
    if (!up.ok()) fail('sayfa güncelleme: ' + (await up.text()));
    const got = await (await rq.get(BASE + '/api/public/pages/e2e-durum?t=' + cols)).json();
    if (got.layout?.columns !== cols) fail(`genel uçta columns ${got.layout?.columns}, ${cols} bekleniyordu`);
    for (const w of viewports) {
      const p = await browser.newPage({ viewport: { width: w, height: 900 } });
      await p.goto(url);
      await p.waitForSelector('.mon');
      await p.waitForTimeout(300);
      const r = await p.evaluate(() => {
        const over = (el) => el.scrollWidth > el.clientWidth + 1;
        const bad = [...document.querySelectorAll('.mon, .m-top, .bars, .g-head')].filter(over).map((e) => e.className);
        const gc = document.querySelector('.groups.gcols');
        const cols = gc ? getComputedStyle(gc).gridTemplateColumns.split(' ').length : 1;
        const minBar = Math.min(...[...document.querySelectorAll('.bars > *')].map((b) => b.getBoundingClientRect().width));
        return { bad, cols, pageOver: document.documentElement.scrollWidth > innerWidth, minBar };
      });
      if (r.bad.length) fail(`${cols} sütun @${w}: taşan öğeler ${r.bad.slice(0, 3).join(', ')}`);
      if (r.pageOver) fail(`${cols} sütun @${w}: yatay kaydırma var`);
      if (w === 375 && r.cols !== 1) fail(`${cols} sütun @375: ${r.cols} sütun (1 bekleniyordu)`);
      if (w === 1920 && r.cols !== cols) fail(`${cols} sütun @1920: ${r.cols} sütun`);
      if (r.minBar < 2) fail(`${cols} sütun @${w}: çubuk çok ince (${r.minBar} px)`);
      if (SHOTS) await p.screenshot({ path: `${SHOTS}/durum-${cols}sutun-${w}.jpg`, type: 'jpeg', quality: 60 });
      console.log(`ızgara ${cols} sütun @${w}: ${r.cols} sütun, en ince çubuk ${r.minBar.toFixed(1)} px`);
      await p.close();
    }
  }

  // Canlı güncelleme -----------------------------------------------------------------
  const p = await browser.newPage({ viewport: { width: 1280, height: 900 } });
  let navs = 0;
  p.on('framenavigated', (f) => f === p.mainFrame() && navs++);
  const reqs = [];
  p.on('request', (r) => r.url().includes('/api/public/') && reqs.push(r.url().replace(BASE, '')));
  await p.goto(url);
  await p.waitForSelector('.refresh .live-t', { timeout: 10000 }).catch(() => fail('"Canlı" göstergesi çıkmadı'));
  // İlk monitör ("Servis 1", ilk grubun ilk satırı) çalışıyor; düşürülünce satırı "Kesinti" olmalı.
  const first = () => p.evaluate(() => document.querySelector('.mon')?.textContent ?? '');
  if (!(await first()).includes('Servis 1') || (await first()).includes('Kesinti')) fail('ilk satır beklenen durumda değil: ' + (await first()));
  await p.evaluate(() => (window.__durumE2E = 1));
  await rq.get(BASE + '/api/push/' + mons[0].push_token + '?status=down&msg=ic-ayrinti');
  // waitForFunction yerine elle yoklama: Playwright'ın waitForFunction'ı bu
  // sürümde sayfanın açık EventSource bağlantısını sessizce düşürüyor.
  let changed = false;
  for (let i = 0; i < 40 && !changed; i++) {
    await p.waitForTimeout(250);
    changed = (await first()).includes('Kesinti');
  }
  if (!changed) fail('izleyici düştü ama sayfa 10 sn içinde değişmedi; istekler: ' + reqs.join(' ') + ' satır: ' + (await first()))
  if ((await p.evaluate(() => window.__durumE2E)) !== 1 || navs !== 1) fail('sayfa yeniden yüklendi');
  if ((await p.content()).includes('ic-ayrinti')) fail('iç mesaj genel sayfaya sızdı');
  console.log('canlı: izleyici düşünce sayfa yenilenmeden değişti');
  // Kendiliğinden yeniden bağlanma: akış sunucu tarafında kapanınca tekrar "Canlı".
  await p.close();

  // Akış yoksa eski metin (periyodik yenileme).
  const q = await browser.newPage({ viewport: { width: 1280, height: 900 } });
  await q.route('**/events', (r) => r.abort());
  await q.goto(url);
  await q.waitForSelector('.mon');
  await q.waitForTimeout(800);
  const txt = await q.locator('.refresh').innerText();
  if (!txt.includes('60 sn')) fail('akış yokken eski metin yok: ' + txt);
  // Sağ alttaki boş kutu: sayfada yalnız "null" yazan öğe olmamalı.
  const nullBox = await q.evaluate(() =>
    [...document.querySelectorAll('body *')].some((e) => e.children.length === 0 && e.textContent?.trim() === 'null'),
  );
  if (nullBox) fail('sayfada "null" yazan öğe var');
  console.log('yedek: akış yokken "60 sn" metni');
  await q.close();
  console.log('TAMAM');
} finally {
  await browser.close();
}
