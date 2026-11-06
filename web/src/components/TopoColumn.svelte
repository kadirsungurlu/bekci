<script lang="ts">
  // Topoloji sütunu: başlık, gruplama seçimi, gruplar. Tıklamalar sayfada toplanır (data-*).
  import { tick, type Snippet } from 'svelte';
  import Icon from './Icon.svelte';
  import TopoRow from './TopoRow.svelte';
  import { t, type TKey } from '../lib/i18n';
  import { MARK, type Col, type Graph, type GroupView, type St } from '../lib/topomodel';
  import { groupName, groupSummary } from '../lib/topotext';
  import { cellSvg, layoutCol, range, type Metrics } from '../lib/topovirt';

  let {
    col,
    cls = '',
    title,
    label,
    count,
    options,
    by,
    views,
    g,
    rel,
    sel,
    metrics,
    wide,
    active = true,
    now,
    filtered,
    sorun,
    head,
    tail,
    onrender,
  }: {
    col: Col;
    cls?: string;
    title: string;
    label: string;
    count: string;
    options: string[];
    by: string;
    views: GroupView[];
    g: Graph;
    rel: Map<string, St>;
    sel: string | null;
    metrics: Metrics;
    wide: boolean;
    active?: boolean;
    now: number;
    filtered: boolean;
    sorun: boolean;
    head?: Snippet;
    tail?: Snippet;
    onrender?: () => void;
  } = $props();

  const noun = $derived(t(`topology.noun.${col}`, { count: 2 }));
  const empty = $derived(sorun ? t('topology.emptyProblems', { what: noun }) : filtered ? t('topology.emptySearch', { what: noun }) : t('topology.emptyCol', { what: noun }));

  // Sanal liste: yalnız görünen aralık çizilir. Kaydırma konumu 120 px adımla izlenir, araya
  // giren karelerde bileşen hiç güncellenmez. Dar ekranda kaydırıcı penceredir.
  const STEP = 120;
  let k = $state<HTMLElement>();
  let width = $state(0);
  let top = $state(0);
  let height = $state(innerHeight);
  const list = $derived(layoutCol(views, metrics, width || 340));
  const win = $derived(range(list, top, height));
  const slice = $derived(list.items.slice(win[0], win[1]));

  export function sync() {
    if (!k) return;
    const y = wide ? k.scrollTop : Math.max(0, -k.getBoundingClientRect().top);
    const q = Math.floor(y / STEP) * STEP;
    if (q !== top) top = q;
  }
  $effect(() => {
    void [list, wide, active];
    if (!wide) sync(); // geniş ekranda tarayıcı kaydırmayı kırpar, scroll olayı gelir
  });
  $effect(() => {
    void slice;
    onrender?.();
  });
  $effect(() => {
    if (!k) return;
    const el = k;
    const ro = new ResizeObserver(() => {
      if (el.clientWidth) width = el.clientWidth;
      height = wide ? el.clientHeight || height : innerHeight;
      sync();
    });
    ro.observe(el);
    return () => ro.disconnect();
  });

  /** Öğe ya da grup başlığı için içerik içi satır ortası. */
  export function yOf(key: string | undefined): number | null {
    const i = key === undefined ? undefined : list.index.get(key);
    if (i === undefined) return null;
    return list.tops[i] + Math.min(list.tops[i + 1] - list.tops[i], metrics.row) / 2;
  }
  /** Seçili ya da ilk ilişkili satırın içerik içi üstü. */
  export function firstHit(): number | null {
    if (sel) {
      const i = list.index.get(`o-${sel}`);
      if (i !== undefined) return list.tops[i];
    }
    for (let i = 0; i < list.items.length; i++) {
      const it = list.items[i];
      if (it.k === 'r' && rel.has(it.n.id)) return list.tops[i];
    }
    return null;
  }
  /** Odaklanabilir öğeler, sırayla: sanal liste + kuyruktaki gerçek satırlar. */
  export function focusables(): string[] {
    const out = list.items.filter((i) => i.k !== 'c').map((i) => i.key);
    k?.querySelectorAll<HTMLElement>('.kuyruk .o,.kuyruk .kalan').forEach((e) => out.push(e.id));
    return out;
  }
  /** Öğeyi görünür yapar, çizilince elemanını döner. */
  export async function reveal(key: string): Promise<HTMLElement | null> {
    const i = list.index.get(key);
    if (i !== undefined && k) {
      const [a, b] = [list.tops[i], list.tops[i + 1]];
      if (wide) {
        if (a < k.scrollTop) k.scrollTop = a;
        else if (b > k.scrollTop + k.clientHeight) k.scrollTop = b - k.clientHeight;
      } else {
        const y = k.getBoundingClientRect().top + a;
        const bar = (matchMedia('(max-width:900px)').matches ? 54 : 0) + 56;
        if (y < bar) scrollBy({ top: y - bar });
        else if (y + b - a > innerHeight - 8) scrollBy({ top: y + b - a - innerHeight + 8 });
      }
      sync();
      await tick();
    }
    const el = document.getElementById(key);
    if (i === undefined) el?.scrollIntoView({ block: 'nearest' });
    return el;
  }
</script>

<section class="sutun {cls}" id="c-{col}" aria-label={label}>
  <div class="s-b">
    <h2>{title}</h2>
    <span class="say" title={count}>{count}</span>
    {#if options.length > 1}
      <span class="seg" role="group" aria-label={t('topology.by.label')}>
        {#each options as o (o)}
          <button type="button" data-by="{col}:{o}" aria-pressed={by === o}>{t(`topology.by.${o}` as TKey)}</button>
        {/each}
      </span>
    {/if}
  </div>
  {@render head?.()}
  <div class="kay" bind:this={k} data-kol={col} id="k-{col}" onscroll={sync}>
    {#if list.items.length}
      <div class="vl" style:height="{list.tops[list.items.length]}px" style:padding-top="{list.tops[win[0]]}px">
        {#each slice as it (it.key)}
          {#if it.k === 'g'}
            <button type="button" class="gh" class:ilk={it.first} class:yok={it.v.dim} id={it.v.id} data-g={it.v.id} tabindex="-1" aria-expanded={it.v.open}>
              <span class="ch"><Icon name="chevron-right" size={12} /></span>
              {@html MARK[it.v.st]}
              <span class="g-ad">{groupName(col, by, it.v.key)}</span>
              <span class="g-sy">{groupSummary(col, it.v)}</span>
            </button>
          {:else if it.k === 'r'}
            <TopoRow n={it.n} {g} rel={rel.get(it.n.id)} sel={it.n.id === sel} {now} />
          {:else if it.k === 'x'}
            <button type="button" class="kalan" id={it.key} data-tam={it.v.id} tabindex="-1" style:height="{it.h}px">
              {t('topology.rest', { n: it.v.rest, what: t(`topology.noun.${col}`, { count: it.v.rest }) })}
              <span class="hucre" aria-hidden="true">{@html cellSvg(it.v.cells, it.w)}</span>
            </button>
          {:else}
            <span class="hucre" class:yok={it.v.dim} data-g={it.v.id} aria-hidden="true" style:height="{it.h}px">{@html cellSvg(it.v.cells, it.w)}</span>
          {/if}
        {/each}
      </div>
    {:else}
      <p class="bos">{empty}</p>
    {/if}
    <div class="kuyruk">{@render tail?.()}</div>
  </div>
</section>
