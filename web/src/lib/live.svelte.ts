// Monitör listesi + özet durumu ve canlı olay akışı (SSE).
//
// Liste `$state.raw` tutulur: her monitör değişmez bir nesnedir ve bir kontrol sonucu
// geldiğinde yalnızca o monitörün nesnesi yenisiyle değiştirilir. Böylece keyed each
// bloğunda sadece değişen satır yeniden çizilir; yüzlerce monitörde de hızlı kalır.

import { api, errorMessage, PING_DOWN, PING_NONE, STATUS_DOWN, STATUS_MAINTENANCE, STATUS_UP, type BeatEvent, type MonitorView, type ProbeEvent, type ServerView, type Summary } from './api';
import { i18n } from './i18n/locale.svelte';

type BeatListener = (b: BeatEvent) => void;
type MaintListener = (id: number) => void;
type ProbeListener = (p: ProbeEvent) => void;
type ServerListener = (s: ServerView) => void;

/** Listedeki küçük yanıt süresi grafiği için tutulan son kontrol sayısı (sunucuyla aynı). */
const RECENT_PINGS = 30;

/** Yeni kontrol sonucunu son kontroller listesine ekler (sunucunun RecentPings kuralıyla). */
function appendPing(list: number[], b: BeatEvent): number[] {
  const v = b.status === STATUS_DOWN ? PING_DOWN : b.ping >= 0 ? b.ping : PING_NONE;
  const next = list.length >= RECENT_PINGS ? list.slice(list.length - RECENT_PINGS + 1) : list.slice();
  next.push(v);
  return next;
}

class Live {
  monitors = $state.raw<MonitorView[]>([]);
  summary = $state.raw<Summary | null>(null);
  loaded = $state(false);
  loadError = $state('');
  connected = $state(false);

  index = $derived(new Map(this.monitors.map((m) => [m.id, m])));

  private es: EventSource | null = null;
  private running = false;
  private refreshTimer: ReturnType<typeof setInterval> | undefined;
  private reconnectTimer: ReturnType<typeof setTimeout> | undefined;
  private flushTimer: ReturnType<typeof setTimeout> | undefined;
  private summaryTimer: ReturnType<typeof setTimeout> | undefined;
  private pending = new Map<number, BeatEvent>();
  private listeners = new Set<BeatListener>();
  private maintListeners = new Set<MaintListener>();
  private probeListeners = new Set<ProbeListener>();
  private serverListeners = new Set<ServerListener>();
  private reconnectListeners = new Set<() => void>();
  private resumeListeners = new Set<() => void>();
  private statsResetListeners = new Set<(id: number) => void>();
  private locationListeners = new Set<(id: number) => void>();
  private softRefreshTimer: ReturnType<typeof setTimeout> | undefined;
  private maintTimer: ReturnType<typeof setTimeout> | undefined;
  private lastRefresh = 0;
  private everConnected = false;
  /** Liste isteklerinin sıra numarası: geç dönen eski yanıt yenisinin üstüne yazmasın. */
  private refreshSeq = 0;
  private appliedSeq = 0;
  /** Sayfanın gizlendiği an (iPhone'da uygulama arka plana alındığında). */
  private hiddenAt = 0;

  start() {
    if (this.running) return;
    this.running = true;
    this.refresh();
    this.connect();
    this.refreshTimer = setInterval(() => this.refresh(), 60_000);
    document.addEventListener('visibilitychange', this.onVisible);
  }

  stop() {
    this.running = false;
    this.es?.close();
    this.es = null;
    clearInterval(this.refreshTimer);
    clearTimeout(this.reconnectTimer);
    clearTimeout(this.flushTimer);
    clearTimeout(this.summaryTimer);
    clearTimeout(this.maintTimer);
    clearTimeout(this.softRefreshTimer);
    document.removeEventListener('visibilitychange', this.onVisible);
    this.pending.clear();
    this.monitors = [];
    this.summary = null;
    this.loaded = false;
    this.loadError = '';
    this.connected = false;
    this.everConnected = false;
    this.hiddenAt = 0;
  }

