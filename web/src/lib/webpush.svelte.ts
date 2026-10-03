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

class WebPush {
  /** Bu cihazda abone (endpoint sunucuda kayıtlı). */
  subscribed = $state(false);
  /** Bu cihazın endpoint'i (sunucudaki listede işaretlemek için). */
  endpoint = $state('');
  busy = $state(false);

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

  /** Açılışta: tarayıcıda abonelik var mı (sunucuya sormaz). */
  async refresh() {
    try {
      const reg = await this.registration();
      const sub = await reg?.pushManager.getSubscription();
      this.endpoint = sub?.endpoint ?? '';
      this.subscribed = !!sub;
    } catch {
      this.subscribed = false;
    }
  }

  /** İzin ister, abone olur ve sunucuya kaydeder. Hata fırlatır. */
  async subscribe() {
    this.busy = true;
    try {
      const reg = await this.registration();
      if (!reg) throw new Error('no-sw');
      const perm = await Notification.requestPermission();
      if (perm !== 'granted') throw new Error('denied');
      const { public_key } = await api.webpushVapid();
      let sub = await reg.pushManager.getSubscription();
      if (!sub) {
        sub = await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: b64ToBytes(public_key) as BufferSource });
      }
      await api.webpushSubscribe(sub.toJSON() as { endpoint: string; keys: { p256dh: string; auth: string } });
      this.endpoint = sub.endpoint;
      this.subscribed = true;
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
    } finally {
      this.busy = false;
    }
  }
}

export const webpush = new WebPush();
