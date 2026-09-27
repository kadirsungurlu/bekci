<script lang="ts">
  // Sunucu uyarı kuralları: editör düzenler (tamamı tek PUT ile kaydedilir),
  // izleyici yalnızca okur. Tetiklenmiş kurallar vurgulanır.
  import { untrack } from 'svelte';
  import { api, errorMessage, type AlertRule, type AlertRuleInput, type ServerMetric } from '../lib/api';
  import { METRICS, METRIC_ORDER, UNIT_LABELS, fmtMetric, metricLabel } from '../lib/servers.svelte';
  import { fmtRelative } from '../lib/format';
  import { clock, toast } from '../lib/ui.svelte';
  import Icon from './Icon.svelte';

  let {
    serverId,
    rules,
    canEdit,
    mounts = [],
    onsaved,
  }: {
    serverId: number;
    rules: AlertRule[];
    canEdit: boolean;
    mounts?: string[]; // sunucudaki disk bölümleri (disk kuralının bölüm seçimi için)
    onsaved: (rules: AlertRule[]) => void;
  } = $props();

  interface Row {
    key: number;
    metric: ServerMetric;
    mount: string; // disk: bölüm ("" = en dolu bölüm)
    threshold: number;
    minutes: number;
    active: boolean;
  }

  let seq = 0;
  const toRows = (list: AlertRule[]): Row[] =>
    sortRules(list).map((r) => ({
      key: ++seq,
      metric: r.metric,
      mount: r.mount ?? '',
      threshold: r.threshold,
      minutes: r.minutes,
      active: r.active,
    }));
  const sortRules = <T extends { metric: ServerMetric; mount?: string }>(list: T[]) =>
    list
      .slice()
      .sort((a, b) => METRIC_ORDER.indexOf(a.metric) - METRIC_ORDER.indexOf(b.metric) || (a.mount ?? '').localeCompare(b.mount ?? ''));
  // Kural metrik + bölümle tanınır (disk bölüm başına ayrı kural olabilir).
  const ruleKey = (r: { metric: ServerMetric; mount?: string }) => `${r.metric}\u0000${r.mount ?? ''}`;
  const mountLabel = (m: string) => m || 'En dolu bölüm';

  let rows = $state<Row[]>([]);
  let saving = $state(false);
  let error = $state('');

  // Sunucudan gelen kurallar değişince (kayıt, yenileme) düzenleme sıfırlanır;
  // kullanıcı düzenlerken gelen yenileme onun değişikliklerini ezmez.
  let lastSig = '';
  $effect(() => {
    const sig = JSON.stringify(sortRules(rules).map((r) => [r.metric, r.mount ?? '', r.threshold, r.minutes, r.active]));
    untrack(() => {
      if (sig !== lastSig && !dirtyNow()) {
        rows = toRows(rules);
        error = '';
      }
    });
    lastSig = sig;
  });

  const origSig = $derived(JSON.stringify(sortRules(rules).map((r) => [r.metric, r.mount ?? '', +r.threshold, +r.minutes, r.active])));
  const curSig = $derived(JSON.stringify(sortRules(rows).map((r) => [r.metric, r.mount, +r.threshold, +r.minutes, r.active])));
  const dirty = $derived(curSig !== origSig);
  function dirtyNow() {
    return rows.length > 0 && curSig !== origSig;
  }

  const byKey = $derived(new Map(rules.map((r) => [ruleKey(r), r])));
  const used = $derived(new Set(rows.map(ruleKey)));
  // Disk kuralında seçilebilecek bölümler: "en dolu bölüm", sunucudaki
  // bölümler ve şu an görünmeyen ama kuralı olan bölümler.
  const mountOptions = $derived(['', ...new Set([...mounts, ...rows.filter((r) => r.metric === 'disk').map((r) => r.mount)].filter(Boolean))]);
  const freeMount = $derived(mountOptions.find((m) => !used.has(ruleKey({ metric: 'disk', mount: m }))));
  const free = $derived(METRIC_ORDER.filter((m) => (m === 'disk' ? freeMount !== undefined : !used.has(ruleKey({ metric: m, mount: '' })))));

  function add() {
    const m = free[0];
    if (!m) return;
    const info = METRICS[m];
    rows = [...rows, { key: ++seq, metric: m, mount: m === 'disk' ? (freeMount ?? '') : '', threshold: info.threshold, minutes: info.minutes, active: true }];
  }

  function remove(key: number) {
    rows = rows.filter((r) => r.key !== key);
  }

  function changeMetric(r: Row, m: ServerMetric) {
    const info = METRICS[m];
    // Birim değişiyorsa (ör. % → °C) önerilen eşiğe geç.
    if (METRICS[r.metric].unit !== info.unit) r.threshold = info.threshold;
    r.metric = m;
    r.mount = m === 'disk' ? (freeMount ?? '') : '';
    if (m === 'offline') r.threshold = 0;
  }

  function rowError(r: Row): string {
    const info = METRICS[r.metric];
    const min = Number(r.minutes);
    if (!Number.isInteger(min) || min < 1 || min > 60) return 'Süre 1-60 dakika olmalı';
    if (rows.some((o) => o !== r && ruleKey(o) === ruleKey(r))) {
      return r.metric === 'disk' ? `${mountLabel(r.mount)} için zaten bir disk kuralı var` : 'Bu metrik için zaten bir kural var';
    }
    if (r.metric === 'offline') return '';
    const t = Number(r.threshold);
    if (!Number.isFinite(t) || t < info.min || t > info.max) {
      return info.unit === 'pct'
        ? 'Eşik %1-100 arasında olmalı'
        : info.unit === 'temp'
          ? `Eşik ${info.min}-${info.max} °C arasında olmalı`
          : `Eşik ${String(info.min).replace('.', ',')}-${info.max} arasında olmalı`;
    }
    return '';
  }

  const invalid = $derived(rows.some((r) => rowError(r) !== ''));
  let showErrors = $state(false);

  async function save() {
    showErrors = true;
    if (invalid) return;
    error = '';
    saving = true;
    try {
      const body: AlertRuleInput[] = rows.map((r) => ({
        metric: r.metric,
        mount: r.metric === 'disk' ? r.mount : '',
        threshold: r.metric === 'offline' ? 0 : Number(r.threshold),
        minutes: Number(r.minutes),
        active: r.active,
      }));
      const saved = await api.putServerAlerts(serverId, body);
      lastSig = '';
      rows = toRows(saved);
      showErrors = false;
      onsaved(saved);
      toast.success('Uyarı kuralları kaydedildi');
    } catch (e) {
      error = errorMessage(e);
    } finally {
      saving = false;
    }
  }

  function reset() {
    rows = toRows(rules);
    error = '';
    showErrors = false;
  }

  function sentence(r: { metric: ServerMetric; threshold: number; minutes: number }): string {
    if (r.metric === 'offline') return `${r.minutes} dk veri gelmezse`;
    return `${r.minutes} dk ortalaması ${fmtMetric(r.metric, r.threshold)} üzerindeyse`;
  }