  private onVisible = () => {
    if (!this.running) return;
    if (document.hidden) {
      this.hiddenAt = Date.now();
      return;
    }
    const away = this.hiddenAt ? Date.now() - this.hiddenAt : 0;
    this.hiddenAt = 0;
    if (away > 30_000) {
      // Uzun süre arka planda kaldı (ör. iPhone'da uygulamaya geri dönüldü): SSE
      // bağlantısı sessizce kopmuş ve olaylar kaçmış olabilir. Her şeyi tazele.
      this.resume();
    } else if (Date.now() - this.lastRefresh > 30_000) this.refresh();
  };

  /** Elle yenileme (ana ekran uygulamasındaki yenile düğmesi): her şeyi tazeler. */
  resync() {
    if (this.running) this.resume();
  }

  /** Kaçan olayları tamamlamak için liste ve açık ekranlar yeniden yüklenir. */
  private resume() {
    this.refresh();
    this.emit(this.resumeListeners, undefined);
    // Arka planda kopmuş ama tarayıcının fark etmediği bağlantıyı yenile.
    if (this.es && this.es.readyState !== EventSource.OPEN) {
      this.es.close();
      this.es = null;
      clearTimeout(this.reconnectTimer);
      this.connect();
    }
  }

  async refresh() {
    this.lastRefresh = Date.now();
    const seq = ++this.refreshSeq;
    try {
      const [list, sum] = await Promise.all([api.monitors(), api.summary()]);
      // Daha yeni bir istek zaten uygulandıysa bu eski yanıtı at.
      if (!this.running || seq < this.appliedSeq) return;
      this.appliedSeq = seq;
      this.monitors = this.mergeSnapshot(list);
      this.summary = sum;
      this.loaded = true;
      this.loadError = '';
    } catch (e) {
      if (!this.loaded && seq >= this.appliedSeq) this.loadError = errorMessage(e);
    }
  }

  /**
   * Sunucudan gelen anlık listeyi yazar; ama istek yoldayken canlı akıştan daha yeni
   * bir kontrol sonucu gelmiş monitörün durumunu eski anlık görüntüyle geri almaz.
   */
  private mergeSnapshot(list: MonitorView[]): MonitorView[] {
    if (!this.monitors.length) return list;
    const cur = this.index;
    return list.map((n) => {
      const c = cur.get(n.id);
      if (!c || !(c.last_check_at > n.last_check_at)) return n;
      return {
        ...n,
        status: c.status,
        last_check_at: c.last_check_at,
        last_ping_ms: c.last_ping_ms,
        last_message: c.last_message,
        last_change_at: c.last_change_at,
        cert_expires_at: c.cert_expires_at || n.cert_expires_at,
        in_maintenance: c.in_maintenance,
        open_incident_id: c.status === STATUS_UP ? null : (n.open_incident_id ?? c.open_incident_id),
        pings: c.pings ?? n.pings,
      };
    });
  }

  async refreshSummary() {
    try {
      const sum = await api.summary();
      if (this.running) this.summary = sum;
    } catch {
      /* bir sonraki yenilemede tekrar denenir */
    }
  }

  private refreshSummarySoon() {
    clearTimeout(this.summaryTimer);
    this.summaryTimer = setTimeout(() => this.refreshSummary(), 400);
  }

