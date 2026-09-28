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
  teams: 'Microsoft Teams',
  googlechat: 'Google Chat',
  mattermost: 'Mattermost',
  rocketchat: 'Rocket.Chat',
  matrix: 'Matrix',
  signal: 'Signal',
  pagerduty: 'PagerDuty',
  opsgenie: 'Opsgenie',
  homeassistant: 'Home Assistant',
  netgsm: 'Netgsm (SMS)',
  twilio: 'Twilio (SMS)',
  pushbullet: 'Pushbullet',
  bark: 'Bark (iOS)',
  line: 'LINE',
  apprise: 'Apprise',
};

/** Tip seçicideki gruplar (sıra korunur). */
export const NOTIFY_GROUPS: { label: string; types: NotificationType[] }[] = [
  { label: 'Mesajlaşma', types: ['whatsapp', 'telegram', 'discord', 'slack', 'teams', 'googlechat', 'mattermost', 'rocketchat', 'matrix', 'signal', 'line'] },
  { label: 'E-posta ve SMS', types: ['email', 'netgsm', 'twilio'] },
  { label: 'Mobil bildirim', types: ['ntfy', 'gotify', 'pushover', 'pushbullet', 'bark', 'homeassistant'] },
  { label: 'Olay yönetimi', types: ['pagerduty', 'opsgenie'] },
  { label: 'Genel', types: ['webhook', 'apprise'] },
];

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
  teams: { color: 'var(--ch-teams)', icon: 'users' },
  googlechat: { color: 'var(--ch-googlechat)', icon: 'messages' },
  mattermost: { color: 'var(--ch-mattermost)', icon: 'at-sign' },
  rocketchat: { color: 'var(--ch-rocketchat)', icon: 'rocket' },
  matrix: { color: 'var(--ch-matrix)', icon: 'brackets' },
  signal: { color: 'var(--ch-signal)', icon: 'message-circle' },
  pagerduty: { color: 'var(--ch-pagerduty)', icon: 'siren' },
  opsgenie: { color: 'var(--ch-opsgenie)', icon: 'alert-circle' },
  homeassistant: { color: 'var(--ch-homeassistant)', icon: 'home' },
  netgsm: { color: 'var(--ch-netgsm)', icon: 'sms' },
  twilio: { color: 'var(--ch-twilio)', icon: 'sms' },
  pushbullet: { color: 'var(--ch-pushbullet)', icon: 'arrow-up-circle' },
  bark: { color: 'var(--ch-bark)', icon: 'bell-ring' },
  line: { color: 'var(--ch-line)', icon: 'message-dots' },
  apprise: { color: 'var(--ch-apprise)', icon: 'share' },
};

