import type { Shape } from '../types';
import type tr from '../tr/settings';

export default {
  title: 'Settings',
  sectionsLabel: 'Settings sections',
  tabs: {
    account: 'My account',
    users: 'Users',
    general: 'General',
    tags: 'Tags',
    probes: 'Check locations',
    backup: 'Backup / Restore',
    audit: 'Audit log',
  },
  forbiddenTitle: "You don't have access to this section",
  forbiddenText:
    'Only accounts with the admin role can see users, general settings, check locations, backups and the audit log; editors can also manage tags.',
  backToAccount: 'Back to my account',
  general: {
    retentionTitle: 'Data retention and alerts',
    rawDays: 'Raw check history (days)',
    rawDaysHelp: 'A separate record for every check. 1-90 days.',
    hourlyDays: 'Hourly summaries (days)',
    hourlyDaysHelp: '90-3650 days. The 30- and 90-day charts are drawn from these summaries; daily summaries are kept forever.',
    certDays: 'SSL alert days',
    certDaysPlaceholder: 'e.g. 21, 14, 7',
    certDaysHelp:
      "A notification is sent when the certificate is this many days from expiry. Separate with commas (up to 10); leave empty to turn off SSL alerts.",
    backupKeep: 'Nightly backups to keep',
    backupKeepHelp: 'The database is backed up every night and the last N backups are kept. 0 = no backups.',
    notifyLang: 'Notification language',
    notifyLangHelp: 'Language of the messages sent to all notification channels (email, Telegram, webhook…).',
    errRaw: 'Raw check history must be between 1 and 90 days.',
    errHourly: 'Hourly summaries must be kept between 90 and 3650 days.',
    errHourlyShort: "Hourly summaries can't be kept for less time than the raw check history.",
    errCertDays: 'SSL alert days must be whole numbers between 0 and 90 (separated by commas).',
    errCertCount: 'You can enter at most 10 SSL alert days.',
    errBackup: 'Number of backups must be between 0 and 60.',
    saved: 'Settings saved',
    promText: 'Metrics are available at {url} and require an API key (viewer access is enough). Create one under {link}.',
    promLink: 'My account › API keys',
    promAlt: 'Instead of {a}, you can also use {b}.',
    about: 'About',
  },
} satisfies Shape<typeof tr>;
