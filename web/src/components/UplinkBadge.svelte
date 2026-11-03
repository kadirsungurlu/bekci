<script lang="ts">
  // OOB ikinci hat rozeti: ajan hangi hattan bağlı (ana hat / OOB) ve OOB arayüzü
  // bulunamadı uyarısı. OOB tanımlı değilse (name boş) hiçbir şey çizmez.
  import Icon from './Icon.svelte';
  import { t } from '../lib/i18n';
  import { clock } from '../lib/ui.svelte';
  import { fmtDate, fmtRelative } from '../lib/format';

  let {
    name = '',
    ip = '',
    uplink = 'primary',
    since = 0,
    ifaceMissing = false,
    focus = true,
    large = false,
  }: {
    /** OOB hattının görünen adı; boş: OOB tanımlı değil. */
    name?: string;
    ip?: string;
    uplink?: string;
    since?: number;
    ifaceMissing?: boolean;
    /** Klavye odağı (ipucu için); bağlantı içindeyse kapatılır. */
    focus?: boolean;
    /** Başlıktaki .pill ile aynı yükseklik (ServerDetail). */
    large?: boolean;
  } = $props();
  const tab = $derived(focus ? 0 : undefined);

  const onOOB = $derived(uplink === 'oob');
  const at = $derived(since ? `${fmtRelative(since, clock.now)} (${fmtDate(since)})` : '—');
</script>

{#if name}
  <span class="ub" class:large>
    {#if onOOB}
      <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
      <span class="badge pending" tabindex={tab} data-tip={t('probes.uplink.viaTip', { name, ip: ip || '—', at })}>
        <Icon name="network" size={12} />
        <span class="tx">{t('probes.uplink.via', { name })}</span>
      </span>
    {:else}
      <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
      <span class="badge" tabindex={tab} data-tip={t('probes.uplink.primaryTip', { name })}>
        <Icon name="network" size={12} />
        <span class="tx">{t('probes.uplink.primary')}</span>
      </span>
    {/if}
    {#if ifaceMissing}
      <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
      <span class="badge pending" tabindex={tab} data-tip={t('probes.uplink.ifaceMissingTip')}>
        <Icon name="alert-circle" size={12} />
        <span class="tx">{t('probes.uplink.ifaceMissing')}</span>
      </span>
    {/if}
  </span>
{/if}

<style>
  .ub {
    display: inline-flex;
    flex-wrap: wrap;
    gap: 4px;
    max-width: 100%;
    vertical-align: middle;
  }
  .ub .badge {
    gap: 4px;
    max-width: 100%;
    cursor: help;
  }
  .ub.large .badge {
    height: 28px;
    padding: 0 12px;
    border-radius: 999px;
    font-size: 0.78rem;
    gap: 6px;
  }
  .tx {
    overflow: hidden;
    text-overflow: ellipsis;
    min-width: 0;
  }
</style>
