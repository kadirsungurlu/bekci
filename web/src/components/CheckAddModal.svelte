<script lang="ts">
  // Yerleşik denetim ekle / parametrelerini düzenle (yönetici, iş 743).
  // Tür listesi GET /api/check-kinds; alanlar türün parametre şemasından oluşur.
  // Kayıt PUT /api/servers/{id}/checks/{name}: eklerken kind gönderilir, düzenlerken gönderilmez.
  // İstemci doğrulaması basit (zorunlu, min/max, ad biçimi); asıl doğrulama sunucuda.
  import { api, ApiError, type CheckKind, type CheckParam, type LocText, type ServerCheck } from '../lib/api';
  import { i18n, t } from '../lib/i18n';
  import { toast } from '../lib/ui.svelte';
  import Modal from './Modal.svelte';

  let {
    open = $bindable(false),
    serverId,
    check = null,
    taken = [],
    onsaved,
  }: {
    open?: boolean;
    serverId: number;
    /** null: yeni denetim; dolu: yerleşik denetimin parametrelerini düzenle. */
    check?: ServerCheck | null;
    /** Sunucudaki mevcut denetim adları (eklerken çakışma denetimi). */
    taken?: string[];
    onsaved?: () => void;
  } = $props();

  const NAME_RE = /^[a-z0-9][a-z0-9-]{0,63}$/;

  let kinds = $state<CheckKind[] | null>(null);
  let kindsError = $state('');
  let kindName = $state('');
  let name = $state('');
  let nameTouched = $state(false);
  let interval = $state<number | null>(null);
  let enabled = $state(true);
  let vals = $state<Record<string, unknown>>({});
  let error = $state('');
  let busy = $state(false);

  const editing = $derived(!!check);
  const kind = $derived(kinds?.find((k) => k.kind === kindName) ?? null);
  const L = (x?: LocText) => (x ? (i18n.locale === 'en' ? x.en || x.tr : x.tr || x.en) : '');

  const slug = (s: string) =>
    s
      .toLowerCase()
      .replace(/[^a-z0-9-]+/g, '-')
      .replace(/^-+/, '')
      .slice(0, 64);

  function defaults(k: CheckKind | null, from?: Record<string, unknown>) {
    const v: Record<string, unknown> = {};
    for (const p of k?.params ?? []) {
      const cur = from?.[p.name];
      if (cur !== undefined && cur !== null) v[p.name] = cur;
      else if (p.default !== undefined) v[p.name] = p.default;
      else v[p.name] = p.type === 'bool' ? false : p.type === 'int' ? null : '';
    }
    return v;
  }

  async function loadKinds() {
    if (kinds) return;
    try {
      kinds = (await api.getCheckKinds()).kinds ?? [];
      kindsError = '';
    } catch (e) {
      kindsError = (e as Error).message;
    }
  }

  // Her açılışta form sıfırlanır (düzenlemede kayıtlı değerlerle).
  $effect(() => {
    if (!open) return;
    error = '';
    busy = false;
    nameTouched = false;
    kindName = check?.kind ?? '';
    name = check?.name ?? '';
    enabled = check?.enabled ?? true;
    interval = check?.interval || null;
    vals = {};
    loadKinds().then(() => {
      if (!open) return;
      const k = kinds?.find((x) => x.kind === kindName) ?? null;
      if (check) {
        vals = defaults(k, check.params);
        if (!interval && k) interval = k.interval.default;
      } else if (!kindName) {
        const first = kinds?.find((x) => !x.locked);
        if (first) pickKind(first.kind);
      }
    });
  });

  function pickKind(k: string) {
    kindName = k;
    const kk = kinds?.find((x) => x.kind === k) ?? null;
    vals = defaults(kk);
    interval = kk?.interval.default ?? null;
    if (!nameTouched) name = slug(k);
  }

  function hint(p: CheckParam): string {
    const own = L(p.help);
    const extra =
      p.type === 'credential'
        ? t('servers.checks.builtin.credentialHint')
        : p.type === 'target'
          ? t('servers.checks.builtin.targetHint')
          : p.type === 'url'
            ? t('servers.checks.builtin.urlHint')
            : p.type === 'socket'
              ? t('servers.checks.builtin.socketHint')
              : '';
    return [own, extra].filter(Boolean).join(' ');
  }

  // Parametre nesnesi: int sayı, bool boolean, diğerleri string; boş opsiyoneller gönderilmez.
  function collect(k: CheckKind): Record<string, string | number | boolean> | string {
    const out: Record<string, string | number | boolean> = {};
    for (const p of k.params) {
      const field = L(p.label) || p.name;
      const v = vals[p.name];
      if (p.type === 'bool') {
        out[p.name] = !!v;
        continue;
      }
      if (p.type === 'int') {
        const empty = v === null || v === undefined || v === '' || Number.isNaN(Number(v));
        if (empty) {
          if (p.required) return t('servers.checks.builtin.required', { field });
          continue;
        }
        const n = Number(v);
        if ((p.min !== undefined && n < p.min) || (p.max !== undefined && n > p.max))
          return t('servers.checks.builtin.range', { field, min: p.min ?? '', max: p.max ?? '' });
        out[p.name] = Math.trunc(n);
        continue;
      }
      const s = String(v ?? '').trim();
      if (!s) {
        if (p.required) return t('servers.checks.builtin.required', { field });
        continue;
      }
      out[p.name] = s;
    }
    return out;
  }

  async function save(e: Event) {
    e.preventDefault();
    error = '';
    const k = kind;
    if (!k) return void (error = t('servers.checks.builtin.required', { field: t('servers.checks.builtin.kind') }));
    if (!editing) {
      if (k.locked) return void (error = t('servers.checks.builtin.proRequired'));
      if (!NAME_RE.test(name)) return void (error = t('servers.checks.builtin.badName'));
      if (taken.includes(name)) return void (error = t('servers.checks.builtin.nameTaken'));
    }
    const iv = Number(interval);
    if (!interval || iv < k.interval.min || iv > k.interval.max)
      return void (error = t('servers.checks.builtin.range', { field: t('servers.checks.builtin.interval'), min: k.interval.min, max: k.interval.max }));
    const params = collect(k);
    if (typeof params === 'string') return void (error = params);
    busy = true;
    try {
      await api.putServerCheck(serverId, name, {
        ...(editing ? {} : { kind: k.kind }),
        params,
        interval: Math.trunc(iv),
        enabled,
      });
      toast.success(t('servers.checks.builtin.saved'));
      open = false;
      onsaved?.();
    } catch (err) {
      const ae = err instanceof ApiError ? err : null;
      if (ae && (ae.status === 402 || ae.code === 'pro_lisans')) error = t('servers.checks.builtin.proRequired');
      else if (ae?.status === 409) error = t('servers.checks.builtin.limitFull');
      else error = (err as Error).message;
    } finally {
      busy = false;
    }
  }
