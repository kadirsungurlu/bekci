// Uygulama geneli arayüz durumu: bildirim baloncukları (toast), onay penceresi, saat.

import { nowSec } from './format';

export type ToastKind = 'success' | 'error' | 'info';
export interface ToastItem {
  id: number;
  kind: ToastKind;
  text: string;
}

class Toasts {
  items = $state<ToastItem[]>([]);
  private seq = 0;

  show(kind: ToastKind, text: string, ms = kind === 'error' ? 6000 : 3500) {
    const id = ++this.seq;
    this.items = [...this.items.slice(-3), { id, kind, text }];
    setTimeout(() => this.dismiss(id), ms);
  }
  success(text: string) {
    this.show('success', text);
  }
  error(text: string) {
    this.show('error', text);
  }
  info(text: string) {
    this.show('info', text);
  }
  dismiss(id: number) {
    this.items = this.items.filter((t) => t.id !== id);
  }
}

export const toast = new Toasts();

export interface ConfirmRequest {
  title: string;
  message: string;
  confirmText?: string;
  cancelText?: string;
  danger?: boolean;
  resolve: (ok: boolean) => void;
}

class Confirmer {
  current = $state<ConfirmRequest | null>(null);

  ask(opts: Omit<ConfirmRequest, 'resolve'>): Promise<boolean> {
    this.current?.resolve(false);
    return new Promise((resolve) => {
      this.current = { ...opts, resolve };
    });
  }
  answer(ok: boolean) {
    const c = this.current;
    this.current = null;
    c?.resolve(ok);
  }
}

export const confirmer = new Confirmer();
export const confirmDialog = (opts: Omit<ConfirmRequest, 'resolve'>) => confirmer.ask(opts);

/** Göreli zamanlar ("3 dk önce", "Çalışıyor · 2 sa") için birkaç saniyede bir ilerleyen saat. */
class Clock {
  now = $state(nowSec());
  constructor() {
    setInterval(() => (this.now = nowSec()), 5000);
    document.addEventListener('visibilitychange', () => {
      if (!document.hidden) this.now = nowSec();
    });
  }
}

export const clock = new Clock();

/** Metni panoya kopyalar (güvensiz bağlamda eski yönteme düşer). */
export async function copyText(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(text);
      return true;
    }
  } catch {
    /* aşağıdaki yönteme düş */
  }
  try {
    const ta = document.createElement('textarea');
    ta.value = text;
    ta.setAttribute('readonly', '');
    ta.style.position = 'fixed';
    ta.style.opacity = '0';
    document.body.appendChild(ta);
    ta.select();
    const ok = document.execCommand('copy');
    ta.remove();
    return ok;
  } catch {
    return false;
  }
}
