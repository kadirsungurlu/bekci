// @ts-nocheck — Node'da çalışan derleme eklentisi (web/ için @types/node kurulu değil).
//
// Servis çalışanı (public/sw.js) derlemede sürümlenir: dosya adlarından bir derleme
// kimliği ve önbelleğe alınacak giriş dosyaları yazılır. Böylece her yeni sürümde
// sw.js değişir, tarayıcı yeni çalışanı kurar ve uygulama "Yeni sürüm hazır" der.
import { createHash } from 'node:crypto';
import { readFileSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';

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
    },
  };
}
