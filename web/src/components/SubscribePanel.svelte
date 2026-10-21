<script lang="ts">
  // Genel durum sayfasında "Abone ol" düğmesi ve açılır e-posta paneli.
  // Sunucu her durumda aynı yanıtı verir (adresin kayıtlı olup olmadığı sızmaz);
  // yalnızca 429 (çok deneme) ve 400 (geçersiz e-posta) ayrı gösterilir.
  // Esc ve panel dışına tıklama kapatır; açılınca odak e-posta alanına gider.
  import { tick } from 'svelte';
  import { api, ApiError } from '../lib/api';
  import Icon from './Icon.svelte';
  import { tIn, type Locale, type TKey, type TParams } from '../lib/i18n';

  let { slug, lang }: { slug: string; lang: Locale } = $props();
  const t = (key: TKey, params?: TParams) => tIn(lang, key, params);

  let open = $state(false);
  let email = $state('');
  let sending = $state(false);
  let result = $state<{ kind: 'sent' | 'error'; text: string } | null>(null);
  let root = $state<HTMLDivElement>();
  let btn = $state<HTMLButtonElement>();
  let input = $state<HTMLInputElement>();
  const panelId = 'sub-panel';

  async function show() {
    open = true;
    await tick();
    input?.focus();
  }

  function close(returnFocus = true) {
    if (!open) return;
    open = false;
    if (returnFocus) btn?.focus();
  }

  function onOutside(e: PointerEvent) {
    if (open && root && !root.contains(e.target as Node)) close(false);
  }

  function onKey(e: KeyboardEvent) {
    if (e.key === 'Escape' && open) {
      e.preventDefault();
      close();
    }
  }

  const validEmail = (v: string) => /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(v) && v.length <= 254;

  async function submit(e: SubmitEvent) {
    e.preventDefault();
    const v = email.trim();
    if (!validEmail(v)) {
      result = { kind: 'error', text: t('pub.subscribe.errEmail') };
      input?.focus();
      return;
    }
    sending = true;
    result = null;
    try {
      await api.publicSubscribe(slug, v, lang);
      email = '';
      result = { kind: 'sent', text: t('pub.subscribe.sent') };
    } catch (err) {
      if (err instanceof ApiError && err.status === 429) result = { kind: 'error', text: t('pub.subscribe.errRate') };
      else if (err instanceof ApiError && err.status === 400) result = { kind: 'error', text: t('pub.subscribe.errEmail') };
      // Diğer durumlarda da (sayfa kapandı, ağ) adres hakkında bilgi verilmez; nötr metin.
      else result = { kind: 'sent', text: t('pub.subscribe.sent') };
    } finally {
      sending = false;
    }
  }
</script>

