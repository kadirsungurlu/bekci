<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import { api, errorMessage, type OverallStatus, type StatusPageListItem } from '../lib/api';
  import { clock, confirmDialog, copyText, toast } from '../lib/ui.svelte';
  import { collator, fmtDuration, fmtPct, fmtRelative } from '../lib/format';
  import RowMenu, { type MenuItem } from '../components/RowMenu.svelte';
  import Icon, { type IconName } from '../components/Icon.svelte';
  import PageThumb from '../components/PageThumb.svelte';
  import { t } from '../lib/i18n';

  let list = $state.raw<StatusPageListItem[]>([]);
  let loading = $state(true);
  let error = $state('');

  async function load() {
    try {
      list = await api.pages();
      error = '';
    } catch (e) {
      error = errorMessage(e);
    } finally {
      loading = false;
    }
  }
  // Özetteki anlık durum ve uptime dakikada bir tazelenir (sekme görünürken).
  let timer: ReturnType<typeof setInterval> | undefined;
  onMount(() => {
    load();
    timer = setInterval(() => {
      if (!document.hidden) load();
    }, 60_000);
  });
  onDestroy(() => clearInterval(timer));

  const sorted = $derived(list.slice().sort((a, b) => collator.compare(a.title, b.title)));
  const stats = $derived({
    published: list.filter((p) => p.published).length,
    issues: list.filter((p) => p.summary && (p.summary.status === 'partial' || p.summary.status === 'down')).length,
  });

  function publicUrl(p: StatusPageListItem): string {
    return `${location.origin}/durum/${p.slug}`;
  }

  const count = (p: StatusPageListItem) => p.summary?.monitors ?? p.sections.reduce((n, s) => n + s.monitors.length, 0);

  const STATUS_ICON: Record<OverallStatus, IconName> = { up: 'check', partial: 'alert', down: 'x', unknown: 'info' };

  /** Uptime yüzdesinin rengi: düşükse uyarı/kesinti tonu. */
  function upTone(v: number | null | undefined): string {
    if (v === null || v === undefined) return 'none';
    return v >= 99 ? 'good' : v >= 95 ? 'warn' : 'bad';
  }

  async function remove(p: StatusPageListItem) {
    const ok = await confirmDialog({
      title: t('pages.deleteTitle'),
      message: t('pages.list.deleteMsg', { name: p.title }),
      confirmText: t('common.delete'),
      danger: true,
    });
    if (!ok) return;
    try {
      await api.deletePage(p.id);
      list = list.filter((x) => x.id !== p.id);
      toast.success(t('pages.deleted', { name: p.title }));
    } catch (e) {
      toast.error(errorMessage(e));
    }
  }

  async function copyUrl(p: StatusPageListItem) {
    if (await copyText(publicUrl(p))) toast.success(t('pages.list.urlCopied'));
  }
  async function copyDomain(p: StatusPageListItem) {
    if (await copyText(`https://${p.custom_domain}`)) toast.success(t('pages.list.domainCopied'));
  }

  function menu(p: StatusPageListItem): MenuItem[] {
    return [
      { label: t('common.edit'), icon: 'edit', href: `#/status-pages/${p.id}` },
      { label: t('pages.preview'), icon: 'eye', href: `#/status-pages/${p.id}/preview` },
      { label: t('pages.list.copyUrl'), icon: 'copy', onclick: () => copyUrl(p) },
      { label: t('common.delete'), icon: 'trash', danger: true, onclick: () => remove(p) },
    ];
  }
</script>

