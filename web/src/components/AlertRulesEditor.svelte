<script lang="ts">
  // Sunucu uyarı kuralları: editör düzenler (tamamı tek PUT ile kaydedilir),
  // izleyici yalnızca okur. Tetiklenmiş kurallar vurgulanır.
  import { onMount, untrack } from 'svelte';
  import { guardUnsaved } from '../lib/forms';
  import { api, errorMessage, type AlertRule, type AlertRuleInput, type ServerMetric } from '../lib/api';
  import { METRICS, METRIC_ORDER, UNIT_LABELS, fmtMetric, metricLabel, thresholdless } from '../lib/servers.svelte';
  import { fmtRelative } from '../lib/format';
  import { intlLocale, t } from '../lib/i18n';
  import { clock, toast } from '../lib/ui.svelte';
  import Icon from './Icon.svelte';

  let {
    serverId,
    rules,
    canEdit,
    mounts = [],
    containers = [],
    onsaved,
  }: {
    serverId: number;
    rules: AlertRule[];
    canEdit: boolean;
    mounts?: string[]; // sunucudaki disk bölümleri (disk kuralının bölüm seçimi için)
    containers?: string[]; // sunucudaki konteyner adları (konteyner kuralının ad seçimi için)
    onsaved: (rules: AlertRule[]) => void;
  } = $props();

  interface Row {
    key: number;
    metric: ServerMetric;
    mount: string; // disk: bölüm ("" = en dolu bölüm); container: konteyner adı ("" = herhangi biri)
    threshold: number;
    warn: number; // uyarı eşiği (0 = yok)
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
      warn: r.warn_threshold ?? 0,
      minutes: r.minutes,
      active: r.active,
    }));
  const sortRules = <T extends { metric: ServerMetric; mount?: string }>(list: T[]) =>
    list
      .slice()
      .sort((a, b) => METRIC_ORDER.indexOf(a.metric) - METRIC_ORDER.indexOf(b.metric) || (a.mount ?? '').localeCompare(b.mount ?? ''));
  // Kural metrik + bölümle tanınır (disk bölüm başına ayrı kural olabilir).
  const ruleKey = (r: { metric: ServerMetric; mount?: string }) => `${r.metric}\u0000${r.mount ?? ''}`;
  const mountLabel = (m: string) => m || t('alerts.fullest');

  let rows = $state<Row[]>([]);
  let saving = $state(false);
  let error = $state('');

  // Sunucudan gelen kurallar değişince (kayıt, yenileme) düzenleme sıfırlanır;
  // kullanıcı düzenlerken gelen yenileme onun değişikliklerini ezmez.
  let lastSig = '';
  $effect(() => {
    const sig = JSON.stringify(sortRules(rules).map((r) => [r.metric, r.mount ?? '', r.threshold, r.warn_threshold ?? 0, r.minutes, r.active]));
    untrack(() => {
      if (sig !== lastSig && !dirtyNow()) {
        rows = toRows(rules);
        error = '';
      }
    });
    lastSig = sig;
  });

  const origSig = $derived(JSON.stringify(sortRules(rules).map((r) => [r.metric, r.mount ?? '', +r.threshold, +(r.warn_threshold ?? 0), +r.minutes, r.active])));
  const curSig = $derived(JSON.stringify(sortRules(rows).map((r) => [r.metric, r.mount, +r.threshold, +r.warn, +r.minutes, r.active])));
  const dirty = $derived(curSig !== origSig);
  function dirtyNow() {
    return rows.length > 0 && curSig !== origSig;
  }
  // Kaydedilmemiş kural değişikliği varken sayfadan ayrılırken sorulur.
  onMount(() => guardUnsaved(() => canEdit && !saving && dirty));

  const byKey = $derived(new Map(rules.map((r) => [ruleKey(r), r])));
  const used = $derived(new Set(rows.map(ruleKey)));
  // Disk kuralında seçilebilecek bölümler: "en dolu bölüm", sunucudaki
  // bölümler ve şu an görünmeyen ama kuralı olan bölümler.
  const mountOptions = $derived(['', ...new Set([...mounts, ...rows.filter((r) => r.metric === 'disk').map((r) => r.mount)].filter(Boolean))]);
  const freeMount = $derived(mountOptions.find((m) => !used.has(ruleKey({ metric: 'disk', mount: m }))));
  // Konteyner kuralı: "herhangi biri" ve sunucudaki konteyner adları (kuralı olanlar dahil).
  const containerOptions = $derived(['', ...new Set([...containers, ...rows.filter((r) => r.metric === 'container').map((r) => r.mount)].filter(Boolean))]);
  const freeContainer = $derived(containerOptions.find((m) => !used.has(ruleKey({ metric: 'container', mount: m }))));
  const free = $derived(
    METRIC_ORDER.filter((m) =>
      m === 'disk' ? freeMount !== undefined : m === 'container' ? freeContainer !== undefined : !used.has(ruleKey({ metric: m, mount: '' })),
    ),
  );

  function add() {
    const m = free[0];
    if (!m) return;
    const info = METRICS[m];
    rows = [
      ...rows,
      {
        key: ++seq,
        metric: m,
        mount: m === 'disk' ? (freeMount ?? '') : m === 'container' ? (freeContainer ?? '') : '',
        threshold: info.threshold,
        warn: 0,
        minutes: info.minutes,
        active: true,
      },
    ];
  }

  function remove(key: number) {
    rows = rows.filter((r) => r.key !== key);
  }

  function changeMetric(r: Row, m: ServerMetric) {
    const info = METRICS[m];
    // Birim değişiyorsa (ör. % → °C) önerilen eşiğe geç.
    if (METRICS[r.metric].unit !== info.unit) r.threshold = info.threshold;
    r.metric = m;
    r.mount = m === 'disk' ? (freeMount ?? '') : m === 'container' ? (freeContainer ?? '') : '';
    if (thresholdless(m)) r.threshold = 0;
    r.warn = 0;
    if (m === 'reboot') r.minutes = 1;
  }

  // Hatalı alan (kutuyu kırmızı çizmek için) ve mesajı.
  type RowError = { field: 'minutes' | 'threshold' | 'warn' | 'dup' | 'name'; msg: string } | null;
  function rowError(r: Row): RowError {
    const info = METRICS[r.metric];
    const min = Number(r.minutes);
    if (r.metric !== 'reboot' && (!Number.isInteger(min) || min < 1 || min > 60)) return { field: 'minutes', msg: t('alerts.err.minutes') };
    if (rows.some((o) => o !== r && ruleKey(o) === ruleKey(r))) {
      return {
        field: 'dup',
        msg:
          r.metric === 'disk'
            ? t('alerts.err.dupDisk', { mount: mountLabel(r.mount) })
            : r.metric === 'container'
              ? t('alerts.err.dupContainer', { name: r.mount || t('alerts.anyContainer') })
              : t('alerts.err.dup'),
      };
    }
    if (r.metric === 'container' && r.mount && !/^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$/.test(r.mount)) return { field: 'name', msg: t('alerts.err.containerName') };
    if (thresholdless(r.metric)) return null;
    const v = Number(r.threshold);
    const w = Number(r.warn);
    if (r.warn !== 0 && r.warn !== null && (!Number.isFinite(w) || w < info.min || w >= v)) return { field: 'warn', msg: t('alerts.err.warn') };
    if (!Number.isFinite(v) || v < info.min || v > info.max) {
      const num = (n: number) => n.toLocaleString(intlLocale());
      return {
        field: 'threshold',
        msg:
          info.unit === 'pct'
            ? t('alerts.err.pct')
            : info.unit === 'temp'
              ? t('alerts.err.temp', { min: info.min, max: info.max })
              : t('alerts.err.range', { min: num(info.min), max: num(info.max) }),
      };
    }
    return null;
  }

  const invalid = $derived(rows.some((r) => rowError(r) !== null));
  let showErrors = $state(false);

  async function save() {
    showErrors = true;
    if (invalid) return;
    error = '';
    saving = true;
    try {
      const body: AlertRuleInput[] = rows.map((r) => ({
        metric: r.metric,
        mount: r.metric === 'disk' || r.metric === 'container' ? r.mount : '',
        threshold: thresholdless(r.metric) ? 0 : Number(r.threshold),
        warn_threshold: thresholdless(r.metric) ? 0 : Number(r.warn) || 0,
        minutes: r.metric === 'reboot' ? 1 : Number(r.minutes),
        active: r.active,
      }));
      const saved = await api.putServerAlerts(serverId, body);
      lastSig = '';
      rows = toRows(saved);
      showErrors = false;
      onsaved(saved);
      toast.success(t('alerts.saved'));
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

  function sentence(r: { metric: ServerMetric; threshold: number; minutes: number; warn_threshold?: number; mount?: string }): string {
    if (r.metric === 'offline') return t('alerts.sentOffline', { n: r.minutes });
    if (r.metric === 'reboot') return t('alerts.sentReboot');
    if (r.metric === 'container') return t('alerts.sentContainer', { n: r.minutes, name: r.mount || t('alerts.anyContainer') });
    const s = t('alerts.sentOver', { n: r.minutes, v: fmtMetric(r.metric, r.threshold) });
    return r.warn_threshold ? `${s} · ${t('alerts.sentWarn', { v: fmtMetric(r.metric, r.warn_threshold) })}` : s;
  }
  const levelBadge = (r: AlertRule) => (r.level === 'warning' ? t('servers.levels.warning') : r.level === 'critical' && r.warn_threshold ? t('servers.levels.critical') : '');
</script>

{#if !canEdit}
  {#if rules.length === 0}
    <p class="none">{t('alerts.none')}</p>
  {:else}
    <ul class="ro">
      {#each sortRules(rules) as r (r.id)}
        <li class:firing={r.firing} class:off={!r.active}>
          <span class="rm">{metricLabel(r.metric)}{#if r.metric === 'disk'}<span class="mnt" class:path={!!r.mount}>{mountLabel(r.mount ?? '')}</span>{:else if r.metric === 'container'}<span class="mnt path">{r.mount || t('alerts.anyContainer')}</span>{/if}</span>
          <span class="rs">{sentence(r)}</span>
          {#if r.firing}
            <span class="badge {r.level === 'warning' ? 'pending' : 'down'}">{t('alerts.firing')}{levelBadge(r) ? ` · ${levelBadge(r)}` : ''}{r.fired_at ? ` · ${fmtRelative(r.fired_at, clock.now)}` : ''}</span>
          {:else if !r.active}
            <span class="badge paused">{t('common.off')}</span>
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
      {@const err = showErrors ? rowError(r) : null}
      {@const rname = `${metricLabel(r.metric)}${r.metric === 'disk' && r.mount ? ` (${mountLabel(r.mount)})` : ''}`}
      {@const ctx = t('alerts.aria.ctx', { metric: rname })}
      <div class="rule" class:firing={orig?.firing} class:off={!r.active}>
        <div class="rl">
          <select
            class="input msel"
            aria-label={t('alerts.aria.metric', { ctx })}
            value={r.metric}
            onchange={(e) => changeMetric(r, (e.currentTarget as HTMLSelectElement).value as ServerMetric)}
          >
            {#each METRIC_ORDER as m (m)}
              <option value={m} disabled={m !== r.metric && !free.includes(m)}>{metricLabel(m)}</option>
            {/each}
          </select>
          {#if r.metric === 'disk'}
            <select class="input msel mount" aria-label={t('alerts.aria.mount', { ctx })} bind:value={r.mount}>
              {#each mountOptions as mo (mo)}
                <option value={mo} disabled={mo !== r.mount && used.has(ruleKey({ metric: 'disk', mount: mo }))}>{mountLabel(mo)}</option>
              {/each}
            </select>
          {:else if r.metric === 'container'}
            <input
              class="input msel mount"
              class:invalid={err?.field === 'name'}
              list="ct-names-{serverId}"
              placeholder={t('alerts.anyContainer')}
              aria-label={t('alerts.aria.container', { ctx })}
              bind:value={r.mount}
              autocapitalize="none"
              spellcheck="false"
            />
            <datalist id="ct-names-{serverId}">
              {#each containerOptions.filter(Boolean) as co (co)}<option value={co}></option>{/each}
            </datalist>
          {/if}

          {#if !thresholdless(r.metric)}
            <span class="w" title={t('alerts.geTitle')} aria-hidden="true">≥</span>
            <label class="unitbox" class:wide={info.unit === 'load' || info.unit === 'net'}>
              {#if info.unit === 'pct'}<span class="u pre">%</span>{/if}
              <input
                class="input num"
                class:invalid={err?.field === 'threshold'}
                type="number"
                inputmode="decimal"
                min={info.min}
                max={info.max}
                step={info.step}
                bind:value={r.threshold}
                aria-label={t('alerts.aria.threshold', { ctx, unit: UNIT_LABELS[info.unit] })}
              />
              {#if info.unit !== 'pct'}<span class="u">{UNIT_LABELS[info.unit]}</span>{/if}
            </label>
            <label class="unitbox warnbox" class:wide={info.unit === 'load' || info.unit === 'net'} title={t('alerts.warnTitle')}>
              <span class="u pre wl">{t('alerts.warnShort')}</span>
              {#if info.unit === 'pct'}<span class="u pre">%</span>{/if}
              <input
                class="input num"
                class:invalid={err?.field === 'warn'}
                type="number"
                inputmode="decimal"
                min="0"
                max={info.max}
                step={info.step}
                placeholder="—"
                bind:value={r.warn}
                aria-label={t('alerts.aria.warn', { ctx, unit: UNIT_LABELS[info.unit] })}
              />
              {#if info.unit !== 'pct'}<span class="u">{UNIT_LABELS[info.unit]}</span>{/if}
            </label>
            <span class="w sep">·</span>
          {/if}
          {#if r.metric !== 'reboot'}
            <label class="unitbox">
              <input
                class="input num sm"
                class:invalid={err?.field === 'minutes'}
                type="number"
                inputmode="numeric"
                min="1"
                max="60"
                step="1"
                bind:value={r.minutes}
                aria-label={t('alerts.aria.minutes', { ctx })}
              />
              <span class="u">{r.metric === 'offline' ? t('alerts.unitNoData') : r.metric === 'container' ? t('alerts.unitNotRunning') : t('alerts.unitAvg')}</span>
            </label>
          {:else}
            <span class="rs reboot-note">{t('alerts.sentReboot')}</span>
          {/if}
        </div>
        <div class="rr">
          {#if orig?.firing}
            <span class="badge pending fire" title={t('alerts.firingTitle')}>
              <Icon name="alert" size={11} /> {t('alerts.firing')}{orig.fired_at ? ` · ${fmtRelative(orig.fired_at, clock.now)}` : ''}
            </span>
          {/if}
          <label class="check act"><input type="checkbox" bind:checked={r.active} aria-label={t('alerts.aria.active', { ctx })} /> {t('alerts.active')}</label>
          <button
            type="button"
            class="btn ghost sm icon"
            aria-label={t('alerts.aria.remove', { metric: `${metricLabel(r.metric)}${r.metric === 'disk' ? ` (${mountLabel(r.mount)})` : ''}` })}
            onclick={() => remove(r.key)}
          >
            <Icon name="trash" size={15} />
          </button>
        </div>
        {#if err}<div class="rerr">{err.msg}</div>{:else}<div class="rdesc">
            {r.metric === 'disk' && r.mount ? t('alerts.mountUsage', { mount: r.mount }) : info.desc}
            {#if !thresholdless(r.metric) && Number(r.warn) > 0}· {t('alerts.warnDesc', { v: fmtMetric(r.metric, Number(r.warn)) })}{/if}
          </div>{/if}
      </div>
    {:else}
      <p class="none">{t('alerts.empty')}</p>
    {/each}
  </div>

  {#if error}<div class="alert error saveerr" role="alert">{error}</div>{/if}

  <div class="bar">
    <button type="button" class="btn sm" onclick={add} disabled={free.length === 0}><Icon name="plus" size={14} /> {t('alerts.add')}</button>
    <div class="spacer"></div>
    {#if dirty}
      <button type="button" class="btn sm ghost" onclick={reset} disabled={saving}>{t('common.cancel')}</button>
    {/if}
    <button type="button" class="btn sm primary" onclick={save} disabled={!dirty || saving}>
      {#if saving}<span class="spinner"></span>{/if} {t('common.save')}
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
  .u.wl {
    font-weight: 500;
    font-size: 0.78rem;
    text-transform: lowercase;
    margin-right: 0;
  }
  .warnbox {
    margin-left: 4px;
    padding-left: 8px;
    border-left: 1px dashed var(--border);
  }
  .reboot-note {
    font-style: italic;
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
