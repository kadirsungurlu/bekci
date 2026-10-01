<script lang="ts">
  import { tagSurface } from '../lib/tags';
  import Icon from './Icon.svelte';
  import { t } from '../lib/i18n';

  let {
    name,
    color,
    value = '',
    size = 'md',
    onremove,
  }: {
    name: string;
    color: string;
    value?: string;
    size?: 'sm' | 'md';
    /** Verilirse sağda kaldırma düğmesi gösterilir. */
    onremove?: () => void;
  } = $props();

  // Renk sunucudan #rrggbb gelir; yine de yalnızca geçerli değer stile yazılır.
  // Zemin, yazı okunaklı olacak kadar ayarlanır (bkz. tagSurface).
  const surface = $derived(tagSurface(color));
  const bg = $derived(surface.bg);
  const ink = $derived(surface.ink);
</script>

<span class="tag-chip {ink} {size}" style="--tag-bg:{bg}" title={value ? `${name}: ${value}` : name}>
  <span class="tn">{name}</span>{#if value}<span class="tv">: {value}</span>{/if}
  {#if onremove}
    <button type="button" class="tx" aria-label={t('tags.removeTag', { name })} onclick={onremove}><Icon name="x" size={12} stroke={2.5} /></button>
  {/if}
</span>

<style>
  .tag-chip {
    display: inline-flex;
    align-items: center;
    max-width: 100%;
    min-width: 0;
    height: 22px;
    padding: 0 8px;
    border-radius: 999px;
    background: var(--tag-bg);
    font-size: 0.74rem;
    font-weight: 600;
    line-height: 1;
    white-space: nowrap;
  }
  .tag-chip.dark {
    color: var(--tag-ink-dark);
  }
  .tag-chip.light {
    color: var(--tag-ink-light);
  }
  .tag-chip.sm {
    height: 19px;
    padding: 0 7px;
    font-size: 0.7rem;
  }
  .tn,
  .tv {
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .tn {
    flex-shrink: 0;
    max-width: 100%;
  }
  .tv {
    font-weight: 500;
    min-width: 0;
  }
  .tx {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    width: 18px;
    height: 18px;
    margin: 0 -4px 0 4px;
    border: none;
    border-radius: 50%;
    background: transparent;
    color: inherit;
    cursor: pointer;
    padding: 0;
  }
  .tx:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }
  @media (hover: hover) {
    .tx:hover {
      background: var(--tag-x-hover);
    }
  }
</style>
