<script lang="ts">
  // Topoloji sütunu: başlık, gruplama seçimi, gruplar. Tıklamalar sayfada toplanır (data-*).
  import type { Snippet } from 'svelte';
  import Icon from './Icon.svelte';
  import TopoRow from './TopoRow.svelte';
  import { t, type TKey } from '../lib/i18n';
  import { MARK, type Col, type Graph, type GroupView, type St } from '../lib/topomodel';
  import { groupName, groupSummary } from '../lib/topotext';

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
    now,
    filtered,
    sorun,
    head,
    tail,
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
    now: number;
    filtered: boolean;
    sorun: boolean;
    head?: Snippet;
    tail?: Snippet;
  } = $props();

  const noun = $derived(t(`topology.noun.${col}`));
  const empty = $derived(sorun ? t('topology.emptyProblems', { what: noun }) : filtered ? t('topology.emptySearch', { what: noun }) : t('topology.emptyCol', { what: noun }));
</script>

<section class="sutun {cls}" id="c-{col}" aria-label={label}>
  <div class="s-b">
    <h2>{title}</h2>
    <span class="say">{count}</span>
    {#if options.length > 1}
      <span class="seg" role="group" aria-label={t('topology.by.label')}>
        {#each options as o (o)}
          <button type="button" data-by="{col}:{o}" aria-pressed={by === o}>{t(`topology.by.${o}` as TKey)}</button>
        {/each}
      </span>
    {/if}
  </div>
  {@render head?.()}
  <div class="kay" data-kol={col} id="k-{col}">
    {#each views as v (v.id)}
      <div class="grup" class:yok={v.dim}>
        <button type="button" class="gh" id={v.id} data-g={v.id} tabindex="-1" aria-expanded={v.open}>
          <span class="ch"><Icon name="chevron-right" size={12} /></span>
          {@html MARK[v.st]}
          <span class="g-ad">{groupName(col, by, v.key)}</span>
          <span class="g-sy">{groupSummary(col, v)}</span>
        </button>
        {#each v.rows as n (n.id)}
          <TopoRow {n} {g} rel={rel.get(n.id)} sel={n.id === sel} {now} />
        {/each}
        {#if v.rest}
          <button type="button" class="kalan" data-tam={v.id} tabindex="-1">
            {t('topology.rest', { n: v.rest, what: noun })}
            <span class="hucre" aria-hidden="true">{@html v.cells}</span>
          </button>
        {:else if !v.open}
          <span class="hucre" aria-hidden="true">{@html v.cells}</span>
        {/if}
      </div>
    {:else}
      <p class="bos">{empty}</p>
    {/each}
    {@render tail?.()}
  </div>
</section>
