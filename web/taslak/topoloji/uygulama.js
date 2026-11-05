'use strict';
const q = new URLSearchParams(location.search);
const S = { v: q.get('v') || 'a', n: +q.get('n') || 100, by: { mon: 'etiket', pro: 'bolge', srv: 'rol' }, sec: null, tum: {} };
if (q.get('g')) S.sec = `mon:etiket:${q.get('g')}`;
const D = veriUret(S.n);
const P = D.probes, PK = Object.fromEntries(P.map((p) => [p.kod, p]));
const dar = () => matchMedia('(max-width:720px)').matches;
const esc = (s) => String(s).replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c]));
const SIRA = { bad: 3, warn: 2, ok: 1, off: 0 };

function sn(c, b = 14) {
  const r = { ok: '<path d="m4.5 8.2 2.3 2.3 4.7-5" fill="none" stroke="currentColor" stroke-width="1.8"/>', warn: '<path d="M8 4.5v4.2M8 11v.3" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"/>', bad: '<path d="m5 5 6 6m0-6-6 6" stroke="currentColor" stroke-width="1.8"/>', off: '<path d="M4.8 8h6.4" stroke="currentColor" stroke-width="1.8"/>' }[c];
  return `<svg class="sn ${c}" width="${b}" height="${b}" viewBox="0 0 16 16" aria-hidden="true"><circle cx="8" cy="8" r="7" fill="currentColor" opacity=".2"/>${r}</svg>`;
}
const CHEV = '<svg class="ok-i" width="12" height="12" viewBox="0 0 12 12" aria-hidden="true"><path d="m4.5 2.5 3.5 3.5-3.5 3.5" fill="none" stroke="currentColor" stroke-width="1.6"/></svg>';

// --- Öğeler ve gruplar ---------------------------------------------------------------------
const OGE = {
  mon: D.monitors.map((m) => ({ kind: 'mon', ad: m.ad, cls: m.status === 'down' ? 'bad' : m.status === 'paused' ? 'off' : 'ok', m })),
  pro: P.map((p) => ({ kind: 'pro', ad: p.ad, cls: p.online ? 'ok' : 'bad', p })),
  srv: D.servers.map((s) => ({ kind: 'srv', ad: s.ad, cls: s.uyari ? 'warn' : 'ok', s })),
};
const BY = {
  mon: { etiket: ['Etiket', (o) => o.m.tags[0]], tur: ['Tür', (o) => o.m.tur], konum: ['Konum', (o) => o.m.konum], musteri: ['Müşteri', (o) => o.m.musteri] },
  pro: { bolge: ['Bölge', (o) => o.p.bolge], saglayici: ['Sağlayıcı', (o) => o.p.saglayici] },
  srv: { rol: ['Rol', (o) => o.s.rol], yer: ['Konum', (o) => o.s.yer] },
};
const BASLIK = { mon: 'Monitörler', pro: 'Kontrol noktaları', srv: 'Sunucular' };
function gruplar(kind) {
  const f = BY[kind][S.by[kind]][1], h = new Map();
  for (const o of OGE[kind]) { const k = f(o); if (!h.has(k)) h.set(k, []); h.get(k).push(o); }
  return [...h].map(([ad, items]) => {
    items = [...items].sort((a, b) => SIRA[b.cls] - SIRA[a.cls]);
    const c = { ok: 0, warn: 0, bad: 0, off: 0, total: items.length };
    items.forEach((o) => c[o.cls]++);
    const lv = c.bad ? 'bad' : c.warn ? 'warn' : c.ok ? 'ok' : 'off';
    return { id: `${kind}:${S.by[kind]}:${ad}`, kind, ad, items, c, lv };
  }).sort((a, b) => SIRA[b.lv] - SIRA[a.lv] || (b.c.bad + b.c.warn) - (a.c.bad + a.c.warn) || b.c.total - a.c.total);
}
function acikMi(g) {
  try {
    const t = JSON.parse(localStorage.getItem('bekci.topo.' + g.id) || 'null');
    if (t && t.lv === g.lv) return t.o; // tercih, grubun durumu değişene kadar geçerli
  } catch { /* yok say */ }
  return g.lv === 'bad' || g.lv === 'warn' || D.monitors.length <= 12;
}
function cevir(g) { localStorage.setItem('bekci.topo.' + g.id, JSON.stringify({ o: !acikMi(g), lv: g.lv })); }
function sayi(g) {
  const c = g.c, b = g.kind === 'pro' ? 'çevrimdışı' : 'hata';
  return `${c.total} · ${c.ok} tamam` + (c.warn ? ` · <i>${c.warn} uyarı</i>` : '') + (c.bad ? ` · <b>${c.bad} ${b}</b>` : '') + (c.off ? ` · ${c.off} duraklatıldı` : '');
}
const bar = (c) => `<span class="bar" aria-hidden="true">${['bad', 'warn', 'ok', 'off'].map((k) => c[k] ? `<span class="${k}" style="flex:${c[k]}"></span>` : '').join('')}</span>`;
const hucreler = (g) => `<span class="hucre" style="--hb:${D.monitors.length > 200 ? 6 : 9}px" aria-hidden="true">${g.items.map((o) => `<i class="${o.cls === 'ok' ? '' : o.cls}" title="${esc(o.ad)}"></i>`).join('')}</span>`;

