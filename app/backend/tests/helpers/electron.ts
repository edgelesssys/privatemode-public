import {
  _electron as electron,
  type ElectronApplication,
  type Page,
} from '@playwright/test';
import path from 'path';

export interface ElectronTestContext {
  app: ElectronApplication;
  page: Page;
}
export async function launchElectronApp(): Promise<ElectronTestContext> {
  let app: ElectronApplication;
  const backendDir = path.join(__dirname, '..', '..');
  try {
    app = await electron.launch({
      args: [path.join(backendDir, '.vite', 'build', 'main.js')],
      env: {
        ...process.env,
        NODE_ENV: 'development',
        PRIVATEMODE_IS_PLAYWRIGHT_TEST: '1',
      },
      timeout: 30000,
    });
  } catch (error) {
    console.error('Failed to launch Electron:', error);
    if (error instanceof Error) {
      console.error('Error message:', error.message);
      console.error('Error stack:', error.stack);
    }
    throw error;
  }
  app.process().stdout?.on('data', (data) => {
    console.log('[Electron stdout]:', data.toString());
  });
  app.process().stderr?.on('data', (data) => {
    console.error('[Electron stderr]:', data.toString());
  });

  const page = await app.firstWindow();

  await page.waitForLoadState('domcontentloaded');

  return { app, page };
}

export async function closeElectronApp(
  app: ElectronApplication,
): Promise<void> {
  await app.close();
}

export async function clearStorage(page: Page): Promise<void> {
  await page.waitForFunction(
    async () => {
      try {
        localStorage.clear();
        sessionStorage.clear();
      } catch {
        return false;
      }
    },
    { timeout: 10000 },
  );
}

export async function setupTestApiKey(
  page: Page,
  apiKey: string,
): Promise<void> {
  const testApiKey = apiKey;
  await page.evaluate((key) => {
    localStorage.setItem('privatemode_api_key', key);
  }, testApiKey);
}

export async function getProxyPort(page: Page): Promise<number> {
  const port = await page.evaluate(() => {
    return (window as any).electron.getProxyPort();
  });
  return port;
}

export async function waitForAppReady(page: Page): Promise<void> {
  await page.waitForFunction(
    async () => {
      try {
        const port = await (window as any).electron?.getProxyPort();
        return typeof port === 'string' && port.length > 0;
      } catch {
        return false;
      }
    },
    { timeout: 10000 },
  );
}
