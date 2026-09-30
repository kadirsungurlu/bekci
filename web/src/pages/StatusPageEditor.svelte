<script lang="ts">
  import { onMount, tick } from 'svelte';
  import {
    api,
    ApiError,
    errorMessage,
    type BarRange,
    type PageBlockId,
    type PageInput,
    type PageLayout,
    type PageStyle,
    type PageWidth,
    type StatusPage,
  } from '../lib/api';
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
  import PagePreviewPanel, { type PreviewDraft } from '../components/PagePreviewPanel.svelte';
  import { normalizeLayout } from '../components/StatusView.svelte';
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
  let collapsible = $state(false);
  let barRange = $state<BarRange>('recent');
  let pageLang = $state<Locale>('tr');
  let published = $state(true);
  // Dizilim: yerleşim, genişlik, bölüm sırası/görünürlüğü. Olaylar bölümünün
  // görünürlüğü sunucuda show_incidents ile aynı alandır.
  let layout = $state<PageLayout>(normalizeLayout(null));
  const showIncidents = $derived(layout.blocks.find((b) => b.id === 'incidents')?.visible !== false);
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
    layout = normalizeLayout(p.layout, p.show_incidents ?? true);
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
    layout: [layout.style, layout.width, layout.show_uptime, layout.blocks.map((b) => (b.visible ? b.id : `-${b.id}`))],
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

  // Dizilim bölümleri ---------------------------------------------------------------------
  const STYLES: PageStyle[] = ['list', 'grid', 'compact', 'rows'];
  const WIDTHS: PageWidth[] = ['narrow', 'wide'];
  const blockName = (id: PageBlockId) => t(`pages.layout.blockNames.${id}`);

  async function moveBlock(bi: number, dir: -1 | 1) {
    const to = bi + dir;
    if (to < 0 || to >= layout.blocks.length) return;
    const list = layout.blocks.slice();
    [list[bi], list[to]] = [list[to], list[bi]];
    layout.blocks = list;
    const key = list[to].id;
    await tick();
    focusBtn(`[data-bkey="${key}"] .b-${dir < 0 ? 'up' : 'down'}`, `[data-bkey="${key}"] .b-${dir < 0 ? 'down' : 'up'}`);
  }

  // Sürükle-bırak: gruplar, monitörler (gruplar arası dahil) ve dizilim bölümleri.
  // Klavyede aynı işler ok düğmeleriyle yapılır; dokunmatikte tarayıcı sürüklemeyi
  // desteklemiyorsa oklar kullanılır.
  type Drag = { kind: 'group'; si: number } | { kind: 'mon'; si: number; mi: number } | { kind: 'block'; bi: number };
  type Drop =
    | { kind: 'group'; si: number; after: boolean }
    | { kind: 'mon'; si: number; mi: number; after: boolean }
    | { kind: 'empty'; si: number }
    | { kind: 'block'; bi: number; after: boolean };
  let drag: Drag | null = null; // mantık için; görsel durum aşağıda
  let dragging = $state<Drag | null>(null);
  let drop = $state<Drop | null>(null);

  function dragStart(e: DragEvent, d: Drag) {
    if (!e.dataTransfer) return;
    drag = d;
    // Soluklaştırma sürükleme görüntüsü alındıktan sonra uygulanır.
    setTimeout(() => {
      if (drag === d) dragging = d;
    });
    e.dataTransfer.effectAllowed = 'move';
    e.dataTransfer.setData('text/plain', ''); // Firefox veri olmadan sürüklemez
    const row = (e.currentTarget as HTMLElement).closest<HTMLElement>('[data-drag-row]');
    if (row) {
      const r = row.getBoundingClientRect();
      e.dataTransfer.setDragImage(row, Math.min(24, r.width / 2), Math.min(20, r.height / 2));
    }
  }
  function dragEnd() {
    drag = null;
    dragging = null;
    drop = null;
  }
  /** İmleç öğenin alt yarısında mı? */
  function lowerHalf(e: DragEvent): boolean {
    const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
    return e.clientY > r.top + r.height / 2;
  }
  function overGroup(e: DragEvent, si: number) {
    if (!drag) return;
    if (drag.kind === 'group') {
      e.preventDefault();
      drop = { kind: 'group', si, after: lowerHalf(e) };
    } else if (drag.kind === 'mon') {
      // Monitör satırının dışında (başlık, boş alan): grubun sonuna.
      e.preventDefault();
      const n = sections[si].monitors.length;
      drop = n === 0 ? { kind: 'empty', si } : { kind: 'mon', si, mi: n - 1, after: true };
    }
  }
  function overMonitor(e: DragEvent, si: number, mi: number) {
    if (drag?.kind !== 'mon') return;
    e.preventDefault();
    e.stopPropagation();
    drop = { kind: 'mon', si, mi, after: lowerHalf(e) };
  }
  function overBlock(e: DragEvent, bi: number) {
    if (drag?.kind !== 'block') return;
    e.preventDefault();
    drop = { kind: 'block', bi, after: lowerHalf(e) };
  }
  /** from sırasındaki öğeyi, "to önüne/arkasına" bırakılınca oluşan yeni sıra. */
  function reorder<T>(list: T[], from: number, to: number, after: boolean): T[] {
    const out = list.slice();
    const [item] = out.splice(from, 1);
    let at = to + (after ? 1 : 0);
    if (from < at) at--;
    out.splice(at, 0, item);
    return out;
  }
  function onDrop(e: DragEvent) {
    e.preventDefault();
    e.stopPropagation();
    const d = drag,
      p = drop;
    dragEnd();
    if (!d || !p) return;
    if (d.kind === 'group' && p.kind === 'group') {
      sections = reorder(sections, d.si, p.si, p.after);
    } else if (d.kind === 'block' && p.kind === 'block') {
      layout.blocks = reorder(layout.blocks, d.bi, p.bi, p.after);
    } else if (d.kind === 'mon' && (p.kind === 'mon' || p.kind === 'empty')) {
      const list = sections.map((s) => ({ ...s, monitors: s.monitors.slice() }));
      const [m] = list[d.si].monitors.splice(d.mi, 1);
      let at = p.kind === 'empty' ? 0 : p.mi + (p.after ? 1 : 0);
      if (p.kind === 'mon' && p.si === d.si && d.mi < at) at--;
      list[p.si].monitors.splice(at, 0, m);
      sections = list;
    }
  }
  const dropCls = (kind: Drop['kind'], match: (p: Drop) => boolean) =>
    drop && drop.kind === kind && match(drop) ? ('after' in drop && drop.after ? 'drop-after' : 'drop-before') : '';

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
      layout: {
        style: layout.style,
        width: layout.width,
        blocks: layout.blocks.map((b) => ({ id: b.id, visible: b.visible })),
        show_uptime: layout.show_uptime,
      },
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

  // Canlı önizleme ------------------------------------------------------------------------
  // Geniş ekranda formun yanında sürekli; dar ekranda düğmeyle tam ekran açılır.
  const wideMq = matchMedia('(min-width: 1280px)');
  let wide = $state(wideMq.matches);
  let previewOpen = $state(false);
  onMount(() => {
    const on = () => (wide = wideMq.matches);
    wideMq.addEventListener('change', on);
    return () => wideMq.removeEventListener('change', on);
  });
  const draft = $derived<PreviewDraft>({
    pageId: page?.id,
    title,
    description,
    footer,
    sections: sections.map((s) => ({ title: s.title, monitors: s.monitors.map((m) => ({ id: m.id, name: m.name })) })),
    layout: {
      style: layout.style,
      width: layout.width,
      blocks: layout.blocks.map((b) => ({ id: b.id, visible: b.visible })),
      show_uptime: layout.show_uptime,
    },
    barRange,
    showTargets,
    collapsible,
    lang: pageLang,
    logo: page?.has_logo ? `/api/status-pages/${page.id}/logo?v=${page.updated_at}` : '',
  });

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
  <div class="ed" class:with-side={wide}>
  <div class="ed-main">
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
        {@const gName = s.title.trim() || t('pages.editor.groupN', { n: si + 1 })}
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div
          class="group {dropCls('group', (p) => p.kind === 'group' && p.si === si)}"
          class:dragging={dragging?.kind === 'group' && dragging.si === si}
          data-skey={s.key}
          data-drag-row
          ondragover={(e) => overGroup(e, si)}
          ondrop={onDrop}
        >
          <div class="g-head">
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <span
              class="grip"
              draggable="true"
              title={t('pages.layout.dragGroup', { name: gName })}
              ondragstart={(e) => dragStart(e, { kind: 'group', si })}
              ondragend={dragEnd}
            >
              {@render grip()}
            </span>
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
                aria-label={t('pages.editor.groupUp', { name: gName })}
                disabled={si === 0}
                onclick={() => moveSection(si, -1)}
              >
                <Icon name="arrow-up" size={15} />
              </button>
              <button
                type="button"
                class="btn ghost icon sm s-down"
                aria-label={t('pages.editor.groupDown', { name: gName })}
                disabled={si === sections.length - 1}
                onclick={() => moveSection(si, 1)}
              >
                <Icon name="arrow-down" size={15} />
              </button>
              <button type="button" class="btn ghost icon sm del" aria-label={t('pages.editor.groupRemove', { name: gName })} onclick={() => removeSection(si)}>
                <Icon name="trash" size={15} />
              </button>
            </div>
          </div>

          {#if s.monitors.length === 0}
            <div class="g-empty muted small" class:drop-empty={drop?.kind === 'empty' && drop.si === si}>
              {dragging?.kind === 'mon' ? t('pages.layout.dropEmpty') : t('pages.editor.groupEmpty')}
            </div>
          {:else}
            <ol class="mons">
              {#each s.monitors as m, mi (m.id)}
                {@const mon = live.byId(m.id)}
                <li
                  class="mon {dropCls('mon', (p) => p.kind === 'mon' && p.si === si && p.mi === mi)}"
                  class:dragging={dragging?.kind === 'mon' && dragging.si === si && dragging.mi === mi}
                  data-mkey={m.id}
                  data-drag-row
                  ondragover={(e) => overMonitor(e, si, mi)}
                  ondrop={onDrop}
                >
                  <!-- svelte-ignore a11y_no_static_element_interactions -->
                  <span
                    class="grip"
                    draggable="true"
                    title={t('pages.layout.dragMonitor', { name: mon?.name ?? `#${m.id}` })}
                    ondragstart={(e) => dragStart(e, { kind: 'mon', si, mi })}
                    ondragend={dragEnd}
                  >
                    {@render grip()}
                  </span>
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
      <div>
        <h2 class="card-title">{t('pages.layout.title')}</h2>
        <p class="help nomargin">{t('pages.layout.help')}</p>
      </div>
      <fieldset class="opts">
        <legend class="label">{t('pages.layout.style')}</legend>
        <div class="opt-row four">
          {#each STYLES as st (st)}
            <label class="opt" class:on={layout.style === st}>
              <input type="radio" name="lay-style" value={st} bind:group={layout.style} />
              {@render styleArt(st)}
              <span class="opt-t">{t(`pages.layout.styles.${st}`)}</span>
              <small>{t(`pages.layout.styleHelp.${st}`)}</small>
            </label>
          {/each}
        </div>
      </fieldset>
      <fieldset class="opts">
        <legend class="label">{t('pages.layout.width')}</legend>
        <div class="opt-row two">
          {#each WIDTHS as w (w)}
            <label class="opt" class:on={layout.width === w}>
              <input type="radio" name="lay-width" value={w} bind:group={layout.width} />
              {@render widthArt(w)}
              <span class="opt-t">{t(`pages.layout.widths.${w}`)}</span>
              <small>{t(`pages.layout.widthHelp.${w}`)}</small>
            </label>
          {/each}
        </div>
      </fieldset>
      <label class="check">
        <input type="checkbox" bind:checked={layout.show_uptime} />
        <span>
          {t('pages.layout.showUptime')}
          <small>{t('pages.layout.showUptimeHelp')}</small>
        </span>
      </label>
      <div class="field">
        <span class="label" id="blocks-l">{t('pages.layout.blocks')}</span>
        <ol class="blocks" aria-labelledby="blocks-l">
          {#each layout.blocks as b, bi (b.id)}
            <li
              class="blk {dropCls('block', (p) => p.kind === 'block' && p.bi === bi)}"
              class:off={!b.visible}
              class:dragging={dragging?.kind === 'block' && dragging.bi === bi}
              data-bkey={b.id}
              data-drag-row
              ondragover={(e) => overBlock(e, bi)}
              ondrop={onDrop}
            >
              <!-- svelte-ignore a11y_no_static_element_interactions -->
              <span
                class="grip"
                draggable="true"
                title={t('pages.layout.dragBlock', { name: blockName(b.id) })}
                ondragstart={(e) => dragStart(e, { kind: 'block', bi })}
                ondragend={dragEnd}
              >
                {@render grip()}
              </span>
              <label class="check blk-l">
                <input type="checkbox" bind:checked={b.visible} aria-label={t('pages.layout.blockShow', { name: blockName(b.id) })} />
                <span>
                  {blockName(b.id)}
                  <small>{t(`pages.layout.blockDesc.${b.id}`)}</small>
                </span>
              </label>
              {#if !b.visible}<span class="badge">{t('pages.layout.hidden')}</span>{/if}
              <div class="order">
                <button
                  type="button"
                  class="btn ghost icon sm b-up"
                  aria-label={t('pages.layout.blockUp', { name: blockName(b.id) })}
                  disabled={bi === 0}
                  onclick={() => moveBlock(bi, -1)}
                >
                  <Icon name="arrow-up" size={15} />
                </button>
                <button
                  type="button"
                  class="btn ghost icon sm b-down"
                  aria-label={t('pages.layout.blockDown', { name: blockName(b.id) })}
                  disabled={bi === layout.blocks.length - 1}
                  onclick={() => moveBlock(bi, 1)}
                >
                  <Icon name="arrow-down" size={15} />
                </button>
              </div>
            </li>
          {/each}
        </ol>
        <span class="help">{t('pages.layout.blocksHelp')}</span>
        {#if layout.blocks.some((b) => b.id === 'groups' && !b.visible)}
          <div class="alert warning small">{t('pages.layout.groupsHiddenWarn')}</div>
        {/if}
      </div>
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
  </div>
  {#if wide || previewOpen}
    <aside class="ed-side" class:overlay={!wide}>
      <PagePreviewPanel {draft} onclose={wide ? undefined : () => (previewOpen = false)} />
    </aside>
  {/if}
  </div>
  {#if !wide && !previewOpen}
    <button type="button" class="btn primary pv-fab" aria-label={t('pages.livePreview.showAria')} onclick={() => (previewOpen = true)}>
      <Icon name="eye" size={16} />
      {t('pages.livePreview.show')}
    </button>
  {/if}
{/if}

{#snippet grip()}
  <svg width="14" height="14" viewBox="0 0 24 24" aria-hidden="true" fill="currentColor">
    <circle cx="9" cy="5.5" r="1.7" /><circle cx="15" cy="5.5" r="1.7" />
    <circle cx="9" cy="12" r="1.7" /><circle cx="15" cy="12" r="1.7" />
    <circle cx="9" cy="18.5" r="1.7" /><circle cx="15" cy="18.5" r="1.7" />
  </svg>
{/snippet}

{#snippet styleArt(st: PageStyle)}
  <svg class="art" viewBox="0 0 64 40" aria-hidden="true">
    <rect x="0.5" y="0.5" width="63" height="39" rx="5" class="a-frame" />
    {#if st === 'list'}
      {#each [0, 1, 2] as i (i)}
        <rect x="8" y={7 + i * 11} width="18" height="2.5" rx="1.2" class="a-text" />
        <rect x="8" y={11 + i * 11} width="48" height="3.5" rx="1.7" class="a-bar" />
      {/each}
    {:else if st === 'grid'}
      {#each [0, 1] as c (c)}
        <rect x={6 + c * 27} y="6" width="25" height="28" rx="3" class="a-card" />
        {#each [0, 1] as i (i)}
          <rect x={9 + c * 27} y={10 + i * 11} width="11" height="2.5" rx="1.2" class="a-text" />
          <rect x={9 + c * 27} y={14 + i * 11} width="19" height="3" rx="1.5" class="a-bar" />
        {/each}
      {/each}
    {:else if st === 'rows'}
      <!-- ışık, ad, çubuklar ve yüzde aynı satırda -->
      {#each [0, 1, 2, 3] as i (i)}
        <circle cx="8" cy={9.2 + i * 7.5} r="1.9" class="a-dot" />
        <rect x="12" y={8 + i * 7.5} width="9" height="2.5" rx="1.2" class="a-text" />
        {#each { length: 12 } as _, k (k)}
          <rect x={24.5 + k * 2.4} y={6.8 + i * 7.5} width="1.7" height="4.8" rx="0.6" class="a-bar" />
        {/each}
        <rect x="54" y={8 + i * 7.5} width="5.5" height="2.5" rx="1.2" class="a-text" />
      {/each}
    {:else}
      {#each [0, 1, 2, 3, 4] as i (i)}
        <circle cx="10" cy={8 + i * 6} r="1.8" class="a-dot" />
        <rect x="15" y={7 + i * 6} width="24" height="2.5" rx="1.2" class="a-text" />
        <rect x="46" y={7 + i * 6} width="10" height="2.5" rx="1.2" class="a-pill" />
      {/each}
    {/if}
  </svg>
{/snippet}

{#snippet widthArt(w: PageWidth)}
  <svg class="art" viewBox="0 0 64 40" aria-hidden="true">
    <rect x="0.5" y="0.5" width="63" height="39" rx="5" class="a-frame" />
    <rect x={w === 'wide' ? 5 : 17} y="7" width={w === 'wide' ? 54 : 30} height="26" rx="3" class="a-card" />
    <rect x={w === 'wide' ? 9 : 21} y="12" width={w === 'wide' ? 46 : 22} height="3" rx="1.5" class="a-bar" />
    <rect x={w === 'wide' ? 9 : 21} y="19" width={w === 'wide' ? 46 : 22} height="3" rx="1.5" class="a-bar" />
    <rect x={w === 'wide' ? 9 : 21} y="26" width={w === 'wide' ? 30 : 14} height="2.5" rx="1.2" class="a-text" />
  </svg>
{/snippet}

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
  /* Geniş ekranda form ve canlı önizleme yan yana. */
  .ed.with-side {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, 1.05fr);
    gap: 24px;
    align-items: start;
  }
  .ed-main {
    min-width: 0;
  }
  /* Dar ekranda yüzen önizleme düğmesi son düğmelerin üstünü kapatmasın. */
  .ed:not(.with-side) .ed-main {
    padding-bottom: 64px;
  }
  .ed-side {
    position: sticky;
    top: 16px;
    height: calc(100vh - 32px);
    height: calc(100dvh - 32px);
    min-width: 0;
  }
  .ed-side.overlay {
    position: fixed;
    inset: 0;
    z-index: 60;
    height: auto;
    background: var(--bg);
    padding: max(12px, env(safe-area-inset-top)) max(12px, env(safe-area-inset-right)) max(12px, env(safe-area-inset-bottom))
      max(12px, env(safe-area-inset-left));
  }
  .pv-fab {
    position: fixed;
    right: 24px;
    bottom: 24px;
    z-index: 30;
    box-shadow: var(--shadow);
  }
  @media (max-width: 900px) {
    .pv-fab {
      right: max(16px, env(safe-area-inset-right));
      bottom: calc(84px + env(safe-area-inset-bottom));
    }
  }

  /* Sürükleme tutamağı ve bırakma göstergesi */
  .grip {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 22px;
    height: 30px;
    margin-left: -4px;
    flex-shrink: 0;
    color: var(--muted);
    cursor: grab;
    border-radius: 6px;
    touch-action: none;
  }
  .grip:hover {
    color: var(--accent-text);
    background: var(--accent-soft);
  }
  .grip:active {
    cursor: grabbing;
  }
  .dragging {
    opacity: 0.45;
  }
  .drop-before {
    box-shadow: 0 -3px 0 0 var(--accent);
  }
  .drop-after {
    box-shadow: 0 3px 0 0 var(--accent);
  }
  .g-empty.drop-empty {
    border: 1.5px dashed var(--accent);
    border-radius: 8px;
    color: var(--accent-text);
  }

  /* Dizilim seçenekleri */
  .opts {
    border: none;
    margin: 0;
    padding: 0;
    min-width: 0;
  }
  .opts legend {
    padding: 0;
    margin-bottom: 8px;
  }
  .opt-row {
    display: grid;
    gap: 10px;
  }
  .opts {
    container-type: inline-size;
  }
  .opt-row.four {
    grid-template-columns: repeat(4, minmax(0, 1fr));
  }
  /* Dar form sütununda dört seçenek 2×2. */
  @container (max-width: 560px) {
    .opt-row.four {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
  }
  .opt-row.two {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .opt {
    position: relative;
    display: flex;
    flex-direction: column;
    gap: 4px;
    padding: 10px;
    border: 1px solid var(--border-strong);
    border-radius: 10px;
    background: var(--bg-elev);
    cursor: pointer;
    min-width: 0;
  }
  .opt:hover {
    border-color: var(--border-hover);
  }
  .opt.on {
    border-color: var(--accent);
    background: var(--accent-soft);
    box-shadow: 0 0 0 1px var(--accent);
  }
  .opt input {
    position: absolute;
    opacity: 0;
    pointer-events: none;
  }
  .opt:has(input:focus-visible) {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }
  .opt-t {
    font-weight: 650;
    font-size: 0.9rem;
  }
  .opt small {
    color: var(--text-2);
    font-size: 0.78rem;
    line-height: 1.35;
  }
  .art {
    width: 100%;
    max-width: 120px;
    height: auto;
    margin-bottom: 4px;
  }
  .art .a-frame {
    fill: var(--input);
    stroke: var(--border-strong);
  }
  .art .a-card {
    fill: var(--card);
    stroke: var(--border-strong);
  }
  .art .a-text {
    fill: var(--text-2);
    opacity: 0.55;
  }
  .art .a-bar,
  .art .a-dot {
    fill: var(--up);
  }
  .art .a-pill {
    fill: var(--up);
    opacity: 0.35;
  }
  .opt.on .a-frame {
    stroke: var(--accent);
  }
  .blocks {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .blk {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 6px 6px 10px;
    border: 1px solid var(--border);
    border-radius: 8px;
    background: var(--bg-elev);
  }
  .blk.off .blk-l > span {
    color: var(--muted);
  }
  .blk-l {
    flex: 1;
    min-width: 0;
    align-items: center;
  }
  .blk-l small {
    display: block;
    color: var(--muted);
    font-size: 0.78rem;
  }

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
    .opt-row.four {
      grid-template-columns: minmax(0, 1fr);
    }
    .opt-row.four .opt {
      display: grid;
      grid-template-columns: 76px minmax(0, 1fr);
      column-gap: 12px;
      align-items: center;
    }
    .opt-row.four .art {
      grid-row: span 2;
      margin: 0;
    }
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
      flex: 1 1 calc(100% - 70px);
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
