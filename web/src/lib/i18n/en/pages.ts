import type { Shape } from '../types';
import type tr from '../tr/pages';

export default {
  lang: {
    label: 'Page language',
    help: 'Visitors see the page in this language (status texts, dates, durations).',
  },
} satisfies Shape<typeof tr>;
