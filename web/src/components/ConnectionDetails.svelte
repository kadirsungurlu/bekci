<script lang="ts">
  // HTTP dışı monitörlerin başarısızlık tanısı (olay ayrıntısı): özet satırı,
  // hedef/aşama/hata türü, IP başına bağlantı denemeleri, ping ve DNS yanıtı,
  // ham hata. Sunucu kod taşır (phase, error_class, result…); etiketler ve
  // özet cümlesi burada dile göre kurulur.
  import type { CheckDetail, ConnDiag } from '../lib/api';
  import { fmtMs } from '../lib/format';
  import { intlLocale, t, tOr, type TKey } from '../lib/i18n';
  import Icon from './Icon.svelte';
  import CopyButton from './CopyButton.svelte';

  let { detail, type, meta = '', fallbackError = '' }: { detail: CheckDetail; type: string; meta?: string; fallbackError?: string } =
    $props();

  const g = $derived<ConnDiag>(detail.diag ?? {});
  const kind = $derived(detail.kind || type);

  const phaseName = (p?: string) => (p ? tOr(`incidents.conn.phases.${p}`, p) : '');
  const className = (c?: string) => (c ? tOr(`incidents.conn.classes.${c}`, c) : '');

  /** Kesirli milisaniye: 0,42 ms / 12 ms. */
  function fmtRtt(ms?: number): string {
    if (ms === undefined || ms === null) return '—';
    if (ms > 0 && ms < 10) return `${ms.toLocaleString(intlLocale(), { maximumFractionDigits: 2 })} ms`;
    return fmtMs(ms);
  }
  /** Süre: 1 sn altı "850 ms", üstü "4 sn" / "2,5 sn" (zaman aşımı 3999 ms ölçülür → "4 sn"). */
  function fmtDur(ms?: number): string {
    if (ms === undefined || ms === null) return '—';
    if (ms < 1000) return fmtMs(ms);
    const s = Math.round(ms / 100) / 10;
    return t('status.time.sec', { n: s.toLocaleString(intlLocale(), { maximumFractionDigits: 1 }) });
  }
  const fmtTimeout = (ms: number) => fmtDur(ms);

  /** Hedefin sunucu adı (host:port, user@host:port/db, şema://host:port/…). */
  function hostOf(target: string): string {
    let s = target.replace(/^[a-z][a-z0-9+.-]*:\/\//i, '');
    s = s.replace(/^[^@/]*@/, '').split(/[/?,]/)[0];
    const m = s.match(/^\[([^\]]+)\]/);
    if (m) return m[1];
    return s.includes(':') && s.indexOf(':') === s.lastIndexOf(':') ? s.split(':')[0] : s;
  }
  function portOf(g: ConnDiag): number | string {
    if (g.port) return g.port;
    const src = g.dns?.server ?? g.target ?? '';
    const m = src.match(/:(\d+)(?:[/?]|$)/);
    return m ? Number(m[1]) : '?';
  }

  const pingOk = $derived(!!g.ping && !g.ping.error && g.ping.received > 0);
  const pingNo = $derived(!!g.ping && !g.ping.error && g.ping.sent > 0 && g.ping.received === 0);
  const attempts = $derived(g.attempts ?? []);
  const okN = $derived(attempts.filter((a) => a.result === 'ok').length);

  /** Tanıdan tek satırlık özet (en olası neden). */
  const verdict = $derived.by((): string => {
    const V = (k: string, p?: Record<string, string | number>) => t(`incidents.conn.verdict.${k}` as TKey, p);
    const cls = g.error_class ?? '';
    const phase = phaseName(g.phase) || phaseName('connect');
    const port = portOf(g);
    const host = hostOf(g.target ?? '');
    if (kind === 'dns' && g.dns) {
      const d = g.dns;
      if (cls === 'rcode') {
        if (d.rcode === 'NXDOMAIN') return V('dnsNx', { query: d.query });
        if (d.rcode === 'SERVFAIL') return V('dnsServfail');
        if (d.rcode === 'REFUSED') return V('dnsRefused');
        return V('dnsRcode', { rcode: d.rcode ?? '?' });
      }
      if (cls === 'no_record') return V('dnsNoRecord', { query: d.query, type: d.type });
      if (cls === 'mismatch') return V('dnsMismatch');
      if (cls === 'timeout') return pingOk ? V('dnsTimeoutPing', { port }) : V('dnsTimeout', { server: d.server });
    }
    if (kind === 'ping' && g.ping) {
      if (g.ping.error === 'unavailable') return V('pingUnavailable');
      if (g.ping.sent > 0 && g.ping.received === 0) return V('pingDown');
      if (g.ping.loss_pct > 0) return V('pingLoss', { loss: g.ping.loss_pct });
    }
    if (okN > 0 && okN < attempts.length) return V('partial');
    const timeout = g.timeout_ms ? fmtTimeout(g.timeout_ms) : '—';
    switch (cls) {
      case 'dns_notfound':
        return V('dnsNotFound', { host });
      case 'dns_error':
        return V('dnsError', { host });
      case 'refused':
        return pingOk ? V('portClosed', { port }) : V('portRefused', { port });
      case 'timeout':
        if (okN > 0) return V('portOpenSlow', { timeout, phase });
        if (g.phase && g.phase !== 'connect') return V('slow', { timeout, phase });
        if (pingOk) return V('portFiltered', { port });
        if (pingNo) return V('portFilteredNoPing');
        return V('portTimeout', { port });
      case 'unreachable':
        return V('unreachable');
      case 'reset':
      case 'closed':
        return V('reset', { phase });
      case 'tls_error':
        return V('tls');
      case 'auth':
        return V('auth');
      case 'not_found':
        return V('notFound', { phase });
      case 'mismatch':
      case 'unhealthy':
        return V('mismatch');
    }
    return cls ? V('generic', { phase, cls: className(cls) }) : '';
  });

  const rawError = $derived(g.raw_error || fallbackError);
