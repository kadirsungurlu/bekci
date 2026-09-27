<script lang="ts">
  import type { Incident } from '../lib/api';
  import { fmtDate, fmtDuration } from '../lib/format';
  import { navigate } from '../lib/router.svelte';
  import Icon from './Icon.svelte';

  let {
    incidents,
    now,
    showMonitor = false,
    emptyText = 'Kayıtlı olay yok.',
  }: { incidents: Incident[]; now: number; showMonitor?: boolean; emptyText?: string } = $props();

  // Satırın tamamı olay sayfasına götürür; içteki bağlantılar (monitör adı) kendi işini yapar.
  function open(e: MouseEvent, id: number) {
    if ((e.target as Element | null)?.closest('a, button')) return;
    if (window.getSelection()?.toString()) return; // metin seçiliyorsa gezinme
    navigate(`/incidents/${id}`);
  }
</script>

{#if incidents.length === 0}
  <div class="none">{emptyText}</div>
{:else}
  <table class="table responsive" class:with-mon={showMonitor}>
    <thead>
      <tr>
        {#if showMonitor}<th>Monitör</th>{/if}
        <th>Başlangıç</th>
        <th>Süre</th>
        <th>Neden</th>
        <th>Durum</th>
        <th class="go-h"><span class="sr">Ayrıntı</span></th>
      </tr>
    </thead>
    <tbody>
      {#each incidents as inc (inc.id)}
        {@const ongoing = inc.resolved_at === 0}
        <!-- Klavyeyle erişim satır sonundaki bağlantıyla; satır tıklaması fare/dokunma kolaylığı. -->
        <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_noninteractive_element_interactions -->
        <tr class="inc-row" onclick={(e) => open(e, inc.id)}>
          {#if showMonitor}
            <td data-label="Monitör" class="mon"><a href="#/monitors/{inc.monitor_id}">{inc.monitor_name}</a></td>
          {/if}
          <td data-label="Başlangıç" class="nowrap start">{fmtDate(inc.started_at)}</td>
          <td data-label="Süre" class="nowrap dur" class:c-down={ongoing}>
            {fmtDuration((ongoing ? now : inc.resolved_at) - inc.started_at)}
          </td>
          <td data-label="Neden" class="cause">{inc.cause || '—'}</td>
          <td data-label="Durum" class="st">
            {#if ongoing}
              <span class="badge down">Devam ediyor</span>
            {:else}
              <span class="badge up" title="Çözüldü: {fmtDate(inc.resolved_at)}">Çözüldü</span>
            {/if}
          </td>
          <td class="go">
            <a href="#/incidents/{inc.id}" aria-label="Olay ayrıntıları" data-tip="Olay ayrıntıları"><Icon name="chevron-right" size={16} /></a>
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
