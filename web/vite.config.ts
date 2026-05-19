import { svelte } from '@sveltejs/vite-plugin-svelte';
import { defineConfig } from 'vite';

export default defineConfig({
  plugins: [svelte()],
  server: {
    proxy: {
      '/api': 'http://127.0.0.1:8097',
      '/healthz': 'http://127.0.0.1:8097'
    }
  },
  test: {
    environment: 'jsdom'
  }
});
