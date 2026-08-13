import adapter from '@sveltejs/adapter-static';
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';

const versionName =
  process.env.SOURCE_DATE_EPOCH ??
  (process.env.PRIVATEMODE_WEB_BUILD_VERSION ??= Date.now().toString());

/** @type {import('@sveltejs/kit').Config} */
const config = {
  preprocess: vitePreprocess(),

  kit: {
    adapter: adapter({
      pages: 'build',
      assets: 'build',
      fallback: 'index.html',
      precompress: false,
      strict: true,
    }),
    version: {
      name: versionName,
    },
  },
};

export default config;