// --- Gözlem hücreleri: hangi nokta görüyor -----------------------------------------------------
function matris(goren, cevrim = true) {
  return `<span class="mat">${P.map((p) => `<span class="${!p.online && cevrim ? 'h-off' : goren.includes(p.kod) ? 'h-bad' : 'h-ok'}" title="${esc(p.ad)}"></span>`).join('')}</span>`;
}
function hukum(m) {
  const canli = P.filter((p) => p.online), n = m.gorur.length, top = canli.length;
  if (n === top) return `<b>${n}/${top} nokta</b>, her yerden`;
  const bolgeler = [...new Set(m.gorur.map((k) => PK[k].bolge))];
  if (n > 1 && bolgeler.length === 1 && canli.filter((p) => p.bolge === bolgeler[0]).length === n) return `<b>${n}/${top} nokta</b>, ${AYRILMA[bolgeler[0]]}`;
  return `<b>${n}/${top} nokta</b>, ${m.gorur.map((k) => PK[k].ad).join(', ')}`;
}
function sorunlar() {
  const r = [];
  for (const o of OGE.mon) if (o.cls === 'bad') r.push({ tip: 'mon', o, cls: 'bad', ad: o.ad, grup: `${grupAdi('mon', o)}`, hata: o.m.hata, sure: o.m.sure, mat: matris(o.m.gorur), son: hukum(o.m) });
  r.sort((a, b) => b.o.m.gorur.length - a.o.m.gorur.length);
  for (const o of OGE.pro) if (o.cls === 'bad') { const dig = P.filter((p) => p.online); r.push({ tip: 'pro', o, cls: 'bad', ad: `${o.ad} noktası`, grup: `${grupAdi('pro', o)}`, hata: 'Çevrimdışı', sure: '12 dk', mat: matris(dig.map((p) => p.kod)), son: `<b>${dig.length}/${dig.length} eş</b> ulaşamıyor, panel de` }); }
  for (const o of OGE.srv) if (o.cls === 'warn') r.push({ tip: 'srv', o, cls: 'warn', ad: o.ad, grup: `${grupAdi('srv', o)}`, hata: o.s.uyari.split(',')[0], sure: o.s.sure, mat: '<span class="s-ajan">Sunucu ajanı bildiriyor</span>', son: `<span class="m-yal">Sunucu ajanı: </span>${o.s.uyari.split(', ')[1] || ''}` });
  return r;
}
const grupAdi = (k, o) => BY[k][S.by[k]][1](o);
function cizelge() {
  const r = sorunlar(), mon = r.filter((x) => x.tip === 'mon').length, pr = r.filter((x) => x.tip === 'pro').length, sv = r.filter((x) => x.tip === 'srv').length;
  const parca = [mon && `${mon} monitör`, pr && `${pr} kontrol noktası`, sv && `${sv} sunucu`].filter(Boolean).join(', ');
  return `<section class="sorun" aria-label="Sorunlar">
    <div class="sorun-b"><b>${r.length} sorun</b><span>${parca}</span><span class="lej"><i class="h-bad"></i>hata görüyor<i class="h-ok"></i>temiz<i class="h-off"></i>çevrimdışı, oy yok</span><em>Çalışan: ${OGE.mon.filter((o) => o.cls === 'ok').length}/${OGE.mon.length} monitör, ${P.filter((p) => p.online).length}/${P.length} nokta, ${OGE.srv.filter((o) => o.cls === 'ok').length}/${OGE.srv.length} sunucu</em></div>
    <div class="s-sat bs"><span></span><span class="s-gr">Nerede</span><span class="s-gr">Grup</span><span class="s-gr">Belirti</span><span class="s-gr">Süre</span><span class="mat kod">${P.map((p) => `<span title="${esc(p.ad)}">${p.kod}</span>`).join('')}</span><span class="s-gr">Kim görüyor</span></div>
    ${r.map((x, i) => `<button class="s-sat ${x.cls}" data-sorun="${i}">${sn(x.cls)}<span class="s-ad">${esc(x.ad)}</span><span class="s-gr">${esc(x.grup)}</span><span class="s-ha">${esc(x.hata)}</span><span class="s-su">${esc(x.sure)}</span>${x.mat}<span class="s-so">${x.son}</span></button>`).join('')}
  </section>`;
}

