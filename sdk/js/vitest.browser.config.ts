import { resolve } from 'path';
import { defineConfig } from 'vitest/config';
import { playwright } from '@vitest/browser-playwright';

export default defineConfig({
  define: {
    'import.meta.env.PRIVATEMODE_API_KEY': JSON.stringify(
      process.env.PRIVATEMODE_API_KEY ?? '',
    ),
    'import.meta.env.PRIVATEMODE_RUN_INTEGRATION_TESTS': JSON.stringify(
      process.env.PRIVATEMODE_RUN_INTEGRATION_TESTS ?? '',
    ),
  },
  server: {
    fs: {
      // Serve files from sdk/*, which is needed to load the Wasm module.
      allow: [resolve(__dirname, '..'), '/nix/store'],
    },
  },
  test: {
    include: ['src/**/*.test.ts'],
    browser: {
      enabled: true,
      provider: playwright({
        launchOptions: {
          // Set BROWSER_PATH to test against a specific browser executable (e.g. a locally built Chromium).
          // Useful for running the tests on NixOS.
          ...(process.env.BROWSER_PATH && {
            executablePath: process.env.BROWSER_PATH,
          }),
        },
      }),
      instances: [{ browser: 'chromium' }],
    },
  },
});
