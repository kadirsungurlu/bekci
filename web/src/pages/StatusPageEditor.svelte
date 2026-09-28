<script lang="ts">
  import { onMount, tick } from 'svelte';
  import { api, ApiError, errorMessage, type BarRange, type PageInput, type StatusPage } from '../lib/api';
  import { live } from '../lib/live.svelte';
  import { navigate } from '../lib/router.svelte';
  import { confirmDialog, toast } from '../lib/ui.svelte';
  import { monitorKind } from '../lib/format';
  import { session } from '../lib/session.svelte';
  import { guardUnsaved, markInvalid, snapshot } from '../lib/forms';
  import Modal from '../components/Modal.svelte';
  import MonitorPicker from '../components/MonitorPicker.svelte';
  import StatusIcon from '../components/StatusIcon.svelte';
  import Icon from '../components/Icon.svelte';
  import CopyButton from '../components/CopyButton.svelte';
  import Announcements from './Announcements.svelte';
  import { LOCALES, t, type Locale } from '../lib/i18n';

  let { id }: { id?: number } = $props();
  // svelte-ignore state_referenced_locally
  const isEdit = id !== undefined;

  interface EdMonitor {
    id: number;
    name: string;
  }
  interface EdSection {
    key: number;
    title: string;
    monitors: EdMonitor[];
  }

  let loading = $state(isEdit);
  let loadError = $state('');
  let saving = $state(false);
  let error = $state('');
  let errorEl: HTMLDivElement | undefined = $state();
  let page = $state<StatusPage | null>(null);

  let title = $state('');
  let slug = $state('');
  let slugTouched = $state(isEdit);
  let description = $state('');
  let footer = $state('');
  let customDomain = $state('');
  let showTargets = $state(false);
  let showIncidents = $state(true);
  let collapsible = $state(false);
  let barRange = $state<BarRange>('recent');
  let pageLang = $state<Locale>('tr');
  let published = $state(true);
  let pwMode = $state<'keep' | 'set' | 'remove'>('keep');
  let password = $state('');

  let seq = 0;
  let sections = $state<EdSection[]>([{ key: ++seq, title: t('pages.editor.defaultSection'), monitors: [] }]);

  const SLUG_RE = /^[a-z0-9](?:[a-z0-9-]{0,48}[a-z0-9])?$/;
  const TR: Record<string, string> = { ç: 'c', ğ: 'g', ı: 'i', İ: 'i', ö: 'o', ş: 's', ü: 'u' };

  function slugify(s: string): string {
    return s
      .toLocaleLowerCase('tr')
      .replace(/[çğıİöşü]/g, (c) => TR[c] ?? c)
      .normalize('NFKD')
      .replace(/[^a-z0-9]+/g, '-')
      .replace(/^-+|-+$/g, '')
      .slice(0, 50)
      .replace(/-+$/g, '');
  }

  function onTitle() {
    if (!slugTouched) slug = slugify(title);
  }

  function fill(p: StatusPage) {
    page = p;
    title = p.title;
    slug = p.slug;
    description = p.description;
    footer = p.footer;
    customDomain = p.custom_domain;
    showTargets = p.show_targets;
    showIncidents = p.show_incidents ?? true;
    collapsible = p.collapsible ?? false;
    barRange = p.bar_range ?? 'recent';
    pageLang = p.lang ?? 'tr';
    published = p.published;
    pwMode = 'keep';
    password = '';
    sections = p.sections.map((s) => ({ key: ++seq, title: s.title, monitors: s.monitors.map((m) => ({ ...m })) }));
    if (sections.length === 0) sections = [{ key: ++seq, title: '', monitors: [] }];
  }

  onMount(async () => {
    if (isEdit) {
      try {
        fill(await api.page(id!));
      } catch (e) {
        loadError = e instanceof ApiError && e.status === 404 ? t('pages.editor.notFound') : errorMessage(e);
      } finally {
        loading = false;
      }
    }
    await tick();
    resetBaseline();
  });

  // Kaydedilmemiş değişiklikler (logo ve duyurular anında kaydedildiği için hariç).
  let baseline = '';
  let saved = false;
  const formState = () => ({
    title: title.trim(),
    slug: slug.trim().toLowerCase(),
    description: description.trim(),
    footer: footer.trim(),
    customDomain: customDomain.trim().toLowerCase(),
    showTargets,
    showIncidents,
    collapsible,
    barRange,
    pageLang,
    published,
    pwMode,
    password,
    sections: sections.map((s) => [s.title.trim(), s.monitors.map((m) => [m.id, m.name.trim()])]),
  });
  const resetBaseline = () => (baseline = snapshot(formState()));
  onMount(() => guardUnsaved(() => !saved && !!baseline && !loading && !loadError && snapshot(formState()) !== baseline));

  // Gruplar ve monitörler -----------------------------------------------------------------
  const onPage = $derived(new Set(sections.flatMap((s) => s.monitors.map((m) => m.id))));
  const total = $derived(onPage.size);

  function addSection() {
    sections = [...sections, { key: ++seq, title: '', monitors: [] }];
    tick().then(() => document.getElementById(`sec-${sections[sections.length - 1].key}`)?.focus());
  }

  async function removeSection(si: number) {
    const s = sections[si];
    if (s.monitors.length) {
      const ok = await confirmDialog({
        title: t('pages.editor.removeGroupTitle'),
        message: t('pages.editor.removeGroupMsg', { name: s.title || t('pages.editor.unnamedGroup'), count: s.monitors.length }),
        confirmText: t('common.remove'),
        danger: true,
      });
      if (!ok) return;
    }
    sections = sections.filter((_, i) => i !== si);
  }

  async function moveSection(si: number, dir: -1 | 1) {
    const to = si + dir;
    if (to < 0 || to >= sections.length) return;
    const list = sections.slice();
    [list[si], list[to]] = [list[to], list[si]];
    sections = list;
    const key = list[to].key;
    await tick();
    focusBtn(`[data-skey="${key}"] .s-${dir < 0 ? 'up' : 'down'}`, `[data-skey="${key}"] .s-${dir < 0 ? 'down' : 'up'}`);
  }

  /** Monitörü bir sıra yukarı/aşağı taşır; grubun başında/sonundaysa komşu gruba geçer. */
  async function moveMonitor(si: number, mi: number, dir: -1 | 1) {
    const list = sections.map((s) => ({ ...s, monitors: s.monitors.slice() }));
    const mons = list[si].monitors;
    const m = mons[mi];
    const to = mi + dir;
    if (to >= 0 && to < mons.length) {
      [mons[mi], mons[to]] = [mons[to], mons[mi]];
    } else if (dir < 0 && si > 0) {
      mons.splice(mi, 1);
      list[si - 1].monitors.push(m);
    } else if (dir > 0 && si < list.length - 1) {
      mons.splice(mi, 1);
      list[si + 1].monitors.unshift(m);
    } else return;
    sections = list;
    await tick();
    focusBtn(`[data-mkey="${m.id}"] .m-${dir < 0 ? 'up' : 'down'}`, `[data-mkey="${m.id}"] .m-${dir < 0 ? 'down' : 'up'}`);
  }

  function focusBtn(sel: string, fallback: string) {
    const b = document.querySelector<HTMLButtonElement>(sel);
    if (b && !b.disabled) b.focus();
    else document.querySelector<HTMLButtonElement>(fallback)?.focus();
  }

  function removeMonitor(si: number, mi: number) {
    sections = sections.map((s, i) => (i === si ? { ...s, monitors: s.monitors.filter((_, j) => j !== mi) } : s));
  }

  const isFirstMon = (si: number, mi: number) => si === 0 && mi === 0;
  const isLastMon = (si: number, mi: number) => si === sections.length - 1 && mi === sections[si].monitors.length - 1;

  // Monitör ekleme penceresi
  let addOpen = $state(false);
  let addTo = $state(0);
  let addSel = $state<number[]>([]);

  function openAdd(si: number) {
    addTo = si;
    addSel = [];
    addOpen = true;
  }

  function confirmAdd() {
    const add = addSel.filter((mid) => !onPage.has(mid)).map((mid) => ({ id: mid, name: '' }));
    sections = sections.map((s, i) => (i === addTo ? { ...s, monitors: [...s.monitors, ...add] } : s));
    addOpen = false;
  }

  // Kaydet ------------------------------------------------------------------------------
  // Sunucu şifreyi en az 4 karakter, en fazla 72 bayt (bcrypt sınırı) kabul eder.
  const utf8Len = (v: string) => new TextEncoder().encode(v).length;

  function validate(): { msg: string; field?: string } | null {
    if (!title.trim()) return { msg: t('pages.editor.errTitle'), field: 'sp-title' };
    // Kısa ad sunucuda küçük harfe çevrilir; "Acme" de geçerli sayılır.
    if (!SLUG_RE.test(slug.trim().toLowerCase()))
      return { msg: t('pages.editor.errSlug'), field: 'sp-slug' };
    if (pwMode === 'set' && ([...password].length < 4 || utf8Len(password) > 72))
      return {
        msg: t('pages.editor.errPassword'),
        field: 'sp-pw',
      };
    if (customDomain.trim() && /[/:\s]/.test(customDomain.trim()))
      return { msg: t('pages.editor.errDomain'), field: 'sp-dom' };
    if (total > 200) return { msg: t('pages.editor.errTooMany') };
    return null;
  }

  async function showError(msg: string, field?: string) {
    error = msg;
    await tick();
    markInvalid(field, 'sp-error');
    errorEl?.scrollIntoView({ behavior: 'smooth', block: 'center' });
  }

  async function submit(e: SubmitEvent) {
    e.preventDefault();
    error = '';
    markInvalid(null, 'sp-error');
    const v = validate();
    if (v) return showError(v.msg, v.field);
    slug = slug.trim().toLowerCase();
    const body: PageInput = {
      slug: slug.trim(),
      title: title.trim(),
      description: description.trim(),
      footer: footer.trim(),
      sections: sections
        .filter((s) => s.title.trim() || s.monitors.length)
        .map((s) => ({ title: s.title.trim(), monitors: s.monitors.map((m) => ({ id: m.id, name: m.name.trim() })) })),
      custom_domain: customDomain.trim().toLowerCase(),
      show_targets: showTargets,
      show_incidents: showIncidents,
      collapsible,
      bar_range: barRange,
      lang: pageLang,
      published,
    };
    if (pwMode === 'set') body.password = password;
    else if (pwMode === 'remove') body.password = '';
    saving = true;
    try {
      const res = isEdit ? await api.updatePage(id!, body) : await api.createPage(body);
      if (isEdit) {
        fill(res);
        resetBaseline();
        toast.success(t('pages.editor.saved'));
      } else {
        saved = true;
        toast.success(t('pages.editor.created', { name: res.title }));
        navigate(`/status-pages/${res.id}`, true);
      }
    } catch (err) {
      showError(errorMessage(err));
    } finally {
      saving = false;
    }
  }

  async function remove() {
    if (!page) return;
    const ok = await confirmDialog({
      title: t('pages.deleteTitle'),
      message: t('pages.editor.deleteMsg', { name: page.title }),
      confirmText: t('common.delete'),
      danger: true,
    });
    if (!ok) return;
    try {
      await api.deletePage(page.id);
      saved = true;
      toast.success(t('pages.editor.deleted'));
      navigate('/status-pages');
    } catch (e) {
      toast.error(errorMessage(e));
    }
  }

  // Logo --------------------------------------------------------------------------------
  let logoBusy = $state(false);
  let logoError = $state('');
  let fileInput: HTMLInputElement | undefined = $state();

  async function onLogo(e: Event) {
    const input = e.currentTarget as HTMLInputElement;
    const f = input.files?.[0];
    input.value = '';
    if (!f || !page) return;
    logoError = '';
    if (!['image/png', 'image/jpeg', 'image/webp'].includes(f.type)) return (logoError = t('pages.editor.logoType'));
    if (f.size > 512 * 1024) return (logoError = t('pages.editor.logoSize'));
    logoBusy = true;
    try {
      page = await api.uploadLogo(page.id, f);
      toast.success(t('pages.editor.logoUploaded'));
    } catch (err) {
      logoError = errorMessage(err);
    } finally {
      logoBusy = false;
    }
  }

  async function removeLogo() {
    if (!page) return;
    logoBusy = true;
    try {
      page = await api.deleteLogo(page.id);
      toast.success(t('pages.editor.logoRemoved'));
    } catch (err) {
      logoError = errorMessage(err);
    } finally {
      logoBusy = false;
    }
  }

  const publicUrl = $derived(page ? `${location.origin}/durum/${page.slug}` : '');
  const host = location.host;

