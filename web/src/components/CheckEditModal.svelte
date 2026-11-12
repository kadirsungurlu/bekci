<script lang="ts">
  // Denetim ekle / düzenle (yönetici): imzalı zarf + gövde yükleme, aç/kapat,
  // aralık, durum kuralı ve metrik eşik kuralları (PUT /api/servers/{id}/checks/{name}).
  // İmza ajanda doğrulanır; burada yalnız zarftan kimlik ve aralık sınırı okunur.
  import { api, type CheckRuleInput, type ServerAlertRule, type ServerCheck } from '../lib/api';
  import { t } from '../lib/i18n';
  import { toast } from '../lib/ui.svelte';
  import FilePick from './FilePick.svelte';
  import Icon from './Icon.svelte';
  import Modal from './Modal.svelte';

  let {
    open = $bindable(false),
    serverId,
    check = null,
    rules = [],
    onsaved,
  }: {
    open?: boolean;
    serverId: number;
    /** null: yeni eklenti. */
    check?: ServerCheck | null;
    rules?: ServerAlertRule[];
    onsaved?: () => void;
  } = $props();

  type MRule = { metric: string; threshold: string; warn: string; minutes: number; below: boolean; active: boolean };

  let envFile = $state<File | null>(null);
  let bodyFile = $state<File | null>(null);
  let envelope = $state<unknown>(null);
  let envId = $state('');
  let limits = $state<{ lo: number; hi: number } | null>(null);
  let enabled = $state(true);
  let interval = $state(0);
  let mask = $state(2);
  let statusMinutes = $state(5);
  let statusActive = $state(true);
  let mrules = $state<MRule[]>([]);
  let error = $state('');
  let busy = $state(false);

  const name = $derived(check?.name ?? envId);
  const metricNames = $derived((check?.metrics ?? []).map((m) => m.name));

  // Pencere her açılışta kayıtlı değerlerle doldurulur.
  $effect(() => {
    if (!open) return;
    envFile = bodyFile = null;
    envelope = null;
    envId = '';
    limits = null;
    error = '';
    enabled = check?.enabled ?? true;
    interval = check?.interval ?? 0;
    const st = rules.find((r) => r.mount === check?.name);
    mask = st ? st.threshold : 2;
    statusMinutes = st?.minutes ?? 5;
    statusActive = st ? st.active : true;
    mrules = rules
      .filter((r) => check && r.mount.startsWith(check.name + '/'))
      .map((r) => ({
        metric: r.mount.slice(check!.name.length + 1),
        threshold: String(r.threshold),
        warn: r.warn_threshold ? String(r.warn_threshold) : '',
        minutes: r.minutes,
        below: !!r.below,
        active: r.active,
      }));
  });

  // Zarf seçilince kimlik ve aralık sınırı imzalı payload'dan okunur (yalnız gösterim).
  $effect(() => {
    const f = envFile;
    envelope = null;
    envId = '';
    limits = null;
    if (!f) return;
    f.text().then((txt) => {
      try {
        const env = JSON.parse(txt) as { payload?: string };
        const m = JSON.parse(atob(env.payload ?? '')) as { id?: string; interval_min?: number; interval_max?: number };
        envelope = env;
        envId = m.id ?? '';
        if (m.interval_min && m.interval_max) {
          limits = { lo: m.interval_min, hi: m.interval_max };
          if (!interval || interval < m.interval_min || interval > m.interval_max) interval = m.interval_min;
        }
        error = envId ? '' : t('servers.checks.edit.noId');
      } catch {
        error = t('servers.checks.edit.badEnvelope');
      }
    });
  });

  const toggle = (bit: number) => (mask = mask ^ bit);

  function b64(buf: ArrayBuffer): string {
    const bytes = new Uint8Array(buf);
    let s = '';
    for (let i = 0; i < bytes.length; i += 0x8000) s += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
    return btoa(s);
  }

  async function save(e: Event) {
    e.preventDefault();
    error = '';
    if (!name) return void (error = t('servers.checks.edit.noId'));
    if (envelope && !bodyFile) return void (error = t('servers.checks.edit.needBody'));
    const rr: CheckRuleInput[] = [];
    if (mask) rr.push({ metric: '', threshold: mask, warn_threshold: 0, minutes: statusMinutes, below: false, active: statusActive });
    for (const r of mrules) {
      if (!r.metric.trim() || r.threshold.trim() === '') continue;
      rr.push({
        metric: r.metric.trim(),
        threshold: Number(r.threshold),
        warn_threshold: r.warn.trim() === '' ? 0 : Number(r.warn),
        minutes: r.minutes,
        below: r.below,
        active: r.active,
      });
    }
    busy = true;
    try {
      await api.putServerCheck(serverId, name, {
        enabled,
        interval: interval || undefined,
        rules: rr,
        ...(envelope && bodyFile ? { manifest: envelope, body: b64(await bodyFile.arrayBuffer()) } : {}),
      });
      toast.success(t('servers.checks.edit.saved'));
      open = false;
      onsaved?.();
    } catch (err) {
      error = (err as Error).message;
    } finally {
      busy = false;
    }
  }
</script>

