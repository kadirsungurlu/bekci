<script lang="ts">
  // Lisans şeridi (GET /api/license `uyari`): yalnız Pro derlemesinde, tüm oturumlarda.
  // Kapatılınca bu oturum (sekme) boyunca o uyarı gizlenir; uyarı değişirse yeniden çıkar.
  import { license } from '../lib/pro.svelte';
  import { session } from '../lib/session.svelte';
  import { t } from '../lib/i18n';
  import Icon from './Icon.svelte';

  const KEY = 'bekci.license.dismissed';

  function readDismissed(): string {
    try {
      return sessionStorage.getItem(KEY) ?? '';
    } catch {
      return '';
    }
  }

  let dismissed = $state(readDismissed());

  const st = $derived(license.state);
  const uyari = $derived(st?.pro_derleme ? (st.uyari ?? '') : '');
  // Aynı uyarının gün sayısı değişince (14 -> 7) şerit yeniden çıksın.
  const sig = $derived(uyari ? `${uyari}:${st?.kalan_gun ?? ''}` : '');
  const days = $derived(Math.max(0, Math.ceil(st?.kalan_gun ?? 0)));

  const msg = $derived.by(() => {
    switch (uyari) {
      case 'bitis_14':
      case 'bitis_7':
      case 'bitis_1':
        return t('license.banner.expiresIn', { count: days });
      case 'deneme':
        return t('license.banner.trialIn', { count: days });
      case 'sunucu_ulasilamiyor':
        return t('license.banner.unreachable');
      case 'saat_geri':
        return t('license.banner.clockBack');
      case 'suresi_doldu':
      case 'kira_bitti':
        return t('license.banner.ended');
      default:
        return '';
    }
  });
  const severe = $derived(uyari === 'saat_geri' || uyari === 'suresi_doldu' || uyari === 'kira_bitti' || uyari === 'bitis_1');

  function dismiss() {
    dismissed = sig;
    try {
      sessionStorage.setItem(KEY, sig);
    } catch {
      /* gizli pencere: yalnız bellekte */
    }
  }
</script>

{#if msg && sig !== dismissed}
  <div class="lic" class:severe role="region" aria-label={t('license.title')} data-testid="license-banner" data-warning={uyari}>
    <Icon name={severe ? 'alert' : 'key'} size={15} />
    <span class="msg" role="status">{msg}</span>
    {#if session.isAdmin}
      <a href="#/settings/license">{t('license.banner.open')}</a>
    {/if}
    <button type="button" class="x" aria-label={t('license.banner.dismiss')} title={t('license.banner.dismiss')} onclick={dismiss} data-testid="license-dismiss">
      <Icon name="x" size={15} />
    </button>
  </div>
{/if}

<style>
  .lic {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
    min-height: 32px;
    margin: 0 0 16px;
    padding: 6px 6px 6px 14px;
    border: 1px solid var(--border);
    border-radius: 10px;
    background: var(--accent-soft, var(--bg-elev));
    font-size: 0.86rem;
  }
  .lic > :global(svg) {
    flex: none;
    color: var(--accent);
  }
  .lic.severe {
    background: var(--down-soft);
  }
  .lic.severe > :global(svg) {
    color: var(--down);
  }
  .msg {
    flex: 1 1 auto;
    min-width: 0;
    font-weight: 600;
  }
  a {
    color: var(--accent);
    font-weight: 600;
    text-decoration: none;
  }
  a:hover {
    text-decoration: underline;
  }
  .x {
    display: grid;
    place-items: center;
    width: 36px;
    height: 36px;
    flex: none;
    border: 0;
    border-radius: 8px;
    background: none;
    color: var(--muted);
    cursor: pointer;
  }
  .x:hover {
    color: var(--text);
    background: var(--bg-hover, rgba(127, 127, 127, 0.12));
  }
  @media (max-width: 640px) {
    .x {
      width: 44px;
      height: 44px;
      margin: -6px -2px -6px auto;
    }
  }
</style>
