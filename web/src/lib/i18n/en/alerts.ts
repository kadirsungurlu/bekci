import type { Shape } from '../types';
import type tr from '../tr/alerts';

export default {
  fullest: 'Fullest disk',
  none: 'No alert rules for this server.',
  empty: 'No rules: no alerts will be sent for this server.',
  add: 'Add rule',
  saved: 'Alert rules saved',
  firing: 'Firing',
  firingTitle: 'This rule is currently firing',
  active: 'Enabled',
  geTitle: 'When the value is at or above the threshold',
  unitNoData: 'min without data',
  unitAvg: 'min average',
  mountUsage: 'Usage of {mount}',
  sentOffline: 'if no data for {n} min',
  sentOver: 'if the {n}-min average is {v} or higher',
  aria: {
    ctx: '{metric} rule',
    metric: '{ctx}: metric',
    mount: '{ctx}: disk',
    threshold: '{ctx}: threshold, at least ({unit})',
    minutes: '{ctx}: duration (minutes)',
    active: '{ctx} enabled',
    remove: 'Remove {metric} rule',
  },
  err: {
    minutes: 'Duration must be 1–60 minutes',
    dupDisk: 'There is already a disk rule for {mount}',
    dup: 'There is already a rule for this metric',
    pct: 'Threshold must be between 1% and 100%',
    temp: 'Threshold must be between {min} and {max} °C',
    range: 'Threshold must be between {min} and {max}',
  },
} satisfies Shape<typeof tr>;
