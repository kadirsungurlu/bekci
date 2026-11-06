// Gerçekçi topoloji yanıtı üretir (GET /api/topology?view=summary biçimi). Görsel denetim ve
// ölçüm için: tarayıcıda yanıt yerine verilir, panele yazmaz. Tohumlu, her çalıştırmada aynıdır.
export function fakeTopology({ mon = 120, srv = 24, pro = 10, lang = 'tr' } = {}) {
  let seed = 42;
  const rnd = (n) => ((seed = (seed * 1103515245 + 12345) & 0x7fffffff) >> 8) % n;
  const now = Math.floor(Date.now() / 1000);
  const REG = [['Avrupa', 'Hetzner'], ['Türkiye', 'Turhost'], ['Kuzey Amerika', 'DigitalOcean'], ['Asya', 'Vultr']];
  const CITY = ['İstanbul-1', 'İstanbul-2', 'Ankara', 'İzmir', 'Frankfurt', 'Helsinki', 'Amsterdam', 'Londra', 'New York', 'Dallas', 'Toronto', 'Singapur', 'Tokyo', 'Mumbai', 'Sidney', 'Paris'];
  const probes = Array.from({ length: pro }, (_, i) => {
    const [region, provider] = REG[i % 4 === 0 ? 1 : i % 4 === 1 ? 0 : i % 4];
    const off = i === pro - 2;
    return { id: i + 1, name: CITY[i % CITY.length] + (i >= CITY.length ? `-${i}` : ''), online: !off, region, provider, panel_rtt_ms: 8 + rnd(90), late: i === 3, peers: pro - 1, peers_up: off ? 0 : pro - 2, version: '1.7.0', last_seen_at: off ? now - 780 : now - 4 };
  });
  const ROLES = ['Web', 'Veritabanı', 'Önbellek', 'Kuyruk', 'Yedek'];
  const channels = [
    ['Nöbetçi Telegram', 'telegram'], ['Operasyon e-posta', 'email'], ['Ekip Slack', 'slack'], ['Müşteri bildirimi', 'webhook'], ['Yönetici WhatsApp', 'whatsapp'], ['Discord uyarı', 'discord'],
  ].map(([name, type], i) => ({ id: i + 1, name, type, active: i !== 5, sent_24h: 40 + rnd(300), failed_24h: i === 3 ? 3 : 0, last_send: { at: now - 60 - rnd(4000), ok: i !== 3, ...(i === 3 ? { error: 'HTTP 502, 3 deneme' } : {}) } }));
  const servers = Array.from({ length: srv }, (_, i) => {
    const hot = i === 2;
    const dead = i === 7;
    return { id: i + 1, name: `${['web', 'db', 'cache', 'mq', 'yedek'][i % 5]}-${String(i + 1).padStart(2, '0')}.kadir.app`, state: dead ? 'offline' : 'online', role: ROLES[i % 5], region: REG[i % 4][0], customers: [1 + (i % 4)], channels: [1 + (i % 2), 3], cpu: hot ? 94 : 8 + rnd(60), mem: 30 + rnd(50), disk: i === 12 ? 92 : 20 + rnd(60), firing: hot ? ['cpu'] : i === 12 ? ['disk'] : [], alert: hot ? 'CPU yüksek' : dead ? 'Ajan yanıt vermiyor' : undefined, alert_since: hot || dead ? now - 2100 : undefined, version: '1.7.0', metrics_at: now - 12, last_seen_at: dead ? now - 900 : now - 12 };
  });
  const TYPES = ['http', 'http', 'http', 'tcp', 'dns', 'ping', 'postgres'];
  const tags = ['Üretim', 'Ödeme', 'Müşteri siteleri', 'Altyapı', 'API', 'Hazırlık'].map((name, i) => ({ id: i + 1, name, color: '#3b82f6' }));
  const SITES = ['ayder', 'celik', 'magaza', 'odeme', 'hesap', 'blog', 'destek', 'panel', 'api', 'cdn', 'mail', 'dosya'];
  const monitors = Array.from({ length: mon }, (_, i) => {
    const state = i % 41 === 5 ? 'down' : i % 57 === 9 ? 'partial' : i % 23 === 17 ? 'paused' : i % 97 === 50 ? 'maintenance' : 'up';
    const n = 3 + (i % Math.max(1, pro - 2));
    const loc_ids = Array.from({ length: Math.min(n, pro) }, (_, k) => ((i + k) % pro) + 1);
    if (i % 7 === 0) loc_ids.push(0);
    const offId = pro - 1;
    const loc_off = loc_ids.includes(offId) ? [offId] : [];
    const voters = loc_ids.filter((x) => x !== offId);
    const loc_bad = state === 'down' ? voters : state === 'partial' ? voters.slice(0, 1) : [];
    const ping = 12 + rnd(280);
    return {
      id: i + 1, name: `${SITES[i % SITES.length]}${i >= SITES.length ? `-${Math.floor(i / SITES.length)}` : ''}.kadir.app`, type: TYPES[i % TYPES.length], state, last_check_at: now - rnd(30), ping_ms: state === 'down' ? 0 : ping, loc_ids, loc_bad, loc_off,
      message: state === 'down' ? 'Zaman aşımı' : state === 'partial' ? 'HTTP 503' : undefined, since: state === 'down' || state === 'partial' ? now - 300 - rnd(4000) : undefined,
      series: Array.from({ length: 30 }, () => (state === 'down' ? -2 : Math.max(5, ping + rnd(40) - 20))),
      last_incident: i % 3 ? { id: i, kind: 'down', started_at: now - 86400 * 4, resolved_at: state === 'down' ? 0 : now - 86400 * 4 + 420, cause: 'Zaman aşımı', acked: false } : undefined,
      tags: [1 + (i % 6)], customers: [1 + (i % 4)], channels: [1 + (i % 3), 4 + (i % 2)], server_id: i % 4 === 3 ? undefined : 1 + (i % srv),
    };
  });
  return {
    now, admin: true, local_name: 'Ana sunucu', stats: { probes_online: pro - 1, probes_total: pro, open_incidents: 0 }, monitors, probes, servers, channels, incidents: [], tags,
    customers: ['Ayder', 'Çelik Nakliyat', 'Kadir', 'Ortak'].map((name, i) => ({ id: i + 1, name })), lang,
  };
}
