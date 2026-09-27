<script lang="ts">
  import type { Snippet } from 'svelte';
  import Icon from './Icon.svelte';

  let {
    open = $bindable(false),
    title,
    width = 560,
    onclose,
    children,
    footer,
  }: {
    open?: boolean;
    title: string;
    width?: number;
    onclose?: () => void;
    children: Snippet;
    footer?: Snippet;
  } = $props();

  let dialog: HTMLDialogElement | undefined = $state();
  let downOnBackdrop = false;

  $effect(() => {
    if (!dialog) return;
    if (open && !dialog.open) dialog.showModal();
    else if (!open && dialog.open) dialog.close();
  });

  function handleClose() {
    if (open) {
      open = false;
      onclose?.();
    }
  }
</script>

<dialog
  bind:this={dialog}
  style="--w:{width}px"
  onclose={handleClose}
  onpointerdown={(e) => (downOnBackdrop = e.target === dialog)}
  onclick={(e) => {
    if (downOnBackdrop && e.target === dialog) dialog?.close();
    downOnBackdrop = false;
  }}
>
  {#if open}
    <div class="box">
      <header>
        <h2>{title}</h2>
        <button type="button" class="btn ghost icon" aria-label="Kapat" onclick={() => dialog?.close()}>
          <Icon name="x" />
        </button>
      </header>
      <div class="body">
        {@render children()}
      </div>
      {#if footer}
        <footer>{@render footer()}</footer>
      {/if}
    </div>
  {/if}
</dialog>

<style>
  dialog {
    padding: 0;
    border: none;
    background: transparent;
    color: var(--text);
    width: min(var(--w), calc(100vw - 24px));
    max-width: none;
    max-height: calc(100dvh - 24px);
    overflow: visible;
  }
  dialog::backdrop {
    background: rgba(3, 7, 15, 0.7);
    backdrop-filter: blur(2px);
  }
  .box {
    background: var(--bg-elev);
    border: 1px solid var(--border-strong);
    border-radius: 14px;
    box-shadow: var(--shadow);
    display: flex;
    flex-direction: column;
    max-height: calc(100dvh - 24px);
  }
  header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 16px 16px 12px 20px;
    border-bottom: 1px solid var(--border);
  }
  .body {
    padding: 18px 20px;
    overflow-y: auto;
    overscroll-behavior: contain;
  }
  footer {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
    padding: 14px 20px;
    border-top: 1px solid var(--border);
  }
  @media (max-width: 640px) {
    .body {
      padding: 16px;
    }
    footer {
      padding: 12px 16px;
    }
  }
</style>
