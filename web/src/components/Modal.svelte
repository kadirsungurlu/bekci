<script lang="ts">
  import type { Snippet } from 'svelte';
  import { lockScroll } from '../lib/ui.svelte';
  import Icon from './Icon.svelte';
  import { t } from '../lib/i18n';

  let {
    open = $bindable(false),
    title,
    width = 560,
    dismissable = true,
    canClose,
    onclose,
    children,
    footer,
  }: {
    open?: boolean;
    title: string;
    width?: number;
    /** false: Esc, arka plana dokunma ve × ile kapanmaz (ör. kurtarma kodları adımı). */
    dismissable?: boolean;
    /**
     * Esc, arka plan ve × ile kapatmadan önce sorulur (ör. kaydedilmemiş
     * değişiklik); false dönerse pencere açık kalır.
     */
    canClose?: () => boolean | Promise<boolean>;
    onclose?: () => void;
    children: Snippet;
    footer?: Snippet;
  } = $props();

  const uid = $props.id();
  const titleId = `${uid}-title`;

  let dialog: HTMLDialogElement | undefined = $state();
  let downOnBackdrop = false;

  $effect(() => {
    if (!dialog) return;
    if (open && !dialog.open) {
      dialog.showModal();
      // Odak kapatma düğmesine değil, ilk görünür form alanına gitsin.
      const first = dialog.querySelector<HTMLElement>(
        '.body input:not([type=hidden]):not([type=checkbox]):not([hidden]):not([disabled]), .body select:not([disabled]), .body textarea:not([disabled])',
      );
      if (first && !isTouch()) first.focus();
    } else if (!open && dialog.open) dialog.close();
  });

  // Açıkken arkadaki sayfa kaymasın (iOS'ta parmakla kaydırma arkaya geçer).
  $effect(() => {
    if (open) return lockScroll();
  });

  // Dokunmatik cihazda otomatik odak klavyeyi açıp pencereyi kaydırır; orada yapma.
  const isTouch = () => matchMedia('(hover: none)').matches;

  /** Kullanıcının kapatma isteği (Esc, arka plan, ×): canClose sorulur. */
  let asking = false;
  async function requestClose() {
    if (!dismissable || asking) return;
    if (canClose) {
      asking = true;
      try {
        if (!(await canClose())) return;
      } finally {
        asking = false;
      }
    }
    dialog?.close();
  }

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
  aria-labelledby={titleId}
  onclose={handleClose}
  oncancel={(e) => {
    // Esc: kapatılamayan pencerede yok sayılır; kapatmadan önce sorulacaksa önce sorulur.
    if (!dismissable || canClose) e.preventDefault();
    if (dismissable && canClose) requestClose();
  }}
  onpointerdown={(e) => (downOnBackdrop = e.target === dialog)}
  onclick={(e) => {
    if (dismissable && downOnBackdrop && e.target === dialog) requestClose();
    downOnBackdrop = false;
  }}
>
  {#if open}
    <div class="box">
      <header>
        <h2 id={titleId}>{title}</h2>
        {#if dismissable}
          <button type="button" class="btn ghost icon" aria-label={t('common.close')} onclick={requestClose}>
            <Icon name="x" />
          </button>
        {/if}
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
    max-height: calc(100dvh - 24px - env(safe-area-inset-top) - env(safe-area-inset-bottom));
    overflow: visible;
  }
  dialog::backdrop {
    background: var(--overlay);
    backdrop-filter: blur(2px);
  }
  .box {
    background: var(--bg-elev);
    border: 1px solid var(--border-strong);
    border-radius: 14px;
    box-shadow: var(--shadow);
    display: flex;
    flex-direction: column;
    max-height: calc(100dvh - 24px - env(safe-area-inset-top) - env(safe-area-inset-bottom));
  }
  header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    min-height: 58px;
    padding: 10px 16px 10px 20px;
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
    /* Dokunmatikte kapatma düğmesi 44 px hedef. */
    header :global(.btn.icon) {
      width: 44px;
      height: 44px;
      margin: -3px -6px -3px 0;
    }
  }
</style>
