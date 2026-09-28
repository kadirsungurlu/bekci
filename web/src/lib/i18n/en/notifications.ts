import type { Shape } from '../types';
import type tr from '../tr/notifications';

export default {
  list: {
    title: 'Notifications',
    newChannel: 'New channel',
    loadFailed: "Couldn't load channels",
    emptyTitle: 'No notification channels yet',
    emptyText: 'Add WhatsApp, Telegram, email or another channel to get notified when a monitor goes down.',
    addChannel: 'Add channel',
    default: 'Default',
    disabled: 'Disabled',
    monitorCount: '{count} monitor|{count} monitors',
    footHelp:
      '“Default” channels are selected automatically on newly added monitors. You can change each monitor’s channels on its edit page.',
    added: '“{name}” added',
    saved: 'Channel saved',
    deletedNamed: '“{name}” deleted',
    deleted: 'Channel deleted',
  },
  form: {
    titleEdit: 'Edit notification channel',
    titleNew: 'New notification channel',
    type: 'Type',
    namePlaceholder: 'e.g. {type} — Team',
    nameRequired: 'Channel name is required.',
    exampleSummary: 'Example JSON payload',
    exampleHelp:
      '{event}: {down}, {up}, {reminder}, {cert} or {test}. {downtime_seconds} is included in recovery notifications, {cert_days} in SSL alerts.',
    active: 'Enabled',
    activeHelp: 'Disabled channels don’t send notifications.',
    isDefault: 'Add to new monitors by default',
    applyExisting: 'Add to all existing monitors',
    applyExistingHelp: 'When you save, this channel is attached to all current monitors.',
    test: 'Send test notification',
    testSent: 'Test notification sent. Check your channel.',
    rebind:
      '{dest} changed, so for security the saved {fields} won’t be carried over to the new destination. Re-enter before saving.',
    deleteTitle: 'Delete channel',
    deleteMessage: 'The “{name}” notification channel will be deleted and removed from the monitors that use it.',
  },
} satisfies Shape<typeof tr>;
