import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import { pwaBuild } from './pwa-build.js';

// Geliştirme sırasında (npm run dev) API isteklerini yerelde çalışan Go sunucusuna yönlendirir.
const backend = 'http://127.0.0.1:18080';

export default defineConfig({
  base: '/',
  // pwaBuild: public/sw.js'e derleme kimliği ve önbellek listesi yazar (bkz. pwa-build.js).
  plugins: [svelte(), pwaBuild()],
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    assetsDir: 'assets',
    target: 'es2022',
    sourcemap: false,
  },
  server: {
    proxy: {
      '/api': { target: backend, changeOrigin: false },
    },
  },
});
