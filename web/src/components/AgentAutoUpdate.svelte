<script lang="ts" module>
  import type { AgentUpdate } from '../lib/api';

  export type AutoChoice = 'inherit' | 'on' | 'off';

  /** Ajanın kayıtlı ayarı → seçim (null: genel ayar). */
  export const autoChoice = (u?: AgentUpdate): AutoChoice => (u?.auto == null ? 'inherit' : u.auto ? 'on' : 'off');

  /** Seçim → API değeri (null: genel ayara dön). */
  export const autoValue = (c: AutoChoice): boolean | null => (c === 'inherit' ? null : c === 'on');
</script>

<script lang="ts">
  // Ajan başına otomatik güncelleme seçimi (genel ayar / açık / kapalı).
  import { t } from '../lib/i18n';

  let {
    value = $bindable('inherit'),
    update,
    id,
  }: { value?: AutoChoice; update?: AgentUpdate; id: string } = $props();

  // Genel ayarın değeri yalnızca ajan genel ayarı kullanıyorsa bilinir (auto_effective).
  const inheritLabel = $derived(
    update && update.auto == null
      ? t('probes.update.autoInherit', { state: update.auto_effective ? t('probes.update.autoOn') : t('probes.update.autoOff') })
      : t('probes.update.autoInheritPlain'),
  );
</script>

<div class="field">
  <label for={id}>{t('probes.update.auto')}</label>
  <select {id} class="input" bind:value>
    <option value="inherit">{inheritLabel}</option>
    <option value="on">{t('probes.update.autoOn')}</option>
    <option value="off">{t('probes.update.autoOff')}</option>
  </select>
  <span class="help">{t('probes.update.autoHelp')}</span>
</div>
