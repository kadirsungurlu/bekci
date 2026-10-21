// Özel alan adlı durum sayfasında abonelik, GERÇEK alan adıyla (TLS'li ters
// vekil arkasında): sayfa alanın kökünde açılır, canlı akış (SSE) alanda bağlanır,
// onay e-postasındaki bağlantı alana gider ve açılır, e-postadaki çıkış bağlantısı
// (tarayıcıda) ve List-Unsubscribe tek tık (RFC 8058) alanda çalışır.
// Sahte SMTP bu süreçte 0.0.0.0:SMTP_PORT'ta açılır; panel SMTP_HOST adıyla erişir.
//   ADMIN=http://panel:8080 DOMAIN=durum.ornek.com SMTP_HOST=<bu konteyner> node ozelalan.mjs
import { chromium } from 'playwright';
import { startSink } from './smtp-sink.mjs';

const ADMIN = process.env.ADMIN || 'http://127.0.0.1:8080';
const DOMAIN = process.env.DOMAIN;
const SMTP_HOST = process.env.SMTP_HOST || '127.0.0.1';
const SMTP_PORT = Number(process.env.SMTP_PORT || 2525);
const MAIL_WAIT = Number(process.env.MAIL_WAIT_MS || 150000);
const USER = 'e2e';
const PASS = 'e2e-parola-12345';
const H = { 'Content-Type': 'application/json', 'X-Uptime': '1' };
const SLUG = 'e2e-ozel';
const A = 'tarayici@e2e.test';
const B = 'tektik@e2e.test';

function fail(msg) {
  console.error('HATA:', msg);
  process.exit(1);
}
if (!DOMAIN) fail('DOMAIN gerekli');
const ORIGIN = 'https://' + DOMAIN;
const link = (m, kind) => {
  const r = new RegExp(`https?://[^\\s"'<>]*abonelik=${kind}&(?:amp;)?t=[A-Za-z0-9._%-]+`);
  const x = m.body.match(r);
  return x ? x[0].replace('&amp;', '&') : null;
};
const onDomain = (u, what) => {
  if (!u || !u.startsWith(ORIGIN + '/')) fail(`${what} özel alana gitmiyor: ${u}`);
};

