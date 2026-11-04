// Betik akışı: kütüphane, yükleme, başlatma + step-up, onay, imzalı bilet, ajan reddi,
// geçmiş, çıktı, acil durdurma. Panel olarak internal/api/scripts_tarayici_test.go çalışır
// (gerçek handler + PostgreSQL + ileri sarılabilir saat, tohum veri, /__e2e/* yardımcıları):
//   BEKCI_E2E_ADDR=127.0.0.1:8081 UPTIME_TEST_PG=... go test -v -timeout 30m -run TestE2EBetikPanel ./internal/api
//   BASE=http://127.0.0.1:8081 E2E_STOP=1 node betik.mjs
// SHOTS=<klasör> verilirse pencerelerin ekran görüntüsü de alınır.
import { chromium } from 'playwright';

const BASE = process.env.BASE || 'http://127.0.0.1:8081';
const SHOTS = process.env.SHOTS || '';
const H = { 'Content-Type': 'application/json', 'X-Uptime': '1' };
const LONG = 'postgresql-yedek-dogrulama-ve-uzak-depo-esitleme-gunluk';

function fail(msg) {
  throw new Error(msg);
}

const browser = await chromium.launch();
let exit = 0;

async function session(user, password, viewport = { width: 1440, height: 900 }) {
  const ctx = await browser.newContext({ viewport, locale: 'tr-TR' });
  await ctx.addInitScript(() => localStorage.setItem('uptime.lang', 'tr'));
  const rq = ctx.request;
  const l = await (await rq.post(BASE + '/api/auth/login', { data: { username: user, password }, headers: H })).json();
  if (!l.challenge) fail('2FA sorulmadı: ' + JSON.stringify(l));
  const ok = await rq.post(BASE + '/api/auth/login/2fa', { data: { challenge: l.challenge, code: await totp(rq, user) }, headers: H });
  if (!ok.ok()) fail('2FA girişi: ' + ok.status());
  const page = await ctx.newPage();
  return { ctx, rq, page, user };
}

async function totp(rq, user) {
  return (await (await rq.get(`${BASE}/__e2e/totp?user=${user}`)).json()).code;
}

async function runsOf(rq) {
  return (await (await rq.get(BASE + '/api/scripts/runs')).json()).runs;
}

const dlg = (p) => p.locator('dialog[open]');

async function shot(p, name) {
  if (SHOTS) await p.screenshot({ path: `${SHOTS}/${name}.jpg`, type: 'jpeg', quality: 55 });
}

/** Açık kod penceresine geçerli kodu yazar, gönderir ve kapanmasını bekler. */
async function confirmCode(s) {
  const d = dlg(s.page);
  await d.getByLabel('Doğrulama kodu').fill(await totp(s.rq, s.user));
  await d.locator('button[type=submit]').click();
  await d.waitFor({ state: 'detached' }).catch(() => {});
  await s.page.locator('dialog[open]').waitFor({ state: 'hidden' });
}

const state = (p) => p.getByTestId('run-state');
const waitState = (p, text) => p.locator('[data-testid=run-state]', { hasText: text }).waitFor();

