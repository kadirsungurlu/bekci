<script lang="ts">
  // Durum sayfası düzenleyicisinde e-posta aboneleri: onaylı/bekleyen sayısı,
  // liste, satır silme (onaylı) ve CSV dışa aktarımı. available (sistem e-postası
  // var mı) üst bileşene bağlanır; aç/kapa anahtarı onu kullanır.
  import { onMount, untrack } from 'svelte';
  import { api, errorMessage, type PageSubscribers } from '../lib/api';
  import { fmtDate } from '../lib/format';
  import { confirmDialog, toast } from '../lib/ui.svelte';
  import Icon from './Icon.svelte';
  import { t } from '../lib/i18n';

  let {
    pageId,
    enabled,
    available = $bindable(true),
    inline = false,
  }: {
    pageId: number;
    /** Sayfada abonelik açık (kayıtlı ayar). */
    enabled: boolean;
    available?: boolean;
    /** Düzenleyici formunun içinde (kart çerçevesi olmadan). */
    inline?: boolean;
  } = $props();

  let data = $state.raw<PageSubscribers | null>(null);
  let loading = $state(true);
  let loadError = $state('');

  async function load() {
    try {
      data = await api.pageSubscribers(pageId);
      available = data.available;
      loadError = '';
    } catch (e) {
      loadError = errorMessage(e);
    } finally {
      loading = false;
    }
  }
  onMount(load);

  // Ayar kaydedilince (açıldı/kapandı) liste tazelenir.
  let lastEnabled = untrack(() => enabled);
  $effect(() => {
    if (enabled !== lastEnabled) {
      lastEnabled = enabled;
      load();
    }
  });

  const total = $derived(data ? data.items.length : 0);

  async function remove(id: number, email: string) {
    const ok = await confirmDialog({
      title: t('pages.subs.deleteTitle'),
      message: t('pages.subs.deleteMsg', { email }),
      confirmText: t('common.delete'),
      danger: true,
    });
    if (!ok) return;
    try {
      await api.deletePageSubscriber(pageId, id);
      toast.success(t('pages.subs.deleted'));
      await load();
    } catch (e) {
      toast.error(errorMessage(e));
    }
  }
</script>

{#if enabled || total > 0}
  <section class={inline ? 'inline' : 'card'} data-testid="subs-section">
    <div class="head">
      <div>
        <h2 class="card-title">{t('pages.subs.title')}</h2>
        {#if data}
          <p class="help nomargin" data-testid="subs-count">
            {t('pages.subs.summary', { count: data.count, pending: data.pending })}
          </p>
        {/if}
      </div>
      {#if total > 0}
        <a class="btn sm" href={api.pageSubscribersCsvUrl(pageId)} download title={t('pages.subs.csvTitle')} data-testid="subs-csv">
          <Icon name="download" size={14} />
          {t('pages.subs.csv')}
        </a>
      {/if}
    </div>
    {#if loading}
      <div class="skeleton" style="height:60px"></div>
    {:else if loadError}
      <div class="alert error">{t('pages.subs.loadFailed')} {loadError}</div>
    {:else if data && total === 0}
      <div class="muted small none">{t('pages.subs.empty')}</div>
    {:else if data}
      <div class="tw">
        <table class="table" data-testid="subs-table">
          <thead>
            <tr>
              <th scope="col">{t('pages.subs.email')}</th>
              <th scope="col" class="c-st">{t('pages.subs.status')}</th>
              {#if !inline}<th scope="col" class="c-dt">{t('pages.subs.date')}</th>{/if}
              <th scope="col" class="c-act"><span class="sr">{t('common.delete')}</span></th>
            </tr>
          </thead>
          <tbody>
            {#each data.items as s (s.id)}
              <tr data-testid="subs-row" data-id={s.id}>
                <td class="em">
                  {s.email}
                  <span class="badge st-m {s.confirmed ? 'up' : 'pending'}">{s.confirmed ? t('pages.subs.confirmed') : t('pages.subs.pending')}</span>
                  {#if inline}<small class="dt sub" title={t('pages.subs.date')}>{fmtDate(s.confirmed && s.confirmed_at ? s.confirmed_at : s.created_at)}</small>{/if}
                </td>
                <td class="st">
                  <span class="badge {s.confirmed ? 'up' : 'pending'}">
                    {s.confirmed ? t('pages.subs.confirmed') : t('pages.subs.pending')}
                  </span>
                </td>
                {#if !inline}<td class="dt">{fmtDate(s.confirmed && s.confirmed_at ? s.confirmed_at : s.created_at)}</td>{/if}
                <td class="act">
                  <button
                    type="button"
                    class="btn sm icon danger"
                    title={t('common.delete')}
                    aria-label={t('pages.subs.deleteLabel', { email: s.email })}
                    data-testid="subs-delete"
                    onclick={() => remove(s.id, s.email)}
                  >
                    <Icon name="trash" size={14} />
                  </button>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {/if}
  </section>
{/if}

<style>
  .inline {
    margin: 4px 0 18px 28px;
    padding: 12px 14px;
    border: 1px solid var(--border);
    border-radius: 10px;
  }
  .inline .card-title {
    font-size: 0.95rem;
  }
  @media (max-width: 640px) {
    .inline {
      margin-left: 0;
    }
  }
  .head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 12px;
    flex-wrap: wrap;
    margin-bottom: 12px;
  }
  .nomargin {
    margin: 0;
  }
  .none {
    padding: 6px 0;
  }
  .tw {
    overflow-x: auto;
    max-height: 420px;
    overflow-y: auto;
  }
  /* Sabit düzen: sil düğmesi kart kenarında kesilmesin, uzun adres kırılsın. */
  .table {
    table-layout: fixed;
    width: 100%;
  }
  .c-st {
    width: 120px;
  }
  .c-dt {
    width: 160px;
  }
  .c-act {
    width: 56px;
  }
  .em {
    overflow-wrap: anywhere;
  }
  .dt {
    color: var(--text-2);
  }
  .st-m {
    display: none;
  }
  @media (max-width: 640px) {
    /* Dar ekranda durum rozeti e-postanın altında; sütun kalkar, adres sığar. */
    .c-st,
    .st {
      display: none;
    }
    .st-m {
      display: flex;
      width: fit-content;
      margin-top: 4px;
    }
    .c-act {
      width: 68px;
    }
    .act :global(.btn),
    .head :global(.btn) {
      min-height: 44px;
      min-width: 44px;
    }
  }
  .sub {
    display: block;
    margin-top: 2px;
    font-size: 0.78rem;
  }
  .act {
    text-align: right;
  }
  .sr {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip: rect(0 0 0 0);
    white-space: nowrap;
  }
</style>
