// Tarayıcı bildirimi (Web Push) uçtan uca: gerçek Chromium + bildirim izni,
// arayüzden abone ol (PushSubscription.toJSON() çıktısı "expirationTime" taşır:
// null ve sayı), kayıt sunucuda, "deneme gönder", ve bir olay uyarısı push olarak
// gider. Push servisi sahte bir yerel HTTP sunucusudur; yük RFC 8291 ile çözülür.
//
// Çalıştırma (panel ayrıca açık olmalı, taze veri klasörüyle):
//   BASE=http://127.0.0.1:8080 FAKE_PORT=9911 node push.mjs
// Not: headless Chromium gerçek FCM'e abone olamaz; PushManager.subscribe yerine,
// standardın verdiği toJSON biçimini (gerçek P-256 anahtarlarıyla) üreten bir
// taklit konur. Arayüz, izin akışı, API ve şifreli gönderim gerçektir.
import { chromium } from 'playwright';
import http from 'node:http';
import crypto from 'node:crypto';

const BASE = process.env.BASE || 'http://127.0.0.1:8080';
const FAKE_PORT = Number(process.env.FAKE_PORT || 9911);
const FAKE = `http://127.0.0.1:${FAKE_PORT}`;
const USER = 'e2e', PASS = 'e2e-parola-12345';
const H = { 'Content-Type': 'application/json', 'X-Uptime': '1' };

const hits = [];
const fake = http.createServer((req, res) => {
  const chunks = [];
  req.on('data', (c) => chunks.push(c));
  req.on('end', () => {
    hits.push({ path: req.url, headers: req.headers, body: Buffer.concat(chunks) });
    res.writeHead(201).end();
  });
});
await new Promise((r) => fake.listen(FAKE_PORT, '0.0.0.0', r));

const fail = (m) => { console.error('E2E HATA: ' + m); process.exitCode = 1; throw new Error(m); };
const ok = (m) => console.log('E2E tamam: ' + m);
const until = async (what, fn, ms = 60000) => {
  const t0 = Date.now();
  for (;;) {
    const v = await fn();
    if (v) return v;
    if (Date.now() - t0 > ms) fail('zaman aşımı: ' + what);
    await new Promise((r) => setTimeout(r, 300));
  }
};
// Projede emoji yok: bildirim başlıkları ve gövdesi düz metin olmalı.
const EMOJI = /[\u{1F300}-\u{1FAFF}\u{2600}-\u{27BF}\u{2B00}-\u{2BFF}\u{23E9}-\u{23FA}\u{FE0F}]/u;
const b64uToBuf = (s) => Buffer.from(s.replace(/-/g, '+').replace(/_/g, '/'), 'base64');

// RFC 8291 (aes128gcm) çözümü; tarayıcı tarafındaki özel anahtar ve auth ile.
function decrypt(body, uaPriv, uaPubRaw, auth) {
  const salt = body.subarray(0, 16);
  const idlen = body[20];
  const asPub = body.subarray(21, 21 + idlen);
  const data = body.subarray(21 + idlen);
  const ecdh = crypto.createECDH('prime256v1');
  ecdh.setPrivateKey(uaPriv);
  const shared = ecdh.computeSecret(asPub);
  const keyInfo = Buffer.concat([Buffer.from('WebPush: info\0'), uaPubRaw, asPub]);
  const ikm = Buffer.from(crypto.hkdfSync('sha256', shared, auth, keyInfo, 32));
  const cek = Buffer.from(crypto.hkdfSync('sha256', ikm, salt, Buffer.from('Content-Encoding: aes128gcm\0'), 16));
  const nonce = Buffer.from(crypto.hkdfSync('sha256', ikm, salt, Buffer.from('Content-Encoding: nonce\0'), 12));
  const d = crypto.createDecipheriv('aes-128-gcm', cek, nonce);
  d.setAuthTag(data.subarray(data.length - 16));
  const plain = Buffer.concat([d.update(data.subarray(0, data.length - 16)), d.final()]);
  let end = plain.length;
  while (end > 0 && plain[end - 1] === 0) end--;
  return JSON.parse(plain.subarray(0, end - 1).toString()); // son bayt 0x02 sınırlayıcı
}

