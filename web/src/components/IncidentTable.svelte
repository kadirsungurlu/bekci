<script lang="ts">
  import type { Incident } from '../lib/api';
  import { fmtDate, fmtDuration } from '../lib/format';

  let {
    incidents,
    now,
    showMonitor = false,
    emptyText = 'Kayıtlı olay yok.',
  }: { incidents: Incident[]; now: number; showMonitor?: boolean; emptyText?: string } = $props();
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
      </tr>
    </thead>
    <tbody>
      {#each incidents as inc (inc.id)}
        {@const ongoing = inc.resolved_at === 0}
        <tr>
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
