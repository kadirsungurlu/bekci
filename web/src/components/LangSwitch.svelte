<script lang="ts">
  // Hızlı dil seçici (TR | EN): menüde seçim hesaba kaydedilir (her cihazda
  // geçerli) ve arayüz anında değişir; giriş ekranında yalnızca bu cihazda. Ayrıntılı seçenek (tarayıcı dili)
  // Ayarlar → Hesabım'da.
  import { api, errorMessage } from '../lib/api';
  import { session } from '../lib/session.svelte';
  import { live } from '../lib/live.svelte';
  import { toast } from '../lib/ui.svelte';
  import { i18n, LOCALES, setLocale, t, type Locale } from '../lib/i18n';

  let busy = $state(false);

  async function pick(l: Locale) {
    if (busy || l === i18n.locale) return;
    const prev = i18n.locale;
    setLocale(l);
    // Oturum yok (giriş ekranı): seçim yalnızca bu cihazda hatırlanır. Girişte
    // hesabın kayıtlı dili yoksa bu seçim geçerli kalır (App → deviceLocale).
    if (!session.user) return;
    busy = true;
    try {
      const res = await api.setPreferences({ lang: l });
      session.set(res.user);
      // Sunucudan gelen metinler (kontrol mesajları, olaylar) de yeni dilde gelsin.
      live.relocalize();
    } catch (err) {
      setLocale(prev);
      toast.error(errorMessage(err));
    } finally {
      busy = false;
    }
  }
</script>

<div class="lang" role="group" aria-label={t('account.language.label')}>
  {#each LOCALES as l (l)}
    <button
      type="button"
      class:on={i18n.locale === l}
      aria-pressed={i18n.locale === l}
      disabled={busy}
      lang={l}
      title={t(`common.languages.${l}`)}
      onclick={() => pick(l)}>{l.toUpperCase()}</button
    >
  {/each}
</div>

<style>
  .lang {
    display: inline-flex;
    padding: 2px;
    gap: 2px;
    border: 1px solid var(--border);
    border-radius: 8px;
    background: color-mix(in srgb, var(--bg) 60%, transparent);
  }
  button {
    min-width: 34px;
    height: 26px;
    padding: 0 8px;
    border: 0;
    border-radius: 6px;
    background: none;
    color: var(--muted);
    font: inherit;
    font-size: 0.76rem;
    font-weight: 700;
    letter-spacing: 0.03em;
    cursor: pointer;
  }
  button:hover:not(.on) {
    color: var(--text);
  }
  button.on {
    background: var(--accent-soft, color-mix(in srgb, var(--accent) 18%, transparent));
    color: var(--accent);
  }
  button:disabled {
    cursor: default;
  }
  button:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }
  @media (pointer: coarse) {
    button {
      height: 36px;
      min-width: 44px;
    }
  }
</style>
