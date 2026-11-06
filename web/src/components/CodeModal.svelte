<script lang="ts">
  // Doğrulama kodu isteyen işlem penceresi (istek başına TOTP, kod tek kullanımlık).
  // Hata olursa pencere açık kalır ve kod temizlenir: aynı kod ikinci kez geçmez.
  import type { Snippet } from 'svelte';
  import Modal from './Modal.svelte';
  import { scriptError } from '../lib/scripts';
  import { t } from '../lib/i18n';

  let {
    open = $bindable(false),
    title,
    confirmText,
    danger = false,
    ready = true,
    width = 520,
    onconfirm,
    children,
  }: {
    open?: boolean;
    title: string;
    confirmText: string;
    danger?: boolean;
    /** false: ek alanlar eksik, düğme kapalı. */
    ready?: boolean;
    width?: number;
    /** Hata fırlatırsa metni pencerede gösterilir. */
    onconfirm: (code: string) => Promise<void>;
    children?: Snippet;
  } = $props();

  const uid = $props.id();
  let code = $state('');
  let error = $state('');
  let busy = $state(false);

  $effect(() => {
    if (open) {
      code = '';
      error = '';
    }
  });

  const valid = $derived(/^\d{6}$/.test(code.replace(/\s/g, '')));

  async function submit(e: SubmitEvent) {
    e.preventDefault();
    if (busy || !valid || !ready) return;
    busy = true;
    error = '';
    try {
      await onconfirm(code.replace(/\s/g, ''));
      open = false;
    } catch (err) {
      error = scriptError(err);
      code = '';
    } finally {
      busy = false;
    }
  }
</script>

<Modal bind:open {title} {width}>
  <form id="cm-{uid}" class="stack" onsubmit={submit}>
    {@render children?.()}
    {#if error}<div class="alert error" role="alert">{error}</div>{/if}
    <div class="field">
      <label for="cm-code-{uid}">{t('scripts.totp.label')}</label>
      <input
        id="cm-code-{uid}"
        class="input mono"
        bind:value={code}
        inputmode="numeric"
        autocomplete="one-time-code"
        maxlength="7"
        placeholder="123456"
      />
      <span class="help">{t('scripts.totp.help')}</span>
    </div>
  </form>
  {#snippet footer()}
    <button class="btn" type="button" onclick={() => (open = false)}>{t('common.cancel')}</button>
    <button class="btn {danger ? 'danger' : 'primary'}" type="submit" form="cm-{uid}" disabled={busy || !valid || !ready}>
      {busy ? t('common.loading') : confirmText}
    </button>
  {/snippet}
</Modal>
