import type { Shape } from '../types';
import type tr from '../tr/apiKeys';

export default {
  title: 'API keys',
  intro: 'For accessing the API from your scripts and tools such as Prometheus. Example:',
  newKey: 'New key',
  showAll: "All users' keys",
  empty: 'No API keys yet.',
  col: {
    owner: 'Owner',
    scope: 'Scope',
    prefix: 'Prefix',
    lastUsed: 'Last used',
    expires: 'Expires',
    status: 'Status',
    action: 'Action',
  },
  status: {
    active: 'Active',
    expired: 'Expired',
    revoked: 'Revoked',
  },
  never: 'Never',
  noExpiry: 'Never',
  revoke: 'Revoke',
  revokeTitle: 'Revoke key',
  revokeMessage: '“{name}” — Revoke this key? Scripts that use it will stop working.',
  revoked: 'Key revoked',
  form: {
    title: 'New API key',
    readyTitle: 'Your key is ready',
    errName: 'Give the key a name (e.g. “Grafana” or “backup script”).',
    errExpires: 'Expiry date must be in the future.',
    namePlaceholder: 'e.g. Grafana, backup script',
    optional: '(optional)',
    help: "A key can't exceed your own role; if your role is lowered, the key's scope is lowered too. Leave the expiry date empty for a key that never expires. “{viewer}” is enough for monitoring and Prometheus only.",
    shownOnce: "This key won't be shown again; copy it now and store it somewhere safe.",
    yourKey: 'Your key:',
    example: 'Usage example',
    copiedClose: "I've copied it, close",
  },
} satisfies Shape<typeof tr>;
