// Monitör tipleri kayıt defteri.
//
// Yeni bir tip eklemek için (ör. veritabanı, docker, grpc, mqtt, smtp …):
//  1. api.ts'teki MonitorType birliğine sunucudaki tip adını ekleyin.
//  2. Aşağıdaki MONITOR_TYPES listesine bir kayıt ekleyin. `fields` verilirse
//     form bu alanları genel alan çiziciyle (ConfigFields) gösterir ve config
//     nesnesini alan anahtarlarından kurar; özel bir arayüz gerekiyorsa
//     MonitorForm.svelte'e o tip için bir bölüm eklenir.
//  3. Tip sunucuda gizli alan döndürüyorsa (maskeli "••••••") alanı `secret`
//     yapın; değer değiştirilmezse maske aynen geri gönderilir.

import type { MonitorType } from './api';
import type { Field } from './notifyTypes';
import type { IconName } from '../components/Icon.svelte';

export type TypeCategory = 'web' | 'network' | 'passive';

export const CATEGORY_LABELS: Record<TypeCategory, string> = {
  web: 'Web ve API',
  network: 'Ağ',
  passive: 'Sinyal ve gruplar',
};

export interface MonitorTypeDef {
  key: MonitorType;
  /** Tip seçicideki ad. */
  label: string;
  /** Listelerdeki kısa rozet. */
  badge: string;
  desc: string;
  icon: IconName;
  category: TypeCategory;
  /** Zaman aşımı ayarı anlamlı mı (push ve grup kendi başına istek atmaz). */
  timeout?: boolean;
  /** "Ters mod" anlamlı mı. */
  upsideDown?: boolean;
  /** Özel form bölümü olmayan tipler için genel alanlar. */
  fields?: Field[];
  /** Genel alanlarda "Ad" boşsa hangi alanın değeri ad olarak önerilsin. */
  nameFrom?: string;
}

export const MONITOR_TYPES: MonitorTypeDef[] = [
  { key: 'http', label: 'HTTP(S)', badge: 'HTTP', desc: 'Web sitesi veya API adresini kontrol eder', icon: 'globe', category: 'web' },
  { key: 'tcp', label: 'TCP Port', badge: 'TCP', desc: 'Sunucudaki bir portun açık olduğunu kontrol eder', icon: 'plug', category: 'network' },
  { key: 'ping', label: 'Ping', badge: 'PING', desc: 'Sunucunun ağdan yanıt verdiğini kontrol eder', icon: 'radio', category: 'network' },
  { key: 'dns', label: 'DNS', badge: 'DNS', desc: 'Alan adının DNS kaydını sorgular', icon: 'server', category: 'network' },
  {
    key: 'push',
    label: 'Push',
    badge: 'PUSH',
    desc: 'Cron işlerinin düzenli sinyal göndermesini bekler',
    icon: 'inbox',
    category: 'passive',
    timeout: false,
  },
  {
    key: 'group',
    label: 'Grup',
    badge: 'GRUP',
    desc: 'Seçtiğiniz monitörlerin durumunu tek monitörde toplar',
    icon: 'layers',
    category: 'passive',
    timeout: false,
    upsideDown: false,
  },
];

const BY_KEY = new Map<string, MonitorTypeDef>(MONITOR_TYPES.map((t) => [t.key, t]));

export function typeDef(key: string): MonitorTypeDef | undefined {
  return BY_KEY.get(key);
}

/** Rozet metni; bilinmeyen (sonradan eklenmiş) tiplerde tip adının büyük harfi. */
export function typeLabel(key: string): string {
  return BY_KEY.get(key)?.badge ?? key.toLocaleUpperCase('tr');
}

export const hasTimeout = (key: string) => BY_KEY.get(key)?.timeout !== false;
export const hasUpsideDown = (key: string) => BY_KEY.get(key)?.upsideDown !== false;

export const GROUP_MODES: { v: 'any_down' | 'all_down'; l: string }[] = [
  { v: 'any_down', l: 'Herhangi biri çalışmıyorsa DOWN' },
  { v: 'all_down', l: 'Hepsi çalışmıyorsa DOWN' },
];