// --- Kartlar ---------------------------------------------------------------------------------
function kart(o) {
  if (o.kind === 'mon') {
    const m = o.m;
    if (o.cls === 'bad') return `<div class="kart c-bad"><div class="sat2"><b>${esc(o.ad)}</b><span class="kh">${esc(m.hata)}, ${m.sure}</span></div>${matris(m.gorur)}<small>${hukum(m)}</small></div>`;
    if (S.v === 'c') return `<div class="kart c-${o.cls}" title="${esc(o.ad)}, ${m.tur}${m.ping ? `, ${m.ping} ms` : ''}"><b>${esc(o.ad)}</b>${o.cls === 'off' ? '<small>Durdu</small>' : ''}</div>`;
    return `<div class="kart c-${o.cls}"><b title="${esc(o.ad)}">${esc(o.ad)}</b><small>${o.cls === 'off' ? 'Duraklatıldı' : `${m.tur}, ${m.ping} ms`}</small></div>`;
  }
  if (o.kind === 'srv') {
    const s = o.s;
    if (o.cls === 'warn') return `<div class="kart c-warn"><div class="sat2"><b>${esc(o.ad)}</b><span class="kh">${esc(s.uyari)}</span></div><small>CPU %${s.cpu}, RAM %${s.ram}, Disk %${s.disk}. ${s.sure} sürüyor</small></div>`;
    return `<div class="kart"><b title="${esc(o.ad)}">${esc(o.ad)}</b><small>${S.v === 'c' ? `CPU %${s.cpu}` : `CPU %${s.cpu}, Disk %${s.disk}`}</small></div>`;
  }
  const p = o.p;
  if (p.online) return `<div class="kart"><b title="${esc(o.ad)}">${esc(o.ad)}</b><small>${p.rtt} ms${S.v === 'c' ? '' : ' panele'}</small></div>`;
  return `<div class="kart c-${o.cls === 'bad' ? 'bad' : 'ok'}"><div class="sat2"><b>${esc(o.ad)}</b><span class="kh">${p.online ? '' : `Çevrimdışı, ${p.son}`}</span></div><small>${p.online ? `${p.rtt} ms panele` : `${p.saglayici}, oy veremiyor`}</small></div>`;
}
function kartlar(g) {
  const ust = S.tum[g.id] ? 1e9 : (g.items.length > 20 ? 15 : 20);
  const tel = dar() && !S.tum[g.id] && D.monitors.length > 12;
  const gor = tel ? g.items.filter((o) => o.cls !== 'ok' && o.cls !== 'off') : g.items.slice(0, ust), kal = g.items.length - gor.length;
  const uzun = Math.max(...gor.filter((o) => o.cls === 'ok' || o.cls === 'off').map((o) => o.ad.length), 8);
  const km = dar() ? '' : `style="--km:${S.v === 'c' ? (uzun >= 24 ? 200 : 136) : Math.min(300, Math.max(132, Math.round(uzun * 6.7 + 26)))}px"`;
  return `<div class="kart-iz" ${km}>${gor.map(kart).join('')}${kal ? `<button class="dahaz" data-tum="${esc(g.id)}">${tel ? `${kal} sağlıklı öğeyi göster` : `${kal} tane daha`}</button>` : ''}</div>`;
}
const yongalar = (g) => `<div class="yonga">${g.items.map((o) => `<span class="${o.cls === 'bad' ? 'bad' : ''}" title="${esc(o.ad)}">${o.p.kod}</span>`).join('')}</div>`;
function slab(g) {
  const acik = acikMi(g), c = S.v === 'c';
  return `<section class="g lv-${g.lv} ${acik ? 'acik' : ''}" data-g="${esc(g.id)}">
    <button class="g-h" aria-expanded="${acik}" data-ac="${esc(g.id)}">${CHEV}${sn(g.lv)}<span class="g-ad">${esc(g.ad)}</span><span class="g-sy">${sayi(g)}</span>${c || g.kind === 'pro' ? '' : bar(g.c)}</button>
    ${g.kind === 'pro' && !acik ? yongalar(g) : ''}${c && g.kind !== 'pro' && !acik ? hucreler(g) : ''}
    ${acik ? `<div class="g-b">${kartlar(g)}</div>` : ''}
  </section>`;
}
function dagit(gs, n) {
  const k = Array.from({ length: n }, () => ({ h: 0, g: [] }));
  for (const g of gs) {
    const a = k.reduce((m, x) => (x.h < m.h ? x : m));
    a.g.push(g);
    a.h += 40 + (acikMi(g) ? 30 + 50 * Math.ceil(g.items.length / 3) : (S.v === 'c' ? 26 : 0));
  }
  return `<div class="kol" style="--k:${n}">${k.map((x) => `<div>${x.g.map(slab).join('')}</div>`).join('')}</div>`;
}
const secici = (kind) => `<span class="sec" role="group" aria-label="Gruplama">${Object.entries(BY[kind]).map(([k, [ad]]) => `<button class="${S.by[kind] === k ? 'on' : ''}" data-by="${kind}:${k}">${ad}</button>`).join('')}</span>`;
function pano(kind, kol) {
  const gs = gruplar(kind), c = OGE[kind].length, lv = gs[0]?.lv || 'ok';
  return `<section class="pano lv-${lv}" data-pano="${kind}"><div class="pano-b"><h3>${BASLIK[kind]}</h3><span class="say">${c} ${{ mon: 'monitör', pro: 'nokta', srv: 'sunucu' }[kind]}, ${gs.length} grup</span>${secici(kind)}</div>${dagit(gs, kol)}</section>`;
}
const kanalPano = () => `<section class="pano" data-pano="kan"><div class="pano-b"><h3>Bildirim kanalları</h3></div>${D.kanallar.map(([a, t, s]) => `<div class="kanal"><b>${a}</b><small>${s} önce</small></div>`).join('')}</section>`;
function hub() {
  const r = sorunlar().length;
  return `<div class="hub" id="hub"><b>Bekci paneli</b><span>v1.7.0, saniyede 14 sonuç, ${P.filter((p) => p.online).length}/${P.length} nokta bağlı</span><span><em>${r} açık sorun</em></span></div>`;
}

