/// <reference types="node" />
import { defineConfig, devices } from '@playwright/test';

const baseURL = process.env.BASE_URL || 'http://localhost:4173';
const isExternal = baseURL !== 'http://localhost:4173';

export default defineConfig({
  testDir: './tests',
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  workers: 1,
  reporter: 'html',
  timeout: 60000,
  use: {
    baseURL,
    ignoreHTTPSErrors: !!process.env.IGNORE_HTTPS_ERRORS,
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',
  },
  ...(!isExternal && {
    webServer: {
      command: 'pnpm build && pnpm preview',
      port: 4173,
      reuseExistingServer: !process.env.CI,
    },
  }),
  projects: [
    {
      name: 'chromium',
      use: {
        ...devices['Desktop Chrome'],
        launchOptions: {
          executablePath: process.env.BROWSER_PATH || undefined,
        },
      },
    },
  ],
});
