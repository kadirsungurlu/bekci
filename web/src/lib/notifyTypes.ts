// Bildirim kanalı tipleri ve form alanları.

import type { NotificationType } from './api';
import type { IconName } from '../components/Icon.svelte';

export const NOTIFY_LABELS: Record<NotificationType, string> = {
  whatsapp: 'WhatsApp',
  telegram: 'Telegram',
  email: 'E-posta (SMTP)',
  discord: 'Discord',
  slack: 'Slack',
  webhook: 'Webhook',
  ntfy: 'ntfy',
  gotify: 'Gotify',
  pushover: 'Pushover',
};

/** Liste simgeleri: tip başına ikon ve app.css'teki renk değişkeni. */
export const NOTIFY_STYLE: Record<NotificationType, { color: string; icon: IconName }> = {
  whatsapp: { color: 'var(--ch-whatsapp)', icon: 'phone' },
  telegram: { color: 'var(--ch-telegram)', icon: 'send' },
  email: { color: 'var(--ch-email)', icon: 'mail' },
  discord: { color: 'var(--ch-discord)', icon: 'message' },
  slack: { color: 'var(--ch-slack)', icon: 'hash' },
  webhook: { color: 'var(--ch-webhook)', icon: 'code' },
  ntfy: { color: 'var(--ch-ntfy)', icon: 'bell' },
  gotify: { color: 'var(--ch-gotify)', icon: 'server' },
  pushover: { color: 'var(--ch-pushover)', icon: 'smartphone' },
};

export type FieldKind = 'text' | 'secret' | 'number' | 'select' | 'textarea' | 'url';

export interface Field {
  key: string;
  label: string;
  kind: FieldKind;
  placeholder?: string;
  help?: string;
  required?: boolean;
  optional?: boolean;
  options?: { v: string; l: string }[];
  def?: string | number;
  min?: number;
  max?: number;
  wide?: boolean;
}

export interface NotifySchema {
  fields: Field[];
  help?: string;
}

