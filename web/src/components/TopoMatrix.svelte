<script lang="ts">
  // Canlı topoloji: kontrol noktaları arası bağlantı matrisi (satır ölçen, sütun hedef, değer RTT).
  // Çok noktada (tuvalde yay çizilmediğinde) mesh'in ana görünümü budur.
  import { t } from '../lib/i18n';
  import { fmtMs } from '../lib/topoview';
  import type { MeshMatrix } from '../lib/api';

  let { mesh }: { mesh: MeshMatrix } = $props();

  function cell(src: number, dst: number): { v: string; cls: string } {
    const l = mesh.links?.find((x) => x.src === src && x.dst === dst);
    let st = l?.state;
    if (!st) {
      const p = mesh.pairs.find((x) => (x.a === src && x.b === dst) || (x.a === dst && x.b === src));
      st = p ? (p.a === src ? p.ab : p.ba) : 'unknown';
    }
    if (st === 'up') return { v: l?.rtt_ms !== undefined ? fmtMs(l.rtt_ms) : t('topology.mesh.up'), cls: 'ok' };
    if (st === 'down') return { v: t('topology.mesh.down'), cls: 'bad' };
    return { v: '—', cls: 'unk' };
  }
</script>

<section class="card mx" data-testid="topology-matrix">
  <h2>{t('topology.matrix.title')}</h2>
  <p class="muted small">{t('topology.matrix.caption')}</p>
  <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
  <div class="scroll" tabindex="0" role="region" aria-label={t('topology.matrix.title')}>
    <table>
      <thead>
        <tr>
          <th scope="col" aria-label={t('topology.matrix.corner')}></th>
          {#each mesh.nodes as n (n.id)}<th scope="col" title={n.name}>{n.name}</th>{/each}
        </tr>
      </thead>
      <tbody>
        {#each mesh.nodes as r (r.id)}
          <tr>
            <th scope="row" title={r.name}>{r.name}</th>
            {#each mesh.nodes as c (c.id)}
              {#if r.id === c.id}
                <td class="self" aria-label={t('topology.matrix.self')}></td>
              {:else}
                {@const x = cell(r.id, c.id)}
                <td class={x.cls}>{x.v}</td>
              {/if}
            {/each}
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
  {#if mesh.findings.length}
    <ul class="findings">
      {#each mesh.findings as f, i (i)}<li>{f}</li>{/each}
    </ul>
  {/if}
</section>

<style>
  .mx {
    min-width: 0;
  }
  h2 {
    margin: 0 0 4px;
    font-size: 1.05rem;
  }
  .scroll {
    overflow-x: auto;
    max-width: 100%;
    margin-top: 10px;
  }
  /* Dar ekranda satır adı sabit kalır, sütunlar kayar. */
  th[scope='row'] {
    position: sticky;
    left: 0;
    z-index: 1;
    background: var(--card);
  }
  table {
    border-collapse: collapse;
    font-size: 0.82rem;
    font-variant-numeric: tabular-nums;
  }
  th,
  td {
    padding: 6px 10px;
    border: 1px solid var(--border);
    text-align: center;
    white-space: nowrap;
  }
  th {
    max-width: 140px;
    overflow: hidden;
    text-overflow: ellipsis;
    font-weight: 600;
    color: var(--text-2);
    background: var(--card-2);
  }
  tbody th {
    text-align: left;
  }
  td.ok {
    color: var(--up-text);
  }
  td.bad {
    color: var(--down-text);
    background: var(--down-soft);
  }
  td.unk {
    color: var(--muted);
  }
  td.self {
    background: repeating-linear-gradient(45deg, transparent 0 4px, var(--border) 4px 5px);
  }
  .findings {
    margin: 12px 0 0;
    padding-left: 18px;
    font-size: 0.86rem;
  }
</style>
