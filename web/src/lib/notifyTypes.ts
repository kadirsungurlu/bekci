// Bildirim kanalı tipleri ve form alanları.
//
// Metinler lib/i18n/{tr,en}/notifyTypes.ts'tedir. Tablolar modül düzeyinde bir kez
// kurulduğundan çevrilen değerler getter'dır: her okunuşta geçerli dilde t() çağrılır
// (şablonda veya $derived içinde okununca dil değişince yeniden çizilir).

import type { NotificationType, NotifyKind } from './api';
import type { IconName } from '../components/Icon.svelte';
import { APP_NAME } from './brand';
import { t } from './i18n';

/** Örnek adreslerdeki alan adı dile göre: tr "ornek.com", en "example.com". */
const ex = (s: string) => s.replaceAll('ornek.com', t('notifyTypes.exampleDomain'));

export const NOTIFY_LABELS: Record<NotificationType, string> = {
  whatsapp: 'WhatsApp',
  telegram: 'Telegram',
  get email() {
    return t('notifyTypes.email.label');
  },
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

export type NotifyGroupId = 'messaging' | 'emailSms' | 'mobile' | 'incident' | 'general';

/** Tip seçicideki gruplar (sıra korunur). Etiket: t(`notifyTypes.groups.${id}`). */
export const NOTIFY_GROUPS: { id: NotifyGroupId; types: NotificationType[] }[] = [
  { id: 'messaging', types: ['whatsapp', 'telegram', 'discord', 'slack', 'teams', 'googlechat', 'mattermost', 'rocketchat', 'matrix', 'signal', 'line'] },
  { id: 'emailSms', types: ['email', 'netgsm', 'twilio'] },
  { id: 'mobile', types: ['ntfy', 'gotify', 'pushover', 'pushbullet', 'bark', 'homeassistant'] },
  { id: 'incident', types: ['pagerduty', 'opsgenie'] },
  { id: 'general', types: ['webhook', 'apprise'] },
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
    get help() {
      return t('notifyTypes.whatsapp.help');
    },
    fields: [
      {
        key: 'url',
        get label() {
          return t('notifyTypes.whatsapp.fields.url.label');
        },
        kind: 'url',
        placeholder: 'https://wp-api.k3r.app',
        required: true,
        wide: true,
      },
      {
        key: 'api_key',
        get label() {
          return t('notifyTypes.whatsapp.fields.api_key.label');
        },
        kind: 'secret',
        required: true,
        wide: true,
      },
      {
        key: 'to',
        get label() {
          return t('notifyTypes.whatsapp.fields.to.label');
        },
        kind: 'text',
        get placeholder() {
          return t('notifyTypes.whatsapp.fields.to.placeholder');
        },
        get help() {
          return t('notifyTypes.whatsapp.fields.to.help');
        },
        required: true,
      },
      {
        key: 'from_number_id',
        get label() {
          return t('notifyTypes.whatsapp.fields.from_number_id.label');
        },
        kind: 'text',
        optional: true,
        get help() {
          return t('notifyTypes.whatsapp.fields.from_number_id.help');
        },
      },
    ],
  },
  telegram: {
    get help() {
      return t('notifyTypes.telegram.help');
    },
    fields: [
      {
        key: 'bot_token',
        get label() {
          return t('notifyTypes.telegram.fields.bot_token.label');
        },
        kind: 'secret',
        placeholder: '123456789:ABC...',
        required: true,
        wide: true,
      },
      {
        key: 'chat_id',
        get label() {
          return t('notifyTypes.telegram.fields.chat_id.label');
        },
        kind: 'text',
        placeholder: '-1001234567890',
        required: true,
      },
      {
        key: 'thread_id',
        get label() {
          return t('notifyTypes.telegram.fields.thread_id.label');
        },
        kind: 'number',
        numeric: true,
        optional: true,
        get help() {
          return t('notifyTypes.telegram.fields.thread_id.help');
        },
      },
    ],
  },
  email: {
    fields: [
      {
        key: 'host',
        get label() {
          return t('notifyTypes.email.fields.host.label');
        },
        kind: 'text',
        get placeholder() {
          return ex('smtp.ornek.com');
        },
        required: true,
      },
      {
        key: 'security',
        get label() {
          return t('notifyTypes.email.fields.security.label');
        },
        kind: 'select',
        def: 'starttls',
        options: [
          { v: 'starttls', l: 'STARTTLS (587)' },
          { v: 'tls', l: 'TLS/SSL (465)' },
          {
            v: 'none',
            get l() {
              return t('notifyTypes.email.fields.security.none');
            },
          },
        ],
      },
      { key: 'port', label: 'Port', kind: 'number', numeric: true, def: 587, min: 1, max: 65535 },
      {
        key: 'username',
        get label() {
          return t('common.username');
        },
        kind: 'text',
        optional: true,
      },
      {
        key: 'password',
        get label() {
          return t('common.password');
        },
        kind: 'secret',
        optional: true,
      },
      {
        key: 'from',
        get label() {
          return t('notifyTypes.email.fields.from.label');
        },
        kind: 'text',
        get placeholder() {
          return ex('Uptime <uptime@ornek.com>');
        },
        required: true,
      },
      {
        key: 'to',
        get label() {
          return t('notifyTypes.email.fields.to.label');
        },
        kind: 'text',
        get placeholder() {
          return t('notifyTypes.email.fields.to.placeholder');
        },
        get help() {
          return t('notifyTypes.email.fields.to.help');
        },
        required: true,
        wide: true,
      },
    ],
  },
  discord: {
    get help() {
      return t('notifyTypes.discord.help');
    },
    fields: [
      {
        key: 'webhook_url',
        get label() {
          return t('notifyTypes.discord.fields.webhook_url.label');
        },
        kind: 'secret',
        placeholder: 'https://discord.com/api/webhooks/...',
        required: true,
        wide: true,
      },
    ],
  },
  slack: {
    get help() {
      return t('notifyTypes.slack.help');
    },
    fields: [
      {
        key: 'webhook_url',
        get label() {
          return t('notifyTypes.slack.fields.webhook_url.label');
        },
        kind: 'secret',
        placeholder: 'https://hooks.slack.com/services/...',
        required: true,
        wide: true,
      },
    ],
  },
  webhook: {
    fields: [
      {
        key: 'url',
        get label() {
          return t('notifyTypes.webhook.fields.url.label');
        },
        kind: 'url',
        get placeholder() {
          return ex('https://ornek.com/uptime-webhook');
        },
        required: true,
        wide: true,
      },
      {
        key: 'method',
        get label() {
          return t('notifyTypes.webhook.fields.method.label');
        },
        kind: 'select',
        def: 'POST',
        options: [
          { v: 'POST', l: 'POST' },
          { v: 'PUT', l: 'PUT' },
        ],
      },
      {
        key: 'headers',
        get label() {
          return t('notifyTypes.webhook.fields.headers.label');
        },
        kind: 'textarea',
        optional: true,
        placeholder: 'Authorization: Bearer abc123',
        get help() {
          return t('notifyTypes.webhook.fields.headers.help');
        },
        wide: true,
      },
      {
        key: 'secret',
        get label() {
          return t('notifyTypes.webhook.fields.secret.label');
        },
        kind: 'secret',
        optional: true,
        get help() {
          return t('notifyTypes.webhook.fields.secret.help');
        },
        wide: true,
      },
    ],
  },
  ntfy: {
    fields: [
      {
        key: 'server',
        get label() {
          return t('notifyTypes.ntfy.fields.server.label');
        },
        kind: 'url',
        def: 'https://ntfy.sh',
        placeholder: 'https://ntfy.sh',
      },
      {
        key: 'topic',
        get label() {
          return t('notifyTypes.ntfy.fields.topic.label');
        },
        kind: 'text',
        get placeholder() {
          return t('notifyTypes.ntfy.fields.topic.placeholder');
        },
        required: true,
      },
      {
        key: 'token',
        get label() {
          return t('notifyTypes.ntfy.fields.token.label');
        },
        kind: 'secret',
        optional: true,
        get help() {
          return t('notifyTypes.ntfy.fields.token.help');
        },
      },
      {
        key: 'priority',
        get label() {
          return t('notifyTypes.ntfy.fields.priority.label');
        },
        kind: 'number',
        numeric: true,
        def: 4,
        min: 1,
        max: 5,
        get help() {
          return t('notifyTypes.ntfy.fields.priority.help');
        },
      },
    ],
  },
  gotify: {
    fields: [
      {
        key: 'server',
        get label() {
          return t('notifyTypes.gotify.fields.server.label');
        },
        kind: 'url',
        get placeholder() {
          return ex('https://gotify.ornek.com');
        },
        required: true,
        wide: true,
      },
      {
        key: 'app_token',
        get label() {
          return t('notifyTypes.gotify.fields.app_token.label');
        },
        kind: 'secret',
        required: true,
      },
      {
        key: 'priority',
        get label() {
          return t('notifyTypes.gotify.fields.priority.label');
        },
        kind: 'number',
        numeric: true,
        def: 8,
        min: 1,
        max: 10,
      },
    ],
  },
  pushover: {
    fields: [
      {
        key: 'user_key',
        get label() {
          return t('notifyTypes.pushover.fields.user_key.label');
        },
        kind: 'secret',
        required: true,
      },
      {
        key: 'app_token',
        get label() {
          return t('notifyTypes.pushover.fields.app_token.label');
        },
        kind: 'secret',
        required: true,
      },
      {
        key: 'device',
        get label() {
          return t('notifyTypes.pushover.fields.device.label');
        },
        kind: 'text',
        optional: true,
        get help() {
          return t('notifyTypes.pushover.fields.device.help');
        },
      },
      {
        key: 'priority',
        get label() {
          return t('notifyTypes.pushover.fields.priority.label');
        },
        kind: 'select',
        numeric: true,
        def: '0',
        options: [
          {
            v: '-2',
            get l() {
              return t('notifyTypes.pushover.fields.priority.lowest');
            },
          },
          {
            v: '-1',
            get l() {
              return t('notifyTypes.pushover.fields.priority.low');
            },
          },
          { v: '0', l: 'Normal (0)' },
          {
            v: '1',
            get l() {
              return t('notifyTypes.pushover.fields.priority.high');
            },
          },
        ],
      },
    ],
  },
  teams: {
    get help() {
      return t('notifyTypes.teams.help');
    },
    fields: [
      {
        key: 'webhook_url',
        get label() {
          return t('notifyTypes.teams.fields.webhook_url.label');
        },
        kind: 'secret',
        placeholder: 'https://prod-xx.westus.logic.azure.com/...',
        get help() {
          return t('notifyTypes.teams.fields.webhook_url.help');
        },
        required: true,
        wide: true,
      },
    ],
  },
  googlechat: {
    get help() {
      return t('notifyTypes.googlechat.help');
    },
    fields: [
      {
        key: 'webhook_url',
        get label() {
          return t('notifyTypes.googlechat.fields.webhook_url.label');
        },
        kind: 'secret',
        placeholder: 'https://chat.googleapis.com/v1/spaces/.../messages?key=...&token=...',
        get help() {
          return t('notifyTypes.googlechat.fields.webhook_url.help');
        },
        required: true,
        wide: true,
      },
    ],
  },
  mattermost: {
    get help() {
      return t('notifyTypes.mattermost.help');
    },
    fields: [
      {
        key: 'webhook_url',
        get label() {
          return t('notifyTypes.mattermost.fields.webhook_url.label');
        },
        kind: 'secret',
        get placeholder() {
          return ex('https://mattermost.ornek.com/hooks/xxxxx');
        },
        get help() {
          return t('notifyTypes.mattermost.fields.webhook_url.help');
        },
        required: true,
        wide: true,
      },
      {
        key: 'channel',
        get label() {
          return t('notifyTypes.mattermost.fields.channel.label');
        },
        kind: 'text',
        get placeholder() {
          return t('notifyTypes.mattermost.fields.channel.placeholder');
        },
        optional: true,
        get help() {
          return t('notifyTypes.mattermost.fields.channel.help');
        },
      },
      {
        key: 'username',
        get label() {
          return t('notifyTypes.mattermost.fields.username.label');
        },
        kind: 'text',
        def: APP_NAME,
        optional: true,
      },
      {
        key: 'icon_url',
        get label() {
          return t('notifyTypes.mattermost.fields.icon_url.label');
        },
        kind: 'url',
        optional: true,
        wide: true,
        get placeholder() {
          return t('notifyTypes.mattermost.fields.icon_url.placeholder');
        },
      },
    ],
  },
  rocketchat: {
    get help() {
      return t('notifyTypes.rocketchat.help');
    },
    fields: [
      {
        key: 'webhook_url',
        get label() {
          return t('notifyTypes.rocketchat.fields.webhook_url.label');
        },
        kind: 'secret',
        get placeholder() {
          return ex('https://chat.ornek.com/hooks/...');
        },
        get help() {
          return t('notifyTypes.rocketchat.fields.webhook_url.help');
        },
        required: true,
        wide: true,
      },
      {
        key: 'channel',
        get label() {
          return t('notifyTypes.rocketchat.fields.channel.label');
        },
        kind: 'text',
        get placeholder() {
          return t('notifyTypes.rocketchat.fields.channel.placeholder');
        },
        optional: true,
      },
      {
        key: 'alias',
        get label() {
          return t('notifyTypes.rocketchat.fields.alias.label');
        },
        kind: 'text',
        def: APP_NAME,
        optional: true,
      },
      {
        key: 'avatar',
        get label() {
          return t('notifyTypes.rocketchat.fields.avatar.label');
        },
        kind: 'url',
        optional: true,
        wide: true,
        get placeholder() {
          return ex('https://ornek.com/avatar.png');
        },
      },
    ],
  },
  matrix: {
    get help() {
      return t('notifyTypes.matrix.help');
    },
    fields: [
      {
        key: 'homeserver_url',
        get label() {
          return t('notifyTypes.matrix.fields.homeserver_url.label');
        },
        kind: 'url',
        placeholder: 'https://matrix.org',
        required: true,
        wide: true,
      },
      {
        key: 'access_token',
        get label() {
          return t('notifyTypes.matrix.fields.access_token.label');
        },
        kind: 'secret',
        placeholder: 'syt_...',
        get help() {
          return t('notifyTypes.matrix.fields.access_token.help');
        },
        required: true,
        wide: true,
      },
      {
        key: 'room_id',
        get label() {
          return t('notifyTypes.matrix.fields.room_id.label');
        },
        kind: 'text',
        placeholder: '!AbCdEf:matrix.org',
        get help() {
          return t('notifyTypes.matrix.fields.room_id.help');
        },
        required: true,
        wide: true,
      },
    ],
  },
  signal: {
    get help() {
      return t('notifyTypes.signal.help');
    },
    fields: [
      {
        key: 'url',
        get label() {
          return t('notifyTypes.signal.fields.url.label');
        },
        kind: 'url',
        placeholder: 'http://signal-cli:8080',
        required: true,
        wide: true,
      },
      {
        key: 'number',
        get label() {
          return t('notifyTypes.signal.fields.number.label');
        },
        kind: 'text',
        placeholder: '+905551112233',
        required: true,
      },
      {
        key: 'recipients',
        get label() {
          return t('notifyTypes.signal.fields.recipients.label');
        },
        kind: 'text',
        placeholder: '+905551112233, +905553334455',
        get help() {
          return t('notifyTypes.signal.fields.recipients.help');
        },
        required: true,
        wide: true,
      },
    ],
  },
  pagerduty: {
    get help() {
      return t('notifyTypes.pagerduty.help');
    },
    fields: [
      {
        key: 'routing_key',
        label: 'Routing key',
        kind: 'secret',
        get help() {
          return t('notifyTypes.pagerduty.fields.routing_key.help');
        },
        required: true,
        wide: true,
      },
      {
        key: 'severity',
        get label() {
          return t('notifyTypes.pagerduty.fields.severity.label');
        },
        kind: 'select',
        def: 'critical',
        get help() {
          return t('notifyTypes.pagerduty.fields.severity.help');
        },
        options: [
          {
            v: 'critical',
            get l() {
              return t('notifyTypes.pagerduty.fields.severity.critical');
            },
          },
          {
            v: 'error',
            get l() {
              return t('notifyTypes.pagerduty.fields.severity.error');
            },
          },
          {
            v: 'warning',
            get l() {
              return t('notifyTypes.pagerduty.fields.severity.warning');
            },
          },
          {
            v: 'info',
            get l() {
              return t('notifyTypes.pagerduty.fields.severity.info');
            },
          },
        ],
      },
    ],
  },
  opsgenie: {
    get help() {
      return t('notifyTypes.opsgenie.help');
    },
    fields: [
      {
        key: 'api_key',
        get label() {
          return t('notifyTypes.opsgenie.fields.api_key.label');
        },
        kind: 'secret',
        get help() {
          return t('notifyTypes.opsgenie.fields.api_key.help');
        },
        required: true,
        wide: true,
      },
      {
        key: 'region',
        get label() {
          return t('notifyTypes.opsgenie.fields.region.label');
        },
        kind: 'select',
        def: 'us',
        options: [
          {
            v: 'us',
            get l() {
              return t('notifyTypes.opsgenie.fields.region.us');
            },
          },
          {
            v: 'eu',
            get l() {
              return t('notifyTypes.opsgenie.fields.region.eu');
            },
          },
        ],
      },
      {
        key: 'priority',
        get label() {
          return t('notifyTypes.opsgenie.fields.priority.label');
        },
        kind: 'select',
        def: 'P3',
        options: [
          {
            v: 'P1',
            get l() {
              return t('notifyTypes.opsgenie.fields.priority.p1');
            },
          },
          {
            v: 'P2',
            get l() {
              return t('notifyTypes.opsgenie.fields.priority.p2');
            },
          },
          {
            v: 'P3',
            get l() {
              return t('notifyTypes.opsgenie.fields.priority.p3');
            },
          },
          {
            v: 'P4',
            get l() {
              return t('notifyTypes.opsgenie.fields.priority.p4');
            },
          },
          {
            v: 'P5',
            get l() {
              return t('notifyTypes.opsgenie.fields.priority.p5');
            },
          },
        ],
      },
    ],
  },
  homeassistant: {
    get help() {
      return t('notifyTypes.homeassistant.help');
    },
    fields: [
      {
        key: 'url',
        get label() {
          return t('notifyTypes.homeassistant.fields.url.label');
        },
        kind: 'url',
        placeholder: 'http://homeassistant.local:8123',
        required: true,
        wide: true,
      },
      {
        key: 'token',
        get label() {
          return t('notifyTypes.homeassistant.fields.token.label');
        },
        kind: 'secret',
        get help() {
          return t('notifyTypes.homeassistant.fields.token.help');
        },
        required: true,
        wide: true,
      },
      {
        key: 'service',
        get label() {
          return t('notifyTypes.homeassistant.fields.service.label');
        },
        kind: 'text',
        placeholder: 'mobile_app_kadir_iphone',
        get help() {
          return t('notifyTypes.homeassistant.fields.service.help');
        },
        required: true,
        pattern: /^[a-z0-9_]+$/,
        get patternMsg() {
          return t('notifyTypes.homeassistant.fields.service.pattern');
        },
        wide: true,
      },
    ],
  },
  netgsm: {
    get help() {
      return t('notifyTypes.netgsm.help');
    },
    fields: [
      {
        key: 'usercode',
        get label() {
          return t('notifyTypes.netgsm.fields.usercode.label');
        },
        kind: 'text',
        required: true,
      },
      {
        key: 'password',
        get label() {
          return t('notifyTypes.netgsm.fields.password.label');
        },
        kind: 'secret',
        required: true,
      },
      {
        key: 'msgheader',
        get label() {
          return t('notifyTypes.netgsm.fields.msgheader.label');
        },
        kind: 'text',
        get placeholder() {
          return t('notifyTypes.netgsm.fields.msgheader.placeholder');
        },
        get help() {
          return t('notifyTypes.netgsm.fields.msgheader.help');
        },
        required: true,
      },
      {
        key: 'gsm',
        get label() {
          return t('notifyTypes.netgsm.fields.gsm.label');
        },
        kind: 'text',
        placeholder: '5551112233, 5553334455',
        get help() {
          return t('notifyTypes.netgsm.fields.gsm.help');
        },
        required: true,
        wide: true,
      },
      {
        key: 'turkish_chars',
        get label() {
          return t('notifyTypes.netgsm.fields.turkish_chars.label');
        },
        kind: 'bool',
        get help() {
          return t('notifyTypes.netgsm.fields.turkish_chars.help');
        },
        wide: true,
      },
    ],
  },
  twilio: {
    get help() {
      return t('notifyTypes.twilio.help');
    },
    fields: [
      { key: 'account_sid', label: 'Account SID', kind: 'text', placeholder: 'ACxxxxxxxx…', required: true },
      { key: 'auth_token', label: 'Auth token', kind: 'secret', required: true },
      {
        key: 'from',
        get label() {
          return t('notifyTypes.twilio.fields.from.label');
        },
        kind: 'text',
        placeholder: '+15551234567',
        get help() {
          return t('notifyTypes.twilio.fields.from.help');
        },
        required: true,
      },
      {
        key: 'to',
        get label() {
          return t('notifyTypes.twilio.fields.to.label');
        },
        kind: 'text',
        placeholder: '+905551112233, +905553334455',
        get help() {
          return t('notifyTypes.twilio.fields.to.help');
        },
        required: true,
        wide: true,
      },
    ],
  },
  pushbullet: {
    get help() {
      return t('notifyTypes.pushbullet.help');
    },
    fields: [
      {
        key: 'access_token',
        get label() {
          return t('notifyTypes.pushbullet.fields.access_token.label');
        },
        kind: 'secret',
        get help() {
          return t('notifyTypes.pushbullet.fields.access_token.help');
        },
        required: true,
        wide: true,
      },
      {
        key: 'channel_tag',
        get label() {
          return t('notifyTypes.pushbullet.fields.channel_tag.label');
        },
        kind: 'text',
        optional: true,
        get help() {
          return t('notifyTypes.pushbullet.fields.channel_tag.help');
        },
      },
      {
        key: 'device_iden',
        get label() {
          return t('notifyTypes.pushbullet.fields.device_iden.label');
        },
        kind: 'text',
        optional: true,
        get help() {
          return t('notifyTypes.pushbullet.fields.device_iden.help');
        },
      },
    ],
  },
  bark: {
    get help() {
      return t('notifyTypes.bark.help');
    },
    fields: [
      {
        key: 'server',
        get label() {
          return t('notifyTypes.bark.fields.server.label');
        },
        kind: 'url',
        def: 'https://api.day.app',
        placeholder: 'https://api.day.app',
        wide: true,
      },
      {
        key: 'device_key',
        get label() {
          return t('notifyTypes.bark.fields.device_key.label');
        },
        kind: 'secret',
        required: true,
        wide: true,
      },
      {
        key: 'sound',
        get label() {
          return t('notifyTypes.bark.fields.sound.label');
        },
        kind: 'text',
        optional: true,
        placeholder: 'alarm',
      },
      {
        key: 'group',
        get label() {
          return t('notifyTypes.bark.fields.group.label');
        },
        kind: 'text',
        def: APP_NAME,
        optional: true,
      },
    ],
  },
  line: {
    get help() {
      return t('notifyTypes.line.help');
    },
    fields: [
      {
        key: 'channel_access_token',
        get label() {
          return t('notifyTypes.line.fields.channel_access_token.label');
        },
        kind: 'secret',
        required: true,
        wide: true,
      },
      {
        key: 'to',
        get label() {
          return t('notifyTypes.line.fields.to.label');
        },
        kind: 'text',
        placeholder: 'U0123456789abcdef0123456789abcdef',
        get help() {
          return t('notifyTypes.line.fields.to.help');
        },
        required: true,
        pattern: /^[UCR][0-9a-f]{32}$/,
        get patternMsg() {
          return t('notifyTypes.line.fields.to.pattern');
        },
        wide: true,
      },
    ],
  },
  apprise: {
    get help() {
      return t('notifyTypes.apprise.help');
    },
    fields: [
      {
        key: 'server',
        get label() {
          return t('notifyTypes.apprise.fields.server.label');
        },
        kind: 'url',
        placeholder: 'http://apprise:8000',
        required: true,
        wide: true,
      },
      {
        key: 'urls',
        get label() {
          return t('notifyTypes.apprise.fields.urls.label');
        },
        kind: 'secret',
        placeholder: 'tgram://token/chatid, discord://…',
        get help() {
          return t('notifyTypes.apprise.fields.urls.help');
        },
        required: true,
        wide: true,
      },
    ],
  },
};

