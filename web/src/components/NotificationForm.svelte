<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import {
    api,
    errorMessage,
    MASK,
    type NotificationChannel,
    type NotificationInput,
    type NotificationType,
  } from '../lib/api';
  import { confirmDialog } from '../lib/ui.svelte';
  import { changedDestinations, destinationPhrase, guardUnsaved, snapshot } from '../lib/forms';
  import { EMAIL_PORTS, NOTIFY_GROUPS, NOTIFY_LABELS, NOTIFY_SCHEMAS, webhookExample, type Field } from '../lib/notifyTypes';
  import { t, tParts } from '../lib/i18n';
  import Modal from './Modal.svelte';
  import Icon from './Icon.svelte';

  let {
    open = $bindable(false),
    channel,
    onsaved,
    ondeleted,
  }: {
    open?: boolean;
    channel: NotificationChannel | null;
    onsaved: (ch: NotificationChannel, created: boolean) => void;
    ondeleted: (id: number) => void;
  } = $props();

  // svelte-ignore state_referenced_locally
  const orig = channel;

  function defaults(nt: NotificationType): Record<string, string> {
    const v: Record<string, string> = {};
    for (const f of NOTIFY_SCHEMAS[nt].fields) v[f.key] = f.def !== undefined ? String(f.def) : f.kind === 'bool' ? 'false' : '';
    return v;
  }

  function fromConfig(ch: NotificationChannel): Record<string, string> {
    const v = defaults(ch.type);
    for (const f of NOTIFY_SCHEMAS[ch.type].fields) {
      const raw = ch.config?.[f.key];
      if (raw === undefined || raw === null) continue;
      if (f.key === 'thread_id' && raw === 0) v[f.key] = '';
      else v[f.key] = String(raw);
    }
    return v;
  }

  let type = $state<NotificationType>(orig?.type ?? 'whatsapp');
  let name = $state(orig?.name ?? '');
  let values = $state<Record<string, string>>(orig ? fromConfig(orig) : defaults('whatsapp'));
  let isDefault = $state(orig?.is_default ?? true);
  let active = $state(orig?.active ?? true);
  let applyExisting = $state(false);

  // Kaydedilmemiş değişiklik: pencere kapatılırken (Vazgeç, ×, Esc, arka plan) ve
  // sayfadan ayrılırken sorulur.
  const formSnap = () => snapshot({ type, name: name.trim(), values, isDefault, active, applyExisting });
  const baseline = untrack(formSnap);
  const dirty = () => !saving && formSnap() !== baseline;
  async function canClose() {
    if (!dirty()) return true;
    return confirmDialog({
      title: t('common.forms.unsavedTitle'),
      message: t('common.forms.unsavedModalMessage'),
      confirmText: t('common.forms.discard'),
      cancelText: t('common.forms.keepEditing'),
      danger: true,
    });
  }
  onMount(() => guardUnsaved(() => open && dirty()));
  async function cancel() {
    if (await canClose()) open = false;
  }

  let error = $state('');
  let saving = $state(false);
  let testing = $state(false);
  let testResult = $state<{ ok: boolean; msg: string } | null>(null);

  const schema = $derived(NOTIFY_SCHEMAS[type]);

  function changeType(nt: NotificationType) {
    type = nt;
    cleared = [];
    values = orig && orig.type === nt ? fromConfig(orig) : defaults(nt);
    testResult = null;
    error = '';
  }

  function changeSecurity(sec: string) {
    const prev = values.security;
    const p = values.port?.trim();
    if (!p || Number(p) === EMAIL_PORTS[prev]) values.port = String(EMAIL_PORTS[sec] ?? 587);
    values.security = sec;
  }

  function buildConfig(): Record<string, unknown> {
    const cfg: Record<string, unknown> = {};
    for (const f of schema.fields) {
      const raw = values[f.key] ?? '';
      if (f.kind === 'bool') {
        cfg[f.key] = raw === 'true';
      } else if (f.numeric) {
        // Boş bırakılan e-posta portunu 0 gönder: sunucu güvenlik seçimine göre (465/587/25) doldurur.
        const def = f.key === 'port' ? 0 : typeof f.def === 'number' ? f.def : 0;
        cfg[f.key] = raw.trim() === '' ? def : Number(raw);
      } else if (f.kind === 'textarea') {
        cfg[f.key] = raw;
      } else {
        cfg[f.key] = raw.trim();
      }
    }
    return cfg;
  }

  function validate(): string {
    if (!name.trim()) return t('notifications.form.nameRequired');
    for (const f of schema.fields) {
      const v = (values[f.key] ?? '').trim();
      if (f.required && !v) return t('notifyTypes.errors.required', { field: f.label });
      if (v && f.kind === 'url' && !/^https?:\/\/\S+$/i.test(v)) return t('notifyTypes.errors.url', { field: f.label });
      if (v && f.pattern && v !== MASK && !f.pattern.test(v)) return f.patternMsg ?? t('notifyTypes.errors.invalid', { field: f.label });
      if (v && f.numeric && f.kind !== 'select') {
        const n = Number(v);
        if (!Number.isInteger(n)) return t('notifyTypes.errors.integer', { field: f.label });
        if ((f.min !== undefined && n < f.min) || (f.max !== undefined && n > f.max))
          return t('notifyTypes.errors.range', { field: f.label, min: f.min ?? '', max: f.max ?? '' });
      }
    }
    return '';
  }

  async function test() {
    testResult = null;
    const msg = validate();
    if (msg) {
      testResult = { ok: false, msg };
      return;
    }
    testing = true;
    try {
      await api.testNotification({
        ...(orig && orig.type === type ? { id: orig.id } : {}),
        type,
        config: buildConfig(),
      });
      testResult = { ok: true, msg: t('notifications.form.testSent') };
    } catch (e) {
      testResult = { ok: false, msg: errorMessage(e) };
    } finally {
      testing = false;
    }
  }

  async function save() {
    error = validate();
    if (error) return;
    const body: NotificationInput = {
      name: name.trim(),
      type,
      config: buildConfig(),
      is_default: isDefault,
      active,
      apply_existing: applyExisting,
    };
    saving = true;
    try {
      const res = orig ? await api.updateNotification(orig.id, body) : await api.createNotification(body);
      onsaved(res, !orig);
      open = false;
    } catch (e) {
      error = errorMessage(e);
    } finally {
      saving = false;
    }
  }

  async function remove() {
    if (!orig) return;
    const ok = await confirmDialog({
      title: t('notifications.form.deleteTitle'),
      message: t('notifications.form.deleteMessage', { name: orig.name }),
      confirmText: t('common.delete'),
      danger: true,
    });
    if (!ok) return;
    try {
      await api.deleteNotification(orig.id);
      ondeleted(orig.id);
      open = false;
    } catch (e) {
      error = errorMessage(e);
    }
  }

  const isMasked = (f: Field) => values[f.key] === MASK;

  // Hedef (adres, sunucu, port…) değişince sunucu kayıtlı (maskeli) gizli değeri yeni
  // hedefe taşımaz. Maskeli alanlar boşaltılıp yeniden girilmesi istenir; hedef eski
  // hâline dönerse kayıtlı değer geri gelir. E-postada güvenlik seçimi portu da
  // değiştirdiği için STARTTLS↔TLS geçişinde şifre yeniden istenir.
  const destChanged = $derived(orig && orig.type === type ? changedDestinations(buildConfig(), orig.config ?? {}) : []);
  let cleared = $state<string[]>([]);
  $effect(() => {
    const changed = destChanged.length > 0;
    untrack(() => {
      if (changed) {
        const keys = schema.fields.filter((f) => values[f.key] === MASK).map((f) => f.key);
        if (!keys.length) return;
        for (const k of keys) values[k] = '';
        cleared = [...new Set([...cleared, ...keys])];
      } else if (cleared.length) {
        for (const k of cleared) if (values[k] === '') values[k] = MASK;
        cleared = [];
      }
    });
  });
  const rebindMsg = $derived.by(() => {
    if (!cleared.length || !destChanged.length) return '';
    const labels = cleared.map((k) => schema.fields.find((f) => f.key === k)?.label ?? k);
    return t('notifications.form.rebind', { dest: destinationPhrase(destChanged), fields: labels.join(', ') });
  });
