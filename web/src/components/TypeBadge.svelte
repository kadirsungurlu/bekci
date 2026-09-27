<script lang="ts">
  import { typeLabel } from '../lib/monitorTypes';

  let { type }: { type: string } = $props();
</script>

<span class="badge">{typeLabel(type)}</span>
