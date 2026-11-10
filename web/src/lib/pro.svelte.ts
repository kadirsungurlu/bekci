// Lisans durumu deposu ve Pro sayfa kaydı (docs/LISANS-TASARIM.md "Kancalar" son paragraf).
//
// Pro sayfaları açık depoda yoktur: bekci-pro derlemesi web/src/pro/sayfalar/<id>.svelte dosyalarını
// kopyalar; burada tembel yüklenir (klasör boşken glob {} döner, derleme hatasız). Menü PRO_MENU'dedir;
// menü öğesi kilitliyken de görünür (ProLocked), modül + lisans varsa sayfa açılır.

import type { Component } from 'svelte';
import { licenseApi, type LicenseState } from './api';
import type { IconName } from '../components/Icon.svelte';
import type { Role } from './api';

const REFRESH_MS = 10 * 60 * 1000;

class LicenseStore {
  /** Son okunan durum; null = henüz okunmadı ya da okunamadı (eski sunucu). */
  state = $state.raw<LicenseState | null>(null);
  loaded = $state(false);
  private timer: ReturnType<typeof setInterval> | undefined;
  private req = 0;

  /** Oturum açılınca bir kez çağrılır: hemen okur, sonra 10 dakikada bir yeniler. */
  start() {
    if (this.timer) return;
    void this.load();
    this.timer = setInterval(() => void this.load(), REFRESH_MS);
  }

  stop() {
    clearInterval(this.timer);
    this.timer = undefined;
    this.req++;
    this.state = null;
    this.loaded = false;
  }

  /** Durumu yeniden okur (Lisans ekranında değişiklikten sonra da çağrılır). */
  async load() {
    const my = ++this.req;
    try {
      const s = await licenseApi.getLicense();
      if (my === this.req) this.state = s;
    } catch {
      // sessiz: eski sunucuda uç yoksa kilitli menüler görünür, şerit çıkmaz
    } finally {
      if (my === this.req) this.loaded = true;
    }
  }
}

export const license = new LicenseStore();

/** Özellik bu kurulumda lisansla açık mı (["*"] = hepsi). */
export function proAcik(ozellik: string): boolean {
  const s = license.state;
  if (!s?.etkin) return false;
  const l = s.ozellikler ?? [];
  return l.includes('*') || l.includes(ozellik);
}

export interface ProMenuItem {
  /** Adres `#/pro/<id>` ve sayfa dosyası `pro/sayfalar/<id>.svelte`. */
  id: string;
  /** Lisans özellik bayrağı (internal/lisans). */
  ozellik: string;
  ikon: IconName;
  /** Ad ve açıklama: license.pages.<metin> / <metin>Desc. */
  metin: 'filoGuvenlik' | 'denetimler' | 'raporlar' | 'musteriler';
  /** Menüyü görebilecek en düşük rol. */
  rol: Role;
}

export const PRO_MENU: readonly ProMenuItem[] = [
  { id: 'filo-guvenlik', ozellik: 'filo_guvenlik', ikon: 'shield-check', metin: 'filoGuvenlik', rol: 'admin' },
  { id: 'denetimler', ozellik: 'hosting_denetim', ikon: 'check-square', metin: 'denetimler', rol: 'admin' },
  { id: 'raporlar', ozellik: 'rapor', ikon: 'file', metin: 'raporlar', rol: 'editor' },
  { id: 'musteriler', ozellik: 'musteri', ikon: 'users', metin: 'musteriler', rol: 'admin' },
];

type PageModule = { default: Component };
const MODULES = import.meta.glob<PageModule>('../pro/sayfalar/*.svelte');

function pagePath(id: string) {
  return `../pro/sayfalar/${id}.svelte`;
}

/** Bu derlemede sayfa modülü var mı. */
export function proSayfaVar(id: string): boolean {
  return pagePath(id) in MODULES;
}

/** Sayfa modülünü tembel yükler; yoksa null. */
export async function proSayfaYukle(id: string): Promise<Component | null> {
  const load = MODULES[pagePath(id)];
  if (!load) return null;
  const m = await load();
  return m.default;
}

export const proMenuItem = (id: string) => PRO_MENU.find((m) => m.id === id);
