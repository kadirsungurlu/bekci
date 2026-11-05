<script lang="ts">
  // Durum işareti: renge ek olarak şekil (işaret) ve metin (ekran okuyucu, ipucu) verir.
  // Topoloji kartları, sayaç rozetleri ve sunucu zaman çizelgesi aynı işareti kullanır.
  import { t } from '../lib/i18n';
  import type { Cls } from '../lib/topomodel';

  let { cls, size = 14, label }: { cls: Cls; size?: number; label?: string } = $props();

  const GLYPH: Record<Cls, string> = {
    ok: 'M4.2 8.4 6.9 11 11.8 5.4',
    warn: 'M8 3.8v5M8 12v.1',
    bad: 'M5 5l6 6M11 5l-6 6',
    pend: 'M4.4 8h.01M8 8h.01M11.6 8h.01',
    off: 'M6 5v6M10 5v6',
    maint: 'M8 4.2V8l2.6 1.6',
  };
  const text = $derived(label ?? t(`topology.cls.${cls}`));
</script>

<span class="sm {cls}" style={`--s:${size}px`} role="img" aria-label={text} title={text}>
  <svg viewBox="0 0 16 16" width={size} height={size} fill="none" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
    <path d={GLYPH[cls]} />
  </svg>
</span>

<style>
  .sm {
    --bg: var(--paused);
    --fg: var(--on-paused);
    display: inline-flex;
    flex: none;
    width: var(--s);
    height: var(--s);
    border-radius: 50%;
    background: var(--bg);
  }
  .sm path {
    stroke: var(--fg);
  }
  .sm.ok {
    --bg: var(--up);
    --fg: var(--on-up);
  }
  .sm.warn,
  .sm.pend {
    --bg: var(--pending);
    --fg: var(--on-pending);
  }
  .sm.bad {
    --bg: var(--down);
    --fg: var(--on-down);
  }
  .sm.maint {
    --bg: var(--maint);
    --fg: var(--on-maint);
  }
</style>
