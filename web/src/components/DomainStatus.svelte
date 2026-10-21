<script lang="ts">
  // Durum sayfası özel alan adının durumu (yalnız yönetici): DNS kaydı hedefi,
  // DNS doğru mu, HTTPS ile açılıp bu sayfayı sunuyor mu; rozet + yeniden denetle.
  import { untrack } from 'svelte';
  import { api, errorMessage, type PageDomainStatus } from '../lib/api';
  import Icon from './Icon.svelte';
  import { t } from '../lib/i18n';

  let { pageId, domain }: { pageId: number; /** Kayıtlı alan adı. */ domain: string } = $props();

  let st = $state.raw<PageDomainStatus | null>(null);
  let checking = $state(false);
  let loadError = $state('');

  async function check() {
    checking = true;
    loadError = '';
    try {
      st = await api.pageDomainStatus(pageId);
    } catch (e) {
      loadError = errorMessage(e);
    } finally {
      checking = false;
    }
  }

  $effect(() => {
    if (domain) untrack(check);
  });

  const tone: Record<string, string> = { active: 'up', cert: 'pending', dns: 'pending', unpublished: 'paused' };
</script>

<div class="dst" data-testid="domain-status" data-state={st?.state ?? ''} role="status" aria-live="polite">
  <div class="row">
    <span class="lbl">{t('pages.editor.domainStatus')}</span>
    {#if st && st.state !== 'none'}
      <span class="badge {tone[st.state]}">{t(`pages.editor.domainState.${st.state}` as never)}</span>
    {/if}
    <button type="button" class="btn sm" onclick={check} disabled={checking} data-testid="domain-check">
      <Icon name="refresh" size={14} />
      {checking ? t('pages.editor.domainChecking') : t('pages.editor.domainCheck')}
    </button>
  </div>
  {#if loadError}
    <div class="alert error">{loadError}</div>
  {:else if st && st.state !== 'none'}
    {#if !st.https}
      <ul class="help recs">
        <li>{t('pages.editor.domainCname', { domain: st.domain, target: st.target })}</li>
        {#if st.target_ips.length}<li>{t('pages.editor.domainA', { ips: st.target_ips.join(', ') })}</li>{/if}
      </ul>
      <p class="help">
        {st.domain_ips.length ? t('pages.editor.domainNow', { ips: st.domain_ips.join(', ') }) : t('pages.editor.domainNone')}
      </p>
      {#if st.state === 'cert'}<p class="help">{t('pages.editor.domainCertHint')}</p>{/if}
      {#if st.https_error}
        <p class="help err" title={st.https_error}>
          {t('pages.editor.domainHttpsError', { error: t(`pages.editor.domainErr.${st.https_code || 'other'}` as never) })}
        </p>
      {/if}
    {/if}
  {/if}
</div>

<style>
  .dst {
    margin-top: 10px;
    display: grid;
    gap: 6px;
  }
  .row {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 8px;
  }
  .lbl {
    font-size: 0.85rem;
    color: var(--text-2);
  }
  .row .btn {
    margin-left: auto;
  }
  .recs {
    margin: 0;
    padding-left: 18px;
    overflow-wrap: anywhere;
  }
  .help {
    margin: 0;
    overflow-wrap: anywhere;
  }
  .err {
    color: var(--down-text-2);
  }
  @media (max-width: 640px) {
    .row .btn {
      min-height: 44px;
    }
  }
</style>
