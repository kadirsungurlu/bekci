<script lang="ts">
  import { onMount, tick } from 'svelte';
  import { api, ApiError, errorMessage, type BarRange, type PageInput, type StatusPage } from '../lib/api';
  import { live } from '../lib/live.svelte';
  import { navigate } from '../lib/router.svelte';
  import { confirmDialog, copyText, toast } from '../lib/ui.svelte';
  import { monitorKind } from '../lib/format';
  import Modal from '../components/Modal.svelte';
  import MonitorPicker from '../components/MonitorPicker.svelte';
  import StatusIcon from '../components/StatusIcon.svelte';
  import Icon from '../components/Icon.svelte';
  import Announcements from './Announcements.svelte';

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
  let published = $state(true);
  let pwMode = $state<'keep' | 'set' | 'remove'>('keep');
  let password = $state('');

  let seq = 0;
  let sections = $state<EdSection[]>([{ key: ++seq, title: 'Servisler', monitors: [] }]);

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
    published = p.published;
    pwMode = 'keep';
    password = '';
    sections = p.sections.map((s) => ({ key: ++seq, title: s.title, monitors: s.monitors.map((m) => ({ ...m })) }));
    if (sections.length === 0) sections = [{ key: ++seq, title: '', monitors: [] }];
  }

  onMount(async () => {
    if (!isEdit) return;
    try {
      fill(await api.page(id!));
    } catch (e) {
      loadError = e instanceof ApiError && e.status === 404 ? 'Durum sayfası bulunamadı.' : errorMessage(e);
    } finally {
      loading = false;
    }
  });

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
        title: 'Grubu kaldır',
        message: `“${s.title || 'Adsız grup'}” ve içindeki ${s.monitors.length} monitör sayfadan kaldırılacak (monitörlerin kendisi silinmez).`,
        confirmText: 'Kaldır',
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
  function validate(): string {
    if (!title.trim()) return 'Başlık gerekli.';
    if (!SLUG_RE.test(slug.trim())) return 'Adres (kısa ad) 1-50 karakter olmalı; küçük harf, rakam ve tire kullanılabilir.';
    if (pwMode === 'set' && (password.length < 4 || password.length > 72)) return 'Sayfa şifresi 4-72 karakter olmalı.';
    if (customDomain.trim() && /[/:\s]/.test(customDomain.trim()))
      return 'Özel alan adını http:// ve / olmadan yazın (ör. durum.ornek.com).';
    if (total > 200) return 'Bir sayfada en fazla 200 monitör olabilir.';
    return '';
  }

  async function showError(msg: string) {
    error = msg;
    await tick();
    errorEl?.scrollIntoView({ behavior: 'smooth', block: 'center' });
  }

  async function submit(e: SubmitEvent) {
    e.preventDefault();
    error = '';
    const v = validate();
    if (v) return showError(v);
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
      published,
    };
    if (pwMode === 'set') body.password = password;
    else if (pwMode === 'remove') body.password = '';
    saving = true;
    try {
      const res = isEdit ? await api.updatePage(id!, body) : await api.createPage(body);
      if (isEdit) {
        fill(res);
        toast.success('Durum sayfası kaydedildi');
      } else {
        toast.success(`“${res.title}” oluşturuldu. Şimdi logo ve duyuru ekleyebilirsiniz.`);
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
      title: 'Durum sayfasını sil',
      message: `“${page.title}” sayfası, logosu ve duyuruları kalıcı olarak silinecek.`,
      confirmText: 'Sil',
      danger: true,
    });
    if (!ok) return;
    try {
      await api.deletePage(page.id);
      toast.success('Durum sayfası silindi');
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
    if (!['image/png', 'image/jpeg', 'image/webp'].includes(f.type)) return (logoError = 'Logo PNG, JPEG veya WebP olmalı.');
    if (f.size > 512 * 1024) return (logoError = 'Logo en fazla 512 KB olabilir.');
    logoBusy = true;
    try {
      page = await api.uploadLogo(page.id, f);
      toast.success('Logo yüklendi');
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
      toast.success('Logo kaldırıldı');
    } catch (err) {
      logoError = errorMessage(err);
    } finally {
      logoBusy = false;
    }
  }

  const publicUrl = $derived(page ? `${location.origin}/durum/${page.slug}` : '');
  const host = location.host;

  async function copyUrl() {
    if (publicUrl && (await copyText(publicUrl))) toast.success('Adres panoya kopyalandı');
  }
