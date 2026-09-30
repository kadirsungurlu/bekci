<script lang="ts">
  // Durum sayfası listesindeki küçük önizleme: sayfanın özetinden (genel durum,
  // grup adları, monitör durumları) çizilen hafif bir minyatür. iframe/ekran görüntüsü değil.
  import type { OverallStatus, PageLayout, PublicMonitorStatus } from '../lib/api';
  import Icon, { type IconName } from './Icon.svelte';

  let {
    title,
    logo = '',
    status,
    sections,
    emptyText = '',
    moreText,
    blank = false,
    layout,
  }: {
    title: string;
    logo?: string;
    status: OverallStatus;
    sections: { title: string; statuses: PublicMonitorStatus[] }[];
    emptyText?: string;
    moreText?: (n: number) => string;
    /** Boş durum çizimi: metinler gri çizgi olarak gösterilir. */
    blank?: boolean;
    /** Sayfanın dizilimi: yerleşim ve genel durum kutusunun yeri/görünürlüğü kabaca yansıtılır. */
    layout?: PageLayout;
  } = $props();

  const style = $derived(layout?.style ?? 'list');
  const blockOn = (id: string) => layout?.blocks.find((b) => b.id === id)?.visible !== false;
  const heroOn = $derived(blockOn('overall'));
  const groupsOn = $derived(blockOn('groups'));
  const heroAfter = $derived.by(() => {
    const ids = layout?.blocks.map((b) => b.id) ?? [];
    return ids.indexOf('overall') > ids.indexOf('groups');
  });

  const MAX_SECTIONS = 3;
  const MAX_ROWS = 3; // grup başına monitör satırı
  const PILLS = 16; // satır başına minik çubuk (herkese açık sayfadaki çubukların taklidi)
  const visible = $derived(sections.filter((s) => s.statuses.length > 0));
  const shown = $derived(visible.slice(0, MAX_SECTIONS));
  const more = $derived(visible.length - shown.length);
  const ICON: Record<OverallStatus, IconName> = { up: 'check', partial: 'alert', down: 'x', unknown: 'info' };
  const pillClass = (st: PublicMonitorStatus) => (st === 'maintenance' ? 'maint' : st);
</script>