// --- Varyantlar ------------------------------------------------------------------------------
function tuvalA() {
  const c = S.v === 'c', mk = dar() ? 1 : (c ? 4 : 3);
  return `<div class="tuval v-${S.v}" id="tuval" style="--gut:${c ? '18px' : '26px'}">
    <div class="ust">${pano('mon', mk)}${kanalPano()}</div>
    <div class="orta">${hub()}</div>
    <div class="alt">${pano('pro', dar() ? 1 : 2)}${pano('srv', dar() ? 1 : (c ? 3 : 2))}</div>
    <svg class="ciz" id="ciz" aria-hidden="false"></svg></div>`;
}
function grupBul(id) { for (const k of ['mon', 'pro', 'srv']) { const g = gruplar(k).find((x) => x.id === id); if (g) return g; } }
function solBolum(kind) {
  const gs = gruplar(kind), secik = S.sec;
  return `<section class="b-bol"><header><h3>${BASLIK[kind]}</h3><span class="say">${OGE[kind].length}</span>${secici(kind)}</header>${gs.map((g) => {
    const acik = acikMi(g), sorun = g.items.filter((o) => o.cls === 'bad' || o.cls === 'warn');
    const ic = sorun.map((o) => `<div class="b-sa ${o.cls}">${sn(o.cls, 12)}<b>${esc(o.ad)}</b><small>${esc(o.m?.hata || o.s?.uyari?.split(',')[0] || 'Çevrimdışı')}</small></div>`).join('');
    const kal = g.items.length - sorun.length;
    return `<section class="g lv-${g.lv} ${acik ? 'acik' : ''} ${g.id === secik ? 'sec-g' : ''}" data-g="${esc(g.id)}"><button class="g-h" aria-expanded="${acik}" data-sec="${esc(g.id)}">${CHEV}${sn(g.lv)}<span class="g-ad">${esc(g.ad)}</span><span class="g-sy">${sayi(g)}</span></button>${acik ? `<div class="g-b">${dar() ? kartlar(g) : ic}</div>` : ''}</section>`;
  }).join('')}</section>`;
}
function sahne() {
  const g = grupBul(S.sec); if (!g) return '';
  const bas = `<div class="s-bas lv-${g.lv}" id="s-bas">${sn(g.lv, 16)}<h2>${esc(g.ad)}</h2><span class="g-sy">${sayi(g)}</span>${bar(g.c)}</div>`;
  const hubk = `<div class="s-kol s-hub">${hub()}${kanalPano()}</div>`;
  if (g.kind !== 'mon') return `<div class="sahne" id="sahne"><div class="sahne-g iki"><div class="s-kol">${bas}${kartlar({ ...g, id: g.id })}</div>${hubk}</div><svg class="ciz" id="ciz"></svg></div>`;
  const bolge = [...new Set(P.map((p) => p.bolge))];
  const goren = new Set(g.items.flatMap((o) => o.m.gorur || []));
  const nk = bolge.map((b) => `<div class="s-ba">${b}</div>` + P.filter((p) => p.bolge === b).map((p) => `<div class="nokta ${!p.online ? 'c-off' : goren.has(p.kod) ? 'c-bad' : ''}" data-n="${p.kod}"><b>${p.ad}</b><small>${!p.online ? 'oy yok' : goren.has(p.kod) ? 'hata görüyor' : 'temiz'}</small></div>`).join('')).join('');
  return `<div class="sahne" id="sahne"><div class="sahne-g"><div class="s-kol">${bas}${kartlar(g)}</div><div class="s-kol" id="s-nok">${nk}</div>${hubk}</div><svg class="ciz" id="ciz"></svg></div>`;
}
function tuvalB() {
  if (!S.sec || !grupBul(S.sec)) { const ilk = [...gruplar('mon'), ...gruplar('pro'), ...gruplar('srv')].sort((a, b) => SIRA[b.lv] - SIRA[a.lv])[0]; S.sec = ilk.id; }
  return `<div class="b-duz" id="tuval"><div class="b-sol">${solBolum('mon')}${solBolum('pro')}${solBolum('srv')}</div>${dar() ? '' : sahne()}${dar() ? '' : '<svg class="ciz" id="ciz-dis"></svg>'}</div>`;
}

