<script lang="ts">
  // İçe aktarma/geri yükleme sonucu (önizleme veya gerçek): özet tablo, uyarılar ve kayıt listesi.
  import type { ImportCounts, ImportItem, ImportKind, ImportResultKind, ImportSummary, NotificationType } from '../lib/api';
  import { NOTIFY_LABELS } from '../lib/notifyTypes';
  import { typeDef } from '../lib/monitorTypes';
  import Icon from './Icon.svelte';
  import { t } from '../lib/i18n';

  let { result }: { result: ImportSummary } = $props();

  const KINDS: { k: keyof ImportCounts; kind: ImportKind }[] = [
    { k: 'monitors', kind: 'monitor' },
    { k: 'notifications', kind: 'notification' },
    { k: 'tags', kind: 'tag' },
    { k: 'status_pages', kind: 'status_page' },
  ];

  const KIND_KEYS: ImportKind[] = ['monitor', 'notification', 'tag', 'status_page', 'settings'];
  const kindLabel = (k: ImportKind) => (KIND_KEYS.includes(k) ? t(`backup.result.kind.${k}`) : k);

  const dry = $derived(result.dry_run);
  const RES = $derived<Record<ImportResultKind, { l: string; c: string }>>({
    created: { l: dry ? t('backup.result.willCreate') : t('backup.result.created'), c: 'up' },
    existing: { l: t('backup.result.existing'), c: 'paused' },
    skipped: { l: dry ? t('backup.result.willSkip') : t('backup.result.skipped'), c: 'pending' },
  });

  const source = (s: ImportSummary['source']) => t(`backup.result.sources.${s}`);

  const items = $derived(result.items ?? []);
  const warnings = $derived(result.warnings ?? []);
  const rows = $derived(
    KINDS.filter(
      (k) =>
        result.created[k.k] + result.existing[k.k] + result.skipped[k.k] + (result.deleted?.[k.k] ?? 0) > 0 ||
        k.k === 'monitors',
    ),
  );

  let filter = $state<'all' | ImportResultKind>('all');
  let showAll = $state(false);
  const LIMIT = 150;

  const count = (r: ImportResultKind) => items.filter((i) => i.result === r).length;
  const filtered = $derived(filter === 'all' ? items : items.filter((i) => i.result === filter));
  const shown = $derived(showAll ? filtered : filtered.slice(0, LIMIT));

  function typeText(it: ImportItem): string {
    if (!it.type) return '';
    if (it.kind === 'monitor') return typeDef(it.type)?.label ?? it.type;
    if (it.kind === 'notification') return NOTIFY_LABELS[it.type as NotificationType] ?? it.type;
    return it.type;
  }
</script>

