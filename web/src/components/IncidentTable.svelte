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
  <table class="table responsive">
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
          <td data-label="Başlangıç" class="nowrap">{fmtDate(inc.started_at)}</td>
          <td data-label="Süre" class="nowrap" class:c-down={ongoing}>
            {fmtDuration((ongoing ? now : inc.resolved_at) - inc.started_at)}
          </td>
          <td data-label="Neden" class="cause">{inc.cause || '—'}</td>
          <td data-label="Durum">
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
</style>