// --- Bağlantılar ------------------------------------------------------------------------------
function kose(pts, r = 5) { // dik köşeli yol, yuvarlatılmış
  let d = `M${pts[0][0]} ${pts[0][1]}`;
  for (let i = 1; i < pts.length - 1; i++) {
    const [a, b, c] = [pts[i - 1], pts[i], pts[i + 1]];
    const u = (p, q2) => { const dx = q2[0] - p[0], dy = q2[1] - p[1], l = Math.hypot(dx, dy) || 1; const rr = Math.min(r, l / 2); return [p[0] + (dx / l) * rr, p[1] + (dy / l) * rr]; };
    const g = u(b, a), h = u(b, c);
    d += ` L${g[0]} ${g[1]} Q${b[0]} ${b[1]} ${h[0]} ${h[1]}`;
  }
  const z = pts[pts.length - 1];
  return `${d} L${z[0]} ${z[1]}`;
}
const iz = (d, cls, ad) => ({ d, cls, ad });
function ciz(svg, yollar) {
  yollar.sort((a, b) => ['l-off', 'l-ok', 'l-warn', 'l-bad'].indexOf(a.cls) - ['l-off', 'l-ok', 'l-warn', 'l-bad'].indexOf(b.cls));
  svg.innerHTML = yollar.map((y) => `<path class="${y.cls}" d="${y.d}"/>`).join('') + yollar.map((y) => `<path class="vuru" d="${y.d}"><title>${esc(y.ad)}</title></path>`).join('');
}
function hatlarA() {
  const svg = document.getElementById('ciz'), kok = document.getElementById('tuval').getBoundingClientRect();
  const R = (e) => { const r = e.getBoundingClientRect(); return { l: r.left - kok.left, r: r.right - kok.left, t: r.top - kok.top, b: r.bottom - kok.top, cx: (r.left + r.right) / 2 - kok.left, cy: (r.top + r.bottom) / 2 - kok.top }; };
  const hub = R(document.getElementById('hub')), yol = [];
  const adim = S.v === 'c' ? 3.5 : 5;
  for (const [kind, yon] of [['mon', 'asagi'], ['pro', 'yukari'], ['srv', 'yukari']]) {
    const pano = document.querySelector(`[data-pano="${kind}"]`), pr = R(pano), parcalar = [];
    pano.querySelectorAll('.kol > div').forEach((kol) => {
      const gs = [...kol.querySelectorAll('.g')], kr = R(kol), n = gs.length;
      gs.forEach((el, i) => {
        const h = R(el.querySelector('.g-h')), lane = yon === 'asagi' ? n - 1 - i : i;
        const x = R(el).r + 6 + lane * adim, g = gruplar(kind).find((y) => y.id === el.dataset.g);
        parcalar.push({ x, h, g, el: R(el) });
      });
    });
    parcalar.sort((a, b) => a.x - b.x);
    const asagi = yon === 'asagi', sh = asagi ? hub.t : hub.b;
    const baslangic = kind === 'mon' ? hub.l + 16 : (kind === 'pro' ? hub.l + 12 : hub.cx + 16);
    const genis = kind === 'mon' ? hub.r - hub.l - 32 : (hub.r - hub.l) / 2 - 28;
    parcalar.forEach((p, i) => { p.hx = baslangic + (i + .5) / parcalar.length * genis; });
    // Ray: panele en uzak hat göbeğe en yakın rayı alır, hatlar birbirini kesmez.
    const sol = parcalar.filter((p) => p.x < p.hx), sag = parcalar.filter((p) => p.x >= p.hx).reverse();
    for (const tk of [sol, sag]) tk.forEach((p, i) => {
      const ry = sh + (asagi ? -1 : 1) * (8 + i * Math.min(4, 22 / tk.length));
      yol.push(iz(kose([[p.el.r, p.h.cy], [p.x, p.h.cy], [p.x, ry], [p.hx, ry], [p.hx, sh]], 4), `l-${p.g.lv}`, `${p.g.ad}: ${{ bad: 'hata var', warn: 'uyarı var', ok: 'hepsi tamam', off: 'duraklatıldı' }[p.g.lv]}`));
    });
  }
  const k = R(document.querySelector('[data-pano="kan"]'));
  yol.push(iz(kose([[hub.r, hub.cy], [k.cx, hub.cy], [k.cx, k.b]], 6), 'l-ok', 'Bildirim kanalları'));
  ciz(svg, yol);
}
function hatlarB() {
  const dis = document.getElementById('ciz-dis'), sah = document.getElementById('sahne'), kokD = document.getElementById('tuval').getBoundingClientRect();
  const sat = document.querySelector('.sec-g .g-h'), bas = document.getElementById('s-bas'), g = grupBul(S.sec);
  const rel = (e, k) => { const r = e.getBoundingClientRect(); return { l: r.left - k.left, r: r.right - k.left, t: r.top - k.top, b: r.bottom - k.top, cy: (r.top + r.bottom) / 2 - k.top, cx: (r.left + r.right) / 2 - k.left }; };
  const a = rel(sat, kokD), b = rel(bas, kokD);
  ciz(dis, [iz(`M${a.r} ${a.cy} C${a.r + 26} ${a.cy} ${b.l - 26} ${b.cy} ${b.l} ${b.cy}`, `l-${g.lv}`, g.ad)]);
  const svg = document.getElementById('ciz'), kok = sah.getBoundingClientRect(), hubR = rel(document.getElementById('hub'), kok), s = rel(bas, kok), yol = [];
  const kan = rel(sah.querySelector('[data-pano="kan"]'), kok);
  yol.push(iz(`M${hubR.cx} ${hubR.b} L${hubR.cx} ${kan.t}`, 'l-ok', 'Bildirim'));
  const nok = [...sah.querySelectorAll('.nokta')];
  if (!nok.length) yol.push(iz(`M${s.r} ${s.cy} C${s.r + 40} ${s.cy} ${hubR.l - 40} ${hubR.cy} ${hubR.l} ${hubR.cy}`, `l-${g.lv}`, g.ad));
  nok.forEach((e, i) => {
    const n = rel(e, kok), c = e.classList.contains('c-off') ? 'l-off' : e.classList.contains('c-bad') ? 'l-bad' : 'l-ok';
    const sy = s.cy + (i - (nok.length - 1) / 2) * 3;
    yol.push(iz(`M${s.r} ${sy} C${s.r + 34} ${sy} ${n.l - 34} ${n.cy} ${n.l} ${n.cy}`, c, `${g.ad} > ${e.dataset.n}`));
    const hy = hubR.t + 8 + (i + .5) / nok.length * (hubR.b - hubR.t - 16);
    yol.push(iz(`M${n.r} ${n.cy} C${n.r + 34} ${n.cy} ${hubR.l - 34} ${hy} ${hubR.l} ${hy}`, c === 'l-off' ? 'l-off' : 'l-ok', `${e.dataset.n} > panel`));
  });
  ciz(svg, yol);
}

