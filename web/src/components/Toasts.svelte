<script lang="ts">
  import { toast } from '../lib/ui.svelte';
  import Icon from './Icon.svelte';
</script>

<div class="toasts" aria-live="polite">
  {#each toast.items as t (t.id)}
    <div class="toast {t.kind}" role="status">
      <span class="ic"><Icon name={t.kind === 'error' ? 'alert' : t.kind === 'success' ? 'check' : 'info'} size={16} /></span>
      <span class="txt">{t.text}</span>
      <button type="button" class="close" aria-label="Kapat" onclick={() => toast.dismiss(t.id)}><Icon name="x" size={14} /></button>
    </div>
  {/each}
</div>

<style>
  .toasts {
    position: fixed;
    right: 20px;
    bottom: 20px;
    z-index: 1100;
    display: flex;
    flex-direction: column;
    gap: 8px;
    max-width: min(380px, calc(100vw - 32px));
    pointer-events: none;
  }
  .toast {
    pointer-events: auto;
    display: flex;
    align-items: flex-start;
    gap: 10px;
    padding: 11px 12px 11px 14px;
    border-radius: 10px;
    background: #0b111c;
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
    border-color: rgba(239, 68, 68, 0.5);
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
  }
</style>