export const EMAIL_PORTS: Record<string, number> = { starttls: 587, tls: 465, none: 25 };

/**
 * Kanal olay süzgecindeki gruplar: her grup birlikte açılıp kapanan türleri
 * toplar (🔴 ile 🟢 çifti aynı grupta değildir: "düzelme istemeyen kanal"
 * kurulabilsin). Etiket: t(`notifications.rules.events.${id}`).
 */
export type NotifyEventGroupId = 'down' | 'up' | 'reminder' | 'cert' | 'domain' | 'slow' | 'location' | 'server' | 'probe';
export const NOTIFY_EVENT_GROUPS: { id: NotifyEventGroupId; kinds: NotifyKind[] }[] = [
  { id: 'down', kinds: ['down'] },
  { id: 'up', kinds: ['up'] },
  { id: 'reminder', kinds: ['reminder'] },
  { id: 'cert', kinds: ['cert'] },
  { id: 'domain', kinds: ['domain'] },
  { id: 'slow', kinds: ['slow', 'slow_resolved'] },
  { id: 'location', kinds: ['location_down', 'location_up'] },
  { id: 'server', kinds: ['server_alert', 'server_resolved'] },
  { id: 'probe', kinds: ['probe_offline', 'probe_online'] },
];
export const ALL_NOTIFY_KINDS: NotifyKind[] = NOTIFY_EVENT_GROUPS.flatMap((g) => g.kinds);

/** Webhook kanalının gönderdiği JSON örneği (monitör adı örnek veridir, dile göre). */
export const webhookExample = (d = t('notifyTypes.exampleDomain')) => `{
  "event": "up",
  "title": "…",
  "text": "…",
  "message": "HTTP 200 OK",
  "time": "2026-09-27T10:15:00+03:00",
  "downtime_seconds": 754,
  "monitor": {
    "id": 1,
    "name": ${JSON.stringify(t('notifyTypes.webhook.exampleName'))},
    "type": "http",
    "target": "https://${d}",
    "url": "https://uptime.${d}/#/monitors/1"
  },
  "incident": { "id": 42, "url": "https://uptime.${d}/#/incidents/42" },
  "locations": [{ "name": "Frankfurt", "message": "Timeout" }],
  "server": { "id": 3, "name": "web-1", "metric": "cpu", "value": 94, "threshold": 90, "minutes": 10 }
}`;