// --- Kabuk ------------------------------------------------------------------------------------
function ciz_tumu() {
  const t0 = performance.now();
  const kok = document.getElementById('kok'), sec = (ad, d, v) => `<button class="${d === v ? 'on' : ''}" ${ad}="${v}">${String(v).toUpperCase()}</button>`;
  const v = S.v;
  const nav = ['Genel bakış', 'Monitörler', 'Canlı topoloji', 'Olaylar', 'Sunucular', 'Durum sayfaları', 'Bakım', 'Ayarlar'];
  kok.innerHTML = `${dar() ? '<div class="ust-c"><i>K</i>Bekci</div>' : ''}<div class="kabuk">
    <aside class="yan"><div class="logo"><i>K</i>Bekci</div>${nav.map((n) => `<a class="${n === 'Canlı topoloji' ? 'on' : ''}">${n}</a>`).join('')}</aside>
    <main class="icerik"><div class="bas"><h1>Canlı topoloji<span>.</span></h1>
      <div class="ara"><span class="sec" aria-label="Varyant">${['a', 'b', 'c'].map((x) => sec('data-v', v, x)).join('')}</span><span class="sec" aria-label="Senaryo">${[9, 100, 500].map((x) => sec('data-n', S.n, x)).join('')}</span><span class="canli">Canlı, 3 sn önce</span></div></div>
      ${cizelge()}${v === 'b' ? tuvalB() : tuvalA()}${v === 'b' && dar() ? '' : ''}</main></div>
    ${dar() ? '<nav class="tab"><span>Genel</span><span>Monitörler</span><span class="on">Topoloji</span><span>Daha fazla</span></nav>' : ''}`;
  if (!dar()) (v === 'b' ? hatlarB : hatlarA)();
  S.ms = Math.round(performance.now() - t0);
}
document.addEventListener('click', (e) => {
  const t = e.target.closest('button'); if (!t) return; const d = t.dataset;
  if (d.ac) { cevir(grupBul(d.ac)); } else if (d.sec) { if (S.sec === d.sec || dar()) cevir(grupBul(d.sec)); S.sec = d.sec; if (!acikMi(grupBul(d.sec)) && !dar()) cevir(grupBul(d.sec)); }
  else if (d.by) { const [k, b] = d.by.split(':'); S.by[k] = b; S.sec = null; }
  else if (d.tum) { S.tum[d.tum] = 1; }
  else if (d.v) { q.set('v', d.v); location.search = q; return; } else if (d.n) { q.set('n', d.n); location.search = q; return; }
  else if (d.sorun !== undefined) { const x = sorunlar()[+d.sorun], g = gruplar(x.tip).find((y) => y.items.includes(x.o)); S.sec = g.id; if (!acikMi(g)) cevir(g); }
  else return;
  ciz_tumu();
});
let zm; addEventListener('resize', () => { clearTimeout(zm); zm = setTimeout(ciz_tumu, 80); });
if (q.has('temiz')) localStorage.clear();
ciz_tumu();
if (document.fonts?.ready) document.fonts.ready.then(() => !dar() && (S.v === 'b' ? hatlarB : hatlarA)());

