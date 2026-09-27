// Monitör listesi + özet durumu ve canlı olay akışı (SSE).
//
// Liste `$state.raw` tutulur: her monitör değişmez bir nesnedir ve bir kontrol sonucu
// geldiğinde yalnızca o monitörün nesnesi yenisiyle değiştirilir. Böylece keyed each
// bloğunda sadece değişen satır yeniden çizilir; yüzlerce monitörde de hızlı kalır.

import { api, errorMessage, STATUS_MAINTENANCE, type BeatEvent, type MonitorView, type Summary } from './api';

type BeatListener = (b: BeatEvent) => void;
type MaintListener = (id: number) => void;

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
  private maintTimer: ReturnType<typeof setTimeout> | undefined;
  private lastRefresh = 0;
  private everConnected = false;

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
    document.removeEventListener('visibilitychange', this.onVisible);
    this.pending.clear();
    this.monitors = [];
    this.summary = null;
    this.loaded = false;
    this.loadError = '';
    this.connected = false;
    this.everConnected = false;
  }

  private onVisible = () => {
    if (!document.hidden && this.running && Date.now() - this.lastRefresh > 30_000) this.refresh();
  };

  async refresh() {
    this.lastRefresh = Date.now();
    try {
      const [list, sum] = await Promise.all([api.monitors(), api.summary()]);
      if (!this.running) return;
      this.monitors = list;
      this.summary = sum;
      this.loaded = true;
      this.loadError = '';
    } catch (e) {
      if (!this.loaded) this.loadError = errorMessage(e);
    }
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
    const es = new EventSource('/api/events');
    this.es = es;
    es.onopen = () => {
      this.connected = true;
      // Bağlantı koptuysa arada kaçan değişiklikleri almak için listeyi yenile.
      if (this.everConnected) this.refresh();
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
    };
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
    const beats = this.pending;
    this.pending = new Map();
    this.monitors = this.monitors.map((m) => {
      const b = beats.get(m.id);
      if (!b) return m;
      if (b.status !== m.status) statusChanged = true;
      return {
        ...m,
        status: b.status,
        last_check_at: b.time,
        last_ping_ms: b.ping,
        last_message: b.message,
        last_change_at: b.last_change_at,
        cert_expires_at: b.cert_expires_at || m.cert_expires_at,
        in_maintenance: b.status === STATUS_MAINTENANCE,
      };
    });
    if (statusChanged) this.refreshSummarySoon();
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
    this.monitors = this.monitors.filter((m) => m.id !== id);
    this.refreshSummarySoon();
  }
}

export const live = new Live();
