// bekci.app belgeleri: site/docs-src/{tr,en}/*.md → statik HTML.
//
// Kullanım: node build.mjs --src <docs-src> --site <site kökü> --out <çıktı>
//   <çıktı>/docs/…, <çıktı>/en/docs/…      sayfalar (klasör + index.html)
//   <çıktı>/assets/docs-search-{tr,en}.json arama dizinleri
//   <çıktı>/sitemap.xml                      ana sayfalar + belgeler
//
// Markdown uzantıları (ayrıntı: site/docs-src/README.md):
//   ön bilgi (front-matter)   --- id/title/nav/description/section/order/slug ---
//   uyarı kutuları            > [!NOTE] / [!TIP] / [!IMPORTANT] / [!WARNING] /
//                             [!CAUTION] / [!CHECK] (isteğe bağlı başlık aynı satırda)
//   sekmeler                  :::tabs key=db label="…"  /  @tab Ad  /  :::
//   kod başlığı               ```yaml title="docker-compose.yml" download="/indir/…"
//   yer tutucu                ⟦bekci.ornek.com⟧ → vurgulu (kod içinde ve satır içi kodda)
//   başlık kimliği            ## Başlık {#kimlik}
//
// Derleme hatası (çıkış kodu 1): kırık iç bağlantı veya çapa, yinelenen kimlik,
// eşi olmayan sayfa, sekme içinde başlık, bozuk yer tutucu.

import fs from 'node:fs';
import path from 'node:path';
import { Marked } from 'marked';
import hljs from 'highlight.js/lib/core';
import bash from 'highlight.js/lib/languages/bash';
import yaml from 'highlight.js/lib/languages/yaml';
import nginx from 'highlight.js/lib/languages/nginx';
import apache from 'highlight.js/lib/languages/apache';
import ini from 'highlight.js/lib/languages/ini';
import powershell from 'highlight.js/lib/languages/powershell';
import json from 'highlight.js/lib/languages/json';
import plaintext from 'highlight.js/lib/languages/plaintext';

// ---------------------------------------------------------------- ayarlar

const args = Object.fromEntries(
  process.argv.slice(2).reduce((acc, a, i, all) => (a.startsWith('--') ? [...acc, [a.slice(2), all[i + 1] && !all[i + 1].startsWith('--') ? all[i + 1] : '']] : acc), []),
);
const SRC = path.resolve(args.src || '../docs-src');
const SITE = path.resolve(args.site || '..');
const OUT = path.resolve(args.out || '../../.docs-out');
const ORIGIN = 'https://bekci.app';
const GITHUB = 'https://github.com/kadirsungurlu/bekci';

// Caddyfile için küçük bir dil tanımı (highlight.js'te yok).
function caddyfile(h) {
  return {
    name: 'Caddyfile',
    contains: [
      h.HASH_COMMENT_MODE,
      { className: 'variable', begin: /\{\$?[A-Za-z_][\w.\[\]]*\}/ },
      h.QUOTE_STRING_MODE,
      { className: 'keyword', begin: /^[ \t]*[a-z_]+(?=[ \t]|$)/, relevance: 0 },
      { className: 'number', begin: /(?<=\s)-?\d+[a-z]*\b/, relevance: 0 },
    ],
  };
}
hljs.registerLanguage('bash', bash);
hljs.registerLanguage('yaml', yaml);
hljs.registerLanguage('nginx', nginx);
hljs.registerLanguage('apache', apache);
hljs.registerLanguage('ini', ini);
hljs.registerLanguage('powershell', powershell);
hljs.registerLanguage('json', json);
hljs.registerLanguage('plaintext', plaintext);
hljs.registerLanguage('caddyfile', caddyfile);
const LANG_ALIAS = { sh: 'bash', shell: 'bash', console: 'bash', yml: 'yaml', env: 'ini', dotenv: 'ini', ps: 'powershell', ps1: 'powershell', text: 'plaintext', txt: 'plaintext', caddy: 'caddyfile' };
const LANG_LABEL = { bash: 'terminal', powershell: 'PowerShell', yaml: 'YAML', nginx: 'Nginx', apache: 'Apache', ini: '.env', json: 'JSON', caddyfile: 'Caddyfile', plaintext: '' };

