// Güvenlik sekmesi + filo şeridi uçtan uca (730 bölüm A): rapor yokken boş durum,
// ajandan sahte Linux/Windows güvenlik raporu (POST /api/probe/security), sekmede
// güncelleme/yeniden başlatma/SSH/güvenlik duvarı/oturum kartları, paket süzgeci,
// GET /api/security/summary sayıları, Sunucular listesinde filo şeridi süzmesi, 390 px.
// Çalışan bir panele karşı:  BASE=http://127.0.0.1:8080 node guvenlik.mjs
// SHOTS=<klasör> verilirse 1920 ve 390 görüntüleri kaydedilir.
import { chromium } from 'playwright';
import { mkdirSync } from 'node:fs';

const BASE = process.env.BASE || 'http://127.0.0.1:8080';
const SHOTS = process.env.SHOTS || '';
const USER = 'e2e';
const PASS = 'e2e-parola-12345';
const H = { 'Content-Type': 'application/json', 'X-Uptime': '1' };
const HOUR = 3600;

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

  const stamp = Date.now();
  const mk = async (name) => {
    const c = await (await rq.post(BASE + '/api/servers', { data: { name }, headers: H })).json();
    const agentH = { ...H, Authorization: 'Bearer ' + c.token };
    // Metrik: sunucu listede "bağlı" görünsün.
    const r = await rq.post(BASE + '/api/probe/metrics', {
      headers: agentH,
      data: {
        time: Date.now(),
        host: { hostname: name, platform: name.includes('win') ? 'windows' : 'debian', arch: 'amd64', cores: 2 },
        stats: { cpu: 5, load1: 0.1, load5: 0.1, load15: 0.1, mem_total: 2 ** 32, mem_used: 2 ** 31, uptime: 9000, disks: [] },
      },
    });
    if (!r.ok()) fail(`metrik ${name}: ${r.status()} ${await r.text()}`);
    return { id: c.probe.id, name, agentH };
  };
  const lin = await mk('guv-linux-' + stamp);
  const ok = await mk('guv-temiz-' + stamp);
  const win = await mk('guv-win-' + stamp);

  const page = await ctx.newPage();
  const errors = [];
  page.on('pageerror', (e) => errors.push(String(e)));
  const shot = async (name) => {
    if (SHOTS) await page.screenshot({ path: `${SHOTS}/${name}.jpg`, type: 'jpeg', quality: 60 });
  };
  if (SHOTS) mkdirSync(SHOTS, { recursive: true });

  // Rapor yok: boş durum.
  await page.goto(`${BASE}/#/servers/${lin.id}`);
  await page.getByRole('tab', { name: /^(Güvenlik|Security)$/ }).click();
  await page.locator('.card.empty').waitFor();
  if (!/Henüz güvenlik raporu yok|No security report yet/.test(await page.locator('.card.empty').innerText())) fail('boş durum metni');

  // Sahte raporlar.
  const nowS = Math.floor(Date.now() / 1000);
  const hourStart = nowS - (nowS % HOUR);
  const send = async (srv, rep) => {
    const r = await rq.post(BASE + '/api/probe/security', { data: { time: Date.now(), ...rep }, headers: srv.agentH });
    if (r.status() !== 204) fail(`güvenlik raporu ${srv.name}: ${r.status()} ${await r.text()}`);
  };
  await send(lin, {
    updates: {
      manager: 'apt', total: 3, security: 2, index_time: nowS - 2 * HOUR,
      packages: [
        { name: 'openssl', current: '3.0.15-1', new: '3.0.17-1', security: true },
        { name: 'libssl3', current: '3.0.15-1', new: '3.0.17-1', security: true },
        { name: 'vim', current: '9.1.1', new: '9.1.2' },
      ],
    },
    reboot: { required: true, reason: 'file', packages: ['linux-image-6.12.0-9-amd64'] },
    auth: {
      source: 'journal', failed: 42, accepted: 1, invalid: 30,
      hours: [{ hour: hourStart, failed: 42, accepted: 1 }],
      top_ips: [{ key: '203.0.113.7', count: 40 }, { key: '198.51.100.4', count: 2 }],
      top_users: [{ key: 'root', count: 25 }, { key: 'admin', count: 17 }],
    },
    sessions: [{ user: 'deploy', tty: 'pts/0', from: '192.0.2.10', since: nowS - HOUR, kind: 'ssh' }],
    firewall: { active: false, backend: 'nftables', rules: 0, policy: 'accept' },
  });
  await send(ok, {
    updates: { manager: 'apt', total: 0, security: 0, index_time: nowS - HOUR, packages: [] },
    reboot: { required: false },
    sessions: [],
    firewall: { active: true, backend: 'ufw', rules: 4, policy: 'drop' },
  });
  await send(win, {
    updates: { manager: 'wu', total: 0, security: 0, packages: [] },
    reboot: { required: false },
    sessions: [{ user: 'Administrator', from: '192.0.2.20', since: nowS - HOUR, kind: 'rdp', state: 'Active' }],
    firewall: { active: true, profiles: [{ name: 'public', enabled: true, inbound: 'block', connected: true }] },
    av: { product: 'defender', service: true, enabled: true, realtime: false, sig_updated: nowS - 2 * HOUR, quick_age_d: 1, full_age_d: -1, threats_30d: 0, active_threats: 0, tamper: true },
  });

  // Sekme: kartlar dolu.
  await page.locator('.card.empty .btn').click();
  const cards = page.locator('.cards');
  await cards.waitFor();
  const ctext = await cards.innerText();
  for (const [want, what] of [
    [/Gelen trafik süzülmüyor|Incoming traffic is not filtered/, 'güvenlik duvarı kapalı'],
    [/42/, 'başarısız giriş sayısı'],
  ]) if (!want.test(ctext)) fail(`kart: ${what} yok`);
  const secNum = (await page.locator('.cards .nb .nv').nth(1).innerText()).trim();
  if (secNum !== '2') fail('güvenlik güncellemesi sayısı: ' + secNum);
  if ((await page.locator('.cards .pill.pending').count()) < 1) fail('yeniden başlatma rozeti yok');
  const body = await page.locator('main').innerText();
  if (!body.includes('203.0.113.7')) fail('IP tablosunda saldıran yok');
  if (!body.includes('deploy') || !body.includes('192.0.2.10')) fail('oturum tablosu');

  // Paket tablosu: tümü 3, yalnız güvenlik 2, arama.
  const pkgCard = page.locator('section.card', { has: page.getByRole('heading', { name: /Bekleyen paketler|Pending packages/ }) });
  const pkgRows = () => pkgCard.locator('tbody tr').count();
  if ((await pkgRows()) !== 3) fail('paket satırı: ' + (await pkgRows()));
  await pkgCard.getByRole('button', { name: /Yalnız güvenlik|Security only/ }).click();
  if ((await pkgRows()) !== 2) fail('güvenlik süzgeci: ' + (await pkgRows()));
  await pkgCard.getByRole('searchbox').fill('libssl');
  if ((await pkgRows()) !== 1) fail('paket arama: ' + (await pkgRows()));
  await pkgCard.getByRole('searchbox').fill('');

  if (SHOTS) {
    await page.setViewportSize({ width: 1920, height: 1080 });
    await shot('guvenlik-sekme-1920');
  }
  await page.setViewportSize({ width: 390, height: 844 });
  {
    const over = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
    if (over > 0) fail('390 px yatay taşma (sekme): ' + over);
  }
  await shot('guvenlik-sekme-390');
  await page.setViewportSize({ width: 1440, height: 1000 });

  // Filo özeti API'si.
  const sum = await (await rq.get(BASE + '/api/security/summary')).json();
  const has = (k, s) => (sum[k]?.servers ?? []).some((x) => x.id === s.id);
  if (!has('security_updates', lin) || has('security_updates', ok)) fail('özet security_updates: ' + JSON.stringify(sum.security_updates));
  if (!has('reboot', lin)) fail('özet reboot');
  if (!has('firewall_off', lin) || has('firewall_off', ok)) fail('özet firewall_off');
  if (!has('av_problem', win)) fail('özet av_problem: ' + JSON.stringify(sum.av_problem));

  // Sunucular listesi: filo şeridi + süzme.
  // Liste deposu 15 sn içinde yeniden yüklenmez: güvenlik raporu SSE ile gelmediğinden sayfayı tazele.
  await page.goto(`${BASE}/#/servers`);
  await page.reload();
  const fleet = page.getByRole('group', { name: /Filo güvenliği|Fleet security/ });
  await fleet.waitFor();
  const item = (re) => fleet.locator('button.fl-item', { hasText: re });
  for (const re of [/güncellemesi bekleyen|security updates/i, /Yeniden başlatma|reboot/i, /Güvenlik duvarı|Firewall/i, /Defender/]) {
    if ((await item(re).count()) !== 1) fail('şerit öğesi yok: ' + re);
  }
  const visible = async (s) => (await page.getByText(s.name, { exact: true }).count()) > 0;
  if (!(await visible(lin)) || !(await visible(ok)) || !(await visible(win))) fail('liste başta üç sunucuyu göstermiyor');
  await item(/Güvenlik duvarı|Firewall/i).click();
  if ((await item(/Güvenlik duvarı|Firewall/i).getAttribute('aria-pressed')) !== 'true') fail('süzgeç basılı değil');
  if (!(await visible(lin)) || (await visible(ok)) || (await visible(win))) fail('güvenlik duvarı süzmesi');
  await shot('guvenlik-filo-1440');
  await item(/Defender/).click();
  if ((await visible(lin)) || !(await visible(win))) fail('Defender süzmesi');
  await fleet.locator('button.fl-clear').click();
  if (!(await visible(ok))) fail('süzgeç temizlenmedi');

  await page.setViewportSize({ width: 390, height: 844 });
  {
    const over = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
    if (over > 0) fail('390 px yatay taşma (liste): ' + over);
  }
  await shot('guvenlik-filo-390');

  if (errors.length) fail('sayfa hataları: ' + errors.join(' | '));
  console.log('guvenlik.mjs: tamam');
} finally {
  await browser.close();
}
