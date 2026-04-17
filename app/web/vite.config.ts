import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';
import { sriPlugin } from './scripts/sri.js';

export default defineConfig({
  plugins: [sveltekit(), sriPlugin()],
  build: {
    rollupOptions: {
      // The JS SDK uses dynamic import() for Node-only modules behind a runtime browser check.
      // Suppress Vite's externalization warnings for these.
      external: ['node:fs/promises', 'node:path'],
    },
  },
});