<div class="ir">
  <div class="ir-head">
    {#if dry}
      <span class="badge accent"><Icon name="eye" size={12} /> {t('backup.result.preview')}</span>
      <span class="text-2 small"
        >{t('backup.result.nothingChanged')} {source(result.source)} · {result.mode === 'replace'
          ? t('backup.result.replace')
          : t('backup.result.merge')}</span
      >
    {:else}
      <span class="badge up"><Icon name="check" size={12} /> {t('backup.result.completed')}</span>
      <span class="text-2 small"
        >{source(result.source)} · {result.mode === 'replace' ? t('backup.result.replace') : t('backup.result.merge')}</span
      >
    {/if}
  </div>

  <div class="sum-wrap">
    <table class="sum">
      <thead>
        <tr>
          <th></th>
          <th>{RES.created.l}</th>
          <th>{RES.existing.l}</th>
          <th>{RES.skipped.l}</th>
          {#if result.deleted}<th>{dry ? t('backup.result.willDelete') : t('backup.result.deleted')}</th>{/if}
        </tr>
      </thead>
      <tbody>
        {#each rows as r (r.k)}
          <tr>
            <th scope="row">{t(`backup.result.kinds.${r.k}`)}</th>
            <td class:c-up={result.created[r.k] > 0}>{result.created[r.k]}</td>
            <td>{result.existing[r.k]}</td>
            <td class:c-pending={result.skipped[r.k] > 0}>{result.skipped[r.k]}</td>
            {#if result.deleted}<td class:c-down={result.deleted[r.k] > 0}>{result.deleted[r.k]}</td>{/if}
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
  {#if result.settings_applied}
    <p class="help nomargin">{dry ? t('backup.result.settingsWill') : t('backup.result.settingsDone')}</p>
  {/if}

  {#if warnings.length}
    <div class="alert warning warns">
      <b>{t('backup.result.warnings')}</b>
      <ul>
        {#each warnings as w, i (i)}<li>{w}</li>{/each}
      </ul>
    </div>
  {/if}

  {#if items.length}
    <div class="seg" role="radiogroup" aria-label={t('backup.result.filter')}>
      <button type="button" role="radio" aria-checked={filter === 'all'} class:active={filter === 'all'} onclick={() => (filter = 'all')}
        >{t('backup.result.all', { count: items.length })}</button
      >
      {#each ['created', 'existing', 'skipped'] as const as r (r)}
        {#if count(r) > 0}
          <button type="button" role="radio" aria-checked={filter === r} class:active={filter === r} onclick={() => (filter = r)}>
            {RES[r].l} ({count(r)})
          </button>
        {/if}
      {/each}
    </div>
    <ul class="items">
      {#each shown as it, i (i)}
        <li>
          <div class="it-main">
            <span class="it-name">
              {#if !dry && it.kind === 'monitor' && it.id && it.result === 'created'}
                <a href="#/monitors/{it.id}">{it.name}</a>
              {:else}
                {it.name}
              {/if}
            </span>
            <span class="it-meta muted small">{kindLabel(it.kind)}{typeText(it) ? ` · ${typeText(it)}` : ''}</span>
          </div>
          <span class="badge {RES[it.result]?.c ?? ''}">{RES[it.result]?.l ?? it.result}</span>
          {#if it.messages?.length}
            <ul class="msgs">
              {#each it.messages as m, j (j)}<li>{m}</li>{/each}
            </ul>
          {/if}
        </li>
      {/each}
    </ul>
    {#if filtered.length > shown.length}
      <button type="button" class="linkbtn more" onclick={() => (showAll = true)}>{t('backup.result.showMore', { count: filtered.length - shown.length })}</button
      >
    {/if}
  {/if}
</div>

<style>
  .ir {
    display: flex;
    flex-direction: column;
    gap: 14px;
    border: 1px solid var(--border);
    background: var(--bg-elev);
    border-radius: var(--radius);
    padding: 16px;
  }
  .ir-head {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
  }
  .ir-head .badge :global(svg) {
    margin-right: 4px;
  }
  .sum-wrap {
    overflow-x: auto;
  }
  .sum {
    border-collapse: collapse;
    font-size: 0.88rem;
    min-width: 100%;
  }
  .sum th,
  .sum td {
    padding: 6px 10px;
    text-align: right;
    border-bottom: 1px solid var(--border);
    white-space: nowrap;
  }
  .sum thead th {
    font-size: 0.72rem;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--muted);
    font-weight: 700;
  }
  .sum tbody th {
    text-align: left;
    font-weight: 600;
    color: var(--text-2);
  }
  .sum tbody tr:last-child th,
  .sum tbody tr:last-child td {
    border-bottom: none;
  }
  .sum td {
    font-weight: 700;
    font-variant-numeric: tabular-nums;
  }
  .warns ul {
    margin: 6px 0 0;
    padding-left: 18px;
  }
  .nomargin {
    margin: 0;
  }
  .items {
    list-style: none;
    margin: 0;
    padding: 0 4px 0 0;
    border-top: 1px solid var(--border);
    max-height: 460px;
    overflow-y: auto;
    overscroll-behavior: contain;
  }
  .items > li {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    gap: 4px 12px;
    align-items: center;
    padding: 9px 0;
    border-bottom: 1px solid var(--border);
  }
  .it-main {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .it-name {
    font-weight: 600;
    overflow-wrap: anywhere;
  }
  .msgs {
    grid-column: 1 / -1;
    margin: 0;
    padding-left: 18px;
    font-size: 0.8rem;
    color: var(--text-2);
  }
  .more {
    align-self: flex-start;
    font-size: 0.88rem;
  }
  @media (max-width: 640px) {
    .ir {
      padding: 12px;
    }
  }
</style>