</script>

{#if !canEdit}
  {#if rules.length === 0}
    <p class="none">Bu sunucu için uyarı kuralı yok.</p>
  {:else}
    <ul class="ro">
      {#each sortRules(rules) as r (r.id)}
        <li class:firing={r.firing} class:off={!r.active}>
          <span class="rm">{metricLabel(r.metric)}{#if r.metric === 'disk'}<span class="mnt" class:path={!!r.mount}>{mountLabel(r.mount ?? '')}</span>{/if}</span>
          <span class="rs">{sentence(r)}</span>
          {#if r.firing}
            <span class="badge pending">Tetiklendi{r.fired_at ? ` · ${fmtRelative(r.fired_at, clock.now)}` : ''}</span>
          {:else if !r.active}
            <span class="badge paused">Kapalı</span>
          {/if}
        </li>
      {/each}
    </ul>
  {/if}
{:else}
  <div class="rules">
    {#each rows as r (r.key)}
      {@const info = METRICS[r.metric]}
      {@const orig = byKey.get(ruleKey(r))}
      {@const err = showErrors ? rowError(r) : ''}
      <div class="rule" class:firing={orig?.firing} class:off={!r.active}>
        <div class="rl">
          <select
            class="input msel"
            aria-label="Metrik"
            value={r.metric}
            onchange={(e) => changeMetric(r, (e.currentTarget as HTMLSelectElement).value as ServerMetric)}
          >
            {#each METRIC_ORDER as m (m)}
              <option value={m} disabled={m !== r.metric && !free.includes(m)}>{metricLabel(m)}</option>
            {/each}
          </select>
          {#if r.metric === 'disk'}
            <select class="input msel mount" aria-label="Disk bölümü" bind:value={r.mount}>
              {#each mountOptions as mo (mo)}
                <option value={mo} disabled={mo !== r.mount && used.has(ruleKey({ metric: 'disk', mount: mo }))}>{mountLabel(mo)}</option>
              {/each}
            </select>
          {/if}

          {#if r.metric !== 'offline'}
            <span class="w">≥</span>
            <label class="unitbox" class:wide={info.unit === 'load'}>
              {#if info.unit === 'pct'}<span class="u pre">%</span>{/if}
              <input
                class="input num"
                class:invalid={!!err && err.startsWith('Eşik')}
                type="number"
                inputmode="decimal"
                min={info.min}
                max={info.max}
                step={info.step}
                bind:value={r.threshold}
                aria-label="Eşik ({UNIT_LABELS[info.unit]})"
              />
              {#if info.unit !== 'pct'}<span class="u">{UNIT_LABELS[info.unit]}</span>{/if}
            </label>
            <span class="w sep">·</span>
          {/if}
          <label class="unitbox">
              <input
                class="input num sm"
                class:invalid={!!err && err.startsWith('Süre')}
                type="number"
                inputmode="numeric"
                min="1"
                max="60"
                step="1"
                bind:value={r.minutes}
                aria-label="Süre (dakika)"
              />
              <span class="u">{r.metric === 'offline' ? 'dk veri gelmezse' : 'dk ortalaması'}</span>
            </label>
        </div>
        <div class="rr">
          {#if orig?.firing}
            <span class="badge pending fire" title="Kural şu an tetiklenmiş durumda">
              <Icon name="alert" size={11} /> Tetiklendi{orig.fired_at ? ` · ${fmtRelative(orig.fired_at, clock.now)}` : ''}
            </span>
          {/if}
          <label class="check act"><input type="checkbox" bind:checked={r.active} /> Etkin</label>
          <button
            type="button"
            class="btn ghost sm icon"
            aria-label="{metricLabel(r.metric)}{r.metric === 'disk' ? ` (${mountLabel(r.mount)})` : ''} kuralını kaldır"
            onclick={() => remove(r.key)}
          >
            <Icon name="trash" size={15} />
          </button>
        </div>
        {#if err}<div class="rerr">{err}</div>{:else}<div class="rdesc">
            {r.metric === 'disk' && r.mount ? `${r.mount} bölümünün doluluğu` : info.desc}
          </div>{/if}
      </div>
    {:else}
      <p class="none">Kural yok: bu sunucu için hiç uyarı gönderilmez.</p>
    {/each}
  </div>

  {#if error}<div class="alert error saveerr" role="alert">{error}</div>{/if}

  <div class="bar">
    <button type="button" class="btn sm" onclick={add} disabled={free.length === 0}><Icon name="plus" size={14} /> Kural ekle</button>
    <div class="spacer"></div>
    {#if dirty}
      <button type="button" class="btn sm ghost" onclick={reset} disabled={saving}>Vazgeç</button>
    {/if}
    <button type="button" class="btn sm primary" onclick={save} disabled={!dirty || saving}>
      {#if saving}<span class="spinner"></span>{/if} Kaydet
    </button>
  </div>
{/if}

<style>
  .none {
    color: var(--muted);
    margin: 0 0 4px;
    font-size: 0.9rem;
  }
  .ro {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .ro li {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
    padding: 9px 12px;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    font-size: 0.9rem;
  }
  .ro li.firing,
  .rule.firing {
    border-color: var(--pending-border);
    background: var(--pending-soft);
  }
  .ro li.off .rs,
  .ro li.off .rm {
    opacity: 0.55;
  }
  .rm {
    font-weight: 700;
    min-width: 72px;
  }
  .mnt {
    font-weight: 500;
    color: var(--text-2);
    font-size: 0.9em;
    margin-left: 6px;
  }
  .mnt.path {
    font-family: var(--mono);
    font-size: 0.85em;
  }
  .rs {
    color: var(--text-2);
    flex: 1;
  }
  .rules {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .rule {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    align-items: center;
    gap: 6px 12px;
    padding: 10px 10px 8px 12px;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--input);
  }
  .rule.off .rl {
    opacity: 0.55;
  }
  .rl {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
    min-width: 0;
  }
  .rr {
    display: flex;
    align-items: center;
    gap: 8px;
    justify-content: flex-end;
  }
  .msel {
    width: 132px;
    height: 36px;
    font-weight: 600;
  }
  .msel.mount {
    width: 168px;
    font-weight: 500;
  }
  .w {
    color: var(--muted);
    font-weight: 600;
  }
  .unitbox {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: 0.86rem;
    color: var(--text-2);
    white-space: nowrap;
  }
  .num {
    width: 72px;
    height: 36px;
    text-align: right;
    font-variant-numeric: tabular-nums;
  }
  .num.sm {
    width: 58px;
  }
  .u.pre {
    font-weight: 600;
    color: var(--muted);
    margin-right: -2px;
  }
  .fire {
    gap: 4px;
  }
  .act {
    font-size: 0.86rem;
    align-items: center;
    white-space: nowrap;
  }
  .act input {
    margin: 0;
  }
  .rdesc,
  .rerr {
    grid-column: 1 / -1;
    font-size: 0.76rem;
    color: var(--muted);
  }
  .rerr {
    color: var(--down-text-2);
    font-weight: 600;
  }
  .saveerr {
    margin-top: 10px;
  }
  .bar {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-top: 12px;
  }
  .spacer {
    flex: 1;
  }
  @media (max-width: 640px) {
    .rule {
      grid-template-columns: minmax(0, 1fr);
    }
    .rr {
      justify-content: flex-start;
      flex-wrap: wrap;
    }
    .rr .btn {
      margin-left: auto;
    }
    .msel {
      width: 118px;
    }
    .sep {
      display: none;
    }
  }
</style>