export type FieldKind = 'text' | 'secret' | 'number' | 'select' | 'textarea' | 'url' | 'bool';

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
  /** Sayı olarak gönderilir (select olsa bile). */
  numeric?: boolean;
  /** Doluysa uyması gereken biçim ve uymazsa gösterilecek mesaj. */
  pattern?: RegExp;
  patternMsg?: string;
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
      { key: 'bot_token', label: 'Bot token’ı', kind: 'secret', placeholder: '123456789:ABC...', required: true, wide: true },
      { key: 'chat_id', label: 'Sohbet kimliği (chat ID)', kind: 'text', placeholder: '-1001234567890', required: true },
      {
        key: 'thread_id',
        label: 'Konu kimliği (thread ID)',
        kind: 'number',
        numeric: true,
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
      { key: 'port', label: 'Port', kind: 'number', numeric: true, def: 587, min: 1, max: 65535 },
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
      { key: 'token', label: 'Erişim token’ı', kind: 'secret', optional: true, help: 'Korumalı konular için.' },
      { key: 'priority', label: 'Öncelik (1-5)', kind: 'number', numeric: true, def: 4, min: 1, max: 5, help: 'Sorun bildirimlerinde kullanılır.' },
    ],
  },
  gotify: {
    fields: [
      { key: 'server', label: 'Sunucu', kind: 'url', placeholder: 'https://gotify.ornek.com', required: true, wide: true },
      { key: 'app_token', label: 'Uygulama token’ı', kind: 'secret', required: true },
      { key: 'priority', label: 'Öncelik (1-10)', kind: 'number', numeric: true, def: 8, min: 1, max: 10 },
    ],
  },
  pushover: {
    fields: [
      { key: 'user_key', label: 'Kullanıcı anahtarı', kind: 'secret', required: true },
      { key: 'app_token', label: 'Uygulama token’ı', kind: 'secret', required: true },
      { key: 'device', label: 'Cihaz', kind: 'text', optional: true, help: 'Boşsa tüm cihazlara gider.' },
      {
        key: 'priority',
        label: 'Öncelik',
        kind: 'select',
        numeric: true,
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
  teams: {
    help: 'Teams kanalına Adaptive Card ile bildirim gönderir (Workflows webhook’u).',
    fields: [
      {
        key: 'webhook_url',
        label: 'Webhook adresi',
        kind: 'secret',
        placeholder: 'https://prod-xx.westus.logic.azure.com/...',
        help: 'Teams kanalında “Workflows” › “Post to a channel when a webhook request is received” akışı oluşturup verilen adresi yapıştırın (eski “Incoming Webhook” bağlayıcısı kapatıldı).',
        required: true,
        wide: true,
      },
    ],
  },
  googlechat: {
    help: 'Google Chat alanına (space) web kancası ile bildirim gönderir.',
    fields: [
      {
        key: 'webhook_url',
        label: 'Webhook adresi',
        kind: 'secret',
        placeholder: 'https://chat.googleapis.com/v1/spaces/.../messages?key=...&token=...',
        help: 'Alan ayarlarından “Web kancaları” ile oluşturun.',
        required: true,
        wide: true,
      },
    ],
  },
  mattermost: {
    help: 'Mattermost gelen web kancasına bildirim gönderir.',
    fields: [
      {
        key: 'webhook_url',
        label: 'Webhook adresi',
        kind: 'secret',
        placeholder: 'https://mattermost.ornek.com/hooks/xxxxx',
        help: 'Sistem Konsolu › Entegrasyonlar › Gelen Web Kancaları',
        required: true,
        wide: true,
      },
      { key: 'channel', label: 'Kanal', kind: 'text', placeholder: '#uyarilar', optional: true, help: 'Boşsa web kancasının kanalı.' },
      { key: 'username', label: 'Görünen ad', kind: 'text', def: 'Uptime', optional: true },
      { key: 'icon_url', label: 'Simge adresi', kind: 'url', optional: true, wide: true, placeholder: 'https://ornek.com/simge.png' },
    ],
  },
  rocketchat: {
    help: 'Rocket.Chat gelen web kancasına bildirim gönderir.',
    fields: [
      {
        key: 'webhook_url',
        label: 'Webhook adresi',
        kind: 'secret',
        placeholder: 'https://chat.ornek.com/hooks/...',
        help: 'Yönetim › Entegrasyonlar › Gelen',
        required: true,
        wide: true,
      },
      { key: 'channel', label: 'Kanal', kind: 'text', placeholder: '#uyarilar', optional: true },
      { key: 'alias', label: 'Görünen ad', kind: 'text', def: 'Uptime', optional: true },
      { key: 'avatar', label: 'Avatar adresi', kind: 'url', optional: true, wide: true, placeholder: 'https://ornek.com/avatar.png' },
    ],
  },
  matrix: {
    help: 'Matrix odasına mesaj gönderir.',
    fields: [
      { key: 'homeserver_url', label: 'Sunucu adresi', kind: 'url', placeholder: 'https://matrix.org', required: true, wide: true },
      {
        key: 'access_token',
        label: 'Erişim jetonu',
        kind: 'secret',
        placeholder: 'syt_...',
        help: 'Element › Ayarlar › Yardım ve Hakkında › Gelişmiş › Erişim Jetonu (tercihen ayrı bir bot hesabı).',
        required: true,
        wide: true,
      },
      {
        key: 'room_id',
        label: 'Oda kimliği',
        kind: 'text',
        placeholder: '!AbCdEf:matrix.org',
        help: 'Oda ayarları › Gelişmiş (takma ad değil).',
        required: true,
        wide: true,
      },
    ],
  },
  signal: {
    help: 'signal-cli-rest-api sunucusu üzerinden Signal mesajı gönderir.',
    fields: [
      { key: 'url', label: 'Sunucu adresi', kind: 'url', placeholder: 'http://signal-cli:8080', required: true, wide: true },
      { key: 'number', label: 'Gönderen numara', kind: 'text', placeholder: '+905551112233', required: true },
      {
        key: 'recipients',
        label: 'Alıcılar',
        kind: 'text',
        placeholder: '+905551112233, +905553334455',
        help: 'Birden fazla alıcıyı virgülle ayırın.',
        required: true,
        wide: true,
      },
    ],
  },
  pagerduty: {
    help: 'PagerDuty’de Events API v2 ile olay açar ve düzelince çözer.',
    fields: [
      {
        key: 'routing_key',
        label: 'Routing key',
        kind: 'secret',
        help: 'Servisin “Events API v2” entegrasyon anahtarı.',
        required: true,
        wide: true,
      },
      {
        key: 'severity',
        label: 'Önem derecesi',
        kind: 'select',
        def: 'critical',
        help: 'SSL sertifikası uyarıları her zaman “warning” gönderilir.',
        options: [
          { v: 'critical', l: 'Kritik (critical)' },
          { v: 'error', l: 'Hata (error)' },
          { v: 'warning', l: 'Uyarı (warning)' },
          { v: 'info', l: 'Bilgi (info)' },
        ],
      },
    ],
  },
  opsgenie: {
    help: 'Opsgenie’de alarm açar ve düzelince kapatır.',
    fields: [
      { key: 'api_key', label: 'API anahtarı', kind: 'secret', help: 'Takımlar › Entegrasyonlar › API', required: true, wide: true },
      {
        key: 'region',
        label: 'Bölge',
        kind: 'select',
        def: 'us',
        options: [
          { v: 'us', l: 'ABD (us)' },
          { v: 'eu', l: 'Avrupa (eu)' },
        ],
      },
      {
        key: 'priority',
        label: 'Öncelik',
        kind: 'select',
        def: 'P3',
        options: [
          { v: 'P1', l: 'P1 — Kritik' },
          { v: 'P2', l: 'P2 — Yüksek' },
          { v: 'P3', l: 'P3 — Orta' },
          { v: 'P4', l: 'P4 — Düşük' },
          { v: 'P5', l: 'P5 — Bilgi' },
        ],
      },
    ],
  },
  homeassistant: {
    help: 'Home Assistant notify servisi üzerinden bildirim gönderir (mobil uygulama vb.).',
    fields: [
      { key: 'url', label: 'Home Assistant adresi', kind: 'url', placeholder: 'http://homeassistant.local:8123', required: true, wide: true },
      {
        key: 'token',
        label: 'Uzun ömürlü erişim jetonu',
        kind: 'secret',
        help: 'Profil › Güvenlik › Uzun Ömürlü Erişim Jetonları',
        required: true,
        wide: true,
      },
      {
        key: 'service',
        label: 'Bildirim servisi',
        kind: 'text',
        placeholder: 'mobile_app_kadir_iphone',
        help: '“notify.” öneki olmadan.',
        required: true,
        pattern: /^[a-z0-9_]+$/,
        patternMsg: 'Bildirim servisi yalnızca küçük harf, rakam ve alt çizgi içerebilir (notify. öneki olmadan).',
        wide: true,
      },
    ],
  },
  netgsm: {
    help: 'Netgsm üzerinden Türkiye numaralarına SMS gönderir.',
    fields: [
      { key: 'usercode', label: 'Kullanıcı kodu', kind: 'text', required: true },
      { key: 'password', label: 'API şifresi', kind: 'secret', required: true },
      { key: 'msgheader', label: 'Gönderici başlığı', kind: 'text', placeholder: 'FIRMA', help: 'Netgsm’de onaylı başlık.', required: true },
      {
        key: 'gsm',
        label: 'Telefon numaraları',
        kind: 'text',
        placeholder: '5551112233, 5553334455',
        help: 'Virgülle ayırın. 0, 90 ve +90 önekleri otomatik düzeltilir.',
        required: true,
        wide: true,
      },
      {
        key: 'turkish_chars',
        label: 'Türkçe karakterler',
        kind: 'bool',
        help: 'Açıksa ç, ğ, ı, ö, ş, ü korunur (SMS başına karakter hakkı azalabilir).',
        wide: true,
      },
    ],
  },
  twilio: {
    help: 'Twilio ile SMS gönderir.',
    fields: [
      { key: 'account_sid', label: 'Account SID', kind: 'text', placeholder: 'ACxxxxxxxx…', required: true },
      { key: 'auth_token', label: 'Auth token', kind: 'secret', required: true },
      {
        key: 'from',
        label: 'Gönderen numara',
        kind: 'text',
        placeholder: '+15551234567',
        help: 'Ülke koduyla, + ile başlayarak.',
        required: true,
      },
      {
        key: 'to',
        label: 'Alıcı numaralar',
        kind: 'text',
        placeholder: '+905551112233, +905553334455',
        help: 'Virgülle ayırın; her numara + ve ülke koduyla.',
        required: true,
        wide: true,
      },
    ],
  },
  pushbullet: {
    help: 'Pushbullet bildirimi gönderir.',
    fields: [
      { key: 'access_token', label: 'Erişim jetonu', kind: 'secret', help: 'Ayarlar › Erişim Jetonları', required: true, wide: true },
      { key: 'channel_tag', label: 'Kanal etiketi', kind: 'text', optional: true, help: 'Cihaz kimliği ile birlikte kullanılamaz.' },
      { key: 'device_iden', label: 'Cihaz kimliği', kind: 'text', optional: true, help: 'Boşsa tüm cihazlarınıza gider.' },
    ],
  },
  bark: {
    help: 'Bark uygulamasına iOS bildirimi gönderir.',
    fields: [
      { key: 'server', label: 'Sunucu adresi', kind: 'url', def: 'https://api.day.app', placeholder: 'https://api.day.app', wide: true },
      { key: 'device_key', label: 'Cihaz anahtarı', kind: 'secret', required: true, wide: true },
      { key: 'sound', label: 'Ses', kind: 'text', optional: true, placeholder: 'alarm' },
      { key: 'group', label: 'Grup', kind: 'text', def: 'Uptime', optional: true },
    ],
  },
  line: {
    help: 'LINE Messaging API ile mesaj gönderir (LINE Notify kapatıldı).',
    fields: [
      { key: 'channel_access_token', label: 'Kanal erişim jetonu', kind: 'secret', required: true, wide: true },
      {
        key: 'to',
        label: 'Alıcı kimliği',
        kind: 'text',
        placeholder: 'U0123456789abcdef0123456789abcdef',
        help: 'Kullanıcı (U…), grup (C…) veya oda (R…) kimliği.',
        required: true,
        pattern: /^[UCR][0-9a-f]{32}$/,
        patternMsg: 'Alıcı ID’si U, C veya R ile başlayan 33 karakterlik bir kimlik olmalı.',
        wide: true,
      },
    ],
  },
  apprise: {
    help: 'Kendi Apprise API sunucunuz üzerinden 100’den fazla servise gönderir.',
    fields: [
      { key: 'server', label: 'Apprise sunucu adresi', kind: 'url', placeholder: 'http://apprise:8000', required: true, wide: true },
      {
        key: 'urls',
        label: 'Apprise URL’leri',
        kind: 'secret',
        placeholder: 'tgram://token/chatid, discord://…',
        help: 'Birden fazla adresi virgülle ayırın. Gizli bilgi olarak saklanır.',
        required: true,
        wide: true,
      },
    ],
  },
};


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
