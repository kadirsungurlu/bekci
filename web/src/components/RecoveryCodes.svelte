<script lang="ts">
  // Kurtarma kodlarının tek seferlik gösterimi: kopyala, indir, "kaydettim" onayı.
  import { session } from '../lib/session.svelte';
  import Icon from './Icon.svelte';
  import CopyButton from './CopyButton.svelte';
  import { t } from '../lib/i18n';
  import { APP_NAME } from '../lib/brand';

  let { codes, saved = $bindable(false) }: { codes: string[]; saved?: boolean } = $props();

  const text = $derived(
    `${t('account.recovery.fileTitle', { app: APP_NAME, user: session.user?.username ?? '' })}\n` +
      `${t('account.recovery.fileNote')}\n\n${codes.join('\n')}\n`,
  );
  const href = $derived('data:text/plain;charset=utf-8,' + encodeURIComponent(text));

</script>

<p class="text-2 small intro">
  <b>{t('account.recovery.title')}</b> {t('account.recovery.intro')}
</p>
<ol class="codes">
  {#each codes as c (c)}<li><code>{c}</code></li>{/each}
</ol>
<div class="row btns">
  <a class="btn sm" {href} download={t('account.recovery.fileName')}><Icon name="download" size={15} /> {t('common.download')}</a>
  <CopyButton text={codes.join('\n')} size={15} />
</div>
<label class="check ack">
  <input type="checkbox" bind:checked={saved} />
  <span>{t('account.recovery.saved')}</span>
</label>

<style>
  .intro {
    margin: 0 0 12px;
  }
  .codes {
    list-style: none;
    margin: 0;
    padding: 14px;
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 8px 16px;
    background: var(--input);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-sm);
  }
  .codes code {
    font-size: 0.98rem;
    letter-spacing: 0.04em;
    color: var(--text);
  }
  .btns {
    margin: 12px 0 14px;
  }
  .ack {
    font-weight: 600;
  }
  @media (max-width: 380px) {
    .codes {
      grid-template-columns: minmax(0, 1fr);
    }
  }
</style>