</script>

<Modal bind:open title={check ? t('servers.checks.builtin.editTitle', { name: check.name }) : t('servers.checks.builtin.addTitle')} width={560}>
  {#if kindsError}
    <div class="alert error">{t('servers.checks.builtin.loadKinds')}: {kindsError}</div>
  {:else if !kinds}
    <div class="skeleton" style="height:160px"></div>
  {:else if kinds.length === 0}
    <p class="muted">{t('servers.checks.builtin.noKinds')}</p>
  {:else}
    <form id="chk-add" class="stack" onsubmit={save} novalidate>
      <div class="field">
        <label for="chk-kind">{t('servers.checks.builtin.kind')}</label>
        <select id="chk-kind" class="input" value={kindName} disabled={editing} onchange={(e) => pickKind((e.currentTarget as HTMLSelectElement).value)}>
          {#if !kindName}<option value="" disabled>{t('servers.checks.builtin.choose')}</option>{/if}
          {#each kinds as k (k.kind)}
            <option value={k.kind} disabled={k.locked}>{L(k.label) || k.kind}{k.pro ? ` (${t('servers.checks.builtin.pro')})` : ''}</option>
          {/each}
        </select>
        {#if kind}
          <span class="help">
            {#if kind.pro}<span class="badge accent">{t('servers.checks.builtin.pro')}</span>{/if}
            {L(kind.help)}
            {#if kind.platforms?.length}{' '}{t('servers.checks.builtin.platforms', { p: kind.platforms.join(', ') })}{/if}
          </span>
        {/if}
      </div>

      {#if kind}
        <div class="row2">
          <div class="field">
            <label for="chk-name">{t('servers.checks.builtin.name')}</label>
            <input
              id="chk-name"
              class="input"
              maxlength="64"
              readonly={editing}
              bind:value={name}
              oninput={() => {
                nameTouched = true;
                name = name.toLowerCase();
              }}
            />
          </div>
          <div class="field">
            <label for="chk-biv">{t('servers.checks.builtin.interval')}</label>
            <input id="chk-biv" class="input num" type="number" min={kind.interval.min} max={kind.interval.max} bind:value={interval} />
          </div>
        </div>
        <span class="help">
          {#if !editing}{t('servers.checks.builtin.nameHint')}{' '}{/if}{t('servers.checks.builtin.intervalHint', { lo: kind.interval.min, hi: kind.interval.max })}
        </span>
        <label class="check">
          <input type="checkbox" bind:checked={enabled} />
          <span>{t('servers.checks.builtin.enabled')}</span>
        </label>

        {#if kind.params.length}
          <fieldset class="stack">
            <legend>{t('servers.checks.builtin.params')}</legend>
            {#each kind.params as p (p.name)}
              {@const fid = `chk-p-${p.name}`}
              {@const h = hint(p)}
              {#if p.type === 'bool'}
                <div class="field">
                  <label class="check">
                    <input id={fid} type="checkbox" checked={!!vals[p.name]} onchange={(e) => (vals[p.name] = (e.currentTarget as HTMLInputElement).checked)} />
                    <span>{L(p.label) || p.name}</span>
                  </label>
                  {#if h}<span class="help">{h}</span>{/if}
                </div>
              {:else}
                <div class="field">
                  <label for={fid}>{L(p.label) || p.name}{#if p.required}{' '}<span class="req">*</span>{/if}</label>
                  {#if p.type === 'select'}
                    <select id={fid} class="input" value={String(vals[p.name] ?? '')} onchange={(e) => (vals[p.name] = (e.currentTarget as HTMLSelectElement).value)}>
                      {#if !p.required}<option value="">-</option>{/if}
                      {#each p.options ?? [] as o (o.value)}<option value={o.value}>{L(o.label) || o.value}</option>{/each}
                    </select>
                  {:else if p.type === 'int'}
                    <input
                      id={fid}
                      class="input num"
                      type="number"
                      step="1"
                      min={p.min}
                      max={p.max}
                      value={vals[p.name] ?? ''}
                      oninput={(e) => {
                        const s = (e.currentTarget as HTMLInputElement).value;
                        vals[p.name] = s === '' ? null : Number(s);
                      }}
                    />
                  {:else}
                    <input
                      id={fid}
                      class="input"
                      type={p.type === 'url' ? 'url' : 'text'}
                      autocomplete="off"
                      spellcheck="false"
                      placeholder={p.type === 'target' ? '127.0.0.1:port' : p.type === 'url' ? 'http://127.0.0.1/' : p.type === 'socket' ? '/run/...' : ''}
                      value={String(vals[p.name] ?? '')}
                      oninput={(e) => (vals[p.name] = (e.currentTarget as HTMLInputElement).value)}
                    />
                  {/if}
                  {#if h}<span class="help">{h}</span>{/if}
                </div>
              {/if}
            {/each}
          </fieldset>
        {/if}
      {/if}
      {#if error}<div class="alert error" role="alert">{error}</div>{/if}
    </form>
  {/if}
  {#snippet footer()}
    <div class="spacer"></div>
    <button type="button" class="btn" onclick={() => (open = false)}>{t('common.cancel')}</button>
    <button type="submit" form="chk-add" class="btn primary" disabled={busy || !kind || (!editing && kind.locked)}>{t('servers.checks.builtin.save')}</button>
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
  .row2 {
    display: grid;
    grid-template-columns: 1fr auto;
    gap: 12px;
  }
  .num {
    max-width: 140px;
  }
  .req {
    color: var(--down);
  }
  .help .badge {
    margin-right: 4px;
  }
  @media (max-width: 560px) {
    .row2 {
      grid-template-columns: 1fr;
    }
  }
</style>
