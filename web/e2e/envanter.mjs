// Sunucu envanteri uçtan uca: eski ajan boş durumu, ajandan gelen envanter,
// bölümler, arama/süzgeç, sertifika yoksayma, disk/konteyner grafiği, 390 px.
// Çalışan bir panele karşı:  BASE=http://127.0.0.1:8080 node envanter.mjs
// SHOTS=<klasör> verilirse 1920 ve 390 görüntüleri kaydedilir.
import { chromium } from 'playwright';
import { mkdirSync } from 'node:fs';

const BASE = process.env.BASE || 'http://127.0.0.1:8080';
const SHOTS = process.env.SHOTS || '';
const USER = 'e2e';
const PASS = 'e2e-parola-12345';
const H = { 'Content-Type': 'application/json', 'X-Uptime': '1' };
const DAY = 86400;

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
  const created = await (await rq.post(BASE + '/api/servers', { data: { name: 'envanter-' + Date.now() }, headers: H })).json();
  const id = created.probe.id;
  const agentH = { ...H, Authorization: 'Bearer ' + created.token };

  const page = await ctx.newPage();
  const errors = [];
  page.on('pageerror', (e) => errors.push(String(e)));
  const shot = async (p, name) => {
    if (SHOTS) await p.screenshot({ path: `${SHOTS}/${name}.jpg`, type: 'jpeg', quality: 60 });
  };
  if (SHOTS) mkdirSync(SHOTS, { recursive: true });

  // Eski ajan: envanter yok, boş durum.
  await page.goto(`${BASE}/#/servers/${id}`);
  await page.getByRole('tab', { name: /Envanter|Inventory/ }).click();
  await page.locator('.card.empty').waitFor();
  if (!(await page.locator('.card.empty p').innerText()).length) fail('boş durum açıklaması yok');

  // Ajan metrik ve envanter gönderir.
  const now = Math.floor(Date.now() / 1000);
  const disk = (mount, used) => ({ mount, device: '/dev/sda1', fs: 'ext4', total: 100 * 2 ** 30, used: used * 2 ** 30 });
  const r0 = await rq.post(BASE + '/api/probe/metrics', {
    headers: agentH,
    data: {
      time: Date.now(),
      host: { hostname: 'web-1', platform: 'debian', arch: 'amd64', cores: 4 },
      stats: {
        cpu: 12, load1: 0.2, load5: 0.2, load15: 0.2, mem_total: 2 ** 33, mem_used: 2 ** 32, uptime: 5000,
        disks: [disk('/', 62), disk('/data', 91)],
        containers: [{ name: 'web', state: 'running', cpu: 3, mem: 2 ** 28 }],
      },
    },
  });
  if (!r0.ok()) fail('metrik: ' + r0.status() + ' ' + (await r0.text()));
  const inv = {
    time: Date.now(),
    services: [
      { name: 'nginx', active: 'active', sub: 'running', enabled: 'enabled', description: 'A high performance web server' },
      { name: 'cron', active: 'active', sub: 'running', enabled: 'enabled', description: 'Regular background program processing daemon' },
      { name: 'yedek', active: 'failed', sub: 'failed', enabled: 'disabled', description: 'Gece yedeği' },
    ],
    containers: [
      { id: 'a1', name: 'web', image: 'nginx:1.29', state: 'running', health: 'healthy', restart_count: 0, command: 'nginx -g daemon off;', started_at: new Date((now - DAY) * 1000).toISOString() },
      { id: 'a2', name: 'api', image: 'ghcr.io/ornek/api:2.4.1', state: 'restarting', restart_count: 7, command: 'api --password=gizli-e2e --port 8080', started_at: new Date((now - 60) * 1000).toISOString() },
    ],
    ports: [
      { proto: 'tcp', addr: '0.0.0.0', port: 443, process: 'nginx', cmd: 'nginx: master process' },
      { proto: 'tcp', addr: '127.0.0.1', port: 5432, process: 'postgres' },
      { proto: 'udp', addr: '0.0.0.0', port: 53, process: 'dnsmasq' },
    ],
    certs: [
      { source: 'file', location: '/etc/letsencrypt/live/ornek.com/cert.pem', subject: 'ornek.com', sans: ['ornek.com', 'www.ornek.com'], issuer: "Let's Encrypt", not_after: now + 5 * DAY + 600, fingerprint: 'aa11' },
      { source: 'port', location: '127.0.0.1:8443', subject: 'eski.ornek.com', issuer: 'Ornek CA', not_after: now - 2 * DAY, fingerprint: 'bb22' },
      { source: 'port', location: '0.0.0.0:443', subject: 'site.ornek.com', issuer: "Let's Encrypt", not_after: now + 70 * DAY, fingerprint: 'cc33' },
    ],
    disks: [
      { mount: '/', device: '/dev/sda1', fs: 'ext4', total: 100 * 2 ** 30 },
      { mount: '/data', device: '/dev/sdb1', fs: 'xfs', total: 500 * 2 ** 30 },
    ],
    notes: {},
  };
  const r1 = await rq.post(BASE + '/api/probe/inventory', { data: inv, headers: agentH });
  if (r1.status() !== 204) fail('envanter: ' + r1.status() + ' ' + (await r1.text()));

  await page.locator('.empty .btn').click();
  await page.getByRole('tab', { name: /Servisler|Services/ }).waitFor();
  const rows = () => page.locator('section.card tbody tr').count();
  if ((await rows()) !== 3) fail('servis satırı: ' + (await rows()));

  // Arama + süzgeç + temizleme.
  // Servis durumları çevrilmiş: ham systemd değeri görünmez, ipucunda kalır.
  const pill = page.locator('tbody .pill[title="running"]').first();
  if (!/^(Çalışıyor|Running)$/.test((await pill.textContent()).trim())) fail('servis durumu çevrilmemiş');
  await page.getByRole('searchbox').fill('nginx');
  if ((await rows()) !== 1) fail('arama');
  await page.getByRole('searchbox').fill('olmayan-şey');
  await page.locator('.none .btn').click();
  await page.locator('select.flt').selectOption('failed');
  if ((await rows()) !== 1) fail('süzgeç');
  await page.locator('select.flt').selectOption('all');

  // Konteyner: gizli yok, grafik açılır.
  await page.getByRole('tab', { name: /Konteynerler|Containers/ }).click();
  const text = await page.locator('section.card tbody').innerText();
  if (text.includes('gizli-e2e')) fail('komuttaki parola görünüyor');
  await page.locator('.exp', { hasText: 'web' }).click();
  await page.locator('.chart-row svg').first().waitFor({ timeout: 8000 }).catch(async () => fail('grafik yok: ' + (await page.locator('.chart-row').innerText().catch(() => 'satır yok')) + ' | ' + (await page.locator('.chart-row').innerHTML().catch(() => '')).slice(0, 300)));

  // Sertifika: en yakın önce, süresi dolan işaretli, yoksayma kalıcı.
  await page.getByRole('tab', { name: /Sertifikalar|Certificates/ }).click();
  if (!(await page.locator('tbody tr').first().innerText()).includes('eski.ornek.com')) fail('sertifika sırası');
  await shot(page, 'envanter-sertifika-1440');
  await page.locator('tbody tr').nth(1).locator('.btn').click();
  await page.locator('tbody tr').nth(1).locator('.pill.paused').waitFor();
  const saved = await (await rq.get(`${BASE}/api/servers/${id}/inventory`)).json();
  if (!saved.certs.find((c) => c.fingerprint === 'aa11')?.ignored) fail('yoksayma kaydedilmedi');
  await page.locator('tbody tr').nth(1).locator('.btn').click();

  // Sertifika olayı: 5 gün kalan için uyarı açıldı.
  const incs = await (await rq.get(`${BASE}/api/servers/${id}/incidents`)).json();
  if (!incs.some((i) => (i.data?.metric ?? '') === 'cert')) fail('sertifika olayı yok: ' + JSON.stringify(incs).slice(0, 300));

  // Disk bölümleri.
  await page.getByRole('tab', { name: /Disk/ }).click();
  await page.locator('.exp').nth(1).click();
  await page.locator('.chart-row svg').first().waitFor();

  if (SHOTS) {
    await page.setViewportSize({ width: 1920, height: 1080 });
    await shot(page, 'envanter-disk-1920');
    await page.getByRole('tab', { name: /Konteynerler|Containers/ }).click();
    await shot(page, 'envanter-konteyner-1920');
  }

  // 390 px: yatay taşma yok, dokunma hedefleri yeterli.
  await page.setViewportSize({ width: 390, height: 844 });
  for (const s of [/Servisler|Services/, /Konteynerler|Containers/, /Portlar|Ports/, /Sertifikalar|Certificates/, /Disk/]) {
    await page.getByRole('tab', { name: s }).click();
    const over = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
    if (over > 0) fail(`390 px yatay taşma ${s}: ${over}`);
  }
  await page.getByRole('tab', { name: /Sertifikalar|Certificates/ }).click();
  await shot(page, 'envanter-sertifika-390');
  await page.getByRole('tab', { name: /Portlar|Ports/ }).click();
  await shot(page, 'envanter-port-390');
  const small = await page.locator('.sections button').evaluateAll((l) => l.filter((b) => b.getBoundingClientRect().height < 44).length);
  if (small) fail('dokunma hedefi küçük');

  if (errors.length) fail('sayfa hatası: ' + errors[0]);
  console.log('envanter e2e tamam');
} finally {
  await browser.close();
}
