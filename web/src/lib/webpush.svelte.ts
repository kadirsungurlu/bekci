// Web Push (E-14): bu tarayıcının aboneliği. Servis çalışanı yalnızca
// derlenmiş sürümde kayıtlıdır (pwa.svelte.ts); geliştirmede destek yok sayılır.
// iOS'ta (16.4+) yalnızca ana ekrana eklenmiş uygulamada çalışır.
import { api } from './api';

export type PushSupport = 'unsupported' | 'denied' | 'ios-browser' | 'ready';

function b64ToBytes(s: string): Uint8Array {
  const pad = '='.repeat((4 - (s.length % 4)) % 4);
  const raw = atob((s + pad).replace(/-/g, '+').replace(/_/g, '/'));
  const out = new Uint8Array(raw.length);
  for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i);
  return out;
}

/** Abonelik başka bir uygulama anahtarıyla mı alınmış? Tarayıcı anahtarı
 *  vermezse (eski tarayıcı) karşılaştırma yapılmaz. */
function keyDiffers(sub: PushSubscription, publicKey: string): boolean {
  const cur = sub.options?.applicationServerKey;
  if (!cur) return false;
  const a = new Uint8Array(cur);
  const b = b64ToBytes(publicKey);
  return a.length !== b.length || a.some((v, i) => v !== b[i]);
}

class WebPush {
  /** Bu cihazda abone (endpoint sunucuda kayıtlı). */
  subscribed = $state(false);
  /** Bu cihazın endpoint'i (sunucudaki listede işaretlemek için). */
  endpoint = $state('');
  busy = $state(false);
  /** Bu cihazın aboneliği eski VAPID anahtarına bağlı: sunucuda anahtar
   *  değişmiş, bildirim gelmez; yeniden abone olmak gerekir. */
  stale = $state(false);

  support(): PushSupport {
    if (typeof window === 'undefined' || !('serviceWorker' in navigator) || !('PushManager' in window) || !('Notification' in window)) {
      // iPhone/iPad Safari: yalnızca ana ekrana eklenmiş uygulamada destek var.
      const ios = /iP(hone|ad|od)/.test(navigator.userAgent);
      const standalone = (navigator as Navigator & { standalone?: boolean }).standalone === true;
      return ios && !standalone ? 'ios-browser' : 'unsupported';
    }
    if (Notification.permission === 'denied') return 'denied';
    return 'ready';
  }

  private async registration(): Promise<ServiceWorkerRegistration | null> {
    if (!('serviceWorker' in navigator)) return null;
    const reg = await navigator.serviceWorker.getRegistration('/');
    return reg ?? null;
  }

  /** Açılışta: tarayıcıda abonelik var mı, sunucunun anahtarıyla uyuşuyor mu. */
  async refresh() {
    try {
      const reg = await this.registration();
      const sub = await reg?.pushManager.getSubscription();
      this.endpoint = sub?.endpoint ?? '';
      this.subscribed = !!sub;
      this.stale = !!sub && keyDiffers(sub, (await api.webpushVapid()).public_key);
    } catch {
      this.stale = false;
    }
  }

  /** İzin ister, abone olur ve sunucuya kaydeder. Hata fırlatır. Eski anahtara
   *  bağlı abonelik önce kaldırılır: tarayıcı aynı aboneliği yeni anahtarla
   *  kabul etmez. old: yeni kayıttan sonra silinecek eski sunucu kaydı. */
  async subscribe(old?: { id: number; endpoint: string } | null) {
    this.busy = true;
    try {
      const reg = await this.registration();
      if (!reg) throw new Error('no-sw');
      const perm = await Notification.requestPermission();
      if (perm !== 'granted') throw new Error('denied');
      const { public_key } = await api.webpushVapid();
      let sub = await reg.pushManager.getSubscription();
      if (sub && keyDiffers(sub, public_key)) {
        await sub.unsubscribe();
        sub = null;
      }
      if (!sub) {
        sub = await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: b64ToBytes(public_key) as BufferSource });
      }
      await api.webpushSubscribe(sub.toJSON() as { endpoint: string; keys: { p256dh: string; auth: string } });
      if (old && old.endpoint !== sub.endpoint) await api.webpushUnsubscribe(old.id);
      this.endpoint = sub.endpoint;
      this.subscribed = true;
      this.stale = false;
    } finally {
      this.busy = false;
    }
  }

  /** Tarayıcı aboneliğini ve sunucu kaydını kaldırır. */
  async unsubscribe(serverId?: number) {
    this.busy = true;
    try {
      const reg = await this.registration();
      const sub = await reg?.pushManager.getSubscription();
      await sub?.unsubscribe();
      if (serverId) await api.webpushUnsubscribe(serverId);
      this.endpoint = '';
      this.subscribed = false;
      this.stale = false;
    } finally {
      this.busy = false;
    }
  }
}

export const webpush = new WebPush();