<svelte:document onpointerdown={onOutside} />

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div class="sub" bind:this={root} onkeydown={onKey}>
  <button
    type="button"
    class="sub-btn"
    data-testid="sub-button"
    aria-expanded={open}
    aria-controls={panelId}
    aria-haspopup="dialog"
    bind:this={btn}
    onclick={() => (open ? close() : show())}
  >
    <Icon name="mail" size={15} />
    <span>{t('pub.subscribe.button')}</span>
  </button>

  {#if open}
    <div class="sub-panel" id={panelId} role="dialog" aria-labelledby="sub-title" data-testid="sub-panel">
      <div class="sub-head">
        <h2 id="sub-title">{t('pub.subscribe.title')}</h2>
        <button type="button" class="sub-x" aria-label={t('pub.subscribe.close')} onclick={() => close()}>
          <Icon name="x" size={16} />
        </button>
      </div>
      <p class="sub-text">{t('pub.subscribe.text')}</p>
      <form class="sub-form" onsubmit={submit} novalidate>
        <label class="sr" for="sub-email">{t('pub.subscribe.email')}</label>
        <input
          id="sub-email"
          class="input"
          type="email"
          inputmode="email"
          autocomplete="email"
          autocapitalize="none"
          spellcheck="false"
          maxlength="254"
          placeholder={t('pub.subscribe.placeholder')}
          data-testid="sub-email"
          aria-invalid={result?.kind === 'error' ? 'true' : undefined}
          aria-describedby={result ? 'sub-result' : undefined}
          bind:this={input}
          bind:value={email}
        />
        <button class="btn primary" type="submit" disabled={sending} data-testid="sub-submit">
          {#if sending}<span class="spinner"></span>{/if}
          {t('pub.subscribe.submit')}
        </button>
      </form>
      <div id="sub-result" role="status" aria-live="polite">
        {#if result}
          <p class="sub-result {result.kind}" data-testid="sub-result" data-state={result.kind}>
            <Icon name={result.kind === 'sent' ? 'check-circle' : 'alert-circle'} size={15} />
            <span>{result.text}</span>
          </p>
        {/if}
      </div>
    </div>
  {/if}
</div>

<style>
  .sub {
    position: relative;
    flex: none;
  }
  .sub-btn {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    height: 34px;
    padding: 0 14px;
    border-radius: 9px;
    border: 1px solid var(--accent-border);
    background: var(--accent-soft);
    color: var(--accent-text);
    font: inherit;
    font-size: 0.86rem;
    font-weight: 600;
    cursor: pointer;
    white-space: nowrap;
  }
  .sub-btn:hover,
  .sub-btn[aria-expanded='true'] {
    border-color: var(--accent);
  }
  .sub-btn:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }
  .sub-panel {
    position: absolute;
    top: calc(100% + 8px);
    right: 0;
    z-index: 30;
    width: 340px;
    max-width: calc(100vw - 32px);
    padding: 16px;
    background: var(--card);
    border: 1px solid var(--border);
    border-radius: 14px;
    box-shadow: var(--pub-shadow);
    color: var(--text);
    text-align: left;
  }
  .sub-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
  }
  .sub-head h2 {
    margin: 0;
    font-size: 1rem;
    font-weight: 650;
  }
  .sub-x {
    display: grid;
    place-items: center;
    width: 30px;
    height: 30px;
    margin: -6px -6px -6px 0;
    border: 0;
    border-radius: 8px;
    background: none;
    color: var(--muted);
    cursor: pointer;
  }
  .sub-x:hover {
    color: var(--text);
    background: var(--bg-elev);
  }
  .sub-x:focus-visible {
    outline: 2px solid var(--accent);
  }
  .sub-text {
    margin: 6px 0 12px;
    font-size: 0.85rem;
    color: var(--text-2);
  }
  .sub-form {
    display: flex;
    gap: 8px;
  }
  .sub-form .input {
    flex: 1;
    min-width: 0;
  }
  .sub-form .btn {
    flex: none;
  }
  .sub-result {
    display: flex;
    align-items: flex-start;
    gap: 7px;
    margin: 12px 0 0;
    font-size: 0.85rem;
    line-height: 1.4;
    overflow-wrap: anywhere;
  }
  .sub-result :global(svg) {
    flex: none;
    margin-top: 2px;
  }
  .sub-result.sent {
    color: var(--up);
  }
  .sub-result.error {
    color: var(--down);
  }
  .sr {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip: rect(0 0 0 0);
    white-space: nowrap;
  }

  /* Dar alan: panel üst çubuğun tüm genişliğini kaplar (konum .top-in'e göre). */
  @container pubv (max-width: 640px) {
    .sub {
      position: static;
    }
    /* Dokunmatik hedef en az 44 px (kapatma 40 px, köşe hizası negatif boşlukla). */
    .sub-btn {
      height: 44px;
    }
    .sub-x {
      width: 40px;
      height: 40px;
      margin: -10px -10px -10px 0;
    }
    .sub-panel {
      top: calc(100% - 6px);
      left: 16px;
      right: 16px;
      width: auto;
      max-width: none;
    }
  }
  @media (max-width: 380px) {
    .sub-form {
      flex-direction: column;
    }
    /* Sütunda flex: 1 yüksekliği daraltır. */
    .sub-form .input {
      flex: none;
    }
  }
</style>
