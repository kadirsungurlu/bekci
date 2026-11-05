// Taslak tohum verisi. Şekil, stash'teki '700' /api/topology?view=summary sözleşmesini izler
// (tags, locs, loc_ok, loc_total, customer). Ek alanlar taslak.md'de listelidir:
// sorunlu monitörde locations (hangi nokta görüyor), noktada bolge/saglayici, sunucuda rol/yer.
const NOKTALAR = [
  ['İstanbul-1', 'IST1', 'Türkiye', 'Türk Telekom'], ['İstanbul-2', 'IST2', 'Türkiye', 'Turkcell DC'],
  ['Ankara', 'ANK', 'Türkiye', 'Türk Telekom'], ['İzmir', 'IZM', 'Türkiye', 'Hetzner TR'],
  ['Frankfurt', 'FRA', 'Avrupa', 'Hetzner'], ['Amsterdam', 'AMS', 'Avrupa', 'DigitalOcean'],
  ['Londra', 'LON', 'Avrupa', 'DigitalOcean'], ['Helsinki', 'HEL', 'Avrupa', 'Hetzner'],
  ['Virginia', 'VIR', 'Amerika', 'AWS'], ['Singapur', 'SIN', 'Asya', 'AWS'],
];
const AYRILMA = { 'Türkiye': "Türkiye'nin tamamı", 'Avrupa': "Avrupa'nın tamamı", 'Amerika': 'Amerika', 'Asya': 'Asya' };
const GRUPLAR = {
  'Ödeme': ['Ödeme API', '3D Secure dönüşü', 'Sanal POS', 'İade servisi', 'Fatura API', 'Abonelik tahsilat', 'Kart saklama', 'Mutabakat işi', 'Ödeme webhook'],
  'Müşteri portalı': ['Portal giriş', 'Portal ana sayfa', 'Hesabım', 'Sipariş takibi', 'Destek talebi', 'Canlı destek', 'Bildirim tercihleri', 'Belge indirme', 'Portal arama', 'Şifre sıfırlama', 'Kayıt formu', 'Sadakat puanı', 'Portal mobil API'],
  'Ana API': ['Kimlik API', 'Ürün API', 'Stok API', 'Sepet API', 'Sipariş API', 'Fiyat API', 'Kargo API', 'Kampanya API', 'Arama API', 'Yorum API', 'Rapor API', 'Dosya API', 'Webhook dağıtıcı', 'GraphQL ağ geçidi', 'Oran sınırlayıcı'],
  'Web siteleri': ['akinlojistik.com', 'yildizmarket.com', 'denizsigorta.com.tr', 'kuzeyenerji.com', 'atlasmedya.com', 'blog.atlasmedya.com', 'kariyer.akinlojistik.com', 'magaza.yildizmarket.com', 'yatirimci.kuzeyenerji.com', 'acente.denizsigorta.com.tr', 'haber.atlasmedya.com', 'bayi.yildizmarket.com', 'iletisim.kuzeyenerji.com', 'en.akinlojistik.com', 'de.akinlojistik.com', 'kampanya.yildizmarket.com', 'dokuman.denizsigorta.com.tr', 'arsiv.atlasmedya.com'],
  'CDN ve medya': ['CDN görsel uç', 'CDN statik dosya', 'Video kodlama', 'Görsel boyutlandırma', 'Medya yükleme', 'CDN köken', 'HLS akışı', 'Küçük resim', 'Font sunucusu'],
  'DNS ve alan adı': ['ns1.kadir.app', 'ns2.kadir.app', 'DNS kök kaydı', 'MX kaydı', 'SPF kaydı', 'DKIM kaydı', 'Alan adı süresi', 'TLS sertifikası'],
  'E-posta': ['SMTP çıkış', 'IMAP', 'Webmail', 'Toplu gönderim', 'Geri dönen posta', 'Spam süzgeci', 'Posta kuyruğu'],
  'Veritabanı uçları': ['Postgres birincil', 'Postgres okuma-1', 'Postgres okuma-2', 'Redis oturum', 'Redis önbellek', 'Elasticsearch', 'ClickHouse', 'RabbitMQ', 'MinIO'],
  'Dahili araçlar': ['GitLab', 'Harbor kayıt', 'Grafana', 'Vault', 'Jenkins', 'Sentry', 'Metabase', 'Keycloak', 'Wiki', 'Argo CD', 'Eski CRM', 'Test ortamı'],
};
const TUR = { 'Ödeme': ['HTTP', 'HTTP', 'TLS'], 'Müşteri portalı': ['HTTP'], 'Ana API': ['HTTP', 'HTTP', 'TCP'], 'Web siteleri': ['HTTP', 'HTTP', 'Ping'], 'CDN ve medya': ['HTTP', 'Ping'], 'DNS ve alan adı': ['DNS', 'DNS', 'TLS'], 'E-posta': ['TCP', 'TCP', 'HTTP'], 'Veritabanı uçları': ['SQL', 'TCP'], 'Dahili araçlar': ['HTTP', 'Push'] };
const MUSTERI = ['Akın Lojistik', 'Yıldız Market', 'Deniz Sigorta', 'Kuzey Enerji', 'Atlas Medya', 'Ortak altyapı'];
const ROLLER = { 'Web': ['web-ist', 12], 'Uygulama': ['app-ist', 8], 'Veritabanı': ['db-ana', 6], 'Kuyruk': ['mq-fra', 4], 'Yedek': ['yedek-hel', 4], 'Kenar': ['kenar-ams', 6] };
const KANALLAR = [['Nöbet e-postası', 'E-posta', '7 dk'], ['Telegram nöbet', 'Telegram', '7 dk'], ['Slack #uyari', 'Slack', '3 dk'], ['Web Push', 'Tarayıcı', '3 dk'], ['ntfy mobil', 'ntfy', '18 dk']];
const SORUN = [
  { ad: 'Ödeme API', gorur: ['IST1', 'IST2', 'ANK', 'IZM'], hata: 'HTTP 503', sure: '7 dk' },
  { ad: 'CDN görsel uç', gorur: ['FRA', 'AMS'], hata: 'Zaman aşımı', sure: '3 dk' },
  { ad: 'Portal giriş', gorur: 'hepsi', hata: 'HTTP 502', sure: '18 dk' },
];

