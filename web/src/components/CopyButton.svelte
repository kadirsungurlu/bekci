<script lang="ts">
  // Panoya kopyalama düğmesi: başarılı olunca düğmenin kendisi birkaç saniye
  // "Kopyalandı" gösterir (pencere içindeyken de görünür geri bildirim).
  import { onDestroy } from 'svelte';
  import { copyText, toast } from '../lib/ui.svelte';
  import Icon from './Icon.svelte';

  let {
    text,
    label = 'Kopyala',
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

  let copied = $state(false);
  let timer: ReturnType<typeof setTimeout> | undefined;

  async function copy() {
    const t = typeof text === 'function' ? text() : text;
    if (!t) return;
    if (await copyText(t)) {
      copied = true;
      clearTimeout(timer);
      timer = setTimeout(() => (copied = false), 2000);
    } else {
      toast.error('Panoya kopyalanamadı; metni elle seçip kopyalayın.');
    }
  }

  onDestroy(() => clearTimeout(timer));
</script>

<button
  type="button"
  class={cls}
  class:copied
  aria-label={iconOnly ? (copied ? 'Kopyalandı' : (ariaLabel ?? label)) : ariaLabel}
  data-tip={iconOnly ? (copied ? 'Kopyalandı' : (ariaLabel ?? label)) : undefined}
  onclick={copy}
  {disabled}
>
  <Icon name={copied ? 'check' : 'copy'} {size} />
  {#if !iconOnly}<span aria-live="polite">{copied ? 'Kopyalandı' : label}</span>{/if}
</button>

<style>
  .copied :global(svg) {
    color: var(--up);
  }
  :global(.btn.primary).copied :global(svg) {
    color: inherit;
  }
</style>
