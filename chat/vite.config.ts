import adapter from '@sveltejs/adapter-static';
import { sveltekit } from '@sveltejs/kit/vite';
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';
import { defineConfig } from 'vite';
import { sriPlugin } from './scripts/sri.js';

const versionName =
  process.env.SOURCE_DATE_EPOCH ??
  (process.env.PRIVATEMODE_WEB_BUILD_VERSION ??= Date.now().toString());

export default defineConfig({
  plugins: [
    sveltekit({
      preprocess: vitePreprocess(),
      adapter: adapter({
        pages: 'build',
        assets: 'build',
        fallback: 'index.html',
        precompress: false,
        strict: true,
      }),
      version: { name: versionName, pollInterval: 0 },
    }),
    sriPlugin(),
  ],
  resolve: {
    // Resolve the SDK's OpenAI dependency from the application when the SDK is
    // linked from the workspace or a Nix build output.
    dedupe: ['openai'],
  },
  build: {
    rollupOptions: {
      // The JS SDK uses dynamic import() for Node-only modules behind a runtime browser check.
      // Suppress Vite's externalization warnings for these.
      external: ['node:fs/promises', 'node:path'],
    },
  },
});
