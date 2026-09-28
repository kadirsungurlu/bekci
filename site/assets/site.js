/* Bekci tanıtım sitesi: tema, mobil menü, kopyala düğmeleri, ekran görüntüsü
   ışık kutusu. Dış bağımlılık yok. <head> içinde yüklenir ki tema ilk boyamadan
   önce uygulansın; geri kalanı DOMContentLoaded'da bağlanır. */
(function () {
  'use strict';
  var KEY = 'bekci-tema';
  var root = document.documentElement;
  var media = window.matchMedia ? window.matchMedia('(prefers-color-scheme: dark)') : null;
  // Betik çalışıyor: yalnızca betikle çalışan öğeler (ör. belge sekmeleri) buna göre çizilir.
  root.classList.add('js');

  function stored() {
    try {
      var v = window.localStorage.getItem(KEY);
      return v === 'dark' || v === 'light' ? v : null;
    } catch (e) {
      return null;
    }
  }
  function save(v) {
    try {
      window.localStorage.setItem(KEY, v);
    } catch (e) {
      /* gizli pencere vb.: tercih yalnızca bu sayfa için geçerli olur */
    }
  }
  function current() {
    return stored() || (media && media.matches ? 'dark' : 'light');
  }
  function apply(theme, explicit) {
    if (explicit) root.setAttribute('data-theme', theme);
    root.setAttribute('data-theme-now', theme);
  }

  var s = stored();
  apply(current(), !!s);
  if (media && media.addEventListener) {
    media.addEventListener('change', function () {
      if (!stored()) apply(current(), false);
    });
  }

  var tr = (root.getAttribute('lang') || 'tr').indexOf('tr') === 0;
  var T = tr
    ? { copied: 'Kopyalandı', copy: 'Kopyala', toDark: 'Koyu temaya geç', toLight: 'Açık temaya geç', failed: 'Kopyalanamadı' }
    : { copied: 'Copied', copy: 'Copy', toDark: 'Switch to dark theme', toLight: 'Switch to light theme', failed: 'Copy failed' };

  function ready(fn) {
    if (document.readyState !== 'loading') fn();
    else document.addEventListener('DOMContentLoaded', fn);
  }

  ready(function () {
    /* ---- Tema düğmesi ---- */
    var toggle = document.querySelector('.theme-toggle');
    function label() {
      if (!toggle) return;
      var dark = root.getAttribute('data-theme-now') === 'dark';
      toggle.setAttribute('aria-label', dark ? T.toLight : T.toDark);
      toggle.setAttribute('title', dark ? T.toLight : T.toDark);
    }
    if (toggle) {
      label();
      toggle.addEventListener('click', function () {
        var next = root.getAttribute('data-theme-now') === 'dark' ? 'light' : 'dark';
        apply(next, true);
        save(next);
        label();
      });
      if (media && media.addEventListener) media.addEventListener('change', label);
    }

    /* ---- Mobil menü ---- */
    var menuBtn = document.querySelector('.menu-toggle');
    var menu = document.getElementById('mobil-menu');
    function closeMenu(focusBtn) {
      if (!menuBtn || !menu || menu.hidden) return;
      menu.hidden = true;
      menuBtn.setAttribute('aria-expanded', 'false');
      if (focusBtn) menuBtn.focus();
    }
    if (menuBtn && menu) {
      menuBtn.addEventListener('click', function () {
        var open = menu.hidden;
        menu.hidden = !open;
        menuBtn.setAttribute('aria-expanded', open ? 'true' : 'false');
      });
      menu.addEventListener('click', function (e) {
        if (e.target.closest('a')) closeMenu(false);
      });
      document.addEventListener('keydown', function (e) {
        if (e.key === 'Escape') closeMenu(true);
      });
      window.addEventListener('resize', function () {
        if (window.innerWidth >= 960) closeMenu(false);
      });
    }

    /* ---- Kopyala ---- */
    var live = document.getElementById('duyuru');
    function announce(msg) {
      if (!live) return;
      live.textContent = '';
      window.setTimeout(function () {
        live.textContent = msg;
      }, 30);
    }
    function fallbackCopy(text) {
      var ta = document.createElement('textarea');
      ta.value = text;
      ta.setAttribute('readonly', '');
      ta.className = 'sr-only';
      document.body.appendChild(ta);
      ta.select();
      var ok = false;
      try {
        ok = document.execCommand('copy');
      } catch (e) {
        ok = false;
      }
      document.body.removeChild(ta);
      return ok;
    }
    document.querySelectorAll('[data-copy]').forEach(function (btn) {
      btn.addEventListener('click', function () {
        var target = document.getElementById(btn.getAttribute('data-copy'));
        if (!target) return;
        var text = target.innerText.replace(/ /g, ' ').replace(/\s+$/, '') + '\n';
        var done = function (ok) {
          var lbl = btn.querySelector('.copy-label');
          btn.classList.toggle('ok', ok);
          if (lbl) lbl.textContent = ok ? T.copied : T.failed;
          announce(ok ? T.copied : T.failed);
          window.clearTimeout(btn._t);
          btn._t = window.setTimeout(function () {
            btn.classList.remove('ok');
            if (lbl) lbl.textContent = T.copy;
          }, 1800);
        };
        if (navigator.clipboard && window.isSecureContext) {
          navigator.clipboard.writeText(text).then(
            function () {
              done(true);
            },
            function () {
              done(fallbackCopy(text));
            }
          );
        } else {
          done(fallbackCopy(text));
        }
      });
    });

    /* ---- Işık kutusu ---- */
    var dlg = document.getElementById('isik-kutusu');
    var links = Array.prototype.slice.call(document.querySelectorAll('[data-lightbox]'));
    if (dlg && typeof dlg.showModal === 'function' && links.length) {
      var img = dlg.querySelector('img');
      var cap = dlg.querySelector('.lb-cap');
      var count = dlg.querySelector('.lb-count');
      var idx = 0;
      var opener = null;
      function show(i) {
        idx = (i + links.length) % links.length;
        var a = links[idx];
        var fig = a.closest('figure');
        var src = a.getAttribute('data-lightbox');
        var thumb = a.querySelector('img');
        if (thumb) {
          img.setAttribute('width', thumb.getAttribute('width'));
          img.setAttribute('height', thumb.getAttribute('height'));
        }
        img.src = src;
        img.alt = thumb ? thumb.alt : '';
        var fc = fig ? fig.querySelector('figcaption') : null;
        cap.textContent = fc ? fc.textContent.trim() : '';
        count.textContent = idx + 1 + ' / ' + links.length;
      }
      links.forEach(function (a, i) {
        a.addEventListener('click', function (e) {
          if (e.metaKey || e.ctrlKey || e.shiftKey || e.button === 1) return;
          e.preventDefault();
          opener = a;
          show(i);
          dlg.showModal();
        });
      });
      dlg.querySelector('.lb-close').addEventListener('click', function () {
        dlg.close();
      });
      dlg.querySelector('.lb-prev').addEventListener('click', function () {
        show(idx - 1);
      });
      dlg.querySelector('.lb-next').addEventListener('click', function () {
        show(idx + 1);
      });
      dlg.addEventListener('keydown', function (e) {
        if (e.key === 'ArrowLeft') {
          e.preventDefault();
          show(idx - 1);
        } else if (e.key === 'ArrowRight') {
          e.preventDefault();
          show(idx + 1);
        }
      });
      dlg.addEventListener('click', function (e) {
        if (e.target === dlg || e.target.classList.contains('lb-inner')) dlg.close();
      });
      dlg.addEventListener('close', function () {
        if (opener) opener.focus();
      });
    }
  });
})();