// Taşma denetimi: ?denetle=1 sonuçları #rapor içine yazar (headless dump-dom ile okunur).
if (q.has('denetle')) {
  const sorun = [];
  document.querySelectorAll('.s-ad,.s-so,.s-ha,.g-ad,.g-sy,.kart b,.kart small,.kart .kh,.nokta b,.nokta small,.kanal b,.hub span,.pano-b h3,.pano-b .say,.b-sa b').forEach((e) => { if (e.scrollWidth > e.clientWidth + 1 && getComputedStyle(e).display !== 'inline') sorun.push(`KIRPIK ${e.className || e.tagName}: ${e.textContent.slice(0, 40)}`); });
  document.querySelectorAll('.g-h,.pano-b,.s-sat,.sat2,.s-bas,.bas,.icerik').forEach((e) => { if (e.scrollWidth > e.clientWidth + 1) sorun.push(`TAŞMA ${e.className}: ${e.scrollWidth}>${e.clientWidth} ${e.textContent.slice(0, 30)}`); });
  if (document.documentElement.scrollWidth > innerWidth) sorun.push(`YATAY KAYDIRMA ${document.documentElement.scrollWidth}>${innerWidth}`);
  const r = document.createElement('pre'); r.id = 'rapor'; r.hidden = true;
  r.textContent = `w=${innerWidth} h=${document.documentElement.scrollHeight} ms=${S.ms} dugum=${document.querySelectorAll('*').length} sorun=${sorun.length}\n` + sorun.slice(0, 25).join('\n');
  document.body.append(r);
  if (parent !== window) parent.document.body.append(r.cloneNode(true));
}
