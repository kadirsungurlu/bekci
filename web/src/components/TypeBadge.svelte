<script lang="ts">
  import type { MonitorType } from '../lib/api';
  import { TYPE_LABELS } from '../lib/format';

  let { type }: { type: MonitorType } = $props();
</script>

<span class="badge">{TYPE_LABELS[type] ?? type.toUpperCase()}</span>
