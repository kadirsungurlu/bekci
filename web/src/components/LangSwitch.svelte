<script lang="ts">
  // Giriş/kurulum ekranındaki küçük dil seçici. Seçim tarayıcıda hatırlanır;
  // hesapta dil tercihi varsa girişten sonra o geçerli olur (Ayarlar › Hesabım).
  import { i18n, LOCALES, setLocale, t } from '../lib/i18n';
</script>

<div class="langs" role="group" aria-label={t('auth.language')}>
  {#each LOCALES as l, i (l)}
    {#if i > 0}<span class="sep" aria-hidden="true">·</span>{/if}
    <button type="button" class="lang" class:active={i18n.locale === l} aria-pressed={i18n.locale === l} lang={l} onclick={() => setLocale(l)}>
      {t(`common.languages.${l}`)}
    </button>
  {/each}
</div>

<style>
  .langs {
    display: flex;
    align-items: center;
    gap: 2px;
    margin-top: 16px;
    font-size: 0.86rem;
  }
  .lang {
    background: none;
    border: none;
    font: inherit;
    color: var(--muted);
    padding: 8px 10px;
    min-height: 40px;
    border-radius: 8px;
    cursor: pointer;
  }
  .lang.active {
    color: var(--text);
    font-weight: 700;
  }
  @media (hover: hover) {
    .lang:hover {
      color: var(--text);
    }
  }
  .sep {
    color: var(--muted);
  }
</style>
