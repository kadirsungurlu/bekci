<script lang="ts">
  import { confirmer } from '../lib/ui.svelte';
  import Modal from './Modal.svelte';
  import { t } from '../lib/i18n';

  let open = $state(false);
  const req = $derived(confirmer.current);

  $effect(() => {
    open = req !== null;
  });
</script>

<Modal bind:open title={req?.title ?? ''} width={440} onclose={() => confirmer.answer(false)}>
  <p class="msg">{req?.message}</p>
  {#snippet footer()}
    <div class="spacer"></div>
    <button type="button" class="btn" onclick={() => confirmer.answer(false)}>{req?.cancelText ?? t('common.cancel')}</button>
    <button
      type="button"
      class="btn {req?.danger ? 'danger solid' : 'primary'}"
      onclick={() => confirmer.answer(true)}>{req?.confirmText ?? t('common.confirm')}</button
    >
  {/snippet}
</Modal>

<style>
  .msg {
    margin: 0;
    color: var(--text-2);
    white-space: pre-line;
  }
  .spacer {
    flex: 1;
  }
</style>
