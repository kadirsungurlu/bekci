import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';

// Geliştirme sırasında (npm run dev) API isteklerini yerelde çalışan Go sunucusuna yönlendirir.
const backend = 'http://127.0.0.1:18080';

export default defineConfig({
  base: '/',
  plugins: [svelte()],
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