const T = {
  tr: {
    locale: 'tr_TR', docs: 'Belgeler', home: '/', docsHome: '/docs/', onThisPage: 'Bu sayfada', prev: 'Önceki', next: 'Sonraki',
    search: 'Belgelerde ara', searchPh: 'Belgelerde ara…', menu: 'Belgeler menüsü', menuShort: 'Menü', close: 'Kapat', skip: 'İçeriğe geç',
    copy: 'Kopyala', copyAria: 'Kodu kopyala', download: 'İndir', options: 'Seçenekler', breadcrumb: 'Konum', anchor: 'Bu bölümün bağlantısı',
    table: 'Tablo', docsNav: 'Belgeler', mainNav: 'Ana menü', mobileNav: 'Mobil menü', themeAria: 'Temayı değiştir', menuAria: 'Menü',
    homeAria: 'Bekci ana sayfa', langShort: 'EN', langAria: 'English version', langName: 'English', otherLang: 'en',
    feedback: 'Bu sayfada eksik ya da hatalı bir şey mi var?', feedbackLink: 'GitHub’da bildirin', orMail: 'ya da yazın:',
    titleSuffix: 'Bekci belgeleri', ogImage: '/og.png', searchIndex: '/assets/docs-search-tr.json',
    callout: { NOTE: 'Not', TIP: 'İpucu', IMPORTANT: 'Önemli', WARNING: 'Uyarı', CAUTION: 'Dikkat', CHECK: 'Ne görmelisiniz?' },
    nav: [['/#ozellikler', 'Özellikler'], ['/#ekranlar', 'Ekranlar'], ['/#neden', 'Karşılaştırma'], ['/#kurulum', 'Kurulum'], ['/#sss', 'SSS']],
    footer: [['/docs/', 'Belgeler'], ['https://hub.docker.com/r/kadirsungurlu/bekci', 'Docker Hub'], [GITHUB, 'GitHub'], [GITHUB + '/blob/main/LICENSE', 'Lisans: AGPL-3.0']],
    contact: 'İletişim: me@kadir.app', copyright: '© 2026 Bekci · Türkiye’de geliştirildi.',
  },
  en: {
    locale: 'en_US', docs: 'Docs', home: '/en/', docsHome: '/en/docs/', onThisPage: 'On this page', prev: 'Previous', next: 'Next',
    search: 'Search the docs', searchPh: 'Search the docs…', menu: 'Docs menu', menuShort: 'Menu', close: 'Close', skip: 'Skip to content',
    copy: 'Copy', copyAria: 'Copy code', download: 'Download', options: 'Options', breadcrumb: 'Breadcrumb', anchor: 'Link to this section',
    table: 'Table', docsNav: 'Docs', mainNav: 'Main', mobileNav: 'Mobile', themeAria: 'Toggle theme', menuAria: 'Menu',
    homeAria: 'Bekci home', langShort: 'TR', langAria: 'Türkçe sürüm', langName: 'Türkçe', otherLang: 'tr',
    feedback: 'Something missing or wrong on this page?', feedbackLink: 'Report it on GitHub', orMail: 'or email',
    titleSuffix: 'Bekci docs', ogImage: '/og-en.png', searchIndex: '/assets/docs-search-en.json',
    callout: { NOTE: 'Note', TIP: 'Tip', IMPORTANT: 'Important', WARNING: 'Warning', CAUTION: 'Caution', CHECK: 'What you should see' },
    nav: [['/en/#features', 'Features'], ['/en/#screenshots', 'Screenshots'], ['/en/#compare', 'Compare'], ['/en/#install', 'Install'], ['/en/#faq', 'FAQ']],
    footer: [['/en/docs/', 'Docs'], ['https://hub.docker.com/r/kadirsungurlu/bekci', 'Docker Hub'], [GITHUB, 'GitHub'], [GITHUB + '/blob/main/LICENSE', 'License: AGPL-3.0']],
    contact: 'Contact: me@kadir.app', copyright: '© 2026 Bekci · Made in Türkiye.',
  },
};

// ---------------------------------------------------------------- yardımcılar

const errors = [];
const fail = (msg) => errors.push(msg);

function esc(s) {
  return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
}

const TR_MAP = { ç: 'c', ğ: 'g', ı: 'i', ö: 'o', ş: 's', ü: 'u', â: 'a', î: 'i', û: 'u' };
function slugify(s) {
  return String(s)
    .replace(/<[^>]+>/g, '')
    .replace(/&[a-z]+;|&#\d+;/g, ' ')
    .toLocaleLowerCase('tr')
    .replace(/[çğıöşüâîû]/g, (c) => TR_MAP[c])
    .normalize('NFD')
    .replace(/[̀-ͯ]/g, '')
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '');
}

