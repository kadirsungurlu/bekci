// Mesh bağlantı matrisi uçtan uca: iki kontrol noktası kalp atışı gönderir
// (A -> B ulaşıyor, B -> A ulaşamıyor); Ayarlar > Kontrol noktaları'nda matris
// Main dahil 3 düğümle görünür, tek yönlü yorum yazar; B düzelince canlı akışla
// "sağlıklı"ya döner. Çalışan bir panele karşı:
//   BASE=http://127.0.0.1:8080 node mesh.mjs
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
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 900 } });
  const rq = ctx.request;
  await rq.post(BASE + '/api/auth/setup', { data: { username: USER, password: PASS }, headers: H });
  const login = await rq.post(BASE + '/api/auth/login', { data: { username: USER, password: PASS }, headers: H });
  if (!login.ok()) fail('giriş: ' + login.status());

  const mk = async (name) => {
    const r = await rq.post(BASE + '/api/probes', { data: { name }, headers: H });
    if (!r.ok()) fail('kontrol noktası: ' + (await r.text()));
    return r.json();
  };
  const a = await mk('E2E Mesh A ' + Date.now());
  const b = await mk('E2E Mesh B ' + Date.now());
  const beat = async (who, peer, state) => {
    const r = await rq.post(BASE + '/api/probe/mesh', {
      data: { listen_port: 7480, links: [{ peer: peer.probe.id, state }] },
      headers: { 'Content-Type': 'application/json', Authorization: 'Bearer ' + who.token },
    });
    if (!r.ok()) fail('kalp atışı: ' + r.status() + ' ' + (await r.text()));
  };
  await beat(a, b, 'up');
  await beat(b, a, 'down');

  const p = await ctx.newPage();
  await p.goto(BASE + '/#/settings/probes');
  const m = p.getByTestId('mesh-matrix');
  await m.waitFor();
  await m.locator('table').waitFor({ timeout: 10_000 }).catch(async () => fail('matris tablosu yok: ' + (await m.innerText())));
  const rows = await m.locator('tbody tr').count();
  if (rows !== 3) fail('3 satır bekleniyordu: ' + rows);
  if ((await m.locator('td .cell.down').count()) !== 1) fail('tek kopuk hücre bekleniyordu');
  if ((await m.locator('.findings li').count()) !== 1) fail('tek yorum bekleniyordu: ' + (await m.innerText()));
  console.log('mesh: matris ve tek yönlü yorum tamam');

  if (SHOTS) {
    mkdirSync(SHOTS, { recursive: true });
    for (const scheme of ['dark', 'light']) {
      for (const w of [1920, 390]) {
        const c = await browser.newContext({ viewport: { width: w, height: w < 500 ? 800 : 1000 }, colorScheme: scheme });
        await c.addCookies(await ctx.cookies());
        const q = await c.newPage();
        await q.goto(BASE + '/#/settings/probes');
        await q.getByTestId('mesh-matrix').locator('table').waitFor();
        await q.getByTestId('mesh-matrix').scrollIntoViewIfNeeded();
        await q.waitForTimeout(400);
        await q.screenshot({ path: `${SHOTS}/mesh-${scheme}-${w}.jpg`, type: 'jpeg', quality: 60 });
        await c.close();
      }
    }
  }

  // B düzelir; iki nokta da kalp atışını sürdürür (durmuş nokta bayatlar).
  // Canlı "mesh" yayını (en sık panel aralığında bir) matrisi tazeler.
  let healed = false;
  for (let i = 0; i < 15 && !healed; i++) {
    await beat(a, b, 'up');
    await beat(b, a, 'up');
    await p.waitForTimeout(2000);
    healed = (await m.locator('.findings').count()) === 0 && (await m.locator('table').count()) === 1;
  }
  if (!healed) fail('düzelme canlı yansımadı: ' + (await m.innerText()));
  if ((await m.locator('td .cell.down').count()) !== 0) fail('kopuk hücre kalmamalı');
  console.log('TAMAM');
} finally {
  await browser.close();
}