function veriUret(n) {
  let tohum = 7; const rnd = (m) => (tohum = (tohum * 1103515245 + 12345) & 0x7fffffff) % m;
  const kac = n <= 9 ? 3 : 10;
  const secilen = kac === 3 ? [0, 4, 8] : [...Array(10).keys()];
  const probes = secilen.map((i, s) => {
    const [ad, kod, bolge, saglayici] = NOKTALAR[i];
    return { id: s + 1, ad, kod, bolge, saglayici, online: kod !== (kac === 3 ? 'VIR' : 'SIN'), rtt: 12 + rnd(90), son: '12 dk önce' };
  });
  const canli = probes.filter((p) => p.online);
  const monitors = []; const carpan = n >= 1000 ? 10 : n >= 500 ? 5 : 1;
  const adlar = n <= 9 ? { 'Ödeme': GRUPLAR['Ödeme'].slice(0, 3), 'Ana API': GRUPLAR['Ana API'].slice(0, 2), 'CDN ve medya': GRUPLAR['CDN ve medya'].slice(0, 2), 'Web siteleri': GRUPLAR['Web siteleri'].slice(0, 2) } : GRUPLAR;
  let id = 0;
  for (const [etiket, liste] of Object.entries(adlar)) {
    for (let k = 1; k <= carpan; k++) liste.forEach((ad0, i) => {
      const ad = k > 1 ? `${ad0} ${k}` : ad0;
      const sorun = n > 9 || ad !== 'Portal giriş' ? SORUN.find((s) => s.ad === ad) : null;
      const m = { id: ++id, ad, tags: [etiket], tur: TUR[etiket][i % TUR[etiket].length], musteri: MUSTERI[(i + (etiket.length % 6)) % 6], status: 'up', ping: 18 + rnd(240), konum: probes[rnd(probes.length)].bolge, loc_total: probes.length, loc_ok: canli.length };
      if (etiket === 'Dahili araçlar' && (ad0 === 'Eski CRM' || ad0 === 'Test ortamı')) m.status = 'paused';
      if (sorun) {
        const gorur = sorun.gorur === 'hepsi' ? canli.map((p) => p.kod) : sorun.gorur.filter((k2) => canli.some((p) => p.kod === k2));
        Object.assign(m, { status: 'down', ping: null, gorur: gorur.length ? gorur : [canli[0].kod], hata: sorun.hata, sure: sorun.sure });
        m.loc_ok = canli.length - m.gorur.length;
      }
      monitors.push(m);
    });
  }
  const servers = []; let sid = 0;
  const rolAdet = n <= 9 ? { 'Web': 1, 'Veritabanı': 1, 'Yedek': 1 } : Object.fromEntries(Object.entries(ROLLER).map(([r, v]) => [r, v[1]]));
  for (const [rol, adet] of Object.entries(rolAdet)) for (let i = 1; i <= adet; i++) {
    const on = ROLLER[rol][0]; const yer = rol === 'Web' && i > 7 ? 'Frankfurt' : { 'Web': 'İstanbul', 'Uygulama': 'İstanbul', 'Veritabanı': 'İstanbul', 'Kuyruk': 'Frankfurt', 'Yedek': 'Helsinki', 'Kenar': 'Amsterdam' }[rol];
    const s = { id: ++sid, ad: `${rol === 'Web' && i > 7 ? 'web-fra' : on}-${String(i).padStart(2, '0')}`, rol, yer, cpu: 6 + rnd(44), ram: 24 + rnd(48), disk: 18 + rnd(50), uyari: null };
    if (rol === 'Veritabanı' && i === (n <= 9 ? 1 : 2)) { s.disk = 91; s.uyari = 'Disk %91, 3 günde dolar'; s.sure = '2 sa'; s.olcut = 'disk'; }
    if (n > 9 && s.ad === 'web-fra-08') { s.cpu = 96; s.uyari = 'CPU %96, eşik %90'; s.sure = '11 dk'; s.olcut = 'cpu'; }
    servers.push(s);
  }
  return { probes, monitors, servers, kanallar: KANALLAR };
}
