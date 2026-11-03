// Canlı topoloji verisi: GET /api/topology tek istekte gelir.
//
// Tazeleme: panelin canlı akışı (SSE, lib/live) bağlıyken kontrol sonucu,
// konum, kontrol noktası, mesh, sunucu ve olay olayları isteği tetikler
// (en sık THROTTLE_MS'de bir; art arda gelenler birleşir). Akış kopuksa
// POLL_MS'de bir yoklanır. Kanal "son gönderim" ve kontrol noktasının
// çevrimdışı olması (kalp atışı zamanından hesaplanır, akışta olay yok) için
// bağlıyken de SAFETY_MS'de bir tazelenir; kopma 5-15 sn içinde görünür. Sekme gizliyken hiçbir istek
// yapılmaz; görünür olunca hemen tazelenir.

import { api, errorMessage, type Topology } from './api';
import { live } from './live.svelte';

export const POLL_MS = 5_000;
const THROTTLE_MS = 2_000;
const SAFETY_MS = 5_000;

export class TopologyStore {
  data = $state.raw<Topology | null>(null);
  error = $state('');
  loading = $state(false);
  /** Son başarılı yanıtın alındığı an (ms). */
  updatedAt = $state(0);

  private running = false;
  private inFlight = false;
  private again = false;
  private lastFetch = 0;
  private timer: ReturnType<typeof setTimeout> | undefined;
  private tick: ReturnType<typeof setInterval> | undefined;
  private subs: (() => void)[] = [];

  start() {
    if (this.running) return;
    this.running = true;
    const soon = () => this.schedule();
    this.subs = [
      live.onBeat(soon),
      live.onLocations(soon),
      live.onProbe(soon),
      live.onMesh(soon),
      live.onServer(soon),
      live.onIncident(soon),
      live.onResume(() => this.fetch()),
      live.onReconnect(() => this.fetch()),
    ];
    document.addEventListener('visibilitychange', this.onVisible);
    // Yoklama saati: akış kopuksa POLL_MS, bağlıysa SAFETY_MS aralığında ister.
    this.tick = setInterval(() => {
      const every = live.connected ? SAFETY_MS : POLL_MS;
      if (Date.now() - this.lastFetch >= every) this.fetch();
    }, 1_000);
    this.fetch();
  }

  stop() {
    this.running = false;
    this.subs.forEach((u) => u());
    this.subs = [];
    clearTimeout(this.timer);
    clearInterval(this.tick);
    document.removeEventListener('visibilitychange', this.onVisible);
  }

  /** Olay geldi: THROTTLE_MS içinde en çok bir istek. */
  private schedule() {
    if (!this.running || this.timer !== undefined) return;
    const wait = Math.max(0, THROTTLE_MS - (Date.now() - this.lastFetch));
    this.timer = setTimeout(() => {
      this.timer = undefined;
      this.fetch();
    }, wait);
  }

  private onVisible = () => {
    if (this.running && !document.hidden) this.fetch();
  };

  async fetch() {
    if (!this.running || document.hidden) return;
    if (this.inFlight) {
      this.again = true; // yanıt gelince bir kez daha (araya giren değişiklik kaçmasın)
      return;
    }
    this.inFlight = true;
    this.lastFetch = Date.now();
    if (!this.data) this.loading = true;
    try {
      const d = await api.topology();
      if (!this.running) return;
      this.data = d;
      this.error = '';
      this.updatedAt = Date.now();
    } catch (e) {
      if (this.running) this.error = errorMessage(e);
    } finally {
      this.inFlight = false;
      this.loading = false;
      if (this.again) {
        this.again = false;
        this.schedule();
      }
    }
  }
}