  private connect() {
    if (!this.running) return;
    // Dil: kayıtlı tercih yoksa sunucu kontrol mesajlarını bu dile çevirir
    // (EventSource başlık gönderemez).
    const es = new EventSource('/api/events?lang=' + i18n.locale);
    this.es = es;
    es.onopen = () => {
      this.connected = true;
      // Bağlantı koptuysa arada kaçan değişiklikleri almak için listeyi yenile.
      if (this.everConnected) {
        this.refresh();
        this.emit(this.reconnectListeners, undefined);
        this.emit(this.resumeListeners, undefined);
      }
      this.everConnected = true;
    };
    es.onerror = () => {
      this.connected = false;
      if (es.readyState === EventSource.CLOSED) {
        // Tarayıcı kendiliğinden yeniden bağlanmayacak (ör. oturum düştü): biraz
        // bekleyip listeyi yenile (401 ise giriş ekranına gider) ve yeniden bağlan.
        clearTimeout(this.reconnectTimer);
        this.reconnectTimer = setTimeout(async () => {
          if (!this.running) return;
          await this.refresh();
          if (this.running && this.es === es) this.connect();
        }, 10_000);
      }
    };
    es.onmessage = (ev) => {
      let msg: { type?: string; data?: unknown };
      try {
        msg = JSON.parse(ev.data);
      } catch {
        return;
      }
      if (msg.type === 'beat' && msg.data) this.handleBeat(msg.data as BeatEvent);
      else if (msg.type === 'maintenance') this.handleMaintenance((msg.data as { maintenance_id?: number })?.maintenance_id ?? 0);
      else if (msg.type === 'probe' && msg.data) this.handleProbe(msg.data as ProbeEvent);
      else if (msg.type === 'server' && msg.data) this.emit(this.serverListeners, msg.data as ServerView);
      else if (msg.type === 'locations') {
        // Bir konumun durumu değişti (ör. ilk sonucu geldi); genel durum aynı kalmış olabilir.
        this.emit(this.locationListeners, (msg.data as { monitor_id?: number })?.monitor_id ?? 0);
      } else if (msg.type === 'stats_reset') {
        const id = (msg.data as { monitor_id?: number })?.monitor_id ?? 0;
        this.emit(this.statsResetListeners, id);
        this.refreshSoon();
      }
    };
  }

  /** Listeyi kısa bir gecikmeyle (art arda gelen olayları birleştirerek) yeniler. */
  private refreshSoon(ms = 600) {
    clearTimeout(this.softRefreshTimer);
    this.softRefreshTimer = setTimeout(() => this.refresh(), ms);
  }

  /** Çok konumlu monitörde bir konumun durumu değiştiğinde çağrılır (monitör kimliğiyle). */
  onLocations(fn: (id: number) => void): () => void {
    this.locationListeners.add(fn);
    return () => this.locationListeners.delete(fn);
  }

  /** Bir monitörün istatistikleri sıfırlandığında çağrılır (monitör kimliğiyle). */
  onStatsReset(fn: (id: number) => void): () => void {
    this.statsResetListeners.add(fn);
    return () => this.statsResetListeners.delete(fn);
  }

  /** Bakım penceresi değişti: hangi monitörlerin bakımda olduğu değişmiş olabilir. */
  private handleMaintenance(id: number) {
    for (const fn of this.maintListeners) {
      try {
        fn(id);
      } catch {
        /* dinleyici hatası akışı bozmasın */
      }
    }
    clearTimeout(this.maintTimer);
    this.maintTimer = setTimeout(() => this.refresh(), 300);
  }

  /** Kontrol noktası çevrimiçi/çevrimdışı oldu. */
  private handleProbe(p: ProbeEvent) {
    for (const fn of this.probeListeners) {
      try {
        fn(p);
      } catch {
        /* dinleyici hatası akışı bozmasın */
      }
    }
  }

  private emit<T>(set: Set<(v: T) => void>, v: T) {
    for (const fn of set) {
      try {
        fn(v);
      } catch {
        /* dinleyici hatası akışı bozmasın */
      }
    }
  }

  /** Sunucu ajanından yeni örnek veya durum değişimi geldiğinde (liste biçimi) çağrılır. */
  onServer(fn: ServerListener): () => void {
    this.serverListeners.add(fn);
    return () => this.serverListeners.delete(fn);
  }

  /** Canlı bağlantı koptuktan sonra yeniden kurulduğunda (kaçan olayları tamamlamak için). */
  onReconnect(fn: () => void): () => void {
    this.reconnectListeners.add(fn);
    return () => this.reconnectListeners.delete(fn);
  }

