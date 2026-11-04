<script lang="ts">
  // Betik listelerinin filtre çubuğu: Olaylar sayfasındaki düzenin aynısı (bölümlü seçim + arama).
  import Icon from './Icon.svelte';
  import { t } from '../lib/i18n';

  let {
    q = $bindable(''),
    value = $bindable(''),
    options = [],
    placeholder,
  }: { q?: string; value?: string; options?: { k: string; label: string }[]; placeholder: string } = $props();
</script>

<div class="filters">
  {#if options.length}
    <div class="seg" role="radiogroup" aria-label={t('scripts.filter.label')}>
      {#each options as o (o.k)}
        <button type="button" role="radio" aria-checked={value === o.k} class:active={value === o.k} onclick={() => (value = o.k)}>{o.label}</button>
      {/each}
    </div>
  {/if}
  <div class="search">
    <Icon name="search" size={15} />
    <input type="search" class="input" {placeholder} aria-label={t('scripts.filter.search')} bind:value={q} />
  </div>
</div>

<style>
  .filters {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    flex-wrap: wrap;
    margin: -4px 0 14px;
  }
  .search {
    position: relative;
    display: flex;
    align-items: center;
    margin-left: auto;
  }
  .search :global(svg) {
    position: absolute;
    left: 10px;
    color: var(--muted);
    pointer-events: none;
  }
  .search input {
    padding-left: 32px;
    width: 280px;
    height: 34px;
  }
  @media (max-width: 640px) {
    .filters {
      flex-direction: column;
      align-items: stretch;
    }
    .search {
      margin-left: 0;
    }
    .search input {
      width: 100%;
      height: 44px;
    }
  }
</style>