</script>

<div class="card conn">
  <div class="card-head">
    <h2 class="card-title">{t('incidents.conn.title')}<span class="dot">.</span></h2>
    {#if g.error_class}
      <span class="cls">{className(g.error_class)}</span>
    {/if}
  </div>

  {#if verdict}
    <div class="verdict">
      <Icon name="info" size={17} />
      <span>{verdict}</span>
    </div>
  {/if}

  <dl class="kv">
    {#if g.target}
      <dt>{t('incidents.conn.target')}</dt>
      <dd class="mono">{g.target}</dd>
    {/if}
    {#if g.resolved?.length || g.resolve_error}
      <dt>{t('incidents.conn.resolved')}</dt>
      <dd>
        {#if g.resolved?.length}
          <span class="mono">{g.resolved.join(', ')}</span>
          {#if g.resolve_ms}<span class="muted small"> · {t('incidents.conn.resolveMs', { ms: fmtMs(g.resolve_ms) })}</span>{/if}
        {:else}
          <span class="bad">{t('incidents.conn.resolveFailed')}</span>
          <span class="muted small"> · {className(g.resolve_error)}</span>
        {/if}
      </dd>
    {/if}
    {#if g.phase}
      <dt>{t('incidents.conn.phase')}</dt>
      <dd>{phaseName(g.phase)}</dd>
    {/if}
    {#if g.timeout_ms}
      <dt>{t('incidents.conn.timeout')}</dt>
      <dd>
        {fmtTimeout(g.timeout_ms)}
        {#if g.elapsed_ms !== undefined}<span class="muted small"> · {t('incidents.conn.elapsed')}: {fmtDur(g.elapsed_ms)}</span>{/if}
      </dd>
    {/if}
    {#if g.banner}
      <dt>{t('incidents.conn.banner')}</dt>
      <dd class="mono">{g.banner}</dd>
    {/if}
  </dl>

  {#if attempts.length}
    <h3 class="sub-h">{t('incidents.conn.attempts')}</h3>
    <table class="att">
      <thead>
        <tr>
          <th>{t('incidents.conn.colIp')}</th>
          <th>{t('incidents.conn.colResult')}</th>
          <th class="num">{t('incidents.conn.colTime')}</th>
        </tr>
      </thead>
      <tbody>
        {#each attempts as a, i (i)}
          <tr>
            <td class="mono">{a.ip}</td>
            <td>
              <span class="res" class:ok={a.result === 'ok'} title={a.error ?? ''}>{className(a.result)}</span>
            </td>
            <td class="num">{fmtDur(a.elapsed_ms)}</td>
          </tr>
        {/each}
      </tbody>
    </table>
  {/if}

  {#if g.ping}
    <h3 class="sub-h">{t('incidents.conn.ping')} <span class="muted mono small">{g.ping.target}</span></h3>
    {#if g.ping.error}
      <div class="note">
        <Icon name="alert-circle" size={15} />
        <span>
          {g.ping.error === 'unavailable' ? t('incidents.conn.pingUnavailable') : t('incidents.conn.pingFailed')}
          {#if g.ping.raw_error}<span class="mono small muted"> ({g.ping.raw_error})</span>{/if}
        </span>
      </div>
    {:else}
      <div class="stats">
        <div><span class="sl">{t('incidents.conn.pingSent')}</span><span class="sv">{g.ping.sent}</span></div>
        <div><span class="sl">{t('incidents.conn.pingReceived')}</span><span class="sv" class:bad={g.ping.received === 0} class:good={g.ping.received > 0}>{g.ping.received}</span></div>
        <div><span class="sl">{t('incidents.conn.pingLoss')}</span><span class="sv" class:bad={g.ping.loss_pct > 0}>{g.ping.loss_pct}%</span></div>
        <div class="wide">
          {#if g.ping.received === 1}
            <span class="sl">{t('incidents.conn.pingRttOne')}</span>
            <span class="sv">{fmtRtt(g.ping.avg_ms)}</span>
          {:else}
            <span class="sl">{t('incidents.conn.pingRtt')}</span>
            <span class="sv">{g.ping.received ? `${fmtRtt(g.ping.min_ms)} / ${fmtRtt(g.ping.avg_ms)} / ${fmtRtt(g.ping.max_ms)}` : '—'}</span>
          {/if}
        </div>
      </div>
    {/if}
  {/if}

  {#if g.dns}
    <h3 class="sub-h">{t('incidents.conn.dns')}</h3>
    <dl class="kv">
      <dt>{t('incidents.conn.dnsQuery')}</dt>
      <dd class="mono">{g.dns.query} <span class="muted">{g.dns.type}</span></dd>
      <dt>{t('incidents.conn.dnsServer')}</dt>
      <dd class="mono">{g.dns.server}{g.dns.transport ? ` (${g.dns.transport.toUpperCase()})` : ''}</dd>
      {#if g.dns.rcode}
        <dt>{t('incidents.conn.dnsRcode')}</dt>
        <dd>
          <span class="res" class:ok={g.dns.rcode === 'NOERROR'}>{g.dns.rcode === 'timeout' ? className('timeout') : g.dns.rcode}</span>
          {#if g.dns.elapsed_ms}<span class="muted small"> · {fmtDur(g.dns.elapsed_ms)}</span>{/if}
        </dd>
      {/if}
      {#if g.dns.rcode && g.dns.rcode !== 'timeout'}
        <dt>{t('incidents.conn.dnsAnswers')}</dt>
        <dd>
          {#if g.dns.answers?.length}
            <ul class="answers mono">
              {#each g.dns.answers as a, i (i)}<li>{a}</li>{/each}
            </ul>
          {:else}
            <span class="muted">{t('incidents.conn.dnsNoAnswers')}</span>
          {/if}
        </dd>
      {/if}
    </dl>
  {/if}

  {#if rawError}
    <div class="raw-h">
      <h3 class="sub-h">{t('incidents.conn.rawError')}</h3>
      <CopyButton class="btn sm icon" iconOnly text={() => rawError} />
    </div>
    <pre class="raw">{rawError}</pre>
  {/if}

  {#if meta}<div class="cap-meta muted small">{meta}</div>{/if}
</div>

<style>
  .card-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px 12px;
    flex-wrap: wrap;
    margin-bottom: 14px;
  }
  .card-head .card-title {
    margin: 0;
  }
  .cls {
    font-size: 0.78rem;
    font-weight: 700;
    padding: 3px 9px;
    border-radius: 999px;
    background: var(--down-soft);
    color: var(--down-text);
    border: 1px solid var(--down-border);
    white-space: nowrap;
  }
  .verdict {
    display: flex;
    align-items: flex-start;
    gap: 10px;
    padding: 12px 14px;
    border-radius: var(--radius-sm);
    background: var(--accent-soft);
    border: 1px solid var(--accent-border);
    color: var(--text);
    font-size: 0.9rem;
    line-height: 1.45;
    margin-bottom: 14px;
  }
  .verdict :global(svg) {
    flex-shrink: 0;
    margin-top: 1px;
    color: var(--accent-text);
  }
  .kv {
    margin: 0;
    display: grid;
    grid-template-columns: minmax(110px, max-content) minmax(0, 1fr);
    gap: 7px 14px;
    font-size: 0.86rem;
    align-items: baseline;
  }
  .kv dt {
    color: var(--text-2);
    font-weight: 600;
  }
  .kv dd {
    margin: 0;
    min-width: 0;
    word-break: break-word;
  }
  .mono {
    font-family: var(--mono);
    font-size: 0.82rem;
  }
  .bad {
    color: var(--down-text);
  }
  .good {
    color: var(--up-text);
  }
  .sub-h {
    font-size: 0.78rem;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--muted);
    margin: 18px 0 8px;
    display: flex;
    align-items: baseline;
    gap: 8px;
    flex-wrap: wrap;
  }
  .sub-h .mono {
    text-transform: none;
    letter-spacing: 0;
    font-weight: 500;
  }
  .att {
    width: 100%;
    border-collapse: collapse;
    font-size: 0.84rem;
    background: var(--input);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    overflow: hidden;
  }
  .att th,
  .att td {
    text-align: left;
    padding: 7px 12px;
    border-bottom: 1px solid var(--border);
  }
  .att tbody tr:last-child td {
    border-bottom: none;
  }
  .att th {
    color: var(--muted);
    font-size: 0.74rem;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  .att td.mono {
    word-break: break-all;
  }
  .num {
    text-align: right !important;
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }
  .res {
    display: inline-block;
    font-size: 0.76rem;
    font-weight: 700;
    padding: 2px 8px;
    border-radius: 999px;
    background: var(--down-soft);
    color: var(--down-text);
    border: 1px solid var(--down-border);
    white-space: nowrap;
  }
  .res.ok {
    background: var(--up-soft);
    color: var(--up-text);
    border-color: var(--up-border);
  }
  .stats {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr)) minmax(0, 1.6fr);
    gap: 8px;
  }
  .stats > div {
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 8px 10px;
    background: var(--input);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    min-width: 0;
  }
  .sl {
    font-size: 0.72rem;
    color: var(--muted);
    font-weight: 600;
  }
  .sv {
    font-weight: 700;
    font-variant-numeric: tabular-nums;
    font-size: 0.92rem;
    overflow-wrap: anywhere;
  }
  .note {
    display: flex;
    align-items: flex-start;
    gap: 8px;
    font-size: 0.86rem;
    color: var(--text-2);
    padding: 10px 12px;
    background: var(--input);
    border: 1px dashed var(--border-strong);
    border-radius: var(--radius-sm);
    word-break: break-word;
  }
  .note :global(svg) {
    flex-shrink: 0;
    margin-top: 2px;
  }
  .answers {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
    word-break: break-all;
  }
  .raw-h {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    margin-top: 18px;
    margin-bottom: 8px;
  }
  .raw-h .sub-h {
    margin: 0;
  }
  .raw {
    font-family: var(--mono);
    font-size: 0.8rem;
    background: var(--input);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 10px 12px;
    margin: 0;
    white-space: pre-wrap;
    word-break: break-word;
    max-height: 220px;
    overflow: auto;
    color: var(--text);
  }
  .cap-meta {
    margin-top: 12px;
    word-break: break-word;
  }
  @media (max-width: 520px) {
    .kv {
      grid-template-columns: minmax(0, 1fr);
      gap: 2px;
    }
    .kv dd {
      margin-bottom: 8px;
    }
    .stats {
      grid-template-columns: repeat(3, minmax(0, 1fr));
    }
    .stats .wide {
      grid-column: 1 / -1;
    }
  }
</style>
