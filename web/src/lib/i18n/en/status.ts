import type { Shape } from '../types';
import type tr from '../tr/status';

export default {
  up: 'Up',
  down: 'Down',
  pending: 'Pending',
  paused: 'Paused',
  maintenance: 'Maintenance',
  retrying: 'Retrying',
  unknown: 'Unknown',
  time: {
    sec: '{n}s',
    min: '{n}m',
    hour: '{n}h',
    day: '{n}d',
    ago: '{d} ago',
    justNow: 'just now',
    perSec: '/s',
  },
  bars: {
    ongoing: ' (in progress)',
    uptime: 'Uptime {pct}',
    failedChecks: '{count} failed check|{count} failed checks',
    avgResponse: 'Avg. response {ms}',
    summaryNoData: 'Last {n} hours: no data',
    summary: 'Last {n} hours: uptime {pct}',
    summaryErrors: ', errors in {count} hour|, errors in {count} hours',
    summaryNoErrors: ', no errors',
  },
  chart: {
    min: 'Min',
    avg: 'Average',
    max: 'Max',
    avgShort: 'Avg.',
    errors: '{count} error|{count} errors',
    responseChart: 'Response time chart',
    usage: 'Usage',
    summaryNoData: '{label}: no data in this range',
    summary: '{label}. {series}: min {min}, average {avg}, max {max}.',
  },
} satisfies Shape<typeof tr>;
