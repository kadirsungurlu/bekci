import type { Shape } from '../types';
import type tr from '../tr/notifyTypes';

export default {
  /** Örnek adreslerdeki alan adı (placeholder, webhook örneği). */
  exampleDomain: 'example.com',
  groups: {
    messaging: 'Messaging',
    emailSms: 'Email & SMS',
    mobile: 'Push notifications',
    incident: 'Incident management',
    general: 'General',
  },
  form: {
    optional: '(optional)',
    keptHelp: 'The saved value is kept; type a new one to change it.',
    keptKey: 'Saved key is kept',
    keptValue: 'Saved value is kept (hidden)',
    replace: 'Replace',
    keepKey: 'Cancel, keep the saved key',
    keepValue: 'Cancel, keep the saved value',
  },
  errors: {
    required: '{field} is required.',
    url: '{field} must be a valid http(s) URL.',
    invalid: '{field} is invalid.',
    integer: '{field} must be a whole number.',
    number: '{field} must be a number.',
    range: '{field} must be between {min} and {max}.',
    pem: '{field} must be in PEM format (starts with -----BEGIN …).',
  },
  whatsapp: {
    help: 'Sends WhatsApp messages via WP API.',
    fields: {
      url: { label: 'WP API URL' },
      api_key: { label: 'API key' },
      to: {
        label: 'Recipient',
        placeholder: '905xxxxxxxxx or 1203...@g.us',
        help: 'Phone number with country code (without +) or a group ID ending in @g.us.',
      },
      from_number_id: {
        label: 'Sender number ID',
        help: 'Which number to send from, if you have more than one connected.',
      },
    },
  },
  telegram: {
    help: 'Create a bot with @BotFather, add it to your group/channel and enter the chat ID.',
    fields: {
      bot_token: { label: 'Bot token' },
      chat_id: { label: 'Chat ID' },
      thread_id: {
        label: 'Thread ID',
        help: 'The topic to post to in groups that use topics.',
      },
    },
  },
  email: {
    label: 'Email (SMTP)',
    fields: {
      host: { label: 'SMTP server' },
      security: { label: 'Security', none: 'None (25)' },
      from: { label: 'From' },
      to: { label: 'To', help: 'Separate multiple addresses with commas.', placeholder: 'me@example.com, team@example.com' },
    },
  },
  discord: {
    help: 'In Channel settings → Integrations, create a webhook and paste its URL.',
    fields: {
      webhook_url: { label: 'Webhook URL' },
    },
  },
  slack: {
    help: 'Turn on “Incoming Webhooks” in your Slack app and paste the generated URL.',
    fields: {
      webhook_url: { label: 'Webhook URL' },
    },
  },
  webhook: {
    exampleName: 'Website',
    fields: {
      url: { label: 'URL' },
      method: { label: 'Method' },
      headers: { label: 'Headers', help: 'One header per line: “Name: value”. Stored as a secret.' },
    },
  },
  ntfy: {
    fields: {
      server: { label: 'Server' },
      topic: { label: 'Topic', placeholder: 'my-uptime-topic' },
      token: { label: 'Access token', help: 'For protected topics.' },
      priority: { label: 'Priority (1-5)', help: 'Used for down notifications.' },
    },
  },
  gotify: {
    fields: {
      server: { label: 'Server' },
      app_token: { label: 'App token' },
      priority: { label: 'Priority (1-10)' },
    },
  },
  pushover: {
    fields: {
      user_key: { label: 'User key' },
      app_token: { label: 'App token' },
      device: { label: 'Device', help: 'Leave empty to send to all devices.' },
      priority: { label: 'Priority', lowest: 'Lowest (-2)', low: 'Low (-1)', high: 'High (1)' },
    },
  },
  teams: {
    help: 'Sends notifications to a Teams channel as an Adaptive Card (Workflows webhook).',
    fields: {
      webhook_url: {
        label: 'Webhook URL',
        help: 'In the Teams channel, create a “Workflows” › “Post to a channel when a webhook request is received” flow and paste the URL it gives you (the old “Incoming Webhook” connector has been retired).',
      },
    },
  },
  googlechat: {
    help: 'Sends notifications to a Google Chat space via webhook.',
    fields: {
      webhook_url: { label: 'Webhook URL', help: 'Create one under “Webhooks” in the space settings.' },
    },
  },
  mattermost: {
    help: 'Sends notifications to a Mattermost incoming webhook.',
    fields: {
      webhook_url: { label: 'Webhook URL', help: 'System Console › Integrations › Incoming Webhooks' },
      channel: { label: 'Channel', placeholder: '#alerts', help: "Leave empty to use the webhook's channel." },
      username: { label: 'Display name' },
      icon_url: { label: 'Icon URL', placeholder: 'https://example.com/icon.png' },
    },
  },
  rocketchat: {
    help: 'Sends notifications to a Rocket.Chat incoming webhook.',
    fields: {
      webhook_url: { label: 'Webhook URL', help: 'Administration › Integrations › Incoming' },
      channel: { label: 'Channel', placeholder: '#alerts' },
      alias: { label: 'Display name' },
      avatar: { label: 'Avatar URL' },
    },
  },
  matrix: {
    help: 'Sends messages to a Matrix room.',
    fields: {
      homeserver_url: { label: 'Homeserver URL' },
      access_token: {
        label: 'Access token',
        help: 'Element › Settings › Help & About › Advanced › Access Token (preferably a separate bot account).',
      },
      room_id: { label: 'Room ID', help: 'Room settings › Advanced (not the alias).' },
    },
  },
  signal: {
    help: 'Sends Signal messages via a signal-cli-rest-api server.',
    fields: {
      url: { label: 'Server URL' },
      number: { label: 'Sender number' },
      recipients: { label: 'Recipients', help: 'Separate multiple recipients with commas.' },
    },
  },
  pagerduty: {
    help: 'Triggers an incident in PagerDuty via Events API v2 and resolves it on recovery.',
    fields: {
      routing_key: { help: 'The service’s “Events API v2” integration key.' },
      severity: {
        label: 'Severity',
        help: 'SSL certificate alerts are always sent as “warning”.',
        critical: 'Critical',
        error: 'Error',
        warning: 'Warning',
        info: 'Info',
      },
    },
  },
  opsgenie: {
    help: 'Opens an alert in Opsgenie and closes it on recovery.',
    fields: {
      api_key: { label: 'API key', help: 'Teams › Integrations › API' },
      region: { label: 'Region', us: 'US (us)', eu: 'Europe (eu)' },
      priority: {
        label: 'Priority',
        p1: 'P1 — Critical',
        p2: 'P2 — High',
        p3: 'P3 — Moderate',
        p4: 'P4 — Low',
        p5: 'P5 — Informational',
      },
    },
  },
  homeassistant: {
    help: 'Sends notifications through a Home Assistant notify service (mobile app, etc.).',
    fields: {
      url: { label: 'Home Assistant URL' },
      token: { label: 'Long-lived access token', help: 'Profile › Security › Long-lived access tokens' },
      service: {
        label: 'Notify service',
        help: 'Without the “notify.” prefix.',
        pattern: 'Notify service may only contain lowercase letters, digits and underscores (without the notify. prefix).',
      },
    },
  },
  netgsm: {
    help: 'Sends SMS to Turkish phone numbers via Netgsm.',
    fields: {
      usercode: { label: 'User code' },
      password: { label: 'API password' },
      msgheader: { label: 'Sender ID', placeholder: 'COMPANY', help: 'A sender ID approved in Netgsm.' },
      gsm: { label: 'Phone numbers', help: 'Separate with commas. 0, 90 and +90 prefixes are fixed automatically.' },
      turkish_chars: {
        label: 'Turkish characters',
        help: 'When on, ç, ğ, ı, ö, ş, ü are kept (fewer characters may fit in each SMS).',
      },
    },
  },
  twilio: {
    help: 'Sends SMS via Twilio.',
    fields: {
      from: { label: 'From number', help: 'With country code, starting with +.' },
      to: { label: 'Recipient numbers', help: 'Separate with commas; each number with + and country code.' },
    },
  },
  pushbullet: {
    help: 'Sends Pushbullet notifications.',
    fields: {
      access_token: { label: 'Access token', help: 'Settings › Access Tokens' },
      channel_tag: { label: 'Channel tag', help: 'Can’t be used together with a device ID.' },
      device_iden: { label: 'Device ID', help: 'Leave empty to send to all your devices.' },
    },
  },
  bark: {
    help: 'Sends iOS notifications to the Bark app.',
    fields: {
      server: { label: 'Server URL' },
      device_key: { label: 'Device key' },
      sound: { label: 'Sound' },
      group: { label: 'Group' },
    },
  },
  line: {
    help: 'Sends messages via the LINE Messaging API (LINE Notify has been shut down).',
    fields: {
      channel_access_token: { label: 'Channel access token' },
      to: {
        label: 'Recipient ID',
        help: 'User (U…), group (C…) or room (R…) ID.',
        pattern: 'Recipient ID must be a 33-character ID starting with U, C or R.',
      },
    },
  },
  apprise: {
    help: 'Sends to 100+ services through your own Apprise API server.',
    fields: {
      server: { label: 'Apprise server URL' },
      urls: { label: 'Apprise URLs', help: 'Separate multiple URLs with commas. Stored as a secret.' },
    },
  },
} satisfies Shape<typeof tr>;