try {
  const kadir = await session('kadir', 'cok-gizli-sifre');
  const p = kadir.page;
  const errors = [];
  p.on('pageerror', (e) => errors.push(e.message));

  // Kütüphane: uzun ad, iptal ve süresi dolmuş sürüm başlatılamaz.
  await p.goto(`${BASE}/#/scripts`);
  await p.locator('tbody tr code', { hasText: LONG }).waitFor();
  const row = (id) => p.locator('tbody tr').filter({ has: p.locator('code', { hasText: new RegExp(`^${id}$`) }) });
  for (const id of ['kaldirilan-gecici-yama', 'eski-kayit-arsivle']) {
    if (!(await row(id).getByRole('button', { name: 'Çalıştır' }).isDisabled())) fail(id + ' başlatılabilir görünüyor');
  }
  await row('kaldirilan-gecici-yama').getByText('İptal').first().waitFor();
  await row('eski-kayit-arsivle').getByText('Süresi doldu').waitFor();

  // Arama: süzgeç ve boş durum.
  const search = p.getByRole('searchbox', { name: 'Ara' });
  await search.fill('docker');
  await row('docker-imaj-temizle').waitFor();
  if ((await p.locator('tbody tr').count()) !== 2) fail('arama süzmedi');
  await search.fill('yokboyle');
  await p.getByRole('heading', { name: 'Eşleşen kayıt yok' }).waitFor();
  await p.getByRole('button', { name: 'Süzgeci temizle' }).click();
  await row('journal-temizle').waitFor();

  // Ayrıntı: sürüm penceresi.
  await row('journal-temizle').getByRole('button', { name: 'Ayrıntı' }).click();
  await dlg(p).waitFor();
  await shot(p, 'modal-surum');
  await p.keyboard.press('Escape');
  await dlg(p).waitFor({ state: 'hidden' });

  // Yükleme + step-up: yanlış kod pencerede hata verir, kod alanı boşalır, ikinci deneme geçer.
  const bundle = await (await kadir.rq.get(`${BASE}/__e2e/bundle?id=yeni-yuklenen-betik&ver=1`)).body();
  await p.getByRole('button', { name: 'Betik yükle' }).first().click();
  await dlg(p).waitFor();
  await dlg(p).locator('input[type=file]').setInputFiles({ name: 'yeni.betik.json', mimeType: 'application/json', buffer: bundle });
  await dlg(p).getByLabel('Doğrulama kodu').fill('000000');
  await shot(p, 'modal-yukle');
  await dlg(p).locator('button[type=submit]').click();
  await dlg(p).locator('.alert.error, .field-error, [role=alert]').first().waitFor();
  if ((await dlg(p).getByLabel('Doğrulama kodu').inputValue()) !== '') fail('hatalı kod temizlenmedi');
  await confirmCode(kadir);
  await row('yeni-yuklenen-betik').waitFor();

  // Başlatma: desteklemeyen sunucular seçilemez, eksik parametreyle düğme kapalı.
  await row('journal-temizle').getByRole('button', { name: 'Çalıştır' }).click();
  const m = dlg(p);
  await m.waitFor();
  await shot(p, 'modal-calistir');
  const off = await m.locator('#sr-host option[disabled]').count();
  if (off !== 3) fail(`seçilemez sunucu sayısı ${off}, 3 bekleniyordu`);
  await m.getByLabel('Doğrulama kodu').fill('123456');
  await m.locator('#sr-host').selectOption({ label: 'web-01.istanbul' });
  if (!(await m.locator('button[type=submit]').isDisabled())) fail('eksik parametreyle başlatma açık');
  await m.locator('#sr-p-gun').fill('21');
  await confirmCode(kadir);
  await p.waitForURL(/#\/scripts\/runs\/[0-9a-f]{32}$/);
  await waitState(p, 'Kuyrukta');
  const runId = p.url().split('/').pop();

  // Ajan çalıştırır: canlı izleme durumu ve çıktıyı getirir; ANSI temizlenmiş olmalı.
  const out = encodeURIComponent('\x1b[31mtemizlendi\x1b[0m: 1.4G\n<b>kalin degil</b>\n');
  await kadir.rq.get(`${BASE}/__e2e/agent?run=${runId}&host=web&status=ok&output=${out}`);
  await waitState(p, 'Başarılı');
  const text = await p.locator('pre.out').innerText();
  if (!text.includes('temizlendi: 1.4G') || !text.includes('<b>kalin degil</b>')) fail('çıktı: ' + text);
  if (await p.locator('pre.out b').count()) fail('çıktı HTML olarak işlendi');
  await p.getByText('Ajanın beyanı').waitFor();

  // Panel onayı: kendi isteğini onaylayamaz, diğer yönetici onaylar.
  const runs = await runsOf(kadir.rq);
  const own = runs.find((r) => r.state === 'awaiting_approval' && r.requested_by === 'kadir');
  await p.goto(`${BASE}/#/scripts/runs/${own.run_id}`);
  await waitState(p, 'Onay bekliyor');
  await p.getByText('başka bir yönetici', { exact: false }).first().waitFor();
  if (await p.getByRole('button', { name: 'Onayla' }).count()) fail('kendi isteğinde onay düğmesi var');

  const other = runs.find((r) => r.state === 'awaiting_approval' && r.requested_by === 'ayse');
  await p.goto(`${BASE}/#/scripts/runs/${other.run_id}`);
  await p.getByRole('button', { name: 'Onayla' }).click();
  await dlg(p).waitFor();
  await confirmCode(kadir);
  await waitState(p, 'Kuyrukta');

  // Ayşe kendi oturumunda kadir'in isteğini reddeder.
  const ayse = await session('ayse', 'kalici-sifre-1');
  await ayse.page.goto(`${BASE}/#/scripts/runs/${own.run_id}`);
  await ayse.page.getByRole('button', { name: 'Reddet' }).click();
  await ayse.page.locator('dialog[open]').getByRole('button', { name: 'Reddet' }).click();
  await waitState(ayse.page, 'Reddedildi');
  await ayse.ctx.close();

  // İmzalı bilet: istek JSON'u görünür, yapıştırılan bilet işi kuyruğa alır.
  const tick = runs.find((r) => r.state === 'awaiting_ticket');
  await p.goto(`${BASE}/#/scripts/runs/${tick.run_id}`);
  await p.locator('pre.out', { hasText: tick.run_id }).waitFor();
  const ticket = await (await kadir.rq.get(`${BASE}/__e2e/ticket?run=${tick.run_id}`)).text();
  await p.getByRole('button', { name: 'Bilet yapıştır' }).click();
  await dlg(p).locator('#sr-ticket').fill(ticket);
  await shot(p, 'modal-bilet');
  await confirmCode(kadir);
  await waitState(p, 'Kuyrukta');

  // Ajan reddi: ham kod değil çevrilmiş metin.
  await kadir.rq.get(`${BASE}/__e2e/agent?run=${tick.run_id}&host=web&reject=pin_mismatch`);
  await waitState(p, 'Ajan reddetti');
  if ((await p.locator('.alert.error').innerText()).includes('pin_mismatch')) fail('ham kod gösterildi');

  // Geçmiş: seçili süzgeç, yeni kayıtlar.
  await p.goto(`${BASE}/#/scripts/runs`);
  await p.locator('tbody tr').first().waitFor();
  const total = await p.locator('tbody tr').count();
  await p.getByRole('radio', { name: 'Sorunlu' }).click();
  const bad = await p.locator('tbody tr').count();
  if (!bad || bad >= total) fail(`süzgeç: ${bad}/${total}`);
  await p.locator('tbody tr', { hasText: 'Ajan reddetti' }).first().waitFor();

  // Acil durdurma: kod istemez, bekleyen iş iptal olur, başlatma kapanır; devam kod ister.
  await p.goto(`${BASE}/#/scripts/settings`);
  await p.getByRole('button', { name: 'Acil durdur' }).click();
  await dlg(p).getByRole('button', { name: 'Acil durdur' }).click();
  await p.getByRole('button', { name: 'Devam ettir' }).waitFor();
  await p.goto(`${BASE}/#/scripts`);
  await p.getByText('Acil durdurma etkin').waitFor();
  if (!(await row('journal-temizle').getByRole('button', { name: 'Çalıştır' }).isDisabled())) fail('durdurmada başlatma açık');
  await p.goto(`${BASE}/#/scripts/settings`);
  await p.getByRole('button', { name: 'Devam ettir' }).click();
  await dlg(p).waitFor();
  await confirmCode(kadir);
  await p.getByRole('button', { name: 'Acil durdur' }).waitFor();

  // Bekleyen sunucu anahtarı onayı.
  await p.getByRole('button', { name: 'Anahtarı onayla' }).click();
  await dlg(p).waitFor();
  await shot(p, 'modal-anahtar');
  await confirmCode(kadir);
  await p.locator('tr', { hasText: 'yeni-sunucu-04.ankara' }).getByRole('button', { name: 'Anahtarı onayla' }).waitFor({ state: 'detached' });

  // Bütün bu işlemlerden sonra kayıt zinciri çevrimdışı doğrulanır.
  const chain = await kadir.rq.get(`${BASE}/__e2e/verify`);
  if (!chain.ok()) fail('kayıt zinciri doğrulanamadı');
  if (errors.length) fail('sayfa hatası: ' + errors.join('; '));
  console.log('tamam');
} catch (e) {
  console.error('HATA:', e.message);
  exit = 1;
} finally {
  if (process.env.E2E_STOP) await fetch(`${BASE}/__e2e/stop`).catch(() => {});
  await browser.close();
}
process.exit(exit);
