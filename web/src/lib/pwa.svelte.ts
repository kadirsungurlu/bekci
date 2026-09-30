// Ana ekrana eklenmiş uygulama (PWA): servis çalışanı kaydı, "Yeni sürüm hazır"
// bildirimi ve tek başına (standalone) çalışma bilgisi.
//
// Servis çalışanı yalnızca derlenmiş sürümde ve yalnızca yönetim panelinde kaydedilir;
// herkese açık durum sayfaları (/durum/… ve özel alan adları) hiç kaydetmez.

class Pwa {
  /** Yeni sürüm indirildi, onay bekliyor. */
  updateReady = $state(false);
  /** Ana ekrandan açıldı (tarayıcı çubukları yok). */
  standalone = $state(false);

  private reg: ServiceWorkerRegistration | null = null;
  private reloading = false;
  private started = false;
  /** Kullanıcı "Yenile" dedi: yeni çalışan devreye girince sayfa yenilenir. */
  private accepted = false;

  constructor() {
    if (typeof window === 'undefined') return;
    const mq = window.matchMedia('(display-mode: standalone)');
    const iosStandalone = (navigator as Navigator & { standalone?: boolean }).standalone === true;
    this.standalone = mq.matches || iosStandalone;
    mq.addEventListener?.('change', (e) => (this.standalone = e.matches || iosStandalone));
  }

  /** Yönetim paneli açıldığında bir kez çağrılır. */
  register() {
    if (this.started || !import.meta.env.PROD || !('serviceWorker' in navigator)) return;
    this.started = true;
    const sw = navigator.serviceWorker;
    // Sayfayı şu an bir çalışan yönetiyor mu? İlk ziyarette (veya önbellek
    // temizlendikten sonra) yönetmiyordur: yeni kurulan çalışan clients.claim()
    // ile sayfayı sahiplenince controllerchange gelir. Bu İLK sahiplenmede sayfa
    // YENİLENMEZ (giriş formuna yazılanlar silinir, giriş yarıda kalırdı); sayfa
    // zaten güncel dosyalarla açılmıştır.
    let controlled = !!sw.controller;
    sw.addEventListener('controllerchange', () => {
      if (!controlled) {
        controlled = true;
        return;
      }
      if (this.reloading) return;
      if (this.accepted) {
        // Kullanıcı "Yenile" dedi: yeni sürüm devrede, sayfa bir kez yenilenir.
        this.reloading = true;
        location.reload();
        return;
      }
      // Yeni sürümü başka bir sekme devreye aldı: bu sekme kendiliğinden
      // yenilenmez (yazılanlar kaybolmasın), yalnızca "Yeni sürüm hazır" der.
      this.updateReady = true;
    });
    sw.register('/sw.js', { scope: '/' })
      .then((reg) => {
        this.reg = reg;
        // Açılışta zaten bekleyen sürüm varsa (ör. önceki oturumda indirilmiş).
        if (reg.waiting && sw.controller) this.updateReady = true;
        reg.addEventListener('updatefound', () => {
          const nw = reg.installing;
          nw?.addEventListener('statechange', () => {
            // İlk kurulumda (controller yok) sormaya gerek yok.
            if (nw.state === 'installed' && sw.controller) this.updateReady = true;
          });
        });
        // Uzun açık kalan uygulama (telefonda arka planda) yeni sürümü de görsün.
        document.addEventListener('visibilitychange', () => {
          if (!document.hidden) this.check();
        });
        setInterval(() => this.check(), 30 * 60_000);
      })
      .catch(() => {
        /* servis çalışanı olmadan da uygulama çalışır */
      });
  }

  check() {
    this.reg?.update().catch(() => {});
  }

  /** Bekleyen sürümü devreye alır; controllerchange sayfayı yeniler. */
  applyUpdate() {
    const w = this.reg?.waiting;
    if (w) {
      this.accepted = true;
      w.postMessage({ type: 'SKIP_WAITING' });
    } else location.reload();
  }

  dismissUpdate() {
    this.updateReady = false;
  }
}

export const pwa = new Pwa();

/**
 * Herkese açık durum sayfasında yönetim uygulamasının manifest'i ve iOS "uygulama"
 * etiketleri kaldırılır: müşteri sayfayı ana ekrana eklerse panel değil o sayfa açılsın.
 */
export function stripAppManifest() {
  document
    .querySelectorAll('link[rel="manifest"], meta[name="apple-mobile-web-app-capable"], meta[name="mobile-web-app-capable"], meta[name="apple-mobile-web-app-title"]')
    .forEach((el) => el.remove());
}