<div class="th" aria-hidden="true">
  <div class="th-top">
    {#if logo}
      <img src={logo} alt="" />
    {:else}
      <span class="th-mark"><Icon name="activity" size={9} stroke={3} /></span>
    {/if}
    {#if blank}
      <span class="th-line w60"></span>
    {:else}
      <span class="th-title">{title}</span>
    {/if}
  </div>
  {#snippet hero()}
    <div class="th-hero st-{status}">
      <span class="th-hic"><Icon name={ICON[status]} size={8} stroke={3.4} /></span>
      <span class="th-line light w50"></span>
    </div>
  {/snippet}
  <div class="th-in st-{style}">
    {#if heroOn && !heroAfter}{@render hero()}{/if}
    {#if !groupsOn}
      <!-- gruplar gizli: yalnızca genel durum -->
    {:else if shown.length === 0}
      <div class="th-empty">{emptyText}</div>
    {:else}
      <div class="th-secs">
      {#each shown as sec, i (i)}
        <div class="th-sec">
          {#if sec.title && !blank}
            <span class="th-st">{sec.title}</span>
          {:else}
            <span class="th-line w40"></span>
          {/if}
          {#each sec.statuses.slice(0, MAX_ROWS) as st, j (j)}
            {#if style === 'rows'}
              <!-- tek satır: ışık + ad + çubuklar aynı satırda -->
              <div class="th-rrow">
                <span class="th-cdot {pillClass(st)}"></span>
                <span class="th-line"></span>
                <div class="th-pills">
                  {#each { length: PILLS } as _, k (k)}
                    <span class="th-pill {pillClass(st)}"></span>
                  {/each}
                </div>
              </div>
            {:else if style === 'compact'}
              <div class="th-crow">
                <span class="th-cdot {pillClass(st)}"></span>
                <span class="th-line w60"></span>
              </div>
            {:else}
              <div class="th-pills">
                {#each { length: PILLS } as _, k (k)}
                  <span class="th-pill {pillClass(st)}"></span>
                {/each}
              </div>
            {/if}
          {/each}
          {#if sec.statuses.length > MAX_ROWS}<span class="th-plus">+{sec.statuses.length - MAX_ROWS}</span>{/if}
        </div>
      {/each}
      </div>
      {#if more > 0 && moreText}<div class="th-more">{moreText(more)}</div>{/if}
    {/if}
    {#if heroOn && heroAfter}{@render hero()}{/if}
  </div>
</div>

<style>
  .th {
    container-type: inline-size;
    height: 100%;
    min-height: inherit;
    display: flex;
    flex-direction: column;
    border-radius: 12px;
    overflow: hidden;
    background: var(--bg);
    border: 1px solid var(--border);
    font-size: 10px;
    line-height: 1.2;
    user-select: none;
  }
  .th-top {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 8px 10px;
    background: var(--bg-elev);
    border-bottom: 1px solid var(--border);
    min-width: 0;
  }
  .th-top img {
    max-height: 14px;
    max-width: 60px;
    object-fit: contain;
  }
  .th-mark {
    width: 14px;
    height: 14px;
    border-radius: 4px;
    display: grid;
    place-items: center;
    background: #0f766e;
    color: #fff;
    flex-shrink: 0;
  }
  .th-title {
    font-weight: 700;
    color: var(--text);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .th-in {
    flex: 1;
    overflow: hidden;
    display: flex;
    flex-direction: column;
    gap: 7px;
    padding: 9px 10px 10px;
    min-height: 0;
  }
  .th-hero {
    display: flex;
    align-items: center;
    gap: 6px;
    height: 20px;
    padding: 0 7px;
    border-radius: 6px;
    background: linear-gradient(135deg, #1f2b40, #172133);
    flex-shrink: 0;
  }
  .th-hero.st-up {
    background: linear-gradient(135deg, #14532d, #115e59);
  }
  .th-hero.st-partial {
    background: linear-gradient(135deg, #92400e, #78350f);
  }
  .th-hero.st-down {
    background: linear-gradient(135deg, #991b1b, #7f1d1d);
  }
  .th-hic {
    width: 12px;
    height: 12px;
    border-radius: 50%;
    display: grid;
    place-items: center;
    background: rgba(255, 255, 255, 0.22);
    color: #fff;
    flex-shrink: 0;
  }
  .th-line {
    height: 5px;
    border-radius: 3px;
    background: var(--border-strong);
    display: block;
  }
  .th-line.light {
    background: rgba(255, 255, 255, 0.45);
  }
  .w60 {
    width: 60%;
  }
  .w50 {
    width: 50%;
  }
  .w40 {
    width: 40%;
    margin: 2px 0;
  }
  .th-secs {
    display: flex;
    flex-direction: column;
    gap: 7px;
  }
  /* Geniş küçük resimde (dar ekranda tam genişlik) gruplar yan yana. */
  @container (min-width: 280px) {
    .th-secs {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(84px, 1fr));
    }
  }
  /* Izgara yerleşimi: gruplar her genişlikte iki sütun kart. */
  .st-grid .th-secs {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  /* Sık liste: çubuk yerine nokta + satır. */
  .th-crow {
    display: flex;
    align-items: center;
    gap: 4px;
    height: 6px;
  }
  .th-crow .th-line {
    height: 4px;
    flex: 1;
  }
  /* Tek satır: ışık + kısa ad çizgisi + çubuklar. */
  .th-rrow {
    display: flex;
    align-items: center;
    gap: 4px;
  }
  .th-rrow .th-line {
    width: 24%;
    height: 4px;
    flex: none;
  }
  .th-rrow .th-pills {
    flex: 1;
    min-width: 0;
  }
  .th-cdot {
    width: 5px;
    height: 5px;
    border-radius: 50%;
    background: var(--empty-bar);
    flex-shrink: 0;
  }
  .th-cdot.up {
    background: var(--up);
  }
  .th-cdot.down {
    background: var(--down);
  }
  .th-cdot.pending {
    background: var(--pending);
  }
  .th-cdot.maint {
    background: var(--maint);
  }
  .th-cdot.paused {
    background: var(--paused);
  }
  .th-sec {
    display: flex;
    flex-direction: column;
    gap: 4px;
    padding: 6px 7px;
    border-radius: 6px;
    background: var(--card);
    border: 1px solid var(--border);
    min-width: 0;
  }
  .th-st {
    font-size: 9px;
    font-weight: 700;
    color: var(--text-2);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .th-pills {
    display: flex;
    gap: 2px;
  }
  .th-pill {
    flex: 1 1 0;
    height: 6px;
    border-radius: 999px;
    background: var(--empty-bar);
  }
  .th-pill.up {
    background: var(--up);
  }
  .th-pill.down {
    background: var(--down);
  }
  .th-pill.pending {
    background: var(--pending);
  }
  .th-pill.maint {
    background: var(--maint);
  }
  .th-pill.paused {
    background: var(--paused);
  }
  .th-plus {
    font-size: 8.5px;
    color: var(--muted);
    line-height: 1;
  }
  .th-more,
  .th-empty {
    font-size: 9.5px;
    color: var(--muted);
  }
  .th-empty {
    flex: 1;
    display: grid;
    place-items: center;
    text-align: center;
  }
</style>