</script>

<a class="back" href="#/status-pages"><Icon name="chevron-left" size={16} /> Durum sayfaları</a>
<div class="page-head">
  <h1>{isEdit ? 'Durum sayfasını düzenle' : 'Yeni durum sayfası'}<span class="dot">.</span></h1>
  {#if page}
    <div class="row head-acts">
      <a class="btn" href="#/status-pages/{page.id}/preview"><Icon name="eye" size={15} /> Önizle</a>
      {#if page.published}
        <a class="btn" href={publicUrl} target="_blank" rel="noopener noreferrer"><Icon name="external" size={15} /> Sayfayı aç</a>
      {/if}
    </div>
  {/if}
</div>

{#if loading}
  <div class="skeleton" style="height:480px;max-width:900px"></div>
{:else if loadError}
  <div class="card empty">
    <h3>Durum sayfası yüklenemedi</h3>
    <p>{loadError}</p>
    <a class="btn primary" href="#/status-pages">Listeye dön</a>
  </div>
{:else}
  <form class="form" onsubmit={submit} novalidate>
    {#if page}
      <div class="pub-bar card">
        <span class="badge {page.published ? 'up' : 'paused'}">{page.published ? 'Yayında' : 'Taslak'}</span>
        <code class="pub-url">{publicUrl}</code>
        <button type="button" class="btn sm" onclick={copyUrl}><Icon name="copy" size={14} /> Kopyala</button>
      </div>
    {/if}

    <section class="card stack">
      <h2 class="card-title">Genel</h2>
      <div class="field">
        <label for="sp-title">Başlık</label>
        <input id="sp-title" class="input" maxlength="100" bind:value={title} oninput={onTitle} placeholder="Ör. Acme Servis Durumu" />
      </div>
      <div class="field">
        <label for="sp-slug">Adres (kısa ad)</label>
        <div class="prefixed">
          <span class="prefix">/durum/</span>
          <input
            id="sp-slug"
            class="input"
            maxlength="50"
            bind:value={slug}
            oninput={() => (slugTouched = true)}
            autocapitalize="none"
            spellcheck="false"
            placeholder="acme"
          />
        </div>
        <span class="help">Sayfa {host}/durum/&lt;kısa-ad&gt; adresinde yayınlanır. Küçük harf, rakam ve tire.</span>
      </div>
      <div class="field">
        <label for="sp-desc">Açıklama <span class="muted">(isteğe bağlı)</span></label>
        <textarea id="sp-desc" class="input plain" rows="2" maxlength="1000" bind:value={description} placeholder="Başlığın altında görünür"></textarea>
      </div>
      <div class="field">
        <label for="sp-foot">Alt bilgi <span class="muted">(isteğe bağlı)</span></label>
        <input id="sp-foot" class="input" maxlength="500" bind:value={footer} placeholder="Ör. Sorunlar için destek@ornek.com" />
      </div>
    </section>

    <section class="card stack">
      <div class="sec-head">
        <h2 class="card-title">Gruplar ve monitörler</h2>
        <span class="muted small">{total} monitör</span>
      </div>
      <p class="help nomargin">Monitörler sayfada bu sırayla görünür. Görünen adı boş bırakırsanız monitörün kendi adı kullanılır.</p>

      {#each sections as s, si (s.key)}
        <div class="group" data-skey={s.key}>
          <div class="g-head">
            <input
              id="sec-{s.key}"
              class="input g-title"
              maxlength="100"
              bind:value={s.title}
              placeholder="Grup adı (ör. Web siteleri)"
              aria-label="Grup adı"
            />
            <div class="order">
              <button type="button" class="btn ghost icon sm s-up" aria-label="Grubu yukarı taşı" disabled={si === 0} onclick={() => moveSection(si, -1)}>
                <Icon name="arrow-up" size={15} />
              </button>
              <button
                type="button"
                class="btn ghost icon sm s-down"
                aria-label="Grubu aşağı taşı"
                disabled={si === sections.length - 1}
                onclick={() => moveSection(si, 1)}
              >
                <Icon name="arrow-down" size={15} />
              </button>
              <button type="button" class="btn ghost icon sm del" aria-label="Grubu kaldır" onclick={() => removeSection(si)}>
                <Icon name="trash" size={15} />
              </button>
            </div>
          </div>

          {#if s.monitors.length === 0}
            <div class="g-empty muted small">Bu grupta monitör yok.</div>
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
                      placeholder={mon?.name ?? 'Görünen ad'}
                      aria-label="{mon?.name ?? 'Monitör'} için görünen ad (boşsa monitör adı)"
                    />
                  </div>
                  <div class="order">
                    <button
                      type="button"
                      class="btn ghost icon sm m-up"
                      aria-label="Yukarı taşı"
                      disabled={isFirstMon(si, mi)}
                      onclick={() => moveMonitor(si, mi, -1)}
                    >
                      <Icon name="arrow-up" size={15} />
                    </button>
                    <button
                      type="button"
                      class="btn ghost icon sm m-down"
                      aria-label="Aşağı taşı"
                      disabled={isLastMon(si, mi)}
                      onclick={() => moveMonitor(si, mi, 1)}
                    >
                      <Icon name="arrow-down" size={15} />
                    </button>
                    <button type="button" class="btn ghost icon sm del" aria-label="Sayfadan kaldır" onclick={() => removeMonitor(si, mi)}>
                      <Icon name="x" size={15} />
                    </button>
                  </div>
                </li>
              {/each}
            </ol>
          {/if}
          <button type="button" class="btn sm add" onclick={() => openAdd(si)}><Icon name="plus" size={14} /> Monitör ekle</button>
        </div>
      {/each}
      <button type="button" class="btn add-sec" onclick={addSection} disabled={sections.length >= 20}>
        <Icon name="layers" size={15} /> Grup ekle
      </button>
    </section>

    <section class="card stack">
      <h2 class="card-title">Görünüm</h2>
      <div class="logo-row">
        <div class="logo-box">
          {#if page?.has_logo}
            <img src="/api/status-pages/{page.id}/logo?v={page.updated_at}" alt="Sayfa logosu" />
          {:else}
            <Icon name="image" size={24} />
          {/if}
        </div>
        <div class="logo-ctl">
          <div class="label">Logo</div>
          {#if page}
            <div class="row">
              <input bind:this={fileInput} type="file" accept="image/png,image/jpeg,image/webp" class="hidden-file" onchange={onLogo} tabindex="-1" aria-hidden="true" />
              <button type="button" class="btn sm" onclick={() => fileInput?.click()} disabled={logoBusy}>
                {#if logoBusy}<span class="spinner"></span>{:else}<Icon name="upload" size={14} />{/if}
                {page.has_logo ? 'Değiştir' : 'Logo yükle'}
              </button>
              {#if page.has_logo}
                <button type="button" class="btn sm danger" onclick={removeLogo} disabled={logoBusy}>Kaldır</button>
              {/if}
            </div>
            <span class="help">PNG, JPEG veya WebP; en fazla 512 KB. Yatay logolar en iyi sonucu verir.</span>
          {:else}
            <span class="help">Logoyu sayfayı oluşturduktan sonra yükleyebilirsiniz.</span>
          {/if}
          {#if logoError}<div class="alert error small">{logoError}</div>{/if}
        </div>
      </div>
      <div class="field">
        <label for="bar-range">Durum çubukları</label>
        <select id="bar-range" class="input" bind:value={barRange}>
          <option value="recent">Son kontroller (önerilen)</option>
          <option value="24h">Son 24 saat (saatlik)</option>
          <option value="90d">Son 90 gün (günlük)</option>
        </select>
        <span class="help"
          >Ziyaretçiler çoğunlukla anlık durumu merak eder. "Son kontroller"de her çubuk bir kontroldür (Uptime Kuma gibi); uzun
          vadeli güvenilirliği göstermek için 90 gün seçilebilir.</span
        >
      </div>
      <label class="check">
        <input type="checkbox" bind:checked={showTargets} />
        <span>
          Hedef adresleri göster
          <small>Kapalıyken monitörlerin adresleri gizlenir. Açıksa web sitelerinde alan adı (ör. ornek.com), diğerlerinde sunucu adresi gösterilir.</small>
        </span>
      </label>
      <label class="check">
        <input type="checkbox" bind:checked={showIncidents} />
        <span>
          Son 14 günün olaylarını göster
          <small>Kapalıyken sayfada geçmiş kesintiler listelenmez; ziyaretçi yalnızca anlık durumu ve çubukları görür.</small>
        </span>
      </label>
      <label class="check">
        <input type="checkbox" bind:checked={collapsible} />
        <span>
          Gruplar açılıp kapanabilsin
          <small>Ziyaretçi grup başlığına tıklayarak grubu daraltabilir; seçimi kendi tarayıcısında hatırlanır. Gruplar başlangıçta açık gelir.</small>
        </span>
      </label>
    </section>

    <section class="card stack">
      <h2 class="card-title">Yayın ve erişim</h2>
      <label class="check">
        <input type="checkbox" bind:checked={published} />
        <span>Yayında<small>Kapalıyken sayfa herkese kapalıdır; yalnızca buradan önizleyebilirsiniz.</small></span>
      </label>

      <div class="field">
        <span class="label" id="pw-l">Sayfa şifresi</span>
        {#if page?.has_password && pwMode === 'keep'}
          <div class="pw-state">
            <span class="badge"><Icon name="lock" size={11} /> Şifre korumalı</span>
            <button type="button" class="btn sm" onclick={() => ((pwMode = 'set'), (password = ''))}>Şifreyi değiştir</button>
            <button type="button" class="btn sm danger" onclick={() => (pwMode = 'remove')}>Şifreyi kaldır</button>
          </div>
        {:else if pwMode === 'remove'}
          <div class="pw-state">
            <span class="muted small">Kaydettiğinizde şifre kaldırılacak ve sayfa herkese açık olacak.</span>
            <button type="button" class="linkbtn small" onclick={() => (pwMode = 'keep')}>Vazgeç</button>
          </div>
        {:else}
          <input
            class="input pw-in"
            type="password"
            autocomplete="new-password"
            aria-labelledby="pw-l"
            bind:value={password}
            oninput={() => (pwMode = password ? 'set' : page?.has_password ? 'set' : 'keep')}
            placeholder={page?.has_password ? 'Yeni şifre' : 'Boş bırakırsanız sayfa herkese açıktır'}
          />
          <span class="help">
            Boş bırakırsanız sayfa herkese açıktır. 4-72 karakter.
            {#if page?.has_password}<button type="button" class="linkbtn" onclick={() => ((pwMode = 'keep'), (password = ''))}>Mevcut şifreyi koru</button>{/if}
          </span>
        {/if}
      </div>

      <div class="field">
        <label for="sp-dom">Özel alan adı <span class="muted">(isteğe bağlı)</span></label>
        <input id="sp-dom" class="input dom" bind:value={customDomain} placeholder="durum.ornek.com" autocapitalize="none" spellcheck="false" />
        <span class="help">
          DNS kaydını sunucuya yönlendirin ve alan adını Coolify’a ekleyin. Bu alan adında sadece durum sayfası açılır, yönetim paneli açılmaz.
        </span>
      </div>
    </section>

    {#if error}
      <div class="alert error" role="alert" bind:this={errorEl}>{error}</div>
    {/if}

    <div class="actions">
      {#if isEdit}
        <button type="button" class="btn danger" onclick={remove}><Icon name="trash" size={15} /> Sil</button>
        <div class="spacer"></div>
      {/if}
      <a class="btn" href="#/status-pages">Vazgeç</a>
      <button class="btn primary" type="submit" disabled={saving}>
        {#if saving}<span class="spinner"></span>{/if}
        {isEdit ? 'Kaydet' : 'Sayfayı oluştur'}
      </button>
    </div>
  </form>

  {#if page}
    <div class="ann">
      <Announcements pageId={page.id} />
    </div>
  {/if}
{/if}

<Modal bind:open={addOpen} title="Monitör ekle" width={560}>
  <p class="help nomargin sp">
    “{sections[addTo]?.title || 'Adsız grup'}” grubuna eklenecek monitörleri seçin. Sayfada zaten olanlar listede görünmez.
  </p>
  <MonitorPicker bind:selected={addSel} exclude={[...onPage]} label="Eklenecek monitörler" id="add-mp" />
  {#snippet footer()}
    <div class="spacer"></div>
    <button type="button" class="btn" onclick={() => (addOpen = false)}>Vazgeç</button>
    <button type="button" class="btn primary" disabled={addSel.length === 0} onclick={confirmAdd}>
      {addSel.length ? `${addSel.length} monitörü ekle` : 'Ekle'}
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
