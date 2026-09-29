<script lang="ts">
  import { isServerIncident, type Incident } from '../lib/api';
  import { fmtDate, fmtDuration } from '../lib/format';
  import { navigate } from '../lib/router.svelte';
  import Icon from './Icon.svelte';
  import { t } from '../lib/i18n';

  let {
    incidents,
    now,
    showMonitor = false,
    emptyText,
  }: { incidents: Incident[]; now: number; showMonitor?: boolean; emptyText?: string } = $props();

  // Monitör ve sunucu olayları birlikte listeleniyorsa sütun başlığı ikisini de söyler.
  const mixed = $derived(incidents.some((i) => isServerIncident(i.kind)));

  // Satırın tamamı olay sayfasına götürür; içteki bağlantılar (monitör adı) kendi işini yapar.
  function open(e: MouseEvent, id: number) {
    if ((e.target as Element | null)?.closest('a, button')) return;
    if (window.getSelection()?.toString()) return; // metin seçiliyorsa gezinme
    navigate(`/incidents/${id}`);
  }
</script>

{#if incidents.length === 0}
  <div class="none">{emptyText ?? t('incidents.table.empty')}</div>
{:else}
  <table class="table responsive" class:with-mon={showMonitor}>
    <thead>
      <tr>
        {#if showMonitor}<th>{mixed ? t('incidents.table.source') : t('incidents.table.monitor')}</th>{/if}
        <th>{t('incidents.table.started')}</th>
        <th>{t('incidents.table.duration')}</th>
        <th>{t('incidents.table.cause')}</th>
        <th>{t('incidents.table.status')}</th>
        <th class="go-h"><span class="sr">{t('incidents.table.details')}</span></th>
      </tr>
    </thead>
    <tbody>
      {#each incidents as inc (inc.id)}
        {@const ongoing = inc.resolved_at === 0}
        {@const partial = inc.kind === 'partial'}
        {@const server = isServerIncident(inc.kind)}
        <!-- Klavyeyle erişim satır sonundaki bağlantıyla; satır tıklaması fare/dokunma kolaylığı. -->
        <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_noninteractive_element_interactions -->
        <tr class="inc-row" class:partial onclick={(e) => open(e, inc.id)}>
          {#if showMonitor}
            <td data-label={t('incidents.table.monitor')} class="mon">
              {#if server}
                <a href="#/servers/{inc.server_id}"><Icon name="server" size={13} /> {inc.server_name}</a>
              {:else}
                <a href="#/monitors/{inc.monitor_id}">{inc.monitor_name}</a>
              {/if}
            </td>
          {/if}
          <td data-label={t('incidents.table.started')} class="nowrap start">{fmtDate(inc.started_at)}</td>
          <td data-label={t('incidents.table.duration')} class="nowrap dur" class:c-down={ongoing && !partial} class:c-pending={ongoing && partial}>
            {fmtDuration((ongoing ? now : inc.resolved_at) - inc.started_at)}
          </td>
          <td data-label={t('incidents.table.cause')} class="cause">
            {#if partial}
              <span class="badge pending kind" title={t('incidents.kind.partialHint')}>{t('incidents.kind.partial')}</span>
            {:else if server}
              <span class="badge accent kind">{t('incidents.kind.server')}</span>
            {/if}
            {inc.cause || '—'}
          </td>
          <td data-label={t('incidents.table.status')} class="st">
            {#if ongoing}
              <span class="badge {partial ? 'pending' : 'down'}">{t('incidents.table.ongoing')}</span>
            {:else}
              <span class="badge up" title={t('incidents.table.resolvedAt', { date: fmtDate(inc.resolved_at) })}>{t('incidents.table.resolved')}</span>
            {/if}
          </td>
          <td class="go">
            <a href="#/incidents/{inc.id}" aria-label={t('incidents.table.openDetails')} data-tip={t('incidents.table.openDetails')}><Icon name="chevron-right" size={16} /></a>
          </td>
        </tr>
      {/each}
    </tbody>
  </table>
{/if}

<style>
  .none {
    color: var(--muted);
    padding: 18px 0 6px;
    text-align: center;
    font-size: 0.92rem;
  }
  .mon a {
    color: var(--text);
    font-weight: 600;
  }
  .mon a :global(svg) {
    display: inline-block;
    vertical-align: -2px;
    color: var(--muted);
  }
  .kind {
    margin-right: 6px;
  }
  .c-pending {
    color: var(--pending);
  }
  .cause {
    max-width: 420px;
  }
  .inc-row {
    cursor: pointer;
  }
  @media (hover: hover) {
    .inc-row:hover td {
      background: var(--card-hover);
    }
  }
  .go-h,
  .go {
    width: 1%;
    text-align: right;
  }
  .go a {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 28px;
    height: 28px;
    border-radius: 6px;
    color: var(--muted);
  }
  .go a:focus-visible {
    color: var(--accent-text);
  }
  .sr {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip: rect(0 0 0 0);
    white-space: nowrap;
  }

  /* Dar ekranda her olay kompakt bir kart: ad + durum, neden, başlangıç · süre. */
  @media (max-width: 720px) {
    .table.responsive tr {
      display: grid;
      grid-template-columns: auto minmax(0, 1fr) auto;
      grid-template-areas:
        'cause cause st'
        'start dur dur';
      column-gap: 6px;
      row-gap: 4px;
      align-items: center;
    }
    .table.responsive.with-mon tr {
      grid-template-areas:
        'mon mon st'
        'cause cause cause'
        'start dur dur';
    }
    /* Dar ekranda kartın tamamı dokunulabilir; ok simgesi gizlenir. */
    .go {
      display: none !important;
    }
    .table.responsive td {
      display: block;
      width: auto;
      padding: 0;
      min-width: 0;
    }
    .table.responsive td::before {
      display: none;
    }
    .mon {
      grid-area: mon;
      font-size: 0.95rem;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }
    .st {
      grid-area: st;
      justify-self: end;
    }
    .cause {
      grid-area: cause;
      max-width: none;
      font-size: 0.86rem;
    }
    .start,
    .dur {
      font-size: 0.8rem;
      color: var(--muted);
    }
    .start {
      grid-area: start;
    }
    .dur {
      grid-area: dur;
    }
    .table.responsive td.dur::before {
      display: inline;
      content: '·';
      margin-right: 6px;
      color: var(--muted);
    }
    .dur.c-down {
      color: var(--down-text-2);
    }
  }
</style>