<Modal bind:open title={check ? t('servers.checks.edit.title', { name: check.name }) : t('servers.checks.edit.addTitle')} width={560}>
  <form id="chk-edit" class="stack" onsubmit={save} novalidate>
    <fieldset class="stack">
      {#if check}<legend>{t('servers.checks.edit.newVersion')}</legend>{/if}
      <div class="field">
        <label for="chk-env">{t('servers.checks.edit.envelope')}</label>
        <FilePick id="chk-env" accept=".json,application/json" bind:file={envFile} />
        <span class="help">{t('servers.checks.edit.envelopeHint')}{#if envId}{' '}<code>{envId}</code>{/if}</span>
      </div>
      <div class="field">
        <label for="chk-body">{t('servers.checks.edit.body')}</label>
        <FilePick id="chk-body" accept=".sh,text/plain,text/x-sh" bind:file={bodyFile} />
      </div>
    </fieldset>
    <label class="check">
      <input type="checkbox" bind:checked={enabled} />
      <span>{t('servers.checks.edit.enabled')}</span>
    </label>
    <div class="field">
      <label for="chk-iv">{t('servers.checks.edit.interval')}</label>
      <input id="chk-iv" class="input num" type="number" min={limits?.lo ?? 60} max={limits?.hi ?? 86400} bind:value={interval} />
      {#if limits}<span class="help">{t('servers.checks.edit.intervalHint', { lo: limits.lo, hi: limits.hi })}</span>{/if}
    </div>

    <fieldset class="stack">
      <legend>{t('servers.checks.edit.statusRule')}</legend>
      <div class="chips">
        <label class="check"><input type="checkbox" checked={(mask & 2) !== 0} onchange={() => toggle(2)} /> <span>{t('servers.checks.edit.onCritical')}</span></label>
        <label class="check"><input type="checkbox" checked={(mask & 1) !== 0} onchange={() => toggle(1)} /> <span>{t('servers.checks.edit.onWarning')}</span></label>
        <label class="check"><input type="checkbox" checked={(mask & 4) !== 0} onchange={() => toggle(4)} /> <span>{t('servers.checks.edit.onUnknown')}</span></label>
      </div>
      <div class="field">
        <label for="chk-smin">{t('servers.checks.edit.minutes')}</label>
        <input id="chk-smin" class="input num" type="number" min="1" max="1440" bind:value={statusMinutes} />
        <span class="help">{t('servers.checks.edit.statusRuleHint')}</span>
      </div>
    </fieldset>

    <fieldset class="stack">
      <legend>{t('servers.checks.edit.metricRules')}</legend>
      {#each mrules as r, i (i)}
        <div class="mrule">
          <div class="field">
            <label for="chk-m{i}">{t('servers.checks.edit.metric')}</label>
            <input id="chk-m{i}" class="input" list="chk-metrics" maxlength="64" bind:value={r.metric} />
          </div>
          <div class="field">
            <label for="chk-c{i}">{t('servers.checks.edit.crit')}</label>
            <input id="chk-c{i}" class="input num" inputmode="decimal" bind:value={r.threshold} />
          </div>
          <div class="field">
            <label for="chk-w{i}">{t('servers.checks.edit.warn')}</label>
            <input id="chk-w{i}" class="input num" inputmode="decimal" bind:value={r.warn} />
          </div>
          <div class="field">
            <label for="chk-n{i}">{t('servers.checks.edit.minutes')}</label>
            <input id="chk-n{i}" class="input num" type="number" min="1" max="1440" bind:value={r.minutes} />
          </div>
          <label class="check"><input type="checkbox" bind:checked={r.below} /> <span>{t('servers.checks.edit.below')}</span></label>
          <button type="button" class="btn sm icon" aria-label={t('servers.checks.edit.removeRule')} title={t('servers.checks.edit.removeRule')} onclick={() => mrules.splice(i, 1)}><Icon name="trash" size={14} /></button>
        </div>
      {/each}
      <datalist id="chk-metrics">{#each metricNames as m (m)}<option value={m}></option>{/each}</datalist>
      {#if mrules.length < 10}
        <div><button type="button" class="btn sm" onclick={() => mrules.push({ metric: metricNames[0] ?? '', threshold: '', warn: '', minutes: 5, below: false, active: true })}><Icon name="plus" size={14} /> {t('servers.checks.edit.addRule')}</button></div>
      {/if}
    </fieldset>
    {#if error}<div class="alert error" role="alert">{error}</div>{/if}
  </form>
  {#snippet footer()}
    <div class="spacer"></div>
    <button type="button" class="btn" onclick={() => (open = false)}>{t('common.cancel')}</button>
    <button type="submit" form="chk-edit" class="btn primary" disabled={busy || (!check && !envId)}>{t('servers.checks.edit.save')}</button>
  {/snippet}
</Modal>

<style>
  fieldset {
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 10px 12px 12px;
    margin: 0;
  }
  legend {
    padding: 0 4px;
    font-size: 13px;
    font-weight: 600;
  }
  .chips {
    display: flex;
    flex-wrap: wrap;
    gap: 6px 16px;
  }
  .num {
    max-width: 140px;
  }
  .mrule {
    display: grid;
    grid-template-columns: 1.4fr 1fr 1fr 0.8fr auto auto;
    gap: 8px;
    align-items: end;
    padding-bottom: 8px;
    border-bottom: 1px dashed var(--border);
  }
  .mrule .num {
    max-width: none;
  }
  .mrule .check {
    padding-bottom: 8px;
    white-space: nowrap;
  }
  @media (max-width: 560px) {
    .mrule {
      grid-template-columns: 1fr 1fr;
    }
  }
</style>
