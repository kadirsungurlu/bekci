// State mark names (components/StateMark.svelte). The live topology page and its texts are Pro
// (bekci-pro web/ek/topoloji); the namespace name is kept so existing keys stay valid.
import type { Shape } from '../types';
import type tr from '../tr/topology';

export default {
  cls: {
    ok: 'Working',
    warn: 'Warning',
    bad: 'Problem',
    pend: 'Pending',
    off: 'Off',
    maint: 'In maintenance',
  },
} satisfies Shape<typeof tr>;
