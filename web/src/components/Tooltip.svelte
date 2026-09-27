<script lang="ts">
  // data-tip özniteliği olan her öğe için tek bir paylaşılan ipucu kutusu.
  // Satır başına bileşen oluşturmadığı için yüzlerce çubukta da hafiftir.
  let text = $state('');
  let x = $state(0);
  let y = $state(0);
  let ready = $state(false);
  let el: HTMLDivElement | undefined = $state();
  let target: Element | null = null;

  function place(t: Element) {
    const r = t.getBoundingClientRect();
    const w = el?.offsetWidth ?? 160;
    const h = el?.offsetHeight ?? 40;
    let left = r.left + r.width / 2 - w / 2;
    left = Math.max(8, Math.min(left, window.innerWidth - w - 8));
    let top = r.top - h - 8;
    if (top < 8) top = r.bottom + 8;
    x = left;
    y = top;
    ready = true;
  }

  function over(e: PointerEvent) {
    if (e.pointerType === 'touch') return;
    const t = (e.target as Element | null)?.closest?.('[data-tip]') ?? null;
    if (t === target) return;
    target = t;
    if (!t) {
      text = '';
      return;
    }
    ready = false;
    text = t.getAttribute('data-tip') ?? '';
    requestAnimationFrame(() => target === t && place(t));
  }

  function hide() {
    target = null;
    text = '';
  }
</script>

<svelte:document onpointerover={over} onscrollcapture={hide} onpointerdown={hide} />

{#if text}
  <div class="tip" bind:this={el} style="left:{x}px;top:{y}px;visibility:{ready ? 'visible' : 'hidden'}" role="tooltip">{text}</div>
{/if}
