<script lang="ts">
  // Hızlı tema düğmesi: açık ↔ koyu. Oturum varsa tercih hesaba da yazılır
  // (her cihazda geçerli); giriş ekranında yalnızca bu cihazda.
  import { api } from '../lib/api';
  import { session } from '../lib/session.svelte';
  import { theme } from '../lib/theme.svelte';
  import { t } from '../lib/i18n';
  import Icon from './Icon.svelte';

  let { size = 16 }: { size?: number } = $props();

  async function toggle() {
    const next = theme.toggle();
    if (!session.user) return;
    try {
      const res = await api.setPreferences({ theme: next });
      session.set(res.user);
    } catch {
      /* tercih bu cihazda kaldı; sonraki girişte hesaptaki geçerli olur */
    }
  }
</script>

<button
  type="button"
  class="theme-btn"
  onclick={toggle}
  aria-label={theme.current === 'light' ? t('account.theme.toDark') : t('account.theme.toLight')}
  title={theme.current === 'light' ? t('account.theme.toDark') : t('account.theme.toLight')}
>
  <Icon name={theme.current === 'light' ? 'moon' : 'sun'} {size} />
</button>

<style>
  .theme-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 32px;
    height: 30px;
    border: 1px solid var(--border);
    border-radius: 8px;
    background: color-mix(in srgb, var(--bg) 60%, transparent);
    color: var(--muted);
    cursor: pointer;
    padding: 0;
  }
  @media (hover: hover) {
    .theme-btn:hover {
      color: var(--text);
    }
  }
  .theme-btn:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }
  @media (pointer: coarse) {
    .theme-btn {
      width: 44px;
      height: 36px;
    }
  }
</style>