<div class="page-head">
  <div class="ph">
    <h1>{t('pages.list.title')}<span class="dot">.</span></h1>
    {#if list.length}
      <p class="lead">
        {t('pages.list.summary', { count: list.length })} · {t('pages.list.summaryPublished', { count: stats.published })}
        {#if stats.issues}
          · <span class="lead-bad">{t('pages.list.summaryIssues', { count: stats.issues })}</span>
        {/if}
      </p>
    {/if}
  </div>
  <a class="btn primary" href="#/status-pages/new"><Icon name="plus" size={16} /> {t('pages.list.newPage')}</a>
</div>

{#if loading}
  <div class="skeleton" style="height:190px"></div>
{:else if error && list.length === 0}
  <div class="card empty">
    <h3>{t('pages.list.loadFailed')}</h3>
    <p>{error}</p>
    <button class="btn primary" onclick={load}>{t('common.retry')}</button>
  </div>
{:else if list.length === 0}
  <div class="card empty-hero">
    <div class="eh-art" aria-hidden="true">
      <PageThumb
        title={t('pages.list.emptyTitle')}
        status="up"
        sections={[
          { title: '', statuses: ['up', 'up', 'up'] },
          { title: '', statuses: ['up', 'down', 'up'] },
        ]}
        blank
      />
    </div>
    <div class="eh-b">
      <h3>{t('pages.list.emptyTitle')}</h3>
      <p>{t('pages.list.emptyText')}</p>
      <ul class="eh-f">
        <li><Icon name="activity" size={16} /> {t('pages.list.emptyF1')}</li>
        <li><Icon name="megaphone" size={16} /> {t('pages.list.emptyF2')}</li>
        <li><Icon name="globe" size={16} /> {t('pages.list.emptyF3')}</li>
      </ul>
      <a class="btn primary" href="#/status-pages/new"><Icon name="plus" size={16} /> {t('pages.list.create')}</a>
    </div>
  </div>
{:else}
  <div class="list">
    {#each sorted as p (p.id)}
      {@const sm = p.summary}
      {@const lang = p.lang ?? 'tr'}
      <article class="card page" class:draft={!p.published}>
        <a class="thumb" href="#/status-pages/{p.id}/preview" aria-label={t('pages.list.previewOf', { name: p.title })}>
          <PageThumb
            title={p.title}
            logo={p.has_logo ? `/api/status-pages/${p.id}/logo?v=${p.updated_at}` : ''}
            status={sm?.status ?? 'unknown'}
            sections={sm?.sections ?? p.sections.map((s) => ({ title: s.title, statuses: s.monitors.map(() => 'pending' as const) }))}
            emptyText={t('pages.list.noMonitors')}
            moreText={(n) => t('pages.list.moreGroups', { count: n })}
            layout={p.layout}
          />
        </a>

        <div class="body">
          <div class="head">
            <div class="tt">
              <h2><a href="#/status-pages/{p.id}">{p.title}</a></h2>
              <div class="badges">
                {#if p.published}
                  <span class="badge up">{t('pages.published')}</span>
                {:else}
                  <span class="badge paused">{t('pages.draft')}</span>
                {/if}
                {#if p.has_password}<span class="badge"><Icon name="lock" size={11} /> {t('pages.list.passwordProtected')}</span>{/if}
                <span class="badge lang" title={t('pages.list.langTitle', { lang: t(`pages.list.langs.${lang}`) })}>
                  <Icon name="globe" size={11} />
                  {lang.toUpperCase()}
                </span>
              </div>
            </div>
            <div class="acts">
              <a class="btn sm" href="#/status-pages/{p.id}"><Icon name="edit" size={14} /> {t('common.edit')}</a>
              <a class="btn sm ghost hide-sm" href="#/status-pages/{p.id}/preview"><Icon name="eye" size={14} /> {t('pages.preview')}</a>
              <RowMenu items={menu(p)} label={t('pages.list.actionsFor', { name: p.title })} />
            </div>
          </div>

          {#if p.description}<p class="desc">{p.description}</p>{/if}

          <div class="links">
            <span class="url">
              <Icon name="link" size={14} />
              <a href={publicUrl(p)} target="_blank" rel="noopener noreferrer" title={t('pages.list.openNewTab')}>
                <span class="u-t">/durum/{p.slug}</span>
                <Icon name="external" size={12} />
              </a>
              <button type="button" class="u-copy" onclick={() => copyUrl(p)} aria-label={t('pages.list.copyUrl')} data-tip={t('pages.list.copyUrl')}>
                <Icon name="copy" size={13} />
              </button>
            </span>
            {#if p.custom_domain}
              <span class="url">
                <Icon name="globe" size={14} />
                <a href="https://{p.custom_domain}" target="_blank" rel="noopener noreferrer" title={t('pages.list.openNewTab')}>
                  <span class="u-t">{p.custom_domain}</span>
                  <Icon name="external" size={12} />
                </a>
                <button type="button" class="u-copy" onclick={() => copyDomain(p)} aria-label={t('pages.list.copyDomain')} data-tip={t('pages.list.copyDomain')}>
                  <Icon name="copy" size={13} />
                </button>
              </span>
            {/if}
          </div>

          <dl class="stats">
            <div class="st st-status">
              <dt>{t('pages.list.statStatus')}</dt>
              <dd>
                {#if sm}
                  <span class="ov ov-{sm.status}">
                    <span class="ov-ic" aria-hidden="true"><Icon name={STATUS_ICON[sm.status]} size={12} stroke={3} /></span>
                    {t(`pages.list.status.${sm.status}`)}
                  </span>
                  {#if sm.down > 0 && sm.status === 'partial'}
                    <span class="st-sub bad">{t('pages.list.downCount', { count: sm.down })}</span>
                  {/if}
                {:else}
                  <span class="muted">—</span>
                {/if}
              </dd>
            </div>
            <div class="st">
              <dt>{t('pages.list.statMonitors')}</dt>
              <dd class="num">{count(p)}</dd>
            </div>
            <div class="st">
              <dt>{t('pages.list.stat24h')}</dt>
              <dd class="num up-{upTone(sm?.uptime_24h)}">{fmtPct(sm?.uptime_24h)}</dd>
            </div>
            <div class="st">
              <dt>{t('pages.list.stat30d')}</dt>
              <dd class="num up-{upTone(sm?.uptime_30d)}">{fmtPct(sm?.uptime_30d)}</dd>
            </div>
            <div class="st st-inc">
              <dt>{t('pages.list.statIncident')}</dt>
              <dd>
                {#if sm?.last_incident}
                  {@const li = sm.last_incident}
                  {@const ongoing = li.resolved_at === 0}
                  <span class="inc" class:ongoing>
                    <span class="inc-dot" aria-hidden="true"></span>
                    <span class="inc-b">
                      <span class="inc-m">{li.monitor}</span>
                      <span class="inc-w">
                        {ongoing
                          ? t('pages.list.incOngoing', { time: fmtRelative(li.started_at, clock.now) })
                          : t('pages.list.incResolved', {
                              time: fmtRelative(li.started_at, clock.now),
                              d: fmtDuration(li.resolved_at - li.started_at),
                            })}
                      </span>
                    </span>
                  </span>
                {:else if sm}
                  <span class="none"><Icon name="check-circle" size={14} /> {t('pages.list.noIncident')}</span>
                {:else}
                  <span class="muted">—</span>
                {/if}
              </dd>
            </div>
          </dl>
        </div>
      </article>
    {/each}
  </div>
{/if}

<style>
  .ph {
    min-width: 0;
  }
  .lead {
    margin: 4px 0 0;
    color: var(--muted);
    font-size: 0.88rem;
  }
  .lead-bad {
    color: var(--down-text-2);
    font-weight: 600;
  }

  /* Boş durum */
  .empty-hero {
    display: grid;
    grid-template-columns: 300px minmax(0, 1fr);
    gap: 36px;
    align-items: center;
    padding: 36px 40px;
  }
  .eh-art {
    height: 200px;
    opacity: 0.9;
  }
  .eh-b h3 {
    font-size: 1.25rem;
    margin: 0 0 8px;
  }
  .eh-b p {
    margin: 0 0 16px;
    color: var(--text-2);
    max-width: 560px;
  }
  .eh-f {
    list-style: none;
    margin: 0 0 22px;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 8px;
    color: var(--text-2);
    font-size: 0.92rem;
  }
  .eh-f li {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .eh-f :global(svg) {
    color: var(--accent-text);
    flex-shrink: 0;
  }

  /* Sayfa kartları: geniş ekranda yatay satır, dar ekranda dikey kart. */
  .list {
    display: flex;
    flex-direction: column;
    gap: 16px;
  }
  .page {
    display: grid;
    grid-template-columns: 248px minmax(0, 1fr);
    gap: 22px;
    padding: 18px;
    min-width: 0;
    transition: border-color 0.15s;
  }
  @media (hover: hover) {
    .page:hover {
      border-color: var(--border-strong);
    }
  }
  .thumb {
    display: block;
    min-height: 172px;
    border-radius: 12px;
    text-decoration: none;
    color: inherit;
  }
  .thumb:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }
  .draft .thumb {
    opacity: 0.6;
  }
  .body {
    display: flex;
    flex-direction: column;
    gap: 10px;
    min-width: 0;
  }
  .head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 12px;
  }
  .tt {
    min-width: 0;
  }
  h2 {
    font-size: 1.15rem;
    font-weight: 700;
    margin: 0 0 6px;
    overflow-wrap: anywhere;
  }
  h2 a {
    color: var(--text);
  }
  .badges {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-wrap: wrap;
  }
  .badge :global(svg) {
    margin-right: 4px;
  }
  .acts {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-shrink: 0;
  }
  .desc {
    margin: 0;
    font-size: 0.88rem;
    color: var(--text-2);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .links {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }
  .url {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
    max-width: 100%;
    height: 32px;
    padding: 0 4px 0 10px;
    border-radius: 8px;
    background: var(--card-2);
    border: 1px solid var(--border);
    color: var(--muted);
    font-size: 0.86rem;
  }
  .url a {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    min-width: 0;
    color: var(--accent-text);
  }
  .u-t {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .url :global(svg) {
    flex-shrink: 0;
  }
  .u-copy {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 26px;
    height: 26px;
    border-radius: 6px;
    border: none;
    background: none;
    color: var(--muted);
    cursor: pointer;
    flex-shrink: 0;
  }
  .u-copy:focus-visible {
    outline: 2px solid var(--accent);
  }
  @media (hover: hover) {
    .u-copy:hover {
      background: var(--card-hover);
      color: var(--text);
    }
  }

  /* Özet sayıları */
  .stats {
    display: grid;
    grid-template-columns: minmax(150px, 1.3fr) minmax(70px, 0.6fr) minmax(90px, 0.8fr) minmax(90px, 0.8fr) minmax(200px, 2fr);
    gap: 12px 20px;
    margin: auto 0 0;
    padding-top: 14px;
    border-top: 1px solid var(--border);
  }
  .st {
    min-width: 0;
  }
  dt {
    font-size: 0.78rem;
    font-weight: 600;
    color: var(--muted);
    margin-bottom: 6px;
    white-space: nowrap;
  }
  dd {
    margin: 0;
    min-width: 0;
  }
  .num {
    font-size: 1.15rem;
    font-weight: 700;
    font-variant-numeric: tabular-nums;
    color: var(--text);
    line-height: 1.3;
  }
  .num.up-good {
    color: var(--text);
  }
  .num.up-warn {
    color: var(--pending);
  }
  .num.up-bad {
    color: var(--down-text-2);
  }
  .num.up-none {
    color: var(--muted);
  }
  .ov {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    font-weight: 700;
    font-size: 0.95rem;
    line-height: 1.3;
    color: var(--paused-text);
  }
  .ov-ic {
    width: 20px;
    height: 20px;
    border-radius: 50%;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    background: var(--paused);
    color: var(--on-paused);
    flex-shrink: 0;
  }
  .ov-up {
    color: var(--up);
  }
  .ov-up .ov-ic {
    background: var(--up);
    color: var(--on-up);
  }
  .ov-partial {
    color: var(--pending);
  }
  .ov-partial .ov-ic {
    background: var(--pending);
    color: var(--on-pending);
  }
  .ov-down {
    color: var(--down-text-2);
  }
  .ov-down .ov-ic {
    background: var(--down);
    color: var(--on-down);
  }
  .st-sub {
    display: block;
    margin-top: 3px;
    font-size: 0.8rem;
    color: var(--text-2);
  }
  .st-sub.bad {
    color: var(--down-text);
  }
  .inc {
    display: flex;
    align-items: flex-start;
    gap: 8px;
    min-width: 0;
  }
  .inc-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--up);
    margin-top: 7px;
    flex-shrink: 0;
  }
  .inc.ongoing .inc-dot {
    background: var(--down);
    box-shadow: 0 0 0 3px var(--down-soft);
  }
  .inc-b {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .inc-m {
    font-weight: 700;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .inc-w {
    font-size: 0.82rem;
    color: var(--text-2);
  }
  .inc.ongoing .inc-w {
    color: var(--down-text);
  }
  .none {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    color: var(--text-2);
    font-size: 0.9rem;
    line-height: 1.6;
  }
  .none :global(svg) {
    color: var(--up);
  }

  @media (max-width: 1180px) {
    .stats {
      grid-template-columns: repeat(4, minmax(0, 1fr));
    }
    .st-inc {
      grid-column: 1 / -1;
    }
  }
  @media (max-width: 760px) {
    .page {
      grid-template-columns: minmax(0, 1fr);
      gap: 14px;
      padding: 14px;
    }
    .thumb {
      min-height: 0;
      height: 132px;
    }
    .empty-hero {
      grid-template-columns: minmax(0, 1fr);
      padding: 24px 18px;
      gap: 20px;
    }
    .eh-art {
      height: 160px;
    }
    .stats {
      grid-template-columns: repeat(3, minmax(0, 1fr));
      gap: 12px 14px;
    }
    .st-status {
      grid-column: 1 / -1;
      display: flex;
      flex-wrap: wrap;
      align-items: baseline;
      column-gap: 12px;
    }
    .st-status dt {
      flex-basis: 100%;
    }
    .st-status dd {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      column-gap: 12px;
    }
    .st-status .st-sub {
      margin-top: 0;
    }
    dt {
      font-size: 0.74rem;
    }
    .num {
      font-size: 1.05rem;
    }
    .desc {
      white-space: normal;
      display: -webkit-box;
      -webkit-line-clamp: 2;
      line-clamp: 2;
      -webkit-box-orient: vertical;
    }
  }
  @media (max-width: 480px) {
    .hide-sm {
      display: none;
    }
    h2 {
      font-size: 1.05rem;
    }
  }
</style>
