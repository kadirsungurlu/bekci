// Canlı topoloji uçtan uca. Gerçek panelle: boş durum, satır, sayfa yenilenmeden canlı ekleme (SSE),
// gizli sekmede istek yapılmaması. Sabit tohumla (topotohum.mjs): seçim ve ilişki sayıları, çizgi,
// ayrıntı şeridi, Esc, adreste kalan seçim, arama, yalnız sorunlular, gruplama hatırlama, klavye,
// dar ekran sekmeleri ve 390 px. Çalışan bir panele karşı:
//   BASE=http://127.0.0.1:8080 node topoloji.mjs
// CHROME=/usr/bin/google-chrome verilirse o tarayıcı kullanılır.
import { chromium } from 'playwright';
import { fakeTopology } from './topotohum.mjs';

const BASE = process.env.BASE || 'http://127.0.0.1:8080';
const USER = 'e2e';
const PASS = 'e2e-parola-12345';
const H = { 'Content-Type': 'application/json', 'X-Uptime': '1' };

function fail(msg) {
  console.error('HATA:', msg);
  process.exit(1);
}

const browser = await chromium.launch(process.env.CHROME ? { executablePath: process.env.CHROME, args: ['--no-sandbox'] } : {});
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
  // Grup kapalı olabilir: arama eşleşen grubu açar.
  const search = p.getByTestId('topology-search');
  const row = (id) => p.locator('#o-' + id);
  await search.fill('E2E Topo');
  await row('m' + down.id).waitFor({ timeout: 5000 }).catch(() => fail('monitör satırı yok'));
  await row('p' + probe.probe.id).waitFor({ timeout: 5000 }).catch(() => fail('kontrol noktası satırı yok'));
  if (!(await row('p' + probe.probe.id).innerText()).includes('17 ms')) fail('panel RTT yok');
  await row('c' + chan.id).waitFor({ timeout: 5000 }).catch(() => fail('kanal satırı yok'));
  await row('m' + down.id).click();
  const open = p.getByTestId('topology-open');
  await open.waitFor({ timeout: 3000 }).catch(() => fail('ayrıntı şeridi yok'));
  if (!(await open.getAttribute('href'))?.endsWith('/monitors/' + down.id)) fail('Aç bağlantısı monitöre gitmiyor');
  await p.keyboard.press('Escape');
  console.log('tamam: gerçek veriyle satırlar ve ayrıntı şeridi');

  // Canlı ekleme: sayfa yenilenmeden yeni monitör görünür.
  const live = await post('/api/monitors', { name: 'E2E Topo Canlı ' + tag, type: 'push', interval: 60 });
  await row('m' + live.id).waitFor({ timeout: 20_000 }).catch(() => fail('canlı güncelleme gelmedi'));
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
  await row('m' + hid.id).waitFor({ timeout: 5000 }).catch(() => fail('görünür olunca tazelenmedi'));
  console.log('tamam: gizli sekmede duruyor, dönünce tazeleniyor');

  // Sabit tohum: yanıt tarayıcıda verilir, panele yazılmaz.
  const seed = fakeTopology({ mon: 60, srv: 12, pro: 8 });
  const body = JSON.stringify(seed);
  const open2 = async (w, h, hash = '#/topology') => {
    const c = await browser.newContext({ viewport: { width: w, height: h } });
    await c.addCookies(await ctx.cookies());
    const q = await c.newPage();
    await q.route('**/api/topology*', (r) => r.fulfill({ contentType: 'application/json', body }));
    await q.goto(BASE + '/' + hash);
    await q.getByTestId('topology-board').waitFor({ timeout: 10_000 }).catch(() => fail(w + ' px pano yok'));
    await q.locator('.o:visible').first().waitFor({ timeout: 5000 }).catch(() => fail(w + ' px satır yok'));
    return { c, q };
  };
  const m = seed.monitors.find((x) => x.state === 'down' && x.loc_ids.length >= 3);
  const locs = new Set(m.loc_ids).size;
  const hasSel = (q) => q.evaluate(() => /[?&]sec=/.test(location.hash));

  {
    const { c, q } = await open2(1600, 1000);
    if (!(await q.locator('[data-testid="topology-problems"] .sorun-b, [data-testid="topology-problems"]').count())) fail('sorun çizelgesi yok');
    // Monitör: ilgili nokta sayısı, çizgiler, ayrıntı, adres.
    await q.locator('#o-m' + m.id).click();
    if ((await q.locator('.o.sec').count()) !== 1) fail('seçili satır bir değil');
    const seen = await q.locator('.o.pro.ilg').count();
    if (seen !== locs) fail(`ilişkili nokta ${seen}, beklenen ${locs}`);
    await q.waitForFunction(() => document.querySelectorAll('svg.hat path').length > 0, null, { timeout: 3000 }).catch(() => fail('çizgi yok'));
    await q.getByTestId('topology-open').waitFor({ timeout: 3000 }).catch(() => fail('ayrıntı şeridi yok'));
    if (!(await q.getByTestId('topology-open').getAttribute('href'))?.endsWith('/monitors/' + m.id)) fail('Aç monitöre gitmiyor');
    if (!(await hasSel(q))) fail('seçim adreste yok');
    // Adresten geri gelir.
    await q.reload();
    await q.locator('.o.sec').waitFor({ timeout: 8000 }).catch(() => fail('seçim adresten dönmedi'));
    // Esc seçimi bırakır.
    await q.keyboard.press('Escape');
    await q.getByTestId('topology-open').waitFor({ state: 'hidden', timeout: 3000 }).catch(() => fail('Esc şeridi kapatmadı'));
    if (await q.locator('.o.ilg').count()) fail('Esc sonrası ilişki kaldı');
    if (await hasSel(q)) fail('Esc sonrası adreste seçim kaldı');
    if (await q.locator('svg.hat path').count()) fail('Esc sonrası çizgi kaldı');
    // Sunucu, nokta ve kanal seçimi.
    for (const [id, needed] of [['.o.srv.warn', '.o.ilg'], ['.o.pro.ok', '.o.ilg'], ['.o.cha', '.o.ilg']]) {
      await q.locator(id).first().click();
      await q.locator('.o.sec').waitFor({ timeout: 3000 }).catch(() => fail(id + ' seçilmedi'));
      if (!(await q.locator(needed).count())) fail(id + ' ilişkisiz');
      await q.keyboard.press('Escape');
    }
    console.log('tamam: seçim, ilişki sayıları, çizgi, Esc, adres');

    // Arama: '/' odaklar, süzer, adrese yazar.
    const total = await q.locator('.o.mon').count();
    await q.keyboard.press('/');
    if (!(await q.getByTestId('topology-search').evaluate((e) => e === document.activeElement))) fail('/ aramaya odaklamadı');
    await q.keyboard.type(m.name);
    await q.waitForFunction(() => /[?&]q=/.test(location.hash), null, { timeout: 3000 }).catch(() => fail('arama adreste yok'));
    const hit = await q.locator('.o.mon').count();
    if (!hit) fail('arama sonuç vermedi');
    await q.getByTestId('topology-search').fill('');
    // Yalnız sorunlular.
    await q.getByTestId('topology-problems-only').click();
    await q.waitForFunction(() => /[?&]sorun=1/.test(location.hash), null, { timeout: 3000 }).catch(() => fail('süzgeç adreste yok'));
    const notBad = await q.locator('.o.mon:not(.bad):not(.warn)').count();
    if (notBad) fail('yalnız sorunlularda ' + notBad + ' sağlıklı satır');
    await q.getByTestId('topology-problems-only').click();
    console.log(`tamam: arama (${hit}/${total}) ve yalnız sorunlular`);

    // Gruplama seçimi yerelde hatırlanır.
    const by = q.locator('#c-mon button[data-by][aria-pressed="false"]').first();
    const val = await by.getAttribute('data-by');
    await by.click();
    await q.reload();
    await q.locator(`#c-mon button[data-by="${val}"][aria-pressed="true"]`).waitFor({ timeout: 8000 }).catch(() => fail('gruplama hatırlanmadı'));
    // Klavye: ok tuşu ilerletir, Enter seçer.
    const first = q.locator('#k-mon .gh, #k-mon .o').first();
    await first.focus();
    await q.keyboard.press('ArrowDown');
    const moved = await q.evaluate(() => document.activeElement?.id);
    await q.keyboard.press('Enter');
    if (moved?.startsWith('o-') && !(await q.locator('.o.sec').count())) fail('Enter seçmedi');
    console.log('tamam: gruplama hatırlama ve klavye');
    await c.close();
  }

  // 1000 monitör: sanal liste. Çizilen satır az, klavye pencereyi geçer, çizgi uçları satır ortasında.
  {
    const big = JSON.stringify(fakeTopology({ mon: 1000, srv: 100, pro: 30 }));
    const c = await browser.newContext({ viewport: { width: 1600, height: 1000 } });
    await c.addCookies(await ctx.cookies());
    const q = await c.newPage();
    await q.route('**/api/topology*', (r) => r.fulfill({ contentType: 'application/json', body: big }));
    await q.goto(BASE + '/#/topology');
    await q.locator('.o.mon').first().waitFor({ timeout: 10_000 }).catch(() => fail('büyük tohum satır yok'));
    const rows = await q.locator('.o').count();
    if (rows > 150) fail('sanal liste çok satır çizdi: ' + rows);
    // Yükseklikler hesapla aynı: son eleman listenin sonuna oturur.
    const fit = await q.evaluate(() => [...document.querySelectorAll('.vl')].every((v) => {
      const k = v.lastElementChild.getBoundingClientRect().bottom - v.getBoundingClientRect().top;
      const last = v.parentElement.scrollHeight;
      return k <= last + 1;
    }));
    if (!fit) fail('sanal liste yüksekliği taşıyor');
    const drift = async () => q.evaluate(() => {
      const hat = document.querySelector('svg.hat').getBoundingClientRect();
      const rows = [...document.querySelectorAll('.o.ilg,.gh')].map((e) => e.getBoundingClientRect());
      return [...document.querySelectorAll('svg.hat circle')].filter((c) => {
        const y = +c.getAttribute('cy') + hat.top;
        return !rows.some((r) => y >= r.top - 2 && y <= r.bottom + 2) && y > 300 && y < innerHeight - 20;
      }).length;
    });
    await q.locator('.o.pro').first().click();
    await q.waitForFunction(() => document.querySelectorAll('svg.hat path').length > 0, null, { timeout: 3000 }).catch(() => fail('büyük tohum çizgi yok'));
    if (await drift()) fail('çizgi ucu satırda değil');
    await q.keyboard.press('Escape');
    // Klavye: pencere dışına iner, odak kaybolmaz.
    await q.locator('#k-mon .gh').first().focus();
    for (let i = 0; i < 60; i++) await q.keyboard.press('ArrowDown');
    const f = await q.evaluate(() => {
      const e = document.activeElement;
      return { inList: !!e?.closest('#k-mon'), top: document.querySelector('#k-mon').scrollTop };
    });
    if (!f.inList || f.top < 200) fail('ok tuşu pencereyi geçemedi: ' + JSON.stringify(f));
    await q.keyboard.press('End');
    await q.waitForFunction(() => document.querySelector('#k-mon').scrollTop > 1000, null, { timeout: 3000 }).catch(() => fail('End sona gitmedi'));
    await q.keyboard.press('Home');
    await q.waitForFunction(() => document.querySelector('#k-mon').scrollTop === 0 && document.activeElement?.closest('#k-mon'), null, { timeout: 3000 }).catch(() => fail('Home başa dönmedi'));
    // Kapalı grup hücresine tıklamak açar.
    const cell = q.locator('#k-mon span.hucre').first();
    if (await cell.count()) {
      const id = await cell.getAttribute('data-g');
      await cell.click();
      await q.waitForFunction((g) => document.getElementById(g)?.getAttribute('aria-expanded') === 'true', id, { timeout: 3000 }).catch(() => fail('hücre grubu açmadı'));
    }
    await c.close();
    console.log(`tamam: sanal liste (${rows} satır), çizgi ucu, klavye, kapalı grup`);
  }

  // Dar ekran: sekmeler ve ilişki rozeti.
  {
    const { c, q } = await open2(1100, 800);
    await q.locator('.sekme').waitFor({ timeout: 3000 }).catch(() => fail('sekme yok'));
    await q.locator('#o-m' + m.id).click();
    await q.locator('.sekme button[data-tab="pro"]').click();
    if (!(await q.locator('#c-pro').isVisible())) fail('nokta sekmesi açılmadı');
    if (await q.locator('#c-mon').isVisible()) fail('monitör sütunu görünür kaldı');
    if ((await q.locator('.sekme button[data-tab="pro"] b').innerText()).trim() !== String(locs)) fail('sekme rozeti ilişki sayısı değil');
    await q.waitForFunction(() => /[?&]sekme=pro/.test(location.hash), null, { timeout: 3000 }).catch(() => fail('sekme adreste yok'));
    await c.close();
    console.log('tamam: dar ekran sekmeleri');
  }

  // 390 px: yatay kaydırma yok, dokunma alanı >= 44 px, alt sayfa.
  {
    const { c, q } = await open2(390, 800);
    const sw = await q.evaluate(() => document.documentElement.scrollWidth);
    if (sw > 390) fail('390 px yatay kaydırma: ' + sw);
    const small = await q.locator('.o, .gh, .sekme button').evaluateAll((els) => els.filter((e) => { const h = e.getBoundingClientRect().height; return h > 0 && h < 44; }).length);
    if (small) fail(small + ' dokunma alanı 44 px altında');
    await q.locator('#o-m' + m.id).click();
    await q.getByTestId('topology-open').waitFor({ timeout: 3000 }).catch(() => fail('390 px alt sayfa yok'));
    await q.keyboard.press('Escape');
    await q.getByTestId('topology-open').waitFor({ state: 'hidden', timeout: 3000 }).catch(() => fail('390 px Esc kapatmadı'));
    await c.close();
    console.log('tamam: 390 px');
  }
} finally {
  await browser.close();
}
