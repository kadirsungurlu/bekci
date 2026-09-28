// Ana ekran (PWA) simgelerini icon.svg'den PNG olarak üretir: web/public/icons/*.png
//
// Playwright'lı bir kapta çalıştırılır (Chromium SVG'yi birebir çizer). playwright
// paketi web/ bağımlılığı değildir; ayrı bir node_modules klasöründen bağlanır:
//   docker run --rm -v "$PWD/web":/pw/web -v <playwright'lı node_modules>:/pw/node_modules \
//     -w /pw mcr.microsoft.com/playwright:v1.55.0-noble node /pw/web/scripts/icons/render.mjs
//
// Simge tasarımı favicon ile aynıdır (koyu zeminde yeşil durum noktası). "maskable"
// sürümde nokta, Android'in maske güvenli alanı (merkezdeki %80'lik daire) içinde kalır.
import { readFileSync, mkdirSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { chromium } from 'playwright';

const here = dirname(fileURLToPath(import.meta.url));
const out = join(here, '../../public/icons');
mkdirSync(out, { recursive: true });

const regular = readFileSync(join(here, 'icon.svg'), 'utf8');
const maskable = readFileSync(join(here, 'icon-maskable.svg'), 'utf8');

const jobs = [
  { svg: regular, size: 180, file: 'apple-touch-icon.png' },
  { svg: regular, size: 192, file: 'icon-192.png' },
  { svg: regular, size: 512, file: 'icon-512.png' },
  { svg: maskable, size: 512, file: 'icon-maskable-512.png' },
];

const browser = await chromium.launch();
const page = await browser.newPage({ deviceScaleFactor: 1 });
for (const j of jobs) {
  await page.setViewportSize({ width: j.size, height: j.size });
  const svg = j.svg.replace('<svg ', `<svg width="${j.size}" height="${j.size}" `);
  await page.setContent(`<!doctype html><html><body style="margin:0;background:#0f1623">${svg}</body></html>`);
  await page.screenshot({ path: join(out, j.file), clip: { x: 0, y: 0, width: j.size, height: j.size } });
  console.log('yazıldı:', j.file);
}
await browser.close();
