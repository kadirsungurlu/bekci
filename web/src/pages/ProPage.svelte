<script lang="ts">
  // #/pro/<id>[/<alt yol>]: alt yol sayfaya `yol` olarak geçer. Modül var + lisans açık -> sayfa; Pro derlemesi + modül var ama lisans yok -> sayfa
  // salt okunur uyarısıyla (sunucu değiştiren istekleri 402 pro_lisans ile reddeder); aksi -> ProLocked.
  import type { Component } from 'svelte';
  import Icon from '../components/Icon.svelte';
  import { t } from '../lib/i18n';
  import { errorMessage } from '../lib/api';
  import { license, proAcik, proMenuItem, proSayfaVar, proSayfaYukle } from '../lib/pro.svelte';
  import { roleRank, session } from '../lib/session.svelte';
  import ProLocked from './ProLocked.svelte';

  let { id, yol = '' }: { id: string; yol?: string } = $props();

  const item = $derived(proMenuItem(id));
  const allowed = $derived(!!item && roleRank(session.role) >= roleRank(item.rol));
  const hasModule = $derived(proSayfaVar(id));
  const open = $derived(!!item && hasModule && proAcik(item.ozellik));
  const readOnly = $derived(!!item && hasModule && !open && !!license.state?.pro_derleme);
  const show = $derived(open || readOnly);

  let Page = $state.raw<Component<{ yol?: string }> | null>(null);
  let loadError = $state('');

  $effect(() => {
    if (!show) return;
    const want = id;
    let alive = true;
    Page = null;
    loadError = '';
    proSayfaYukle(want)
      .then((c) => {
        if (alive) Page = c;
      })
      .catch((e) => {
        if (alive) loadError = errorMessage(e);
      });
    return () => {
      alive = false;
    };
  });
</script>

{#if !item}
  <div class="card empty">
    <h3>{t('license.pro.unknown')}</h3>
    <a class="btn primary" href="#/">{t('nav.backToMonitors')}</a>
  </div>
{:else if !allowed}
  <div class="card empty">
    <h3>{t('nav.forbiddenTitle')}</h3>
    <p>{t('nav.forbiddenText')}</p>
    <a class="btn primary" href="#/">{t('nav.backToMonitors')}</a>
  </div>
{:else if hasModule && !license.loaded}
  <div class="center"><span class="spinner"></span></div>
{:else if show}
  {#if readOnly}
    <div class="alert warning ro" role="status" data-testid="pro-readonly">
      <Icon name="lock" size={16} />
      <span>{t('license.pro.readOnly')}</span>
      {#if session.isAdmin}<a href="#/settings/license">{t('license.pro.openLicense')}</a>{/if}
    </div>
  {/if}
  {#if loadError}
    <div class="alert error">{t('license.pro.loadFailed')}: {loadError}</div>
  {:else if Page}
    <Page {yol} />
  {:else}
    <div class="center"><span class="spinner"></span></div>
  {/if}
{:else}
  <ProLocked {item} />
{/if}

<style>
  .center {
    display: flex;
    justify-content: center;
    padding: 48px 0;
  }
  .ro {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
    margin-bottom: 16px;
  }
  .ro a {
    font-weight: 600;
  }
</style>
