<script lang="ts">
  // Kontrol noktaları arası bağlantı matrisi: hücre = satırdaki düğümün
  // sütundakine ulaşıp ulaşamadığı. Canlı akıştaki "mesh"/"probe" olaylarıyla tazelenir.
  import { onDestroy, onMount } from 'svelte';
  import { api, type MeshMatrix } from '../lib/api';
  import { live } from '../lib/live.svelte';
  import { t } from '../lib/i18n';

  let data = $state<MeshMatrix | null>(null);
  let busy = false;
  async function load() {
    if (busy) return;
    busy = true;
    try {
      data = await api.mesh();
    } catch {
      // Sessiz: matris yardımcı bilgi; ayarlar sayfası kendi hatasını gösterir.
    } finally {
      busy = false;
    }
  }

  let subs: (() => void)[] = [];
  let timer: ReturnType<typeof setTimeout> | undefined;
  const soon = () => {
    clearTimeout(timer);
    timer = setTimeout(load, 500);
  };
  onMount(() => {
    load();
    subs = [live.onMesh(soon), live.onProbe(soon), live.onResume(load)];
  });
  onDestroy(() => {
    subs.forEach((u) => u());
    clearTimeout(timer);
  });

  // Yönlü durum: src -> dst.
  function edge(m: MeshMatrix, src: number, dst: number): string {
    const p = m.pairs.find((x) => (x.a === src && x.b === dst) || (x.a === dst && x.b === src));
    if (!p) return 'unknown';
    return p.a === src ? p.ab : p.ba;
  }
  const label = (s: string) => t(s === 'up' ? 'probes.matrix.up' : s === 'down' ? 'probes.matrix.down' : 'probes.matrix.unknown');
</script>

<section class="card stack matrix" data-testid="mesh-matrix">
  <div>
    <h2 class="card-title">{t('probes.matrix.title')}</h2>
    <p class="text-2 small sub">{t('probes.matrix.intro')}</p>
  </div>
  {#if data && !data.active}
    <p class="muted small">{t('probes.matrix.inactive')}</p>
  {:else if data}
    <div class="scroll">
      <table class="grid">
        <thead>
          <tr>
            <th scope="col" class="corner">{t('probes.matrix.from')}</th>
            {#each data.nodes as n (n.id)}<th scope="col" title={n.name}><span class="nm">{n.name}</span></th>{/each}
          </tr>
        </thead>
        <tbody>
          {#each data.nodes as r (r.id)}
            <tr>
              <th scope="row"><span class="nm">{r.name}</span></th>
              {#each data.nodes as c (c.id)}
                {#if r.id === c.id}
                  <td class="self"></td>
                {:else}
                  {@const s = edge(data, r.id, c.id)}
                  <td title={t('probes.matrix.cell', { src: r.name, dst: c.name, state: label(s) })}>
                    <span class="cell {s}"><span class="sr">{label(s)}</span></span>
                  </td>
                {/if}
              {/each}
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
    <div class="legend small text-2">
      <span><span class="cell up"></span>{t('probes.matrix.up')}</span>
      <span><span class="cell down"></span>{t('probes.matrix.down')}</span>
      <span><span class="cell unknown"></span>{t('probes.matrix.unknown')}</span>
    </div>
    {#if data.findings.length}
      <ul class="findings">
        {#each data.findings as f, i (i)}<li><span class="cell down"></span>{f}</li>{/each}
      </ul>
    {:else}
      <p class="small text-2"><span class="cell up"></span> {t('probes.matrix.healthy')}</p>
    {/if}
  {/if}
</section>

<style>
  .matrix {
    margin-top: 16px;
  }
  .sub {
    margin: 4px 0 0;
  }
  .scroll {
    overflow-x: auto;
  }
  .grid {
    border-collapse: collapse;
    font-size: 13px;
  }
  .grid th,
  .grid td {
    border: 1px solid var(--border);
    padding: 6px 8px;
    text-align: center;
  }
  .grid th {
    font-weight: 600;
    color: var(--text-2, var(--muted));
    white-space: nowrap;
  }
  .grid th[scope='row'],
  .corner {
    text-align: left;
  }
  .corner {
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--muted);
  }
  .nm {
    display: inline-block;
    max-width: 140px;
    overflow: hidden;
    text-overflow: ellipsis;
    vertical-align: bottom;
  }
  .self {
    background: var(--border);
  }
  .cell {
    display: inline-block;
    width: 12px;
    height: 12px;
    border-radius: 3px;
    background: var(--muted);
    vertical-align: middle;
  }
  .cell.up {
    background: var(--up);
  }
  .cell.down {
    background: var(--down);
  }
  .sr {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip: rect(0 0 0 0);
  }
  .legend {
    display: flex;
    flex-wrap: wrap;
    gap: 14px;
  }
  .legend > span,
  .findings li {
    display: inline-flex;
    gap: 6px;
    align-items: center;
  }
  .findings {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 6px;
    font-size: 14px;
  }
  .findings li {
    align-items: flex-start;
  }
  .findings .cell {
    flex: none;
    margin-top: 4px;
  }
</style>
