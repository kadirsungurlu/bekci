<script lang="ts">
  import { toast } from '../lib/ui.svelte';
  import Icon from './Icon.svelte';

  // Kutucuklar üst katmanda (popover="manual") durur: açık bir pencerenin (<dialog>)
  // arkasında kalmasınlar diye her yeni bildirimde yeniden gösterilip en üste alınır.
  // Popover desteklemeyen eski tarayıcılarda sabit konumlu sıradan bir kutu olarak kalır.
  const canPopover = typeof HTMLElement !== 'undefined' && 'showPopover' in HTMLElement.prototype;

  let el: HTMLDivElement | undefined = $state();
  let lastId = 0;

  $effect(() => {
    const items = toast.items;
    if (!el || !canPopover) return;
    const newest = items.length ? items[items.length - 1].id : 0;
    try {
      if (!items.length) {
        if (el.matches(':popover-open')) el.hidePopover();
      } else if (newest !== lastId || !el.matches(':popover-open')) {
        // Yeniden göstermek, sonradan açılmış pencerelerin de üstüne çıkarır.
        if (el.matches(':popover-open')) el.hidePopover();
        el.showPopover();
      }
    } catch {
      /* üst katman kullanılamıyorsa sabit konumlu kutu olarak görünür */
    }
    lastId = newest;
  });
</script>

<!-- Tek canlı bölge: bilgi/başarı kibarca okunur, hatalar role="alert" ile hemen. -->
<div class="toasts" bind:this={el} popover={canPopover ? 'manual' : undefined} aria-live="polite" aria-relevant="additions">
  {#each toast.items as t (t.id)}
    <div class="toast {t.kind}" role={t.kind === 'error' ? 'alert' : undefined}>
      <span class="ic" aria-hidden="true"><Icon name={t.kind === 'error' ? 'alert' : t.kind === 'success' ? 'check' : 'info'} size={16} /></span>
      <span class="txt">{t.text}</span>
      <button type="button" class="close" aria-label="Bildirimi kapat" onclick={() => toast.dismiss(t.id)}><Icon name="x" size={14} /></button>
    </div>
  {/each}
</div>

<style>
  .toasts {
    position: fixed;
    inset: auto 20px 20px auto;
    margin: 0;
    padding: 0;
    border: none;
    background: transparent;
    color: var(--text);
    overflow: visible;
    width: auto;
    height: auto;
    z-index: 1100;
    display: flex;
    flex-direction: column;
    gap: 8px;
    max-width: min(380px, calc(100vw - 32px));
    pointer-events: none;
  }
  /* Popover kapalıyken tarayıcı gizler; açıkken düzenimiz geçerli. */
  .toasts[popover]:not(:popover-open) {
    display: none;
  }
  .toast {
    pointer-events: auto;
    display: flex;
    align-items: flex-start;
    gap: 10px;
    padding: 11px 12px 11px 14px;
    border-radius: 10px;
    background: var(--tip-bg);
    border: 1px solid var(--border-strong);
    box-shadow: var(--shadow);
    font-size: 0.9rem;
    animation: slide 0.18s ease-out;
  }
  .ic {
    display: inline-flex;
    margin-top: 2px;
  }
  .toast.success .ic {
    color: var(--up);
  }
  .toast.error {
    border-color: var(--down-border);
  }
  .toast.error .ic {
    color: var(--down);
  }
  .toast.info .ic {
    color: var(--accent-text);
  }
  .txt {
    flex: 1;
    word-break: break-word;
  }
  .close {
    background: none;
    border: none;
    color: var(--muted);
    cursor: pointer;
    padding: 2px;
    display: inline-flex;
  }
  .close:hover {
    color: var(--text);
  }
  @keyframes slide {
    from {
      transform: translateY(8px);
      opacity: 0;
    }
  }
  @media (max-width: 900px) {
    .toasts {
      left: 16px;
      right: 16px;
      bottom: calc(76px + env(safe-area-inset-bottom));
      max-width: none;
    }
    .close {
      margin: -10px -8px -10px 0;
      padding: 12px;
    }
  }
</style>
