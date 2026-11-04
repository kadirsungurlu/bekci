// Canlı topoloji uçtan uca: boş panelde boş durum (veri varsa atlanır), gerçek
// veriyle tuval (kartlar, çakışmasızlık, bağlantı ipucu klavye/Escape), 390 px liste, açık olay
// varken "Panelin kararı" kartı, sayfa yenilenmeden canlı ekleme (SSE) ve
// gizli sekmede istek yapılmaması. Çalışan bir panele karşı:
//   BASE=http://127.0.0.1:8080 node topoloji.mjs
// SHOTS=<klasör> verilirse koyu/açık 1920 ve 390 görüntüleri kaydedilir.
import { chromium } from 'playwright';
import { mkdirSync } from 'node:fs';

const BASE = process.env.BASE || 'http://127.0.0.1:8080';
const SHOTS = process.env.SHOTS || '';
const USER = 'e2e';
const PASS = 'e2e-parola-12345';
const H = { 'Content-Type': 'application/json', 'X-Uptime': '1' };

function fail(msg) {
  console.error('HATA:', msg);
  process.exit(1);
}

const browser = await chromium.launch();
try {
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
  const rq = ctx.request;
  await rq.post(BASE + '/api/auth/setup', { data: { username: USER, password: PASS }, headers: H });
  const login = await rq.post(BASE + '/api/auth/login', { data: { username: USER, password: PASS }, headers: H });
  if (!login.ok()) fail('giriş: ' + login.status());
  const post = async (path, data) => {
    const r = await rq.post(BASE + path, { data, headers: H });
    if (!r.ok()) fail(path + ': ' + (await r.text()));
    return r.json();
  };
  const p = await ctx.newPage();
  const first = await (await rq.get(BASE + '/api/topology')).json();

  // Boş durum (yalnız tamamen boş panelde; CI'da önceki betikler veri bırakır).
  if (!first.monitors.length && !first.probes.length && !first.servers.length) {
    await p.goto(BASE + '/#/topology');
    await p.getByTestId('topology-empty').waitFor({ timeout: 10_000 }).catch(() => fail('boş durum yok'));
    if (await p.getByTestId('topology-decision').count()) fail('boş panelde karar kartı');
    console.log('tamam: boş durum');
  }

  const tag = Date.now();
  const down = await post('/api/monitors', { name: 'E2E Topo Kapalı ' + tag, type: 'tcp', interval: 5, config: { host: '127.0.0.1', port: 1 } });
  const probe = await post('/api/probes', { name: 'E2E Topo Nokta ' + tag });
  const hb = await rq.post(BASE + '/api/probe/mesh', {
    data: { main_rtt_ms: 17, links: [] },
    headers: { 'Content-Type': 'application/json', Authorization: 'Bearer ' + probe.token },
  });
  if (!hb.ok()) fail('kalp atışı: ' + hb.status());
  const chan = await post('/api/notifications', { name: 'E2E Topo Kanca ' + tag, type: 'webhook', config: { url: 'http://127.0.0.1:9/kanca' } });

  await p.goto(BASE + '/#/topology');
  const board = p.getByTestId('topology-board');
  await board.waitFor({ timeout: 10_000 }).catch(() => fail('topoloji panosu yok'));
  await p.getByTestId('topology-canvas').waitFor({ timeout: 5000 }).catch(() => fail('tuval yok'));
  // Durum yalnız renkle verilmez: kartta işaret + metin, sayaçta etiketli rozet.
  await p.locator('[data-testid^="node-"] [role="img"][aria-label]').first().waitFor({ timeout: 5000 }).catch(() => fail('kartta durum işareti yok'));
  if (!(await p.locator('[data-count][title]').count())) fail('sayaç rozetinde etiket yok');
  // Gizli kart varsa grubu aç (çok monitörde "N tane daha").
  const node = async (group, id) => {
    const loc = p.getByTestId(`node-${group}-${id}`);
    for (let i = 0; i < 2 && !(await loc.count()); i++) {
      const more = p.getByTestId(`more-${group}`);
      if ((await more.count()) && (await more.getAttribute('aria-expanded')) === 'false') await more.click();
      else await p.waitForTimeout(1000);
    }
    return loc;
  };
  const monNode = await node('monitors', down.id);
  await monNode.waitFor({ timeout: 5000 }).catch(() => fail('monitör kartı yok'));
  if (!(await monNode.getAttribute('href'))?.endsWith('/monitors/' + down.id)) fail('monitör kartı ayrıntıya gitmiyor');
  const pc = await node('probes', probe.probe.id);
  await pc.waitFor({ timeout: 5000 }).catch(() => fail('kontrol noktası kartı yok'));
  if (!(await pc.innerText()).includes('17 ms')) fail('panel RTT yok: ' + (await pc.innerText()));
  await (await node('channels', chan.id)).waitFor({ timeout: 5000 }).catch(() => fail('kanal yok'));
  // Kartlar üst üste binmez.
  const boxes = await p.locator('[data-testid^="node-"], [data-testid="topology-hub"]').evaluateAll((els) =>
    els.map((e) => { const r = e.getBoundingClientRect(); return [r.x, r.y, r.width, r.height]; }));
  for (let i = 0; i < boxes.length; i++)
    for (let j = i + 1; j < boxes.length; j++) {
      const [a, b] = [boxes[i], boxes[j]];
      if (a[0] < b[0] + b[2] - 0.5 && b[0] < a[0] + a[2] - 0.5 && a[1] < b[1] + b[3] - 0.5 && b[1] < a[1] + a[3] - 0.5) fail('kartlar çakışıyor');
    }
  // Bağlantı ipucu: klavyeyle odak, Enter sabitler, Escape kapatır, kapatma düğmesi.
  const edge = p.locator('path[data-edge^="lnk:"]').first();
  await edge.focus();
  const tip = p.getByTestId('topology-tip');
  await tip.waitFor({ timeout: 3000 }).catch(() => fail('odakta ipucu yok'));
  if (!/Panel|Bekci/.test(await tip.innerText())) fail('ipucu içeriği: ' + (await tip.innerText()));
  await p.keyboard.press('Enter');
  await p.keyboard.press('Escape');
  if (await tip.count()) fail('Escape ipucunu kapatmadı');
  await edge.dispatchEvent('click');
  await tip.waitFor({ timeout: 3000 }).catch(() => fail('tıklamada ipucu yok'));
  await tip.getByRole('button').click();
  if (await tip.count()) fail('kapatma düğmesi çalışmadı');
  console.log('tamam: tuval gerçek veriyle (kartlar, çakışmasızlık, ipucu)');

  // Kapalı porta TCP: olay açılır, karar kartı canlı gelir (yenilemeden).
  const dec = p.getByTestId('topology-decision');
  await dec.getByText(down.name).waitFor({ timeout: 30_000 }).catch(() => fail('açık olayda karar kartı yok'));
  console.log('tamam: panelin kararı (açık olay)');

  // Canlı ekleme: sayfa yenilenmeden yeni monitör görünür.
  const live = await post('/api/monitors', { name: 'E2E Topo Canlı ' + tag, type: 'push', interval: 60 });
  await (await node('monitors', live.id)).waitFor({ timeout: 20_000 }).catch(() => fail('canlı güncelleme gelmedi'));
  console.log('tamam: canlı güncelleme');

  // Gizli sekme: topoloji isteği yapılmaz.
  let calls = 0;
  p.on('request', (r) => {
    if (r.url().includes('/api/topology')) calls++;
  });
  await p.evaluate(() => {
    Object.defineProperty(document, 'hidden', { configurable: true, get: () => true });
    document.dispatchEvent(new Event('visibilitychange'));
  });
  const hid = await post('/api/monitors', { name: 'E2E Topo Gizli ' + tag, type: 'push', interval: 60 });
  await p.waitForTimeout(7000);
  if (calls) fail('gizli sekmede ' + calls + ' istek');
  await p.evaluate(() => {
    Object.defineProperty(document, 'hidden', { configurable: true, get: () => false });
    document.dispatchEvent(new Event('visibilitychange'));
  });
  await (await node('monitors', hid.id)).waitFor({ timeout: 5000 }).catch(() => fail('görünür olunca tazelenmedi'));
  console.log('tamam: gizli sekmede duruyor, dönünce tazeleniyor');

  // 390 px: liste görünümü, yatay kaydırma yok, satırlar >= 44 px.
  {
    const c = await browser.newContext({ viewport: { width: 390, height: 800 } });
    await c.addCookies(await ctx.cookies());
    const q = await c.newPage();
    await q.goto(BASE + '/#/topology');
    await q.getByTestId('topology-list').waitFor({ timeout: 10_000 }).catch(() => fail('390 px liste görünümü yok'));
    if (await q.getByTestId('topology-canvas').count()) fail('390 px tuval çizildi');
    const sw = await q.evaluate(() => document.documentElement.scrollWidth);
    if (sw > 390) fail('390 px yatay kaydırma: ' + sw);
    const small = await q.locator('[data-testid="topology-list"] a.row').evaluateAll((els) => els.filter((e) => e.getBoundingClientRect().height < 44).length);
    if (small) fail(small + ' satır 44 px altında');
    await c.close();
    console.log('tamam: 390 px liste');
  }

  if (SHOTS) {
    mkdirSync(SHOTS, { recursive: true });
    for (const scheme of ['dark', 'light']) {
      for (const w of [1920, 390]) {
        const c = await browser.newContext({ viewport: { width: w, height: w < 500 ? 800 : 1000 }, colorScheme: scheme });
        await c.addCookies(await ctx.cookies());
        const q = await c.newPage();
        await q.goto(BASE + '/#/topology');
        await q.getByTestId('topology-board').waitFor();
        await q.waitForTimeout(500);
        await q.screenshot({ path: `${SHOTS}/topo-${scheme}-${w}.jpg`, type: 'jpeg', quality: 60 });
        await c.close();
      }
    }
  }
} finally {
  await browser.close();
}
