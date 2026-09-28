<script lang="ts">
  // Panoya kopyalama düğmesi: başarılı olunca düğmenin kendisi birkaç saniye
  // "Kopyalandı" gösterir (pencere içindeyken de görünür geri bildirim).
  import { onDestroy } from 'svelte';
  import { copyText, toast } from '../lib/ui.svelte';
  import Icon from './Icon.svelte';
  import { t } from '../lib/i18n';

  let {
    text,
    label: labelProp,
    class: cls = 'btn sm',
    iconOnly = false,
    ariaLabel,
    disabled = false,
    size = 14,
  }: {
    /** Kopyalanacak metin (veya tıklanınca hesaplanan metin). */
    text: string | (() => string);
    label?: string;
    class?: string;
    /** Yalnızca simge (ör. şifre alanının yanındaki kare düğme). */
    iconOnly?: boolean;
    ariaLabel?: string;
    disabled?: boolean;
    size?: number;
  } = $props();

  // Varsayılan etiket geçerli dilde ("Kopyala" / "Copy").
  const label = $derived(labelProp ?? t('common.copy'));
  let copied = $state(false);
  let timer: ReturnType<typeof setTimeout> | undefined;

  async function copy() {
    const value = typeof text === 'function' ? text() : text;
    if (!value) return;
    if (await copyText(value)) {
      copied = true;
      clearTimeout(timer);
      timer = setTimeout(() => (copied = false), 2000);
    } else {
      toast.error(t('common.copyFailed'));
    }
  }

  onDestroy(() => clearTimeout(timer));
</script>

<button
  type="button"
  class={cls}
  class:copied
  aria-label={iconOnly ? (copied ? t('common.copied') : (ariaLabel ?? label)) : ariaLabel}
  data-tip={iconOnly ? (copied ? t('common.copied') : (ariaLabel ?? label)) : undefined}
  onclick={copy}
  {disabled}
>
  <Icon name={copied ? 'check' : 'copy'} {size} />
  {#if !iconOnly}<span aria-live="polite">{copied ? t('common.copied') : label}</span>{/if}
</button>

<style>
  .copied :global(svg) {
    color: var(--up);
  }
  :global(.btn.primary).copied :global(svg) {
    color: inherit;
  }
</style>