  /**
   * Kaçırılmış olabilecek değişiklikler için ekranın verisini yeniden yüklemesi
   * gerektiğinde çağrılır: canlı bağlantı koptuktan sonra yeniden kurulunca ve sayfa
   * 30 sn'den uzun süre arka planda kaldıktan sonra öne gelince. Abonelik bitirme
   * fonksiyonu döner.
   */
  onResume(fn: () => void): () => void {
    this.resumeListeners.add(fn);
    return () => this.resumeListeners.delete(fn);
  }

  /** Kontrol noktası durumu değiştiğinde çağrılır; aboneliği bitiren fonksiyon döner. */
  onProbe(fn: ProbeListener): () => void {
    this.probeListeners.add(fn);
    return () => this.probeListeners.delete(fn);
  }

  /** Bakım penceresi eklendiğinde/değiştiğinde çağrılır; aboneliği bitiren fonksiyon döner. */
  onMaintenance(fn: MaintListener): () => void {
    this.maintListeners.add(fn);
    return () => this.maintListeners.delete(fn);
  }

  private handleBeat(b: BeatEvent) {
    for (const fn of this.listeners) {
      try {
        fn(b);
      } catch {
        /* dinleyici hatası akışı bozmasın */
      }
    }
    this.pending.set(b.monitor_id, b);
    if (!this.flushTimer) this.flushTimer = setTimeout(() => this.flush(), 250);
  }

  private flush() {
    this.flushTimer = undefined;
    if (this.pending.size === 0) return;
    let statusChanged = false;
    let needIncident = false;
    const beats = this.pending;
    this.pending = new Map();
    this.monitors = this.monitors.map((m) => {
      const b = beats.get(m.id);
      if (!b) return m;
      if (b.status !== m.status) statusChanged = true;
      // Yeni kesintinin olay kimliği canlı olayda yok: liste bir kez yenilenir.
      // Düzelen monitörün olayı kapanmıştır.
      let incident = m.open_incident_id ?? null;
      if (b.status === STATUS_DOWN && m.status !== STATUS_DOWN && incident === null) needIncident = true;
      if (b.status === STATUS_UP) incident = null;
      return {
        ...m,
        open_incident_id: incident,
        status: b.status,
        last_check_at: b.time,
        last_ping_ms: b.ping,
        last_message: b.message,
        last_change_at: b.last_change_at,
        cert_expires_at: b.cert_expires_at || m.cert_expires_at,
        in_maintenance: b.status === STATUS_MAINTENANCE,
        pings: m.pings ? appendPing(m.pings, b) : undefined,
      };
    });
    if (needIncident) this.refreshSoon(800);
    else if (statusChanged) this.refreshSummarySoon();
  }

  /** Her kontrol sonucunda çağrılır; aboneliği bitiren fonksiyon döner. */
  onBeat(fn: BeatListener): () => void {
    this.listeners.add(fn);
    return () => this.listeners.delete(fn);
  }

  byId(id: number): MonitorView | undefined {
    return this.index.get(id);
  }

  /** Kaydetme/durdurma sonrası sunucudan dönen monitörü listeye yazar. */
  upsert(m: MonitorView) {
    const i = this.monitors.findIndex((x) => x.id === m.id);
    if (i >= 0) {
      const list = this.monitors.slice();
      list[i] = m;
      this.monitors = list;
    } else {
      this.monitors = [...this.monitors, m];
    }
    this.refreshSummarySoon();
  }

  remove(id: number) {
    this.removeMany([id]);
  }

  /** Toplu işlem sonrası dönen monitörleri tek seferde listeye yazar. */
  upsertMany(list: MonitorView[]) {
    if (list.length === 0) return;
    const byId = new Map(list.map((m) => [m.id, m]));
    const next = this.monitors.map((m) => {
      const n = byId.get(m.id);
      if (n) byId.delete(m.id);
      return n ?? m;
    });
    this.monitors = byId.size ? [...next, ...byId.values()] : next;
    this.refreshSummarySoon();
  }

  removeMany(ids: number[]) {
    const gone = new Set(ids);
    this.monitors = this.monitors.filter((m) => !gone.has(m.id));
    this.refreshSummarySoon();
  }
}

export const live = new Live();
