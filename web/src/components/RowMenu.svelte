<script lang="ts" module>
  import type { IconName } from './Icon.svelte';

  export interface MenuItem {
    label: string;
    icon: IconName;
    danger?: boolean;
    href?: string;
    onclick?: () => void;
  }

  // Aynı anda tek menü açık kalsın.
  let closeOpen: (() => void) | null = null;
</script>

<script lang="ts">
  import Icon from './Icon.svelte';
  import { t } from '../lib/i18n';

  let { items, label: labelProp }: { items: MenuItem[]; label?: string } = $props();
  const label = $derived(labelProp ?? t('common.actions'));

  let open = $state(false);
  let up = $state(false);
  let btn: HTMLButtonElement | undefined = $state();

  function close() {
    open = false;
  }

  function toggle(e: MouseEvent) {
    e.stopPropagation();
    if (open) return close();
    closeOpen?.();
    const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
    up = window.innerHeight - r.bottom < 60 + items.length * 40;
    open = true;
    closeOpen = close;
  }

  function pick(it: MenuItem) {
    close();
    it.onclick?.();
  }

  function onKey(e: KeyboardEvent) {
    if (open && e.key === 'Escape') {
      close();
      btn?.focus();
    }
  }
</script>

<svelte:document onclick={close} onkeydown={onKey} />

<div class="rm">
  <button bind:this={btn} type="button" class="btn ghost icon sm" aria-label={label} aria-haspopup="menu" aria-expanded={open} onclick={toggle}>
    <Icon name="more" />
  </button>
  {#if open}
    <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
    <div class="dropdown" class:up role="menu" tabindex="-1" onclick={(e) => e.stopPropagation()}>
      {#each items as it (it.label)}
        {#if it.href}
          <a role="menuitem" href={it.href} class:danger={it.danger} onclick={close}><Icon name={it.icon} size={15} /> {it.label}</a>
        {:else}
          <button role="menuitem" type="button" class:danger={it.danger} onclick={() => pick(it)}>
            <Icon name={it.icon} size={15} />
            {it.label}
          </button>
        {/if}
      {/each}
    </div>
  {/if}
</div>

<style>
  .rm {
    position: relative;
    display: inline-flex;
  }
  .dropdown {
    position: absolute;
    right: 0;
    top: calc(100% + 4px);
    z-index: 30;
    min-width: 190px;
    background: var(--bg-elev);
    border: 1px solid var(--border-strong);
    border-radius: 10px;
    box-shadow: var(--shadow);
    padding: 5px;
    display: flex;
    flex-direction: column;
    animation: pop 0.12s ease-out;
  }
  .dropdown.up {
    top: auto;
    bottom: calc(100% + 4px);
  }
  .dropdown a,
  .dropdown button {
    display: flex;
    align-items: center;
    gap: 9px;
    padding: 8px 10px;
    border-radius: 7px;
    background: none;
    border: none;
    color: var(--text);
    font: inherit;
    font-size: 0.9rem;
    cursor: pointer;
    text-align: left;
    text-decoration: none;
    white-space: nowrap;
  }
  .dropdown a:focus-visible,
  .dropdown button:focus-visible {
    background: var(--card-2);
    outline: none;
  }
  @media (hover: hover) {
    .dropdown a:hover,
    .dropdown button:hover {
      background: var(--card-2);
    }
  }
  .dropdown .danger {
    color: var(--down-text-2);
  }
  @keyframes pop {
    from {
      opacity: 0;
      transform: translateY(-4px);
    }
  }
</style>