function stripTags(html) {
  return html
    .replace(/<[^>]+>/g, ' ')
    .replace(/&#x([0-9a-f]+);/gi, (_, h) => String.fromCodePoint(parseInt(h, 16)))
    .replace(/&#(\d+);/g, (_, d) => String.fromCodePoint(Number(d)))
    .replace(/&lt;/g, '<').replace(/&gt;/g, '>').replace(/&quot;/g, '"').replace(/&nbsp;/g, ' ').replace(/&amp;/g, '&')
    .replace(/\s+/g, ' ')
    .trim();
}

function parseFrontMatter(raw, file) {
  const m = raw.match(/^---\n([\s\S]*?)\n---\n/);
  if (!m) {
    fail(`${file}: ön bilgi (---) yok`);
    return { meta: {}, body: raw };
  }
  const meta = {};
  for (const line of m[1].split('\n')) {
    const kv = line.match(/^([a-z_]+):\s*(.*)$/);
    if (!kv) continue;
    let v = kv[2].trim();
    if (/^".*"$/.test(v) || /^'.*'$/.test(v)) v = v.slice(1, -1);
    meta[kv[1]] = v;
  }
  for (const k of ['id', 'title', 'description', 'section', 'order']) if (!(k in meta)) fail(`${file}: ön bilgide "${k}" eksik`);
  meta.order = Number(meta.order);
  meta.slug = (meta.slug || '').replace(/^\/+|\/+$/g, '');
  return { meta, body: raw.slice(m[0].length) };
}

// Kod bilgisi: ```yaml title="x" download="/y"
function parseInfo(info) {
  const out = { lang: '', attrs: {} };
  const s = (info || '').trim();
  const lm = s.match(/^([A-Za-z0-9_+.-]+)/);
  if (lm) out.lang = lm[1].toLowerCase();
  for (const a of s.matchAll(/([a-z]+)="([^"]*)"/g)) out.attrs[a[1]] = a[2];
  return out;
}

// Yer tutucu: ⟦…⟧ → <mark class="ph">…</mark>. İşaretler aynı metin düğümünde
// kalmalı (vurgulayıcı arasına etiket koymuşsa derleme hatası).
function placeholders(html, where) {
  return html.replace(/⟦([^⟧]*)⟧/g, (all, inner) => {
    if (/[<>]/.test(inner)) {
      fail(`${where}: yer tutucu vurgulayıcı etiketleriyle bölünmüş: ${all.slice(0, 60)}`);
      return inner;
    }
    return `<mark class="ph">${inner}</mark>`;
  }).replace(/[⟦⟧]/g, () => {
    fail(`${where}: eşleşmeyen yer tutucu işareti`);
    return '';
  });
}

const ICON = {
  copy: '<svg class="i-copy" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg><svg class="i-check" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M20 6 9 17l-5-5"/></svg>',
  file: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M14.5 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7.5L14.5 2z"/><path d="M14 2v6h6"/></svg>',
  term: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m4 17 6-6-6-6M12 19h8"/></svg>',
  dl: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M12 3v12M7 10l5 5 5-5M5 21h14"/></svg>',
  NOTE: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="12" cy="12" r="10"/><path d="M12 16v-4M12 8h.01"/></svg>',
  TIP: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M9 18h6M10 22h4M12 2a7 7 0 0 0-4 12.7V17h8v-2.3A7 7 0 0 0 12 2z"/></svg>',
  IMPORTANT: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/><path d="M12 7v4M12 14h.01"/></svg>',
  WARNING: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m21.73 18-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3z"/><path d="M12 9v4M12 17h.01"/></svg>',
  CAUTION: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M7.86 2h8.28L22 7.86v8.28L16.14 22H7.86L2 16.14V7.86z"/><path d="M12 8v4M12 16h.01"/></svg>',
  CHECK: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="12" cy="12" r="10"/><path d="m8 12 3 3 5-6"/></svg>',
  prev: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m15 18-6-6 6-6"/></svg>',
  next: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m9 18 6-6-6-6"/></svg>',
  search: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="11" cy="11" r="7"/><path d="m20 20-3.5-3.5"/></svg>',
  menu: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true"><path d="M4 6h16M4 12h16M4 18h10"/></svg>',
  x: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true"><path d="M18 6 6 18M6 6l12 12"/></svg>',
  github: '<svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M12 .5a11.5 11.5 0 0 0-3.64 22.41c.58.1.79-.25.79-.56v-2c-3.2.7-3.88-1.37-3.88-1.37-.52-1.33-1.28-1.69-1.28-1.69-1.04-.71.08-.7.08-.7 1.15.08 1.76 1.19 1.76 1.19 1.03 1.76 2.69 1.25 3.35.96.1-.75.4-1.25.73-1.54-2.55-.29-5.24-1.28-5.24-5.68 0-1.26.45-2.28 1.19-3.09-.12-.29-.52-1.46.11-3.05 0 0 .97-.31 3.17 1.18a11 11 0 0 1 5.77 0c2.2-1.49 3.17-1.18 3.17-1.18.63 1.59.23 2.76.11 3.05.74.81 1.19 1.83 1.19 3.09 0 4.41-2.69 5.38-5.26 5.67.41.36.78 1.06.78 2.14v3.17c0 .31.21.67.8.56A11.5 11.5 0 0 0 12 .5z"/></svg>',
};

// WebP boyutu (img etiketi için width/height; CLS olmasın).
function webpSize(file) {
  const b = fs.readFileSync(file);
  const kind = b.toString('ascii', 12, 16);
  if (kind === 'VP8X') return [1 + b.readUIntLE(24, 3), 1 + b.readUIntLE(27, 3)];
  if (kind === 'VP8 ') return [b.readUInt16LE(26) & 0x3fff, b.readUInt16LE(28) & 0x3fff];
  if (kind === 'VP8L') {
    const n = b.readUInt32LE(21);
    return [1 + (n & 0x3fff), 1 + ((n >> 14) & 0x3fff)];
  }
  throw new Error('webp okunamadı: ' + file);
}

// ---------------------------------------------------------------- markdown

function makeRenderer(page, ids) {
  const t = T[page.lang];
  let codeN = 0;
  let tabDepth = 0;
  const toc = [];

  const register = (id, where) => {
    if (ids.has(id)) fail(`${page.file}: yinelenen kimlik "${id}" (${where})`);
    ids.add(id);
    return id;
  };
  const unique = (base) => {
    let id = base || 'bolum';
    for (let i = 2; ids.has(id); i++) id = `${base}-${i}`;
    return id;
  };

  const marked = new Marked({ gfm: true });
  marked.use({
    renderer: {
      heading({ tokens, depth }) {
        let inner = this.parser.parseInline(tokens);
        let id;
        const m = inner.match(/\s*\{#([a-z0-9-]+)\}\s*$/);
        if (m) {
          inner = inner.slice(0, m.index);
          id = register(m[1], 'başlık');
        } else {
          id = register(unique(slugify(inner)), 'başlık');
        }
        if (depth === 1) fail(`${page.file}: # başlık kullanmayın (sayfa başlığı ön bilgiden gelir)`);
        if (tabDepth > 0) fail(`${page.file}: sekme içinde başlık olmaz: ${stripTags(inner)}`);
        if (depth <= 3) toc.push({ depth, id, text: stripTags(inner) });
        return `<h${depth} id="${id}">${inner}<a class="h-anchor" href="#${id}" aria-label="${esc(t.anchor)}: ${esc(stripTags(inner))}">#</a></h${depth}>\n`;
      },
      code({ text, lang }) {
        const info = parseInfo(lang);
        const name = LANG_ALIAS[info.lang] || info.lang || 'plaintext';
        if (!hljs.getLanguage(name)) fail(`${page.file}: bilinmeyen kod dili "${info.lang}"`);
        const plain = text.replace(/\n+$/, '');
        if (info.attrs.download) {
          // İndirilebilir dosya ile sayfada gösterilen içerik aynı olmalı.
          const f = path.join(SITE, info.attrs.download);
          if (!fs.existsSync(f)) fail(`${page.file}: indirme dosyası yok: ${info.attrs.download}`);
          else if (fs.readFileSync(f, 'utf8').replace(/\n+$/, '') !== plain.replace(/[⟦⟧]/g, ''))
            fail(`${page.file}: ${info.attrs.download} ile sayfadaki içerik farklı`);
        }
        const hl = hljs.getLanguage(name) ? hljs.highlight(plain, { language: name, ignoreIllegals: true }).value : esc(plain);
        const body = placeholders(hl, `${page.file} kod ${info.attrs.title || name}`);
        const id = `kod-${++codeN}`;
        const title = info.attrs.title;
        const label = title ? `${ICON.file}<span>${esc(title)}</span>` : LANG_LABEL[name] ? `${name === 'bash' || name === 'powershell' ? ICON.term : ''}<span>${esc(LANG_LABEL[name])}</span>` : '<span></span>';
        const dl = info.attrs.download
          ? `<a class="code-dl" href="${esc(info.attrs.download)}" download>${ICON.dl}<span>${t.download}</span></a>`
          : '';
        return (
          `<div class="cmd block doc-code${title ? ' has-title' : ''}">` +
          `<div class="cmd-head"><span class="code-name">${label}</span><span class="code-actions">${dl}` +
          `<button class="copy" type="button" data-copy="${id}" aria-label="${esc(t.copyAria)}${title ? ': ' + esc(title) : ''}">${ICON.copy}<span class="copy-label">${t.copy}</span></button></span></div>` +
          `<pre><code id="${id}" class="hljs language-${name}">${body}</code></pre></div>\n`
        );
      },
      codespan({ text }) {
        return `<code>${placeholders(esc(text).replace(/&quot;/g, '"'), page.file + ' satır içi kod')}</code>`;
      },
      blockquote({ tokens }) {
        const inner = this.parser.parse(tokens);
        const m = inner.match(/^<p>\[!(NOTE|TIP|IMPORTANT|WARNING|CAUTION|CHECK)\][ \t]*([^\n<]*?)[ \t]*(\n|<\/p>\n?)/);
        if (!m) return `<blockquote>\n${inner}</blockquote>\n`;
        const kind = m[1];
        const title = m[2] || t.callout[kind];
        const rest = (m[3] === '\n' ? '<p>' : '') + inner.slice(m[0].length);
        return `<aside class="callout callout-${kind.toLowerCase()}"><p class="callout-title">${ICON[kind]}<span>${title}</span></p>\n${rest}</aside>\n`;
      },
      table(token) {
        const head = token.header.map((c) => stripTags(this.parser.parseInline(c.tokens))).join(', ');
        let out = '<thead><tr>';
        for (const c of token.header) out += `<th${c.align ? ` class="al-${c.align}"` : ''}>${this.parser.parseInline(c.tokens)}</th>`;
        out += '</tr></thead><tbody>';
        for (const row of token.rows) {
          out += '<tr>';
          for (const c of row) out += `<td${c.align ? ` class="al-${c.align}"` : ''}>${this.parser.parseInline(c.tokens)}</td>`;
          out += '</tr>';
        }
        return `<div class="table-scroll" role="region" tabindex="0" aria-label="${esc(t.table)}: ${esc(head)}"><table>${out}</tbody></table></div>\n`;
      },
      link({ href, title, tokens }) {
        const text = this.parser.parseInline(tokens);
        const ext = /^https?:\/\//.test(href);
        return `<a href="${esc(href)}"${title ? ` title="${esc(title)}"` : ''}${ext ? ' rel="noopener"' : ''}>${text}</a>`;
      },
      image({ href, title, text }) {
        if (!href.startsWith('/img/')) fail(`${page.file}: görsel /img/ altında olmalı: ${href}`);
        const file = path.join(SITE, href);
        if (!fs.existsSync(file)) {
          fail(`${page.file}: görsel yok: ${href}`);
          return '';
        }
        const [w, h] = webpSize(file);
        // x-1600.webp yanında x-800.webp varsa ekran genişliğine göre seçilsin.
        const small = href.replace(/-1600\.webp$/, '-800.webp');
        const srcset =
          small !== href && fs.existsSync(path.join(SITE, small))
            ? ` srcset="${esc(small)}?v=dev 800w, ${esc(href)}?v=dev 1600w" sizes="(min-width: 900px) 780px, calc(100vw - 40px)"`
            : '';
        return `<img src="${esc(href)}?v=dev"${srcset} width="${w}" height="${h}" loading="lazy" decoding="async" alt="${esc(text)}"${title ? ` title="${esc(title)}"` : ''}>`;
      },
    },
  });

  return {
    toc,
    setTabDepth: (d) => (tabDepth = d),
    parse: (md) => marked.parse(md),
    unique,
    register,
  };
}

// Sekmeler: :::tabs [key=x] [label="…"]  @tab Ad  …  :::   (kod çitlerinin dışında)
function renderPage(page) {
  const t = T[page.lang];
  const ids = new Set(['icerik', 'content', 'belge-menu', 'docs-menu', 'ara', 'ara-sonuc', 'duyuru', 'mobil-menu', 'bu-sayfada']);
  const r = makeRenderer(page, ids);
  const lines = page.body.split('\n');
  const outLines = [];
  const blocks = [];
  let fence = null;
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    const f = line.match(/^(`{3,}|~{3,})/);
    if (f) {
      if (!fence) fence = f[1];
      else if (line.startsWith(fence) && line.trim() === fence) fence = null;
    }
    const tm = !fence && line.match(/^:::tabs\b(.*)$/);
    if (!tm) {
      outLines.push(line);
      continue;
    }
    const opts = Object.fromEntries([...tm[1].matchAll(/([a-z]+)=(?:"([^"]*)"|(\S+))/g)].map((a) => [a[1], a[2] ?? a[3]]));
    const tabs = [];
    let j = i + 1;
    let inFence = null;
    for (; j < lines.length; j++) {
      const l = lines[j];
      const ff = l.match(/^(`{3,}|~{3,})/);
      if (ff) {
        if (!inFence) inFence = ff[1];
        else if (l.startsWith(inFence) && l.trim() === inFence) inFence = null;
      }
      if (!inFence && l.trim() === ':::') break;
      const at = !inFence && l.match(/^@tab\s+(.+)$/);
      if (at) {
        const idm = at[1].match(/\s*\{#([a-z0-9-]+)\}\s*$/);
        tabs.push({ label: (idm ? at[1].slice(0, idm.index) : at[1]).trim(), id: idm && idm[1], lines: [] });
      }
      else if (tabs.length) tabs[tabs.length - 1].lines.push(l);
      else if (l.trim()) fail(`${page.file}: :::tabs içinde @tab'dan önce içerik var`);
    }
    if (j >= lines.length) fail(`${page.file}: kapanmayan :::tabs`);
    if (tabs.length < 2) fail(`${page.file}: sekme bloğunda en az iki @tab olmalı`);
    blocks.push({ opts, tabs });
    outLines.push('', `<!--SEKMELER:${blocks.length - 1}-->`, '');
    i = j;
  }

  let html = r.parse(outLines.join('\n'));
  const rendered = blocks.map((b, n) => {
    const key = b.opts.key ? slugify(b.opts.key) : '';
    const buttons = [];
    const panels = [];
    b.tabs.forEach((tab, k) => {
      const pid = r.register(tab.id || r.unique(slugify(tab.label)), 'sekme');
      const bid = r.register(`sekme-${pid}`, 'sekme düğmesi');
      r.setTabDepth(1);
      const inner = r.parse(tab.lines.join('\n'));
      r.setTabDepth(0);
      const on = k === 0;
      buttons.push(
        `<button type="button" role="tab" id="${bid}" aria-controls="${pid}" aria-selected="${on}" tabindex="${on ? 0 : -1}" data-tab="${esc(slugify(tab.label))}"${on ? ' class="active"' : ''}>${esc(tab.label)}</button>`,
      );
      panels.push(
        `<div class="tab-panel${on ? ' active' : ''}" role="tabpanel" id="${pid}" aria-labelledby="${bid}" data-tab="${esc(slugify(tab.label))}"><p class="tab-label">${esc(tab.label)}</p>\n${inner}</div>`,
      );
    });
    return `<div class="tabs"${key ? ` data-key="${key}"` : ''}><div class="tab-list" role="tablist" aria-label="${esc(b.opts.label || t.options)}">${buttons.join('')}</div>\n${panels.join('\n')}\n</div>\n`;
  });
  html = html.replace(/<!--SEKMELER:(\d+)-->\n?/g, (_, n) => rendered[Number(n)]);
  // marked yinelenen metinleri tek tek işler; kalıntı işaret kalmasın
  if (/⟦|⟧/.test(html)) fail(`${page.file}: işlenmemiş yer tutucu işareti`);
  page.html = html;
  page.toc = r.toc;
  page.ids = ids;
}

// ---------------------------------------------------------------- sayfalar

function loadPages(lang) {
  const dir = path.join(SRC, lang);
  return fs
    .readdirSync(dir)
    .filter((f) => f.endsWith('.md'))
    .map((f) => {
      const { meta, body } = parseFrontMatter(fs.readFileSync(path.join(dir, f), 'utf8'), `${lang}/${f}`);
      const base = lang === 'tr' ? '/docs/' : '/en/docs/';
      return { ...meta, lang, file: `${lang}/${f}`, body, url: base + (meta.slug ? meta.slug + '/' : '') };
    })
    .sort((a, b) => a.order - b.order);
}

function header(page) {
  const t = T[page.lang];
  const navItems = t.nav.map(([h, l]) => `<li><a href="${h}">${l}</a></li>`).join('\n        ');
  const docsItem = `<li><a href="${t.docsHome}" class="active" aria-current="true">${t.docs}</a></li>`;
  return `<header class="site-header">
  <div class="wrap wrap-docs">
    <a class="brand" href="${t.home}" aria-label="${t.homeAria}">
      <svg viewBox="0 0 64 64" aria-hidden="true"><circle cx="32" cy="32" r="30" fill="#3bd671" opacity=".22"/><circle cx="32" cy="32" r="20" fill="#3bd671" opacity=".35"/><circle cx="32" cy="32" r="12" fill="#3bd671"/></svg>
      Bekci
    </a>
    <nav class="nav" aria-label="${t.mainNav}">
      <ul>
        ${navItems}
        ${docsItem}
      </ul>
    </nav>
    <div class="header-actions">
      <a class="lang" href="${page.pair.url}" hreflang="${t.otherLang}" lang="${t.otherLang}" aria-label="${t.langAria}">${t.langShort}</a>
      <button class="icon-btn theme-toggle" type="button" aria-label="${t.themeAria}">
        <svg class="i-moon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z"/></svg>
        <svg class="i-sun" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.93 4.93l1.41 1.41M17.66 17.66l1.41 1.41M2 12h2M20 12h2M6.34 17.66l-1.41 1.41M19.07 4.93l-1.41 1.41"/></svg>
      </button>
      <a class="icon-btn header-gh" href="${GITHUB}" rel="noopener" aria-label="GitHub" title="GitHub">${ICON.github}</a>
      <a class="btn btn-primary btn-sm header-hub" href="https://hub.docker.com/r/kadirsungurlu/bekci" rel="noopener">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M21 8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16Z"/><path d="m3.3 7 8.7 5 8.7-5"/><path d="M12 22V12"/></svg>
        Docker Hub
      </a>
      <button class="icon-btn menu-toggle" type="button" aria-expanded="false" aria-controls="mobil-menu" aria-label="${t.menuAria}">
        <svg class="i-menu" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true"><path d="M4 6h16M4 12h16M4 18h16"/></svg>
        <svg class="i-x" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true"><path d="M18 6 6 18M6 6l12 12"/></svg>
      </button>
    </div>
  </div>
  <nav class="mobile-nav" id="mobil-menu" aria-label="${t.mobileNav}" hidden>
    <ul>
      ${navItems}
      ${docsItem}
      <li><a href="${GITHUB}" rel="noopener">GitHub</a></li>
    </ul>
    <a class="btn btn-primary" href="https://hub.docker.com/r/kadirsungurlu/bekci" rel="noopener">Docker Hub</a>
  </nav>
</header>`;
}

function footer(page) {
  const t = T[page.lang];
  const links = t.footer
    .map(([h, l]) => `<li><a href="${h}"${h.startsWith('http') ? ' rel="noopener"' : ''}>${l}</a></li>`)
    .join('\n      ');
  return `<footer class="site-footer">
  <div class="wrap wrap-docs">
    <a class="brand" href="${t.home}" aria-label="${t.homeAria}">
      <svg viewBox="0 0 64 64" aria-hidden="true"><circle cx="32" cy="32" r="30" fill="#3bd671" opacity=".22"/><circle cx="32" cy="32" r="20" fill="#3bd671" opacity=".35"/><circle cx="32" cy="32" r="12" fill="#3bd671"/></svg>
      Bekci
    </a>
    <ul class="foot-links">
      ${links}
      <li><a href="${page.pair.url}" hreflang="${t.otherLang}" lang="${t.otherLang}">${t.langName}</a></li>
      <li><a href="mailto:me@kadir.app">${t.contact}</a></li>
    </ul>
    <p>${t.copyright}</p>
  </div>
</footer>`;
}

function sidebar(page, pages) {
  const t = T[page.lang];
  const sections = [];
  for (const p of pages) {
    let s = sections.find((x) => x.name === p.section);
    if (!s) sections.push((s = { name: p.section, pages: [] }));
    s.pages.push(p);
  }
  const nav = sections
    .map(
      (s) => `<li class="side-group"><p class="side-title">${esc(s.name)}</p><ul>${s.pages
        .map((p) => `<li><a href="${p.url}"${p === page ? ' aria-current="page"' : ''}>${esc(p.nav || p.title)}</a></li>`)
        .join('')}</ul></li>`,
    )
    .join('\n');
  return `<aside class="docs-side" id="belge-menu" aria-label="${t.menu}">
  <div class="side-head">
    <p class="side-head-title">${t.docs}</p>
    <button class="icon-btn side-close" type="button" aria-label="${t.close}">${ICON.x}</button>
  </div>
  <div class="doc-search" role="search" data-index="${t.searchIndex}?v=dev">
    <label class="sr-only" for="ara">${t.search}</label>
    <span class="search-icon">${ICON.search}</span>
    <input id="ara" type="text" inputmode="search" enterkeyhint="search" placeholder="${t.searchPh}" autocomplete="off" spellcheck="false" role="combobox" aria-expanded="false" aria-controls="ara-sonuc" aria-autocomplete="list">
    <kbd class="search-kbd" aria-hidden="true">/</kbd>
    <div class="search-results" id="ara-sonuc" role="listbox" aria-label="${t.search}" hidden></div>
  </div>
  <nav class="side-nav" aria-label="${t.docsNav}">
    <ul>
${nav}
    </ul>
  </nav>
</aside>`;
}

function tocHtml(page, cls) {
  const t = T[page.lang];
  const items = page.toc.filter((h) => h.depth === 2 || h.depth === 3);
  if (items.length < 2) return '';
  const list = items.map((h) => `<li class="toc-${h.depth}"><a href="#${h.id}">${esc(h.text)}</a></li>`).join('');
  if (cls === 'inline') {
    return `<details class="toc-inline"><summary>${t.onThisPage}</summary><ul>${list}</ul></details>`;
  }
  return `<nav class="docs-toc" aria-labelledby="bu-sayfada"><p class="toc-title" id="bu-sayfada">${t.onThisPage}</p><ul>${list}</ul></nav>`;
}

function layout(page, pages) {
  const t = T[page.lang];
  const i = pages.indexOf(page);
  const prev = pages[i - 1];
  const next = pages[i + 1];
  const title = `${page.title} — ${t.titleSuffix}`;
  const trUrl = page.lang === 'tr' ? page.url : page.pair.url;
  const enUrl = page.lang === 'en' ? page.url : page.pair.url;
  const crumbs = [`<li><a href="${t.docsHome}">${t.docs}</a></li>`];
  if (page.slug) crumbs.push(`<li><span>${esc(page.section)}</span></li>`);
  const pn = [
    prev ? `<a class="pn pn-prev" href="${prev.url}" rel="prev"><span class="pn-dir">${ICON.prev}${t.prev}</span><span class="pn-title">${esc(prev.nav || prev.title)}</span></a>` : '<span></span>',
    next ? `<a class="pn pn-next" href="${next.url}" rel="next"><span class="pn-dir">${t.next}${ICON.next}</span><span class="pn-title">${esc(next.nav || next.title)}</span></a>` : '<span></span>',
  ].join('');
  return `<!doctype html>
<html lang="${page.lang}" class="docs-page">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>${esc(title)}</title>
<meta name="description" content="${esc(page.description)}">
<link rel="canonical" href="${ORIGIN}${page.url}">
<link rel="alternate" hreflang="tr" href="${ORIGIN}${trUrl}">
<link rel="alternate" hreflang="en" href="${ORIGIN}${enUrl}">
<link rel="alternate" hreflang="x-default" href="${ORIGIN}${trUrl}">
<meta name="theme-color" media="(prefers-color-scheme: light)" content="#f6f8fa">
<meta name="theme-color" media="(prefers-color-scheme: dark)" content="#0b111b">
<meta name="color-scheme" content="light dark">
<link rel="icon" href="/favicon.svg?v=dev" type="image/svg+xml">
<link rel="apple-touch-icon" href="/apple-touch-icon.png?v=dev">
<meta property="og:type" content="article">
<meta property="og:site_name" content="Bekci">
<meta property="og:locale" content="${t.locale}">
<meta property="og:url" content="${ORIGIN}${page.url}">
<meta property="og:title" content="${esc(page.title)} — ${t.titleSuffix}">
<meta property="og:description" content="${esc(page.description)}">
<meta property="og:image" content="${ORIGIN}${t.ogImage}">
<meta name="twitter:card" content="summary_large_image">
<link rel="preload" href="/assets/fonts/inter-4.1.1-bekci.woff2" as="font" type="font/woff2" crossorigin>
<link rel="stylesheet" href="/assets/site.css?v=dev">
<link rel="stylesheet" href="/assets/docs.css?v=dev">
<script src="/assets/site.js?v=dev"></script>
<script src="/assets/docs.js?v=dev" defer></script>
</head>
<body>
<a class="skip" href="#icerik">${t.skip}</a>
${header(page)}
<div class="docs-bar">
  <button class="docs-bar-btn" type="button" aria-expanded="false" aria-controls="belge-menu">${ICON.menu}<span>${t.menuShort}</span></button>
  <span class="docs-bar-where">${esc(page.section)}<span aria-hidden="true"> › </span>${esc(page.nav || page.title)}</span>
</div>
<div class="docs-shade" hidden></div>
<div class="docs wrap-docs">
${sidebar(page, pages)}
<main id="icerik" class="docs-main">
<article class="doc">
<nav class="crumbs" aria-label="${t.breadcrumb}"><ol>${crumbs.join('')}</ol></nav>
<h1>${esc(page.title)}</h1>
<p class="doc-lead">${esc(page.description)}</p>
${tocHtml(page, 'inline')}
<div class="doc-body">
${page.html}</div>
<div class="doc-feedback">${ICON.github}<p>${t.feedback} <a href="${GITHUB}/issues" rel="noopener">${t.feedbackLink}</a> ${t.orMail} <a href="mailto:me@kadir.app">me@kadir.app</a></p></div>
<nav class="pn-nav" aria-label="${t.prev} / ${t.next}">${pn}</nav>
</article>
</main>
${tocHtml(page, 'side')}
</div>
${footer(page)}
<div id="duyuru" class="sr-only" aria-live="polite"></div>
</body>
</html>
`;
}

// ---------------------------------------------------------------- arama dizini

function searchIndex(pages) {
  const out = [];
  for (const p of pages) {
    const parts = p.html.split(/(?=<h[23] id=")/);
    for (const part of parts) {
      const hm = part.match(/^<h([23]) id="([^"]+)">([\s\S]*?)<a class="h-anchor"/);
      const text = stripTags(
        (hm ? part.slice(part.indexOf('</h' + hm[1] + '>') + 5) : part)
          .replace(/<p class="tab-label">[\s\S]*?<\/p>/g, ' ')
          .replace(/<span class="copy-label">[\s\S]*?<\/span>/g, ' ')
          .replace(/<div class="cmd-head">[\s\S]*?<\/div>/g, ' '),
      );
      out.push({
        p: p.title,
        u: p.url + (hm ? '#' + hm[2] : ''),
        s: hm ? stripTags(hm[3]) : '',
        x: (hm ? '' : p.description + ' ') + text.slice(0, 2400),
      });
    }
  }
  return out;
}

// ---------------------------------------------------------------- doğrulama

function checkLinks(allPages, extraFiles) {
  const byUrl = new Map(allPages.map((p) => [p.url, p]));
  for (const p of allPages) {
    const html = layout(p, p.siblings);
    for (const m of html.matchAll(/\s(?:href|src)="([^"]+)"/g)) {
      let href = m[1].replace(/&amp;/g, '&');
      if (/^(https?:|mailto:)/.test(href)) continue;
      if (href.startsWith('#')) href = p.url + href;
      const [pathPart, hash] = href.split('#');
      const clean = pathPart.replace(/\?v=dev$/, '');
      if (byUrl.has(clean)) {
        const target = byUrl.get(clean);
        if (hash && !target.ids.has(hash)) fail(`${p.file}: kırık çapa ${href}`);
        continue;
      }
      if (clean === '/' || clean === '/en/') {
        if (hash && !extraFiles.anchors[clean].has(hash)) fail(`${p.file}: ana sayfada çapa yok: ${href}`);
        continue;
      }
      if (clean.startsWith('/assets/docs-search-')) continue;
      if (!fs.existsSync(path.join(SITE, clean))) fail(`${p.file}: kırık bağlantı ${href}`);
    }
  }
}

// ---------------------------------------------------------------- çalıştır

function write(file, content) {
  fs.mkdirSync(path.dirname(file), { recursive: true });
  fs.writeFileSync(file, content);
}

const langs = { tr: loadPages('tr'), en: loadPages('en') };
for (const lang of ['tr', 'en']) {
  const other = lang === 'tr' ? 'en' : 'tr';
  for (const p of langs[lang]) {
    p.pair = langs[other].find((q) => q.id === p.id);
    if (!p.pair) fail(`${p.file}: "${p.id}" için ${other} eşi yok`);
    p.siblings = langs[lang];
  }
  const urls = new Set();
  for (const p of langs[lang]) {
    if (urls.has(p.url)) fail(`${p.file}: yinelenen adres ${p.url}`);
    urls.add(p.url);
  }
}
if (errors.length) {
  console.error(errors.join('\n'));
  process.exit(1);
}
for (const p of [...langs.tr, ...langs.en]) renderPage(p);

const anchorsOf = (file) => new Set([...fs.readFileSync(path.join(SITE, file), 'utf8').matchAll(/\sid="([^"]+)"/g)].map((m) => m[1]));
checkLinks([...langs.tr, ...langs.en], { anchors: { '/': anchorsOf('index.html'), '/en/': anchorsOf('en/index.html') } });

if (errors.length) {
  console.error(errors.join('\n'));
  // --lenient: yalnızca yerel taslak derlemesi için (hatalar uyarı olarak yazılır)
  if (!('lenient' in args)) process.exit(1);
}

fs.rmSync(OUT, { recursive: true, force: true });
for (const lang of ['tr', 'en']) {
  for (const p of langs[lang]) write(path.join(OUT, p.url, 'index.html'), layout(p, langs[lang]));
  write(path.join(OUT, T[lang].searchIndex), JSON.stringify(searchIndex(langs[lang])));
}

// Site haritası: ana sayfalar + belge çiftleri (hreflang ile).
const pairs = [['/', '/en/'], ...langs.tr.map((p) => [p.url, p.pair.url])];
const urlEntry = (loc, tr, en) => `  <url>
    <loc>${ORIGIN}${loc}</loc>
    <xhtml:link rel="alternate" hreflang="tr" href="${ORIGIN}${tr}"/>
    <xhtml:link rel="alternate" hreflang="en" href="${ORIGIN}${en}"/>
    <xhtml:link rel="alternate" hreflang="x-default" href="${ORIGIN}${tr}"/>
  </url>`;
write(
  path.join(OUT, 'sitemap.xml'),
  `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9" xmlns:xhtml="http://www.w3.org/1999/xhtml">
${pairs.flatMap(([tr, en]) => [urlEntry(tr, tr, en), urlEntry(en, tr, en)]).join('\n')}
</urlset>
`,
);

console.log(`belgeler: ${langs.tr.length} TR + ${langs.en.length} EN sayfa → ${OUT}`);
