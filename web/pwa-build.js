// @ts-nocheck — Node'da çalışan derleme eklentisi (web/ için @types/node kurulu değil).
//
// Servis çalışanı (public/sw.js) derlemede sürümlenir: dosya adlarından bir derleme
// kimliği ve önbelleğe alınacak giriş dosyaları yazılır. Böylece her yeni sürümde
// sw.js değişir, tarayıcı yeni çalışanı kurar ve uygulama "Yeni sürüm hazır" der.
import { createHash } from 'node:crypto';
import { readFileSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';

/**
 * Ürünün görünen adı (src/lib/brand.ts → APP_NAME). Ad tek yerde tutulur:
 * index.html'deki __APP_NAME__ yer tutucuları ve derlenen manifest'in
 * name/short_name alanları buradan yazılır.
 */
export function appName() {
  const src = readFileSync(new URL('./src/lib/brand.ts', import.meta.url), 'utf8');
  const m = src.match(/export const APP_NAME\s*=\s*'([^']+)'/);
  if (!m) throw new Error("brand.ts: export const APP_NAME = '…' satırı bulunamadı");
  return m[1];
}

/** index.html'deki __APP_NAME__ yer tutucularını doldurur (geliştirme ve derleme). @returns {import('vite').Plugin} */
export function brandHtml() {
  return {
    name: 'uptime-brand-html',
    transformIndexHtml(html) {
      return html.replaceAll('__APP_NAME__', appName());
    },
  };
}

/** @returns {import('vite').Plugin} */
export function pwaBuild() {
  let outDir = 'dist';
  let files = [];
  return {
    name: 'uptime-pwa-build',
    apply: 'build',
    configResolved(c) {
      outDir = c.build.outDir;
    },
    generateBundle(_o, bundle) {
      files = Object.keys(bundle)
        .filter((f) => f.startsWith('assets/') && /\.(js|css)$/.test(f))
        .sort();
    },
    closeBundle() {
      const file = join(outDir, 'sw.js');
      const id = createHash('sha256').update(files.join('\n')).digest('hex').slice(0, 12);
      const src = readFileSync(file, 'utf8');
      const out = src
        .replace("'__BUILD__'", JSON.stringify(id))
        .replace('/* __PRECACHE__ */ []', JSON.stringify(files.map((f) => `/${f}`)));
      if (out === src) throw new Error('sw.js: derleme yer tutucuları bulunamadı');
      writeFileSync(file, out);

      // Manifest'teki ad brand.ts'den (public/manifest.webmanifest'teki değer yalnızca geliştirme içindir).
      const mf = join(outDir, 'manifest.webmanifest');
      const manifest = JSON.parse(readFileSync(mf, 'utf8'));
      manifest.name = manifest.short_name = appName();
      writeFileSync(mf, JSON.stringify(manifest, null, 2) + '\n');
    },
  };
}