export const NOTIFY_SCHEMAS: Record<NotificationType, NotifySchema> = {
  whatsapp: {
    help: 'WP API üzerinden WhatsApp mesajı gönderir.',
    fields: [
      { key: 'url', label: 'WP API adresi', kind: 'url', placeholder: 'https://wp-api.k3r.app', required: true, wide: true },
      { key: 'api_key', label: 'API anahtarı', kind: 'secret', required: true, wide: true },
      {
        key: 'to',
        label: 'Alıcı',
        kind: 'text',
        placeholder: '905xxxxxxxxx veya 1203...@g.us',
        help: 'Ülke koduyla telefon numarası (+ olmadan) ya da @g.us ile biten grup kimliği.',
        required: true,
      },
      {
        key: 'from_number_id',
        label: 'Gönderen numara kimliği',
        kind: 'text',
        optional: true,
        help: 'Birden fazla bağlı numaranız varsa hangisinden gönderileceği.',
      },
    ],
  },
  telegram: {
    help: '@BotFather ile bot oluşturun, botu gruba/kanala ekleyin ve sohbet kimliğini girin.',
    fields: [
      { key: 'bot_token', label: 'Bot token', kind: 'secret', placeholder: '123456789:ABC...', required: true, wide: true },
      { key: 'chat_id', label: 'Sohbet kimliği (chat ID)', kind: 'text', placeholder: '-1001234567890', required: true },
      {
        key: 'thread_id',
        label: 'Konu kimliği (thread ID)',
        kind: 'number',
        optional: true,
        help: 'Konulara ayrılmış gruplarda mesajın gideceği konu.',
      },
    ],
  },
  email: {
    fields: [
      { key: 'host', label: 'SMTP sunucusu', kind: 'text', placeholder: 'smtp.ornek.com', required: true },
      {
        key: 'security',
        label: 'Güvenlik',
        kind: 'select',
        def: 'starttls',
        options: [
          { v: 'starttls', l: 'STARTTLS (587)' },
          { v: 'tls', l: 'TLS/SSL (465)' },
          { v: 'none', l: 'Yok (25)' },
        ],
      },
      { key: 'port', label: 'Port', kind: 'number', def: 587, min: 1, max: 65535 },
      { key: 'username', label: 'Kullanıcı adı', kind: 'text', optional: true },
      { key: 'password', label: 'Şifre', kind: 'secret', optional: true },
      { key: 'from', label: 'Gönderen', kind: 'text', placeholder: 'Uptime <uptime@ornek.com>', required: true },
      {
        key: 'to',
        label: 'Alıcılar',
        kind: 'text',
        placeholder: 'ben@ornek.com, ekip@ornek.com',
        help: 'Birden fazla adresi virgülle ayırın.',
        required: true,
        wide: true,
      },
    ],
  },
  discord: {
    help: 'Kanal ayarları → Entegrasyonlar → Webhook oluşturun ve adresini yapıştırın.',
    fields: [
      {
        key: 'webhook_url',
        label: 'Webhook adresi',
        kind: 'secret',
        placeholder: 'https://discord.com/api/webhooks/...',
        required: true,
        wide: true,
      },
    ],
  },
  slack: {
    help: 'Slack uygulamanızda “Incoming Webhooks” açıp oluşan adresi yapıştırın.',
    fields: [
      {
        key: 'webhook_url',
        label: 'Webhook adresi',
        kind: 'secret',
        placeholder: 'https://hooks.slack.com/services/...',
        required: true,
        wide: true,
      },
    ],
  },
  webhook: {
    fields: [
      { key: 'url', label: 'Adres', kind: 'url', placeholder: 'https://ornek.com/uptime-webhook', required: true, wide: true },
      {
        key: 'method',
        label: 'Metot',
        kind: 'select',
        def: 'POST',
        options: [
          { v: 'POST', l: 'POST' },
          { v: 'PUT', l: 'PUT' },
        ],
      },
      {
        key: 'headers',
        label: 'Başlıklar',
        kind: 'textarea',
        optional: true,
        placeholder: 'Authorization: Bearer abc123',
        help: 'Her satıra bir başlık: “Ad: değer”. Gizli bilgi olarak saklanır.',
        wide: true,
      },
    ],
  },
  ntfy: {
    fields: [
      { key: 'server', label: 'Sunucu', kind: 'url', def: 'https://ntfy.sh', placeholder: 'https://ntfy.sh' },
      { key: 'topic', label: 'Konu (topic)', kind: 'text', placeholder: 'benim-uptime-konum', required: true },
      { key: 'token', label: 'Erişim token', kind: 'secret', optional: true, help: 'Korumalı konular için.' },
      { key: 'priority', label: 'Öncelik (1-5)', kind: 'number', def: 4, min: 1, max: 5, help: 'Sorun bildirimlerinde kullanılır.' },
    ],
  },
  gotify: {
    fields: [
      { key: 'server', label: 'Sunucu', kind: 'url', placeholder: 'https://gotify.ornek.com', required: true, wide: true },
      { key: 'app_token', label: 'Uygulama token', kind: 'secret', required: true },
      { key: 'priority', label: 'Öncelik (1-10)', kind: 'number', def: 8, min: 1, max: 10 },
    ],
  },
  pushover: {
    fields: [
      { key: 'user_key', label: 'Kullanıcı anahtarı', kind: 'secret', required: true },
      { key: 'app_token', label: 'Uygulama token', kind: 'secret', required: true },
      { key: 'device', label: 'Cihaz', kind: 'text', optional: true, help: 'Boşsa tüm cihazlara gider.' },
      {
        key: 'priority',
        label: 'Öncelik',
        kind: 'select',
        def: '0',
        options: [
          { v: '-2', l: 'En düşük (-2)' },
          { v: '-1', l: 'Düşük (-1)' },
          { v: '0', l: 'Normal (0)' },
          { v: '1', l: 'Yüksek (1)' },
        ],
      },
    ],
  },
};

/** Sayı olarak gönderilmesi gereken alanlar (select olsa bile). */
export const NUMERIC_KEYS = new Set(['port', 'thread_id', 'priority']);

export const EMAIL_PORTS: Record<string, number> = { starttls: 587, tls: 465, none: 25 };

export const WEBHOOK_EXAMPLE = `{
  "event": "up",
  "title": "…",
  "text": "…",
  "message": "HTTP 200 OK",
  "time": "2026-09-27T10:15:00+03:00",
  "downtime_seconds": 754,
  "monitor": {
    "id": 1,
    "name": "Web sitesi",
    "type": "http",
    "target": "https://ornek.com",
    "url": "https://uptime.ornek.com/#/monitors/1"
  }
}`;
