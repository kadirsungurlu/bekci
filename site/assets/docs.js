/* Bekci belgeleri: mobil menü, sekmeler, "Bu sayfada" vurgusu ve arama.
   Dış bağımlılık yok. Tema ve kopyala düğmeleri site.js'te. */
(function () {
  'use strict';
  var root = document.documentElement;
  var tr = (root.getAttribute('lang') || 'tr').indexOf('tr') === 0;
  var T = tr
    ? { none: 'Sonuç bulunamadı', loading: 'Aranıyor…', failed: 'Arama dizini yüklenemedi', count: ' sonuç' }
    : { none: 'No results', loading: 'Searching…', failed: 'Could not load the search index', count: ' results' };

  function store(key, value) {
    try {
      if (value === undefined) return window.localStorage.getItem(key);
      window.localStorage.setItem(key, value);
    } catch (e) {
      /* gizli pencere: tercih yalnızca bu sayfada geçerli */
    }
    return null;
  }

  /* ---- Mobil belge menüsü ---- */
  var side = document.getElementById('belge-menu');
  var barBtn = document.querySelector('.docs-bar-btn');
  var shade = document.querySelector('.docs-shade');
  function setMenu(open, focusBack) {
    if (!side || !barBtn) return;
    root.classList.toggle('side-open', open);
    barBtn.setAttribute('aria-expanded', open ? 'true' : 'false');
    if (shade) shade.hidden = !open;
    if (open) {
      var cur = side.querySelector('[aria-current="page"]');
      if (cur && cur.scrollIntoView) cur.scrollIntoView({ block: 'center' });
      var input = side.querySelector('input');
      (side.querySelector('.side-close') || input).focus();
    } else if (focusBack) {
      barBtn.focus();
    }
  }
  if (barBtn && side) {
    barBtn.addEventListener('click', function () {
      setMenu(!root.classList.contains('side-open'), false);
    });
    var closeBtn = side.querySelector('.side-close');
    if (closeBtn) closeBtn.addEventListener('click', function () { setMenu(false, true); });
    if (shade) shade.addEventListener('click', function () { setMenu(false, true); });
    document.addEventListener('keydown', function (e) {
      if (e.key === 'Escape' && root.classList.contains('side-open')) setMenu(false, true);
    });
    window.addEventListener('resize', function () {
      if (window.innerWidth >= 900 && root.classList.contains('side-open')) setMenu(false, false);
    });
  }

  /* ---- Sekmeler ---- */
  var groups = Array.prototype.slice.call(document.querySelectorAll('.tabs'));
  function activate(tabs, name, remember) {
    var btns = tabs.querySelectorAll('[role="tab"]');
    var found = false;
    Array.prototype.forEach.call(btns, function (b) {
      if (b.getAttribute('data-tab') === name) found = true;
    });
    if (!found) return false;
    Array.prototype.forEach.call(btns, function (b) {
      var on = b.getAttribute('data-tab') === name;
      b.setAttribute('aria-selected', on ? 'true' : 'false');
      b.tabIndex = on ? 0 : -1;
      b.classList.toggle('active', on);
    });
    Array.prototype.forEach.call(tabs.querySelectorAll('.tab-panel'), function (p) {
      if (p.parentNode === tabs) p.classList.toggle('active', p.getAttribute('data-tab') === name);
    });
    var key = tabs.getAttribute('data-key');
    if (key && remember) {
      store('bekci-sekme-' + key, name);
      groups.forEach(function (g) {
        if (g !== tabs && g.getAttribute('data-key') === key) activate(g, name, false);
      });
    }
    return true;
  }
  groups.forEach(function (tabs) {
    var key = tabs.getAttribute('data-key');
    if (key) {
      var saved = store('bekci-sekme-' + key);
      if (saved) activate(tabs, saved, false);
    }
    var list = tabs.querySelector('[role="tablist"]');
    list.addEventListener('click', function (e) {
      var b = e.target.closest('[role="tab"]');
      if (b) activate(tabs, b.getAttribute('data-tab'), true);
    });
    list.addEventListener('keydown', function (e) {
      var btns = Array.prototype.slice.call(list.querySelectorAll('[role="tab"]'));
      var i = btns.indexOf(document.activeElement);
      if (i < 0) return;
      var n = null;
      if (e.key === 'ArrowRight') n = (i + 1) % btns.length;
      else if (e.key === 'ArrowLeft') n = (i - 1 + btns.length) % btns.length;
      else if (e.key === 'Home') n = 0;
      else if (e.key === 'End') n = btns.length - 1;
      if (n === null) return;
      e.preventDefault();
      btns[n].focus();
      activate(tabs, btns[n].getAttribute('data-tab'), true);
    });
  });
  // Adresteki #kimlik bir sekme paneliyse (ör. #nginx) o sekmeyi aç.
  function fromHash() {
    var id = decodeURIComponent(window.location.hash.slice(1));
    if (!id) return;
    var el = document.getElementById(id);
    if (!el) return;
    var panel = el.closest('.tab-panel');
    if (panel) {
      activate(panel.parentNode, panel.getAttribute('data-tab'), false);
      if (panel === el) {
        var tabs = panel.parentNode;
        window.requestAnimationFrame(function () {
          tabs.scrollIntoView();
        });
      }
    }
  }
  fromHash();
  window.addEventListener('hashchange', fromHash);

  /* ---- Bu sayfada: okunan bölümü vurgula ---- */
  var tocLinks = Array.prototype.slice.call(document.querySelectorAll('.docs-toc a'));
  if (tocLinks.length && 'IntersectionObserver' in window) {
    var heads = tocLinks
      .map(function (a) {
        return document.getElementById(a.getAttribute('href').slice(1));
      })
      .filter(Boolean);
    var visible = {};
    var mark = function () {
      var current = null;
      for (var i = 0; i < heads.length; i++) {
        if (heads[i].getBoundingClientRect().top < 140) current = heads[i];
      }
      if (!current) current = heads[0];
      tocLinks.forEach(function (a) {
        var on = a.getAttribute('href') === '#' + current.id;
        a.classList.toggle('active', on);
        if (on) a.setAttribute('aria-current', 'true');
        else a.removeAttribute('aria-current');
      });
    };
    var io = new IntersectionObserver(function (entries) {
      entries.forEach(function (en) {
        visible[en.target.id] = en.isIntersecting;
      });
      mark();
    }, { rootMargin: '-80px 0px -60% 0px' });
    heads.forEach(function (h) {
      io.observe(h);
    });
    window.addEventListener('scroll', function () {
      window.cancelAnimationFrame(mark._r);
      mark._r = window.requestAnimationFrame(mark);
    }, { passive: true });
    mark();
  }

  /* ---- Arama ---- */
  var box = document.querySelector('.doc-search');
  var input = document.getElementById('ara');
  var panel = document.getElementById('ara-sonuc');
  if (!box || !input || !panel) return;
  var index = null;
  var loading = null;
  var results = [];
  var sel = -1;

  var MAP = { ç: 'c', ğ: 'g', ı: 'i', ö: 'o', ş: 's', ü: 'u', â: 'a', î: 'i', û: 'u' };
  function normChar(c) {
    var l = c.toLocaleLowerCase('tr');
    return l
      .replace(/[çğıöşüâîû]/g, function (x) {
        return MAP[x];
      })
      .normalize('NFD')
      .replace(/[̀-ͯ]/g, '');
  }
  // Metni normalize eder ve her normalize karakterin özgün konumunu tutar.
  function normMap(s) {
    var out = '';
    var pos = [];
    for (var i = 0; i < s.length; i++) {
      var n = normChar(s[i]);
      for (var k = 0; k < n.length; k++) {
        out += n[k];
        pos.push(i);
      }
    }
    return { s: out, pos: pos };
  }
  function norm(s) {
    return normMap(s).s;
  }

  function load() {
    if (index || loading) return loading;
    var url = box.getAttribute('data-index');
    loading = window
      .fetch(url, { credentials: 'same-origin' })
      .then(function (r) {
        if (!r.ok) throw new Error(r.status);
        return r.json();
      })
      .then(function (data) {
        index = data.map(function (d) {
          return { d: d, p: norm(d.p), s: norm(d.s), x: normMap(d.x) };
        });
      })
      .catch(function () {
        index = [];
        loading = null;
        panel.textContent = T.failed;
      });
    return loading;
  }

  function score(item, terms) {
    var total = 0;
    for (var i = 0; i < terms.length; i++) {
      var q = terms[i];
      var sc = 0;
      if (item.s.indexOf(q) >= 0) sc += item.s.indexOf(q) === 0 || item.s.indexOf(' ' + q) >= 0 ? 12 : 8;
      if (item.p.indexOf(q) >= 0) sc += 5;
      var at = item.x.s.indexOf(q);
      if (at >= 0) {
        sc += 2;
        var n = 0;
        while (at >= 0 && n < 5) {
          n++;
          at = item.x.s.indexOf(q, at + q.length);
        }
        sc += n * 0.3;
      }
      if (!sc) return 0;
      total += sc;
    }
    return total;
  }

  function snippet(item, terms) {
    var at = -1;
    var len = 0;
    for (var i = 0; i < terms.length && at < 0; i++) {
      at = item.x.s.indexOf(terms[i]);
      len = terms[i].length;
    }
    var text = item.d.x;
    var frag = document.createDocumentFragment();
    if (at < 0) {
      frag.appendChild(document.createTextNode(text.slice(0, 120) + (text.length > 120 ? '…' : '')));
      return frag;
    }
    var start = item.x.pos[at];
    var end = item.x.pos[Math.min(at + len - 1, item.x.pos.length - 1)] + 1;
    var from = Math.max(0, start - 50);
    var to = Math.min(text.length, end + 80);
    frag.appendChild(document.createTextNode((from > 0 ? '…' : '') + text.slice(from, start)));
    var m = document.createElement('mark');
    m.textContent = text.slice(start, end);
    frag.appendChild(m);
    frag.appendChild(document.createTextNode(text.slice(end, to) + (to < text.length ? '…' : '')));
    return frag;
  }

  function setSel(i) {
    var opts = panel.querySelectorAll('[role="option"]');
    sel = i;
    Array.prototype.forEach.call(opts, function (o, k) {
      o.setAttribute('aria-selected', k === i ? 'true' : 'false');
      if (k === i && o.scrollIntoView) o.scrollIntoView({ block: 'nearest' });
    });
    if (i >= 0 && opts[i]) input.setAttribute('aria-activedescendant', opts[i].id);
    else input.removeAttribute('aria-activedescendant');
  }

  function open(show) {
    panel.hidden = !show;
    input.setAttribute('aria-expanded', show ? 'true' : 'false');
    box.classList.toggle('open', show);
  }

  function run() {
    var q = norm(input.value.trim());
    var terms = q.split(/[^a-z0-9_.:/-]+/).filter(function (x) {
      return x.length > 0;
    });
    panel.textContent = '';
    sel = -1;
    input.removeAttribute('aria-activedescendant');
    if (!terms.length) {
      open(false);
      return;
    }
    open(true);
    if (!index) {
      panel.textContent = T.loading;
      load().then(run);
      return;
    }
    results = index
      .map(function (it) {
        return { it: it, sc: score(it, terms) };
      })
      .filter(function (r) {
        return r.sc > 0;
      })
      .sort(function (a, b) {
        return b.sc - a.sc;
      })
      .slice(0, 8);
    if (!results.length) {
      var none = document.createElement('p');
      none.className = 'search-none';
      none.textContent = T.none;
      panel.appendChild(none);
      return;
    }
    results.forEach(function (r, i) {
      var a = document.createElement('a');
      a.href = r.it.d.u;
      a.id = 'ara-' + i;
      a.setAttribute('role', 'option');
      a.setAttribute('aria-selected', 'false');
      a.className = 'search-hit';
      var t = document.createElement('span');
      t.className = 'hit-title';
      t.textContent = r.it.d.s ? r.it.d.p + ' › ' + r.it.d.s : r.it.d.p;
      var x = document.createElement('span');
      x.className = 'hit-text';
      x.appendChild(snippet(r.it, terms));
      a.appendChild(t);
      a.appendChild(x);
      a.addEventListener('click', function () {
        open(false);
        if (root.classList.contains('side-open')) setMenu(false, false);
      });
      panel.appendChild(a);
    });
    var live = document.getElementById('duyuru');
    if (live) live.textContent = results.length + T.count;
  }

  input.addEventListener('focus', load);
  input.addEventListener('input', run);
  input.addEventListener('keydown', function (e) {
    var n = panel.querySelectorAll('[role="option"]').length;
    if (e.key === 'ArrowDown' && n) {
      e.preventDefault();
      setSel((sel + 1) % n);
    } else if (e.key === 'ArrowUp' && n) {
      e.preventDefault();
      setSel((sel - 1 + n) % n);
    } else if (e.key === 'Enter') {
      var opts = panel.querySelectorAll('[role="option"]');
      var target = opts[sel >= 0 ? sel : 0];
      if (target) {
        e.preventDefault();
        open(false);
        window.location.href = target.href;
        if (root.classList.contains('side-open')) setMenu(false, false);
      }
    } else if (e.key === 'Escape') {
      if (!panel.hidden) {
        e.stopPropagation();
        open(false);
      } else {
        input.value = '';
      }
    }
  });
  document.addEventListener('click', function (e) {
    if (!box.contains(e.target)) open(false);
  });
  document.addEventListener('keydown', function (e) {
    var tag = (e.target && e.target.tagName) || '';
    if (e.key === '/' && !/INPUT|TEXTAREA|SELECT/.test(tag) && !e.target.isContentEditable && !e.metaKey && !e.ctrlKey && !e.altKey) {
      e.preventDefault();
      if (window.innerWidth < 900 && !root.classList.contains('side-open')) setMenu(true, false);
      input.focus();
      input.select();
    }
  });
})();
