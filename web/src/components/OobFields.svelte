<script lang="ts">
  // OOB (ikinci hat) IP + ad alanları: kontrol noktası ve sunucu ajanı ayar
  // pencereleri ortak kullanır (PUT /api/probes/{id} oob_ip, oob_name).
  import { t } from '../lib/i18n';

  let {
    id,
    ip = $bindable(''),
    name = $bindable(''),
    field = '',
    error = '',
  }: {
    id: string;
    ip?: string;
    name?: string;
    /** Sunucunun hatalı bildirdiği alan (oob_ip | oob_name); hata o alanın altında. */
    field?: string;
    error?: string;
  } = $props();
</script>

<fieldset class="oob">
  <legend>{t('probes.oob.title')}</legend>
  <p class="help nomargin">{t('probes.oob.help')}</p>
  <div class="field">
    <label for="{id}-ip">{t('probes.oob.ip')}</label>
    <input
      id="{id}-ip"
      class="input mono"
      maxlength="45"
      bind:value={ip}
      placeholder={t('probes.oob.ipPlaceholder')}
      spellcheck="false"
      autocomplete="off"
      aria-invalid={field === 'oob_ip'}
      aria-describedby={field === 'oob_ip' ? `${id}-ip-err` : undefined}
    />
    {#if field === 'oob_ip'}<span id="{id}-ip-err" class="ferr" role="alert">{error}</span>{/if}
  </div>
  <div class="field">
    <label for="{id}-name">{t('probes.oob.name')}</label>
    <input
      id="{id}-name"
      class="input"
      maxlength="40"
      bind:value={name}
      placeholder={t('probes.oob.namePlaceholder')}
      disabled={!ip.trim()}
      aria-invalid={field === 'oob_name'}
      aria-describedby={field === 'oob_name' ? `${id}-name-err` : `${id}-name-help`}
    />
    {#if field === 'oob_name'}
      <span id="{id}-name-err" class="ferr" role="alert">{error}</span>
    {:else}
      <span id="{id}-name-help" class="help">{t('probes.oob.nameHelp')}</span>
    {/if}
  </div>
</fieldset>

<style>
  .oob {
    display: grid;
    gap: 12px;
    margin: 0;
    padding: 10px 12px 12px;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    min-width: 0;
  }
  .oob legend {
    padding: 0 6px;
    font-weight: 600;
    font-size: 0.9rem;
  }
  .nomargin {
    margin: 0;
  }
  .ferr {
    color: var(--down-text);
    font-size: 0.8rem;
  }
</style>
