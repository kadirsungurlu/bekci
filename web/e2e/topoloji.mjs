// Canlı topoloji uçtan uca: boş panelde boş durum (veri varsa atlanır), gerçek
// veriyle sütunlar (monitör, kontrol noktası + panel RTT, kanal), açık olay
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
  await post('/api/notifications', { name: 'E2E Topo Kanca ' + tag, type: 'webhook', config: { url: 'http://127.0.0.1:9/kanca' } });

  await p.goto(BASE + '/#/topology');
  const board = p.getByTestId('topology-board');
  await board.waitFor({ timeout: 10_000 }).catch(() => fail('topoloji panosu yok'));
  const mon = p.getByTestId('col-monitors');
  await mon.getByText(down.name).waitFor({ timeout: 5000 }).catch(() => fail('monitör sütununda yok'));
  const pc = p.getByTestId('col-probes').locator('.node', { hasText: probe.probe.name });
  await pc.waitFor({ timeout: 5000 }).catch(() => fail('kontrol noktası sütununda yok'));
  if (!(await pc.innerText()).includes('17 ms')) fail('panel RTT yok: ' + (await pc.innerText()));
  await p.getByTestId('col-channels').getByText('E2E Topo Kanca ' + tag).waitFor({ timeout: 5000 }).catch(() => fail('kanal yok'));
  console.log('tamam: sütunlar gerçek veriyle');

  // Kapalı porta TCP: olay açılır, karar kartı canlı gelir (yenilemeden).
  const dec = p.getByTestId('topology-decision');
  await dec.getByText(down.name).waitFor({ timeout: 30_000 }).catch(() => fail('açık olayda karar kartı yok'));
  console.log('tamam: panelin kararı (açık olay)');

  // Canlı ekleme: sayfa yenilenmeden yeni monitör görünür.
  const live = await post('/api/monitors', { name: 'E2E Topo Canlı ' + tag, type: 'push', interval: 60 });
  await mon.getByText(live.name).waitFor({ timeout: 20_000 }).catch(() => fail('canlı güncelleme gelmedi'));
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
  await post('/api/monitors', { name: 'E2E Topo Gizli ' + tag, type: 'push', interval: 60 });
  await p.waitForTimeout(7000);
  if (calls) fail('gizli sekmede ' + calls + ' istek');
  await p.evaluate(() => {
    Object.defineProperty(document, 'hidden', { configurable: true, get: () => false });
    document.dispatchEvent(new Event('visibilitychange'));
  });
  await mon.getByText('E2E Topo Gizli ' + tag).waitFor({ timeout: 5000 }).catch(() => fail('görünür olunca tazelenmedi'));
  console.log('tamam: gizli sekmede duruyor, dönünce tazeleniyor');

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