const sink = await startSink(SMTP_PORT, '0.0.0.0');
const browser = await chromium.launch();
try {
  const ctx = await browser.newContext();
  const rq = ctx.request;
  await rq.post(ADMIN + '/api/auth/setup', { data: { username: USER, password: PASS }, headers: H });
  const login = await rq.post(ADMIN + '/api/auth/login', { data: { username: USER, password: PASS }, headers: H });
  if (!login.ok()) fail('giriş: ' + login.status());

  const mr = await rq.post(ADMIN + '/api/monitors', { data: { name: 'Ödeme API', type: 'push', interval: 60, config: {} }, headers: H });
  if (!mr.ok()) fail('monitör: ' + (await mr.text()));
  const mon = await mr.json();
  const push = (st) => rq.get(ADMIN + '/api/push/' + mon.push_token + '?status=' + st + '&msg=e2e');
  await push('up');

  const ch = await rq.post(ADMIN + '/api/notifications', {
    data: { name: 'Sistem', type: 'email', active: true, config: { host: SMTP_HOST, port: SMTP_PORT, security: 'none', from: 'bekci@e2e.test', to: 'ops@e2e.test' } },
    headers: H,
  });
  if (!ch.ok()) fail('kanal: ' + (await ch.text()));
  const st = await rq.put(ADMIN + '/api/settings', { data: { system_mail_channel_id: (await ch.json()).id }, headers: H });
  if (!st.ok()) fail('ayar: ' + (await st.text()));
  const cr = await rq.post(ADMIN + '/api/status-pages', {
    data: {
      slug: SLUG, title: 'E2E Özel Alan', published: true, lang: 'tr', subscriptions: true, custom_domain: DOMAIN,
      sections: [{ title: 'Hizmetler', monitors: [{ id: mon.id }] }], bar_range: 'recent',
    },
    headers: H,
  });
  if (!cr.ok()) fail('sayfa: ' + (await cr.text()));
  const pageId = (await cr.json()).id;
  const subs = async () => (await rq.get(ADMIN + `/api/status-pages/${pageId}/subscribers`, { headers: H })).json();

  // Alanın kökü sayfayı açar; canlı akış alanda bağlanır.
  const p = await browser.newPage({ viewport: { width: 1280, height: 800 } });
  let sse = null;
  p.on('response', (r) => {
    if (r.url().includes(`/api/public/pages/${SLUG}/events`)) sse = r;
  });
  await p.goto(ORIGIN + '/');
  await p.getByTestId('sub-button').waitFor({ timeout: 15000 });
  for (let i = 0; i < 40 && !(await p.locator('.refresh.on').count()); i++) await p.waitForTimeout(250);
  if (!(await p.locator('.refresh.on').count())) fail('canlı akış bağlanmadı');
  if (!sse || sse.status() !== 200 || !sse.url().startsWith(ORIGIN + '/')) fail('SSE yanıtı: ' + (sse && sse.status() + ' ' + sse.url()));
  console.log('canlı akış:', sse.url());

  // Tarayıcıdan abone ol (A), aynı alandan API ile ikinci abone (B).
  await p.getByTestId('sub-button').click();
  await p.getByTestId('sub-email').fill(A);
  await p.getByTestId('sub-submit').click();
  await p.locator('[data-testid="sub-result"][data-state="sent"]').waitFor();
  const rb = await rq.post(ORIGIN + `/api/public/pages/${SLUG}/subscribe`, { data: { email: B, lang: 'tr' }, headers: H });
  if (rb.status() !== 202) fail('B abone olma ' + rb.status());

  for (const addr of [A, B]) {
    const conf = await sink.waitFor((m) => m.to.includes(addr) && link(m, 'onay'), 20000);
    if (!conf) fail(addr + ': onay e-postası gelmedi');
    const u = link(conf, 'onay');
    onDomain(u, 'onay bağlantısı');
    const pc = await browser.newPage({ viewport: { width: 375, height: 800 } });
    await pc.goto(u);
    await pc.locator('[data-testid="sub-notice"][data-state="ok"]').waitFor({ timeout: 15000 });
    await pc.close();
  }
  if ((await subs()).count !== 2) fail('onaylı abone sayısı 2 değil');
  console.log('onay bağlantıları özel alanda açıldı');

  // Olay e-postası: çıkış bağlantıları özel alanda.
  const before = sink.messages.length;
  await push('down');
  // Açık sayfa (p) yenilenmeden kesintiyi gösterir: sinyal vekil üzerinden akıyor.
  for (let i = 0; i < 60 && !(await p.locator('.hero.st-down').count()); i++) await p.waitForTimeout(250);
  if (!(await p.locator('.hero.st-down').count())) fail('canlı güncelleme özel alanda gelmedi');
  console.log('canlı güncelleme: sayfa yenilenmeden kesinti gösterildi');
  const got = {};
  for (const addr of [A, B]) {
    got[addr] = await sink.waitFor((m) => sink.messages.indexOf(m) >= before && m.to.includes(addr), MAIL_WAIT);
    if (!got[addr]) fail(addr + ': olay e-postası gelmedi');
  }
  const lu = (got[B].headers['list-unsubscribe'] || '').match(/<(https?:[^>]+)>/)?.[1];
  onDomain(lu, 'List-Unsubscribe');
  if (!/One-Click/i.test(got[B].headers['list-unsubscribe-post'] || '')) fail('List-Unsubscribe-Post yok');
  const cik = link(got[A], 'cik');
  onDomain(cik, 'çıkış bağlantısı');

  // A: e-postadaki çıkış bağlantısı tarayıcıda; B: tek tık (X-Uptime'sız form POST).
  const pc = await browser.newPage({ viewport: { width: 375, height: 800 } });
  await pc.goto(cik);
  await pc.locator('[data-testid="sub-notice"][data-state="ok"]').waitFor({ timeout: 15000 });
  await pc.close();
  const one = await browser.newContext();
  const un = await one.request.post(lu, { form: { 'List-Unsubscribe': 'One-Click' } });
  if (!un.ok()) fail('tek tıkla çıkış ' + un.status());
  await one.close();
  const after = await subs();
  if (after.count !== 0 || after.pending !== 0) fail('çıkıştan sonra abone kaldı ' + JSON.stringify(after));
  console.log('çıkış: tarayıcı bağlantısı ve List-Unsubscribe özel alanda çalıştı');
  await push('up');
  console.log('TAMAM');
} finally {
  await browser.close();
  sink.close();
}
