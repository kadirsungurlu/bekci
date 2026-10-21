// Durum sayfası e-posta aboneliği uçtan uca: kapalıyken 404, panelden abone
// olma (375 ve 1280 px), onay bağlantısı, olay başlayınca ve çözülünce e-posta,
// List-Unsubscribe ile tek tıkla çıkış. Sahte SMTP (smtp-sink.mjs) bu süreçte açılır;
// panel 127.0.0.1:SMTP_PORT'a erişebilmeli. Çalışan bir panele karşı:
//   BASE=http://127.0.0.1:8080 node abonelik.mjs
// SHOTS=<klasör> verilirse görünüm alanı görüntüleri kaydedilir.
import { chromium } from 'playwright';
import { mkdirSync } from 'node:fs';
import { startSink } from './smtp-sink.mjs';

const BASE = process.env.BASE || 'http://127.0.0.1:8080';
const SHOTS = process.env.SHOTS || '';
const SMTP_PORT = Number(process.env.SMTP_PORT || 2525);
// Olay e-postaları birleştirme penceresinden sonra gider (varsayılan 60 sn).
const MAIL_WAIT = Number(process.env.MAIL_WAIT_MS || 150000);
const USER = 'e2e';
const PASS = 'e2e-parola-12345';
const H = { 'Content-Type': 'application/json', 'X-Uptime': '1' };
const SLUG = 'e2e-abone';
const EMAIL = 'okur@e2e.test';

function fail(msg) {
  console.error('HATA:', msg);
  process.exit(1);
}
const link = (m, kind) => {
  const r = new RegExp(`https?://[^\\s"'<>]*abonelik=${kind}&(?:amp;)?t=[A-Za-z0-9._%-]+`);
  const x = m.body.match(r);
  return x ? x[0].replace('&amp;', '&') : null;
};

