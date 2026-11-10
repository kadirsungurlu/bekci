<script lang="ts">
  // Kilitli Pro sayfası: modül bu derlemede yok ya da lisans bu özelliği açmıyor.
  import Icon from '../components/Icon.svelte';
  import { t, tOr } from '../lib/i18n';
  import { license, type ProMenuItem } from '../lib/pro.svelte';
  import { session } from '../lib/session.svelte';

  let { item }: { item: ProMenuItem } = $props();

  const st = $derived(license.state);
  const reason = $derived(st?.pro_derleme && !st.etkin && st.neden ? tOr(`license.reasons.${st.neden}`, '') : '');
</script>

<div class="card locked" data-testid="pro-locked" data-pro={item.id}>
  <div class="ic"><Icon name="lock" size={26} /></div>
  <span class="tag">{t('nav.pro')}</span>
  <h2>{t(`license.pages.${item.metin}`)}</h2>
  <p class="desc">{t(`license.pages.${item.metin}Desc`)}</p>
  <p class="with">{t('license.pro.comesWith')}</p>
  {#if st?.pro_derleme && !st.etkin}
    <p class="muted small">{t('license.pro.notActive')}{reason ? ` ${reason}` : ''}</p>
  {/if}
  {#if session.isAdmin}
    <a class="btn primary" href="#/settings/license"><Icon name="key" size={16} /> {t('license.pro.openLicense')}</a>
  {/if}
</div>

<style>
  .locked {
    max-width: 560px;
    margin: 24px auto;
    text-align: center;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 8px;
    padding: 32px 24px;
  }
  .ic {
    width: 56px;
    height: 56px;
    border-radius: 16px;
    display: grid;
    place-items: center;
    background: var(--card-2);
    color: var(--muted);
  }
  .tag {
    font-size: 0.72rem;
    font-weight: 700;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--accent-text);
    background: var(--accent-soft);
    padding: 2px 8px;
    border-radius: 6px;
  }
  h2 {
    margin: 4px 0 0;
  }
  .desc {
    color: var(--text-2);
    margin: 0;
  }
  .with {
    font-weight: 600;
    margin: 4px 0;
  }
  .small {
    margin: 0 0 6px;
  }
  .btn {
    margin-top: 6px;
  }
</style>