</script>

<a class="back" href="#/status-pages"><Icon name="chevron-left" size={16} /> {t('pages.editor.back')}</a>
<div class="page-head">
  <h1>{isEdit ? t('pages.editor.editTitle') : t('pages.editor.newTitle')}<span class="dot">.</span></h1>
  {#if page}
    <div class="row head-acts">
      <a class="btn" href="#/status-pages/{page.id}/preview"><Icon name="eye" size={15} /> {t('pages.preview')}</a>
      {#if page.published}
        <a class="btn" href={publicUrl} target="_blank" rel="noopener noreferrer"><Icon name="external" size={15} /> {t('pages.editor.openPage')}</a>
      {/if}
    </div>
  {/if}
</div>

{#if loading}
  <div class="skeleton" style="height:480px;max-width:900px"></div>
{:else if loadError}
  <div class="card empty">
    <h3>{t('pages.editor.loadFailed')}</h3>
    <p>{loadError}</p>
    <a class="btn primary" href="#/status-pages">{t('pages.editor.backToList')}</a>
  </div>
{:else}
  <form class="form" onsubmit={submit} novalidate>
    {#if page}
      <div class="pub-bar card">
        <span class="badge {page.published ? 'up' : 'paused'}">{page.published ? t('pages.published') : t('pages.draft')}</span>
        <code class="pub-url">{publicUrl}</code>
        <CopyButton text={publicUrl} />
      </div>
    {/if}

    <section class="card stack">
      <h2 class="card-title">{t('pages.editor.general')}</h2>
      <div class="field">
        <label for="sp-title">{t('pages.editor.title')}</label>
        <input id="sp-title" class="input" maxlength="100" bind:value={title} oninput={onTitle} placeholder={t('pages.editor.titlePlaceholder')} />
      </div>
      <div class="field">
        <label for="sp-slug">{t('pages.editor.slug')}</label>
        <div class="prefixed">
          <span class="prefix">/durum/</span>
          <input
            id="sp-slug"
            class="input"
            maxlength="50"
            bind:value={slug}
            oninput={() => (slugTouched = true)}
            onblur={() => (slug = slug.trim().toLowerCase())}
            autocapitalize="none"
            spellcheck="false"
            placeholder="acme"
          />
        </div>
        <span class="help">{t('pages.editor.slugHelp', { url: `${host}/durum/<${t('pages.editor.slugToken')}>` })}</span>
      </div>
      <div class="field">
        <label for="sp-desc">{t('pages.editor.description')} <span class="muted">{t('pages.editor.optional')}</span></label>
        <textarea id="sp-desc" class="input plain" rows="2" maxlength="1000" bind:value={description} placeholder={t('pages.editor.descPlaceholder')}></textarea>
      </div>
      <div class="field">
        <label for="sp-foot">{t('pages.editor.footer')} <span class="muted">{t('pages.editor.optional')}</span></label>
        <input id="sp-foot" class="input" maxlength="500" bind:value={footer} placeholder={t('pages.editor.footerPlaceholder')} />
      </div>
    </section>

    <section class="card stack">
      <div class="sec-head">
        <h2 class="card-title">{t('pages.editor.groups')}</h2>
        <span class="muted small">{t('pages.monitorCount', { count: total })}</span>
      </div>
      <p class="help nomargin">{t('pages.editor.groupsHelp')}</p>

      {#each sections as s, si (s.key)}
        <div class="group" data-skey={s.key}>
          <div class="g-head">
            <input
              id="sec-{s.key}"
              class="input g-title"
              maxlength="100"
              bind:value={s.title}
              placeholder={t('pages.editor.groupPlaceholder')}
              aria-label={t('pages.editor.groupNameAria', { n: si + 1 })}
            />
            <div class="order">
              <button
                type="button"
                class="btn ghost icon sm s-up"
                aria-label={t('pages.editor.groupUp', { name: s.title.trim() || t('pages.editor.groupN', { n: si + 1 }) })}
                disabled={si === 0}
                onclick={() => moveSection(si, -1)}
              >
                <Icon name="arrow-up" size={15} />
              </button>
              <button
                type="button"
                class="btn ghost icon sm s-down"
                aria-label={t('pages.editor.groupDown', { name: s.title.trim() || t('pages.editor.groupN', { n: si + 1 }) })}
                disabled={si === sections.length - 1}
                onclick={() => moveSection(si, 1)}
              >
                <Icon name="arrow-down" size={15} />
              </button>
              <button type="button" class="btn ghost icon sm del" aria-label={t('pages.editor.groupRemove', { name: s.title.trim() || t('pages.editor.groupN', { n: si + 1 }) })} onclick={() => removeSection(si)}>
                <Icon name="trash" size={15} />
              </button>
            </div>
          </div>

          {#if s.monitors.length === 0}
            <div class="g-empty muted small">{t('pages.editor.groupEmpty')}</div>
          {:else}
            <ol class="mons">
              {#each s.monitors as m, mi (m.id)}
                {@const mon = live.byId(m.id)}
                <li class="mon" data-mkey={m.id}>
                  {#if mon}<StatusIcon kind={monitorKind(mon)} size={20} />{:else}<span class="ph"></span>{/if}
                  <div class="m-names">
                    <span class="m-orig" title={mon?.target}>{mon?.name ?? `#${m.id}`}</span>
                    <input
                      class="input m-name"
                      maxlength="100"
                      bind:value={m.name}
                      placeholder={mon?.name ?? t('pages.editor.displayName')}
                      aria-label={t('pages.editor.displayNameAria', { name: mon?.name ?? t('pages.editor.monitor') })}
                    />
                  </div>
                  <div class="order">
                    <button
                      type="button"
                      class="btn ghost icon sm m-up"
                      aria-label={t('pages.editor.monUp', { name: mon?.name ?? `#${m.id}` })}
                      disabled={isFirstMon(si, mi)}
                      onclick={() => moveMonitor(si, mi, -1)}
                    >
                      <Icon name="arrow-up" size={15} />
                    </button>
                    <button
                      type="button"
                      class="btn ghost icon sm m-down"
                      aria-label={t('pages.editor.monDown', { name: mon?.name ?? `#${m.id}` })}
                      disabled={isLastMon(si, mi)}
                      onclick={() => moveMonitor(si, mi, 1)}
                    >
                      <Icon name="arrow-down" size={15} />
                    </button>
                    <button type="button" class="btn ghost icon sm del" aria-label={t('pages.editor.monRemove', { name: mon?.name ?? `#${m.id}` })} onclick={() => removeMonitor(si, mi)}>
                      <Icon name="x" size={15} />
                    </button>
                  </div>
                </li>
              {/each}
            </ol>
          {/if}
          <button type="button" class="btn sm add" onclick={() => openAdd(si)}><Icon name="plus" size={14} /> {t('pages.editor.addMonitor')}</button>
        </div>
      {/each}
      <button type="button" class="btn add-sec" onclick={addSection} disabled={sections.length >= 20}>
        <Icon name="layers" size={15} /> {t('pages.editor.addGroup')}
      </button>
    </section>

    <section class="card stack">
      <h2 class="card-title">{t('pages.editor.appearance')}</h2>
      <div class="logo-row">
        <div class="logo-box">
          {#if page?.has_logo}
            <img src="/api/status-pages/{page.id}/logo?v={page.updated_at}" alt={t('pages.editor.logoAlt')} />
          {:else}
            <Icon name="image" size={24} />
          {/if}
        </div>
        <div class="logo-ctl">
          <div class="label">{t('pages.editor.logo')}</div>
          {#if page}
            <div class="row">
              <input bind:this={fileInput} type="file" accept="image/png,image/jpeg,image/webp" class="hidden-file" onchange={onLogo} tabindex="-1" aria-hidden="true" />
              <button type="button" class="btn sm" onclick={() => fileInput?.click()} disabled={logoBusy}>
                {#if logoBusy}<span class="spinner"></span>{:else}<Icon name="upload" size={14} />{/if}
                {page.has_logo ? t('pages.editor.change') : t('pages.editor.uploadLogo')}
              </button>
              {#if page.has_logo}
                <button type="button" class="btn sm danger" onclick={removeLogo} disabled={logoBusy}>{t('common.remove')}</button>
              {/if}
            </div>
            <span class="help">{t('pages.editor.logoHelp')}</span>
          {:else}
            <span class="help">{t('pages.editor.logoAfterCreate')}</span>
          {/if}
          {#if logoError}<div class="alert error small">{logoError}</div>{/if}
        </div>
      </div>
      <div class="field">
        <label for="bar-range">{t('pages.editor.bars')}</label>
        <select id="bar-range" class="input" bind:value={barRange}>
          <option value="recent">{t('pages.editor.barsRecent')}</option>
          <option value="24h">{t('pages.editor.bars24h')}</option>
          <option value="90d">{t('pages.editor.bars90d')}</option>
        </select>
        <span class="help">{t('pages.editor.barsHelp')}</span>
      </div>
      <div class="field">
        <label for="page-lang">{t('pages.lang.label')}</label>
        <select id="page-lang" class="input" bind:value={pageLang}>
          {#each LOCALES as l (l)}
            <option value={l} lang={l}>{t(`common.languages.${l}`)}</option>
          {/each}
        </select>
        <span class="help">{t('pages.lang.help')}</span>
      </div>
      <label class="check">
        <input type="checkbox" bind:checked={showTargets} />
        <span>
          {t('pages.editor.showTargets')}
          <small>{t('pages.editor.showTargetsHelp')}</small>
        </span>
      </label>
      <label class="check">
        <input type="checkbox" bind:checked={showIncidents} />
        <span>
          {t('pages.editor.showIncidents')}
          <small>{t('pages.editor.showIncidentsHelp')}</small>
        </span>
      </label>
      <label class="check">
        <input type="checkbox" bind:checked={collapsible} />
        <span>
          {t('pages.editor.collapsible')}
          <small>{t('pages.editor.collapsibleHelp')}</small>
        </span>
      </label>
    </section>

    <section class="card stack">
      <h2 class="card-title">{t('pages.editor.access')}</h2>
      <label class="check">
        <input type="checkbox" bind:checked={published} />
        <span>{t('pages.published')}<small>{t('pages.editor.publishedHelp')}</small></span>
      </label>

      <div class="field">
        <span class="label" id="pw-l">{t('pages.editor.password')}</span>
        {#if page?.has_password && pwMode === 'keep'}
          <div class="pw-state">
            <span class="badge"><Icon name="lock" size={11} /> {t('pages.editor.passwordProtected')}</span>
            <button type="button" class="btn sm" onclick={() => ((pwMode = 'set'), (password = ''))}>{t('pages.editor.changePassword')}</button>
            <button type="button" class="btn sm danger" onclick={() => (pwMode = 'remove')}>{t('pages.editor.removePassword')}</button>
          </div>
        {:else if pwMode === 'remove'}
          <div class="pw-state">
            <span class="muted small">{t('pages.editor.removePasswordNote')}</span>
            <button type="button" class="linkbtn small" onclick={() => (pwMode = 'keep')}>{t('common.cancel')}</button>
          </div>
        {:else}
          <input
            id="sp-pw"
            class="input pw-in"
            type="password"
            autocomplete="new-password"
            aria-labelledby="pw-l"
            bind:value={password}
            oninput={() => (pwMode = password ? 'set' : page?.has_password ? 'set' : 'keep')}
            placeholder={page?.has_password ? t('pages.editor.newPassword') : t('pages.editor.emptyPublic')}
          />
          <span class="help">
            {t('pages.editor.passwordHelp')}
            {#if page?.has_password}<button type="button" class="linkbtn" onclick={() => ((pwMode = 'keep'), (password = ''))}>{t('pages.editor.keepPassword')}</button>{/if}
          </span>
        {/if}
      </div>

      <div class="field">
        <label for="sp-dom">{t('pages.editor.customDomain')} <span class="muted">{t('pages.editor.optional')}</span></label>
        <input
          id="sp-dom"
          class="input dom"
          bind:value={customDomain}
          placeholder={session.isAdmin ? 'durum.ornek.com' : ''}
          autocapitalize="none"
          spellcheck="false"
          disabled={!session.isAdmin}
          aria-describedby="sp-dom-help"
        />
        <span class="help" id="sp-dom-help">
          {#if session.isAdmin}
            {t('pages.editor.domainHelpAdmin')}
          {:else}
            {t('pages.editor.domainHelpOther')}
          {/if}
        </span>
      </div>
    </section>

    {#if error}
      <div class="alert error" role="alert" id="sp-error" bind:this={errorEl}>{error}</div>
    {/if}

    <div class="actions">
      {#if isEdit}
        <button type="button" class="btn danger" onclick={remove}><Icon name="trash" size={15} /> {t('common.delete')}</button>
        <div class="spacer"></div>
      {/if}
      <a class="btn" href="#/status-pages">{t('common.cancel')}</a>
      <button class="btn primary" type="submit" disabled={saving}>
        {#if saving}<span class="spinner"></span>{/if}
        {isEdit ? t('common.save') : t('pages.editor.createPage')}
      </button>
    </div>
  </form>

  {#if page}
    <div class="ann">
      <Announcements pageId={page.id} />
    </div>
  {/if}
{/if}

<Modal bind:open={addOpen} title={t('pages.editor.addTitle')} width={560}>
  <p class="help nomargin sp">
    {t('pages.editor.addHelp', { name: sections[addTo]?.title || t('pages.editor.unnamedGroup') })}
  </p>
  <MonitorPicker bind:selected={addSel} exclude={[...onPage]} label={t('pages.editor.addPicker')} id="add-mp" />
  {#snippet footer()}
    <div class="spacer"></div>
    <button type="button" class="btn" onclick={() => (addOpen = false)}>{t('common.cancel')}</button>
    <button type="button" class="btn primary" disabled={addSel.length === 0} onclick={confirmAdd}>
      {addSel.length ? t('pages.editor.addN', { count: addSel.length }) : t('common.add')}
    </button>
  {/snippet}
</Modal>

<style>
  .form {
    max-width: 900px;
    display: flex;
    flex-direction: column;
    gap: 16px;
  }
  .head-acts {
    gap: 8px;
  }
  .card-title {
    margin: 0;
  }
  .nomargin {
    margin: 0;
  }
  .sp {
    margin-bottom: 12px;
  }
  textarea.plain {
    font-family: var(--font);
    font-size: 0.92rem;
  }
  .pub-bar {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 10px 12px;
    flex-wrap: wrap;
  }
  .pub-url {
    flex: 1 1 220px;
    min-width: 0;
    color: var(--text-2);
    word-break: break-all;
  }
  .prefixed {
    display: flex;
    align-items: stretch;
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-sm);
    background: var(--input);
    overflow: hidden;
  }
  .prefixed:focus-within {
    border-color: var(--accent);
    box-shadow: 0 0 0 3px var(--accent-soft);
  }
  .prefix {
    display: inline-flex;
    align-items: center;
    padding: 0 4px 0 12px;
    color: var(--muted);
    font-size: 0.88rem;
    white-space: nowrap;
  }
  .prefixed .input {
    border: none;
    box-shadow: none;
    padding-left: 2px;
    background: transparent;
  }
  .sec-head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 10px;
  }
  .group {
    border: 1px solid var(--border-strong);
    border-radius: 10px;
    padding: 12px;
    display: flex;
    flex-direction: column;
    gap: 10px;
    background: var(--bg-elev);
  }
  .g-head {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .g-title {
    flex: 1;
    font-weight: 700;
  }
  .order {
    display: flex;
    gap: 2px;
    flex-shrink: 0;
  }
  .order .del {
    color: var(--muted);
  }
  .g-empty {
    padding: 6px 2px;
  }
  .mons {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .mon {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 6px 6px 6px 10px;
    border: 1px solid var(--border);
    border-radius: 8px;
    background: var(--card);
  }
  .ph {
    width: 20px;
    height: 20px;
    border-radius: 50%;
    background: var(--empty-bar);
    flex-shrink: 0;
  }
  .m-names {
    flex: 1;
    min-width: 0;
    display: grid;
    grid-template-columns: minmax(0, 0.9fr) minmax(0, 1.1fr);
    gap: 10px;
    align-items: center;
  }
  .m-orig {
    font-weight: 600;
    font-size: 0.9rem;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .m-name {
    height: 34px;
    font-size: 0.88rem;
  }
  .add {
    align-self: flex-start;
  }
  .add-sec {
    align-self: flex-start;
  }
  .logo-row {
    display: flex;
    gap: 16px;
    align-items: flex-start;
  }
  .logo-box {
    width: 120px;
    height: 64px;
    border-radius: 10px;
    border: 1px dashed var(--border-strong);
    display: flex;
    align-items: center;
    justify-content: center;
    color: var(--muted);
    background: var(--input);
    flex-shrink: 0;
    overflow: hidden;
    padding: 6px;
  }
  .logo-box img {
    max-width: 100%;
    max-height: 100%;
    object-fit: contain;
  }
  .logo-ctl {
    display: flex;
    flex-direction: column;
    gap: 8px;
    min-width: 0;
  }
  .hidden-file {
    display: none;
  }
  .pw-state {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }
  .badge :global(svg) {
    margin-right: 3px;
  }
  .pw-in,
  .dom {
    max-width: 420px;
  }
  .actions {
    display: flex;
    justify-content: flex-end;
    gap: 10px;
    flex-wrap: wrap;
  }
  .spacer {
    flex: 1;
  }
  .ann {
    max-width: 900px;
    margin-top: 24px;
  }
  @media (max-width: 640px) {
    .m-names {
      grid-template-columns: minmax(0, 1fr);
      gap: 4px;
    }
    .mon {
      align-items: flex-start;
      padding: 8px 4px 8px 8px;
    }
    .mon :global(.si) {
      margin-top: 2px;
    }
    .mon {
      flex-wrap: wrap;
      row-gap: 2px;
    }
    .m-names {
      flex: 1 1 calc(100% - 40px);
    }
    .mon .order {
      width: 100%;
      justify-content: flex-end;
    }
    .logo-row {
      flex-direction: column;
    }
    .pw-in,
    .dom {
      max-width: none;
    }
    .actions > :global(*:not(.spacer)) {
      flex: 1;
    }
    .spacer {
      display: none;
    }
  }
</style>