const sink = await startSink(SMTP_PORT);
const browser = await chromium.launch();
try {
  const ctx = await browser.newContext();
  const rq = ctx.request;
  await rq.post(BASE + '/api/auth/setup', { data: { username: USER, password: PASS }, headers: H });
  const login = await rq.post(BASE + '/api/auth/login', { data: { username: USER, password: PASS }, headers: H });
  if (!login.ok()) fail('giriş: ' + login.status());

  const mr = await rq.post(BASE + '/api/monitors', { data: { name: 'Ödeme API', type: 'push', interval: 60, config: {} }, headers: H });
  if (!mr.ok()) fail('monitör: ' + (await mr.text()));
  const mon = await mr.json();
  const push = (st) => rq.get(BASE + '/api/push/' + mon.push_token + '?status=' + st + '&msg=e2e');
  await push('up');

  const page = (subscriptions) => ({
    slug: SLUG, title: 'E2E Abonelik', published: true, lang: 'tr', subscriptions,
    sections: [{ title: 'Hizmetler', monitors: [{ id: mon.id }] }], bar_range: 'recent',
  });
  const cr = await rq.post(BASE + '/api/status-pages', { data: page(false), headers: H });
  if (!cr.ok()) fail('sayfa: ' + (await cr.text()));
  const pageId = (await cr.json()).id;

  // Kapalıyken: uç 404, düğme yok; sistem e-postası yokken açılamaz.
  const off = await rq.post(BASE + `/api/public/pages/${SLUG}/subscribe`, { data: { email: EMAIL, lang: 'tr' }, headers: H });
  if (off.status() !== 404) fail('kapalıyken subscribe ' + off.status());
  const noMail = await rq.put(BASE + `/api/status-pages/${pageId}`, { data: page(true), headers: H });
  if (noMail.status() !== 400) fail('sistem e-postası yokken açma ' + noMail.status());

  const ch = await rq.post(BASE + '/api/notifications', {
    data: { name: 'Sistem', type: 'email', active: true, config: { host: '127.0.0.1', port: SMTP_PORT, security: 'none', from: 'bekci@e2e.test', to: 'ops@e2e.test' } },
    headers: H,
  });
  if (!ch.ok()) fail('kanal: ' + (await ch.text()));
  const chId = (await ch.json()).id;
  const st = await rq.put(BASE + '/api/settings', { data: { system_mail_channel_id: chId }, headers: H });
  if (!st.ok()) fail('ayar: ' + (await st.text()));
  const on = await rq.put(BASE + `/api/status-pages/${pageId}`, { data: page(true), headers: H });
  if (!on.ok()) fail('açma: ' + (await on.text()));

  if (SHOTS) mkdirSync(SHOTS, { recursive: true });
  const url = BASE + '/durum/' + SLUG;
  for (const w of [375, 1280]) {
    const p = await browser.newPage({ viewport: { width: w, height: 800 }, colorScheme: w === 1280 ? 'dark' : 'light' });
    await p.goto(url);
    await p.getByTestId('sub-button').click();
    await p.getByTestId('sub-panel').waitFor();
    const box = await p.getByTestId('sub-panel').boundingBox();
    if (!box || box.x < 0 || box.x + box.width > w + 1) fail(`${w}px: panel taşıyor ${JSON.stringify(box)}`);
    const sw = await p.evaluate(() => document.documentElement.scrollWidth);
    if (sw > w) fail(`${w}px: yatay taşma ${sw}`);
    if (SHOTS) await p.screenshot({ path: `${SHOTS}/abone-panel-${w}.jpg`, type: 'jpeg', quality: 60 });
    if (w === 1280) {
      await p.getByTestId('sub-email').fill(EMAIL);
      await p.getByTestId('sub-submit').click();
      await p.locator('[data-testid="sub-result"][data-state="sent"]').waitFor();
      if (SHOTS) await p.screenshot({ path: `${SHOTS}/abone-gonderildi-${w}.jpg`, type: 'jpeg', quality: 60 });
    }
    await p.close();
  }

  // Onay e-postası ve bağlantısı.
  const conf = await sink.waitFor((m) => m.to.includes(EMAIL) && link(m, 'onay'), 20000);
  if (!conf) fail('onay e-postası gelmedi');
  if (!/List-Unsubscribe|abonelik=cik/i.test(JSON.stringify(conf.headers) + conf.body)) console.log('not: onay e-postasında çıkış bağlantısı yok');
  // Aynı adrese hemen ikinci istek: yeni onay postası gitmemeli.
  await rq.post(BASE + `/api/public/pages/${SLUG}/subscribe`, { data: { email: EMAIL, lang: 'tr' }, headers: H });
  const p = await browser.newPage({ viewport: { width: 375, height: 800 } });
  await p.goto(link(conf, 'onay').replace(/^https?:\/\/[^/]+/, BASE));
  await p.locator('[data-testid="sub-notice"][data-state="ok"]').waitFor({ timeout: 10000 });
  if (SHOTS) await p.screenshot({ path: `${SHOTS}/abone-onaylandi-375.jpg`, type: 'jpeg', quality: 60 });
  await p.close();
  if (sink.messages.filter((m) => m.to.includes(EMAIL) && link(m, 'onay')).length !== 1) fail('aynı adrese ikinci onay postası gitti');

  const subs = await (await rq.get(BASE + `/api/status-pages/${pageId}/subscribers`, { headers: H })).json();
  if (subs.count !== 1) fail('onaylı abone sayısı ' + JSON.stringify(subs));
  const csv = await rq.get(BASE + `/api/status-pages/${pageId}/subscribers.csv`, { headers: H });
  if (!(await csv.text()).includes(EMAIL)) fail('CSV abone içermiyor');

  // Olay başlar -> e-posta; çözülür -> e-posta.
  const before = sink.messages.length;
  await push('down');
  const down = await sink.waitFor((m, i) => sink.messages.indexOf(m) >= before && m.to.includes(EMAIL), MAIL_WAIT);
  if (!down) fail('olay başlangıç e-postası gelmedi');
  console.log('olay e-postası:', down.subject);
  if (!down.headers['list-unsubscribe'] || !/One-Click/i.test(down.headers['list-unsubscribe-post'] || '')) fail('List-Unsubscribe başlıkları yok');
  const mid = sink.messages.length;
  await push('up');
  const up = await sink.waitFor((m) => sink.messages.indexOf(m) >= mid && m.to.includes(EMAIL), MAIL_WAIT);
  if (!up) fail('çözüldü e-postası gelmedi');
  console.log('çözüldü e-postası:', up.subject);

  // Tek tıkla çıkış (RFC 8058): X-Uptime'sız form POST.
  const lu = up.headers['list-unsubscribe'].match(/<(https?:[^>]+)>/)[1].replace(/^https?:\/\/[^/]+/, BASE);
  const un = await rq.post(lu, { form: { 'List-Unsubscribe': 'One-Click' } });
  if (!un.ok()) fail('tek tıkla çıkış ' + un.status());
  const after = await (await rq.get(BASE + `/api/status-pages/${pageId}/subscribers`, { headers: H })).json();
  if (after.count !== 0 || (after.items || []).length !== 0) fail('çıkıştan sonra abone kaldı');

  // Panel: abonelik bölümü görünür.
  const ed = await ctx.newPage();
  await ed.setViewportSize({ width: 1280, height: 900 });
  await ed.goto(BASE + `/#/status-pages/${pageId}`);
  await ed.getByTestId('subs-toggle').waitFor({ timeout: 10000 });
  await ed.getByTestId('subs-section').scrollIntoViewIfNeeded();
  if (SHOTS) await ed.screenshot({ path: `${SHOTS}/abone-editor-1280.jpg`, type: 'jpeg', quality: 60 });
  console.log('TAMAM');
} finally {
  await browser.close();
  sink.close();
}