const browser = await chromium.launch();
try {
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 900 } });
  await ctx.grantPermissions(['notifications'], { origin: BASE });
  await ctx.addInitScript((fakeUrl) => {
    localStorage.setItem('uptime.lang', 'en');
    const b64u = (buf) => btoa(String.fromCharCode(...new Uint8Array(buf))).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
    // Headless Chromium'da grantPermissions sonrası bile Notification.permission 'denied'
    // dönebiliyor; izin verilmiş gibi davranması için sabitlenir (izin akışı arayüzde yine çalışır).
    if (Notification.permission !== 'granted') {
      Object.defineProperty(Notification, 'permission', { get: () => 'granted', configurable: true });
      Notification.requestPermission = async () => 'granted';
    }
    window.__exp = null; // sonraki abonelikte expirationTime (null ya da sayı)
    window.__keys = null;
    let sub = null;
    const ready = (async () => {
      const kp = await crypto.subtle.generateKey({ name: 'ECDH', namedCurve: 'P-256' }, true, ['deriveBits']);
      const pub = new Uint8Array(await crypto.subtle.exportKey('raw', kp.publicKey));
      const jwk = await crypto.subtle.exportKey('jwk', kp.privateKey);
      const auth = crypto.getRandomValues(new Uint8Array(16));
      window.__keys = { pub: b64u(pub), priv: jwk.d, auth: b64u(auth) };
    })();
    PushManager.prototype.getSubscription = async () => sub;
    PushManager.prototype.subscribe = async () => {
      await ready;
      const k = window.__keys;
      const exp = window.__exp;
      sub = {
        endpoint: fakeUrl + '/push/e2e',
        expirationTime: exp,
        options: { userVisibleOnly: true },
        unsubscribe: async () => { sub = null; return true; },
        // Tarayıcının gerçek toJSON biçimi: expirationTime her zaman vardır.
        toJSON: () => ({ endpoint: fakeUrl + '/push/e2e', expirationTime: exp, keys: { p256dh: k.pub, auth: k.auth } }),
      };
      return sub;
    };
  }, FAKE);

  // İlk yönetici + giriş.
  await ctx.request.post(BASE + '/api/auth/setup', { data: { username: USER, password: PASS }, headers: H });
  const login = await ctx.request.post(BASE + '/api/auth/login', { data: { username: USER, password: PASS }, headers: H });
  if (!login.ok()) fail('giriş ' + login.status());

  const page = await ctx.newPage();
  const errors = [];
  page.on('pageerror', (e) => errors.push(e.message));
  await page.goto(BASE + '/#/settings');
  const turnOn = page.getByRole('button', { name: 'Turn on for this device' });
  const turnOff = page.getByRole('button', { name: 'Turn off for this device' });

  const subscribeOnce = async (label) => {
    await turnOn.waitFor({ timeout: 30000 });
    await turnOn.click();
    await turnOff.waitFor({ timeout: 15000 }).catch(() => fail(`${label}: abonelik kurulamadı (${page.url()})`));
    const errAlert = await page.locator('.alert.error').allTextContents();
    if (errAlert.length) fail(`${label}: arayüzde hata: ${errAlert.join(' | ')}`);
    const list = await (await ctx.request.get(BASE + '/api/webpush/subscriptions')).json();
    if (!Array.isArray(list) || list.length !== 1 || list[0].endpoint !== FAKE + '/push/e2e') fail(`${label}: kayıt sunucuda yok: ${JSON.stringify(list)}`);
    ok(`${label}: abone olundu, sunucuda kayıtlı`);
  };

  // 1) expirationTime: null (Chromium'un olağan çıktısı; hatanın kaynağı).
  await subscribeOnce('expirationTime=null');

  // 2) Deneme bildirimi: sahte push servisine şifreli istek gider; yük çözülür.
  const keys = await page.evaluate(() => window.__keys);
  const uaPub = b64uToBuf(keys.pub);
  const ecdh = crypto.createECDH('prime256v1');
  // d (JWK) -> ham özel anahtar
  const priv = b64uToBuf(keys.priv);
  ecdh.setPrivateKey(priv);
  const before = hits.length;
  await page.getByRole('button', { name: 'Send test' }).click();
  await until('deneme bildirimi', () => hits.length > before);
  const h1 = hits[before];
  if (h1.path !== '/push/e2e' || !/^vapid /.test(h1.headers.authorization || '') || h1.headers['content-encoding'] !== 'aes128gcm') fail('deneme bildirimi başlıkları: ' + JSON.stringify(h1.headers));
  const p1 = decrypt(h1.body, priv, uaPub, b64uToBuf(keys.auth));
  if (!p1.title) fail('deneme yükü başlıksız: ' + JSON.stringify(p1));
  if (EMOJI.test(JSON.stringify(p1))) fail('deneme yükünde emoji var: ' + JSON.stringify(p1));
  ok('deneme bildirimi şifreli gitti, yük çözüldü: ' + p1.title);

  // 3) Kapat, expirationTime sayı ile yeniden abone ol.
  await turnOff.click();
  await turnOn.waitFor({ timeout: 15000 });
  await page.evaluate(() => { window.__exp = Date.now() + 30 * 24 * 3600 * 1000; });
  await subscribeOnce('expirationTime=sayı');

  // 4) Bir olay uyarısı push olarak gider: webpush kanalı + kapalı porta tcp monitör.
  const ch = await (await ctx.request.post(BASE + '/api/notifications', { data: { name: 'Push', type: 'webpush', config: { users: '' }, apply_existing: true }, headers: H })).json();
  if (!ch.id) fail('kanal oluşmadı: ' + JSON.stringify(ch));
  const mon = await ctx.request.post(BASE + '/api/monitors', {
    data: { name: 'E2E kapali servis', type: 'tcp', interval: 20, timeout: 5, max_retries: 0, config: { host: '127.0.0.1', port: 9 }, notification_ids: [ch.id] },
    headers: H,
  });
  if (!mon.ok()) fail('monitör oluşmadı: ' + mon.status() + ' ' + (await mon.text()));
  const mark = hits.length;
  const h2 = await until('olay uyarısı push', () => hits.slice(mark).find((x) => x.path === '/push/e2e'), 120000);
  const p2 = decrypt(h2.body, priv, uaPub, b64uToBuf(keys.auth));
  if (EMOJI.test(JSON.stringify(p2))) fail('uyarı yükünde emoji var: ' + JSON.stringify(p2));
  if (!JSON.stringify(p2).includes('E2E kapali servis')) fail('uyarı yükü monitörü anmıyor: ' + JSON.stringify(p2));
  ok('olay uyarısı push olarak gitti: ' + (p2.title || JSON.stringify(p2).slice(0, 80)));

  if (errors.length) fail('sayfa hataları: ' + errors.join(' | '));
  console.log('E2E PUSH GEÇTİ');
} finally {
  await browser.close();
  fake.close();
}
