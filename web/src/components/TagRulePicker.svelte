<script lang="ts">
  // Etiket kuralı seçici: "şu etiketi (= değeri) taşıyan monitörler". Bildirim
  // kanalı, kısıtlı kullanıcı ve durum sayfası grubu için ortak. Etiket listesi
  // sunucudan alınır; değer önerileri monitörlerde kullanılan değerlerdir.
  import { onMount } from 'svelte';
  import { api, type Tag, type TagRule } from '../lib/api';
  import { live } from '../lib/live.svelte';
  import { collator } from '../lib/format';
  import { t } from '../lib/i18n';
  import TagChip from './TagChip.svelte';
  import Icon from './Icon.svelte';

  let {
    rules = $bindable([]),
    label,
    help,
    id = 'trp',
    /** Tek kural (durum sayfası grubu): ekleme satırı seçimle anında yazar, liste yok. */
    single = false,
    /** Kurallar değişince (bind yerine kullanılabilir). */
    onchange,
  }: { rules?: TagRule[]; label?: string; help?: string; id?: string; single?: boolean; onchange?: (rules: TagRule[]) => void } = $props();

  let tags = $state.raw<Tag[]>([]);
  let loaded = $state(false);
  let pick = $state('');
  let value = $state('');

  onMount(async () => {
    try {
      tags = await api.tags();
    } catch {
      tags = [];
    } finally {
      loaded = true;
    }
  });

  const tagById = $derived(new Map(tags.map((x) => [x.id, x])));
  const sorted = $derived([...tags].sort((a, b) => collator.compare(a.name, b.name)));
  // Seçili etiketin monitörlerde kullanılan değerleri (öneri listesi).
  const values = $derived.by(() => {
    const tid = Number(pick);
    if (!tid) return [];
    const set = new Set<string>();
    for (const m of live.monitors) for (const tg of m.tags ?? []) if (tg.id === tid && tg.value) set.add(tg.value);
    return [...set].sort(collator.compare);
  });
  // Kurala uyan monitör sayısı (canlı listeden).
  function matches(r: TagRule): number {
    let n = 0;
    for (const m of live.monitors) if ((m.tags ?? []).some((tg) => tg.id === r.tag_id && (!r.value || tg.value === r.value))) n++;
    return n;
  }

  function add() {
    const tid = Number(pick);
    if (!tid) return;
    const v = value.trim();
    if (rules.some((r) => r.tag_id === tid && r.value === v)) {
      pick = '';
      value = '';
      return;
    }
    const tg = tagById.get(tid);
    const rule: TagRule = { tag_id: tid, value: v, name: tg?.name, color: tg?.color };
    rules = single ? [rule] : [...rules, rule];
    onchange?.(rules);
    pick = '';
    value = '';
  }
  function remove(i: number) {
    rules = rules.filter((_, k) => k !== i);
    onchange?.(rules);
  }
</script>

<div class="trp" role="group" aria-labelledby="{id}-l">
  {#if label}<span class="label" id="{id}-l">{label}</span>{/if}
  {#if rules.length}
    <ul class="rules">
      {#each rules as r, i (r.tag_id + ':' + r.value)}
        {@const tg = tagById.get(r.tag_id)}
        <li>
          <TagChip name={r.name ?? tg?.name ?? `#${r.tag_id}`} color={r.color ?? tg?.color ?? ''} value={r.value} onremove={() => remove(i)} />
          <span class="muted small">{r.value ? t('tags.rule.withValue') : t('tags.rule.anyValue')} · {t('tags.rule.matches', { count: matches(r) })}</span>
        </li>
      {/each}
    </ul>
  {/if}
  {#if !loaded}
    <div class="skeleton" style="height:40px"></div>
  {:else if tags.length === 0}
    <span class="muted small">{t('tags.rule.noTags')}</span>
  {:else if !single || rules.length === 0}
    <div class="add">
      <select class="input" bind:value={pick} aria-label={t('tags.rule.tag')}>
        <option value="">{t('tags.rule.pickTag')}</option>
        {#each sorted as tg (tg.id)}<option value={String(tg.id)}>{tg.name}</option>{/each}
      </select>
      <input
        class="input"
        list="{id}-vals"
        maxlength="100"
        bind:value
        disabled={!pick}
        placeholder={t('tags.rule.valuePh')}
        aria-label={t('tags.rule.value')}
        onkeydown={(e) => {
          if (e.key === 'Enter') {
            e.preventDefault();
            add();
          }
        }}
      />
      <datalist id="{id}-vals">
        {#each values as v (v)}<option value={v}></option>{/each}
      </datalist>
      <button type="button" class="btn sm" onclick={add} disabled={!pick}><Icon name="plus" size={14} /> {t('common.add')}</button>
    </div>
  {/if}
  {#if help}<span class="help">{help}</span>{/if}
</div>

<style>
  .trp {
    display: flex;
    flex-direction: column;
    gap: 8px;
    min-width: 0;
  }
  .rules {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .rules li {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
  }
  .add {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, 1fr) auto;
    gap: 8px;
    align-items: center;
  }
  @media (max-width: 640px) {
    .add {
      grid-template-columns: minmax(0, 1fr);
    }
  }
</style>