</script>

<Modal bind:open title={orig ? t('notifications.form.titleEdit') : t('notifications.form.titleNew')} width={600} {canClose}>
  <form
    class="stack"
    id="nf"
    novalidate
    onsubmit={(e) => {
      e.preventDefault();
      save();
    }}
  >
    <div class="grid-2">
      <div class="field">
        <label for="nt">{t('notifications.form.type')}</label>
        <select id="nt" class="input" value={type} onchange={(e) => changeType(e.currentTarget.value as NotificationType)}>
          {#each NOTIFY_GROUPS as g (g.id)}
            <optgroup label={t(`notifyTypes.groups.${g.id}`)}>
              {#each g.types as nt (nt)}<option value={nt}>{NOTIFY_LABELS[nt]}</option>{/each}
            </optgroup>
          {/each}
        </select>
      </div>
      <div class="field">
        <label for="nn">{t('common.name')}</label>
        <input id="nn" class="input" bind:value={name} maxlength="100" placeholder={t('notifications.form.namePlaceholder', { type: NOTIFY_LABELS[type] })} />
      </div>
    </div>

    {#if schema.help}<div class="alert info small">{schema.help}</div>{/if}

    <div class="grid-2">
      {#each schema.fields as f (type + f.key)}
        {#if f.kind === 'bool'}
          <label class="check wide">
            <input
              type="checkbox"
              checked={values[f.key] === 'true'}
              onchange={(e) => (values[f.key] = e.currentTarget.checked ? 'true' : 'false')}
            />
            <span>{f.label}{#if f.help}<small>{f.help}</small>{/if}</span>
          </label>
        {:else}
        <div class="field" class:wide={f.wide || f.kind === 'textarea'}>
          <label for="f-{f.key}">
            {f.label}
            {#if f.optional}<span class="muted">{t('notifyTypes.form.optional')}</span>{/if}
          </label>
          {#if f.kind === 'select'}
            {#if f.key === 'security'}
              <select id="f-{f.key}" class="input" value={values[f.key]} onchange={(e) => changeSecurity(e.currentTarget.value)}>
                {#each f.options ?? [] as o (o.v)}<option value={o.v}>{o.l}</option>{/each}
              </select>
            {:else}
              <select id="f-{f.key}" class="input" bind:value={values[f.key]}>
                {#each f.options ?? [] as o (o.v)}<option value={o.v}>{o.l}</option>{/each}
              </select>
            {/if}
          {:else if f.kind === 'textarea'}
            <textarea id="f-{f.key}" class="input" rows="3" bind:value={values[f.key]} placeholder={f.placeholder}></textarea>
          {:else if f.kind === 'secret'}
            <input
              id="f-{f.key}"
              class="input"
              type="password"
              autocomplete="new-password"
              bind:value={values[f.key]}
              placeholder={f.placeholder}
            />
          {:else if f.kind === 'number'}
            <input id="f-{f.key}" class="input" type="text" inputmode="numeric" bind:value={values[f.key]} placeholder={f.placeholder} />
          {:else}
            <input
              id="f-{f.key}"
              class="input"
              type="text"
              inputmode={f.kind === 'url' ? 'url' : 'text'}
              autocapitalize="none"
              spellcheck="false"
              bind:value={values[f.key]}
              placeholder={f.placeholder}
            />
          {/if}
          {#if isMasked(f)}
            <span class="help">{t('notifyTypes.form.keptHelp')}</span>
          {:else if f.help}
            <span class="help">{f.help}</span>
          {/if}
        </div>
        {/if}
      {/each}
    </div>

    {#if rebindMsg}
      <div class="alert warning small" role="status">{rebindMsg}</div>
    {/if}

    {#if type === 'webhook'}
      <details class="example">
        <summary><span class="chev"><Icon name="chevron-right" size={15} /></span> {t('notifications.form.exampleSummary')}</summary>
        <p class="help">
          {#each tParts('notifications.form.exampleHelp') as p, i (i)}{#if p.slot}<code>{p.slot}</code>{:else}{p.text}{/if}{/each}
        </p>
        <pre>{webhookExample()}</pre>
      </details>
    {/if}

    <div class="divider"></div>

    <label class="check">
      <input type="checkbox" bind:checked={active} />
      <span>{t('notifications.form.active')}<small>{t('notifications.form.activeHelp')}</small></span>
    </label>
    <label class="check">
      <input type="checkbox" bind:checked={isDefault} />
      <span>{t('notifications.form.isDefault')}</span>
    </label>
    <label class="check">
      <input type="checkbox" bind:checked={applyExisting} />
      <span>{t('notifications.form.applyExisting')}<small>{t('notifications.form.applyExistingHelp')}</small></span>
    </label>

    {#if testResult}
      <div class="alert {testResult.ok ? 'success' : 'error'}" role="status">{testResult.msg}</div>
    {/if}
    {#if error}
      <div class="alert error" role="alert">{error}</div>
    {/if}
  </form>

  {#snippet footer()}
    {#if orig}
      <button type="button" class="btn danger" onclick={remove}><Icon name="trash" size={15} /> {t('common.delete')}</button>
    {/if}
    <button type="button" class="btn" onclick={test} disabled={testing}>
      {#if testing}<span class="spinner"></span>{:else}<Icon name="send" size={15} />{/if}
      {t('notifications.form.test')}
    </button>
    <div class="spacer"></div>
    <button type="button" class="btn" onclick={cancel}>{t('common.cancel')}</button>
    <button type="submit" form="nf" class="btn primary" disabled={saving}>
      {#if saving}<span class="spinner"></span>{/if}
      {t('common.save')}
    </button>
  {/snippet}
</Modal>

<style>
  .wide {
    grid-column: 1 / -1;
  }
  .divider {
    height: 1px;
    background: var(--border);
  }
  .spacer {
    flex: 1;
  }
  .example summary {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    cursor: pointer;
    color: var(--accent-text);
    font-size: 0.88rem;
    list-style: none;
    border-radius: 4px;
  }
  /* Tarayıcının üçgen işareti yerine uygulamadaki ok simgesi. */
  .example summary::-webkit-details-marker {
    display: none;
  }
  .chev {
    display: inline-flex;
    transition: transform 0.15s;
  }
  .example[open] .chev {
    transform: rotate(90deg);
  }
  .example pre {
    background: var(--input);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 10px 12px;
    overflow-x: auto;
    margin: 6px 0 0;
    color: var(--text-2);
  }
  .help code {
    color: var(--text-2);
  }
  @media (max-width: 640px) {
    .spacer {
      display: none;
    }
    :global(dialog footer) > .btn.primary {
      flex: 1 1 100%;
    }
  }
</style>
