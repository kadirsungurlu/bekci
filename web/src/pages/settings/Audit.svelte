<script lang="ts">
  import { onMount } from 'svelte';
  import { api, errorMessage, type AuditEntry } from '../../lib/api';
  import { auditHref, auditLabel, auditTargetLabel, auditTone } from '../../lib/audit';
  import { fmtDate } from '../../lib/format';
  import { STRATEGY_LABELS } from '../../lib/maintenance';
  import { t } from '../../lib/i18n';

  // Bakım kayıtlarında ayrıntı strateji kodudur (ör. recurring_weekly).
  const detailText = (a: AuditEntry) =>
    (a.action.startsWith('maintenance.') && STRATEGY_LABELS[a.detail as keyof typeof STRATEGY_LABELS]) || a.detail || '—';

  const LIMIT = 50;

  let items = $state.raw<AuditEntry[]>([]);
  let loading = $state(true);
  let loadingMore = $state(false);
  let hasMore = $state(false);
  let error = $state('');

  async function loadFirst() {
    loading = true;
    try {
      items = await api.audit(0, LIMIT);
      hasMore = items.length === LIMIT;
      error = '';
    } catch (e) {
      error = errorMessage(e);
    } finally {
      loading = false;
    }
  }

  async function loadMore() {
    if (!items.length) return;
    loadingMore = true;
    try {
      const list = await api.audit(items[items.length - 1].id, LIMIT);
      items = [...items, ...list];
      hasMore = list.length === LIMIT;
    } catch (e) {
      error = errorMessage(e);
    } finally {
      loadingMore = false;
    }
  }

  onMount(loadFirst);
</script>

<div class="card">
  <div class="head">
    <h2 class="card-title">{t('audit.title')}</h2>
    <button class="btn sm" onclick={loadFirst} disabled={loading}>{t('common.refresh')}</button>
  </div>
  {#if loading && items.length === 0}
    <div class="skeleton" style="height:220px"></div>
  {:else if error && items.length === 0}
    <div class="empty">
      <h3>{t('audit.loadFailed')}</h3>
      <p>{error}</p>
      <button class="btn primary" onclick={loadFirst}>{t('common.retry')}</button>
    </div>
  {:else if items.length === 0}
    <div class="none">{t('audit.empty')}</div>
  {:else}
    <table class="table responsive log">
      <thead>
        <tr>
          <th>{t('audit.col.time')}</th>
          <th>{t('audit.col.user')}</th>
          <th>{t('audit.col.action')}</th>
          <th>{t('audit.col.target')}</th>
          <th>{t('audit.col.detail')}</th>
          <th>{t('audit.col.ip')}</th>
        </tr>
      </thead>
      <tbody>
        {#each items as a (a.id)}
          {@const tone = auditTone(a.action)}
          {@const href = auditHref(a.target_type, a.target_id)}
          <tr>
            <td data-label={t('audit.col.time')} class="nowrap tm">{fmtDate(a.time)}</td>
            <td data-label={t('audit.col.user')} class="usr">{a.username || '—'}</td>
            <td data-label={t('audit.col.action')} class="act">
              <span class="badge {tone === 'bad' ? 'down' : tone === 'warn' ? 'pending' : tone === 'good' ? 'up' : ''}" title={a.action}
                >{auditLabel(a.action)}</span
              >
            </td>
            <td data-label={t('audit.col.target')} class="tgt">
              {#if a.target_name}
                <span class="muted small">{auditTargetLabel(a.target_type)}:</span>
                {#if href}<a {href}>{a.target_name}</a>{:else}{a.target_name}{/if}
              {:else}
                —
              {/if}
            </td>
            <td data-label={t('audit.col.detail')} class="det">{detailText(a)}</td>
            <td data-label={t('audit.col.ip')} class="ip mono">{a.ip || '—'}</td>
          </tr>
        {/each}
      </tbody>
    </table>
    {#if error}<div class="alert error">{error}</div>{/if}
    {#if hasMore}
      <div class="more">
        <button class="btn" onclick={loadMore} disabled={loadingMore}>
          {#if loadingMore}<span class="spinner"></span>{/if}
          {t('audit.loadMore')}
        </button>
      </div>
    {/if}
  {/if}
</div>

<style>
  .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    margin-bottom: 8px;
  }
  .card-title {
    margin: 0;
  }
  .none {
    color: var(--muted);
    padding: 18px 0 6px;
    text-align: center;
  }
  .log .badge {
    letter-spacing: 0.01em;
    font-size: 0.74rem;
    height: 22px;
  }
  .usr {
    font-weight: 600;
  }
  .tgt,
  .det {
    word-break: break-word;
  }
  .det {
    color: var(--text-2);
    max-width: 320px;
  }
  .ip {
    font-size: 0.82rem;
    color: var(--muted);
    white-space: nowrap;
  }
  .tm {
    color: var(--text-2);
  }
  .more {
    display: flex;
    justify-content: center;
    padding-top: 16px;
    border-top: 1px solid var(--border);
    margin-top: 4px;
  }
  @media (max-width: 720px) {
    .det {
      max-width: none;
    }
  }
</style>
