// Özel alan adının otomatik HTTPS'i, GERÇEK alan adlarıyla (Caddy on-demand TLS,
// deploy/compose/Caddyfile): panelden sayfaya alan adı yazılır, Caddy'ye dokunmadan
// durum rozeti "Etkin" olur ve alan HTTPS ile açılır; tanımsız bir ad için
// sertifika alınmaz (TLS el sıkışması başarısız, ask ucu 404).
//   ADMIN=https://panel.ornek.com DOMAIN=durum.ornek.com UNDEFINED=yok.ornek.com node alanadi.mjs
import { chromium } from 'playwright';
import { connect } from 'node:tls';
import { lookup } from 'node:dns/promises';

const ADMIN = process.env.ADMIN;
const DOMAIN = process.env.DOMAIN;
const UNDEFINED = process.env.UNDEFINED;
const SHOTS = process.env.SHOTS || '';
const USER = 'e2e';
const PASS = 'e2e-parola-12345';
const H = { 'Content-Type': 'application/json', 'X-Uptime': '1' };

function fail(msg) {
  console.error('HATA:', msg);
  process.exit(1);
}
if (!ADMIN || !DOMAIN || !UNDEFINED) fail('ADMIN, DOMAIN ve UNDEFINED gerekli');

// SNI ile TLS el sıkışması: başarılıysa sertifikanın konu adı.
function handshake(ip, name) {
  return new Promise((resolve) => {
    const s = connect({ host: ip, port: 443, servername: name, rejectUnauthorized: false, timeout: 15000 }, () => {
      const cn = s.getPeerCertificate()?.subject?.CN || '';
      s.end();
      resolve({ ok: true, cn });
    });
    s.on('error', (e) => resolve({ ok: false, err: e.code || e.message }));
    s.on('timeout', () => (s.destroy(), resolve({ ok: false, err: 'timeout' })));
  });
}

const browser = await chromium.launch();
try {
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 900 } });
  const rq = ctx.request;
  await rq.post(ADMIN + '/api/auth/setup', { data: { username: USER, password: PASS }, headers: H });
  const login = await rq.post(ADMIN + '/api/auth/login', { data: { username: USER, password: PASS }, headers: H });
  if (!login.ok()) fail('giriş: ' + login.status());
  const mr = await rq.post(ADMIN + '/api/monitors', { data: { name: 'Web', type: 'push', interval: 60, config: {} }, headers: H });
  const mon = await mr.json();
  await rq.get(ADMIN + '/api/push/' + mon.push_token + '?status=up&msg=ok');
  const cr = await rq.post(ADMIN + '/api/status-pages', {
    data: { slug: 'e2e-alan', title: 'E2E Alan Adı', published: true, lang: 'tr', sections: [{ title: 'Hizmetler', monitors: [{ id: mon.id }] }] },
    headers: H,
  });
  if (!cr.ok()) fail('sayfa: ' + (await cr.text()));
  const pageId = (await cr.json()).id;

  // Tanımsız ad: ask 404, sertifika yok.
  const askU = await rq.get(ADMIN + '/api/tls/ask?domain=' + UNDEFINED);
  if (askU.status() !== 404) fail('tanımsız ad ask ' + askU.status());
  const ip = (await lookup(DOMAIN)).address;
  const hu = await handshake(ip, UNDEFINED);
  if (hu.ok) fail('tanımsız ad için TLS el sıkışması başarılı: ' + hu.cn);
  console.log('tanımsız ad: sertifika yok (' + hu.err + ')');

  // Panelden alan adı ekle.
  const p = await ctx.newPage();
  await p.goto(ADMIN + `/#/status-pages/${pageId}`);
  await p.locator('#sp-dom').fill(DOMAIN);
  await p.locator('button[type="submit"]').first().click();
  const box = p.getByTestId('domain-status');
  await box.waitFor({ timeout: 15000 });
  let state = '';
  for (let i = 0; i < 12; i++) {
    await p.locator('[data-testid="domain-check"]:not([disabled])').waitFor({ timeout: 30000 });
    state = await box.getAttribute('data-state');
    if (state === 'active') break;
    await p.waitForTimeout(5000);
    await p.getByTestId('domain-check').click();
  }
  if (state !== 'active') fail('alan adı durumu: ' + state);
  await box.scrollIntoViewIfNeeded();
  if (SHOTS) await p.screenshot({ path: `${SHOTS}/alanadi-etkin-1280.jpg`, type: 'jpeg', quality: 60 });
  console.log('panel rozeti: Etkin');

  // Alan HTTPS ile (geçerli sertifika) açılır.
  const pub = await browser.newPage({ viewport: { width: 375, height: 800 } });
  const resp = await pub.goto('https://' + DOMAIN + '/');
  if (!resp.ok()) fail('alan adı ' + resp.status());
  await pub.getByRole('heading', { name: 'E2E Alan Adı' }).waitFor({ timeout: 15000 });
  const hd = await handshake(ip, DOMAIN);
  if (!hd.ok || hd.cn !== DOMAIN) fail('alan sertifikası: ' + JSON.stringify(hd));
  console.log('alan adı HTTPS ile açıldı, sertifika CN=' + hd.cn);

  // Tanımsız ad hâlâ sertifikasız.
  if ((await handshake(ip, UNDEFINED)).ok) fail('tanımsız ad sonradan sertifika aldı');
  console.log('TAMAM');
} finally {
  await browser.close();
}
