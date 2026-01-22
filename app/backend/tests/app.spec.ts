import { test, expect } from '@playwright/test';
import * as electron from './helpers/electron';

let electronContext: electron.ElectronTestContext;

const PLACEHOLDER_API_KEY = '550e8400-e29b-41d4-a716-446655440000';

test.describe('General functionality', () => {
  test.beforeEach(async () => {
    electronContext = await electron.launchElectronApp();
    await electron.waitForAppReady(electronContext.page);
    await electron.clearStorage(electronContext.page);
    await electron.setupTestApiKey(electronContext.page, PLACEHOLDER_API_KEY);
    await electronContext.page.reload();
    await electron.waitForAppReady(electronContext.page);
  });

  test.afterEach(async () => {
    if (electronContext?.app) {
      await electron.closeElectronApp(electronContext.app);
    }
  });

  test('should launch Electron app and display main window', async () => {
    const { page } = electronContext;

    await expect(page.locator('body')).toBeVisible();

    await expect(page.getByPlaceholder('Type a message...')).toBeVisible();
  });

  test('should have sidebar with navigation', async () => {
    const { page } = electronContext;

    await expect(page.getByRole('button', { name: /new chat/i })).toBeVisible();
    await expect(page.getByRole('link', { name: /settings/i })).toBeVisible();
  });

  test('should navigate to settings page', async () => {
    const { page } = electronContext;

    await page.getByRole('link', { name: /settings/i }).click();
    await expect(page.getByRole('heading', { name: 'Settings' })).toBeVisible();
  });

  test('should have proxy server running', async () => {
    const { page } = electronContext;

    const proxyPort = await page.evaluate(() => {
      return (window as any).electron.getProxyPort();
    });

    await expect(Number(proxyPort)).toBeGreaterThan(0);
    await expect(Number(proxyPort)).toBeLessThan(65536);
  });
});

test.describe('Settings page', () => {
  test.beforeEach(async () => {
    electronContext = await electron.launchElectronApp();
    await electron.waitForAppReady(electronContext.page);
    await electron.clearStorage(electronContext.page);
    await electron.setupTestApiKey(
      electronContext.page,
      '00000000-0000-0000-0000-000000000000',
    );
    await electronContext.page.reload();
    await electron.waitForAppReady(electronContext.page);
    await electronContext.page.getByRole('link', { name: /settings/i }).click();
  });

  test.afterEach(async () => {
    if (electronContext?.app) {
      await electron.closeElectronApp(electronContext.app);
    }
  });

  test('should validate and save API key', async () => {
    const { page } = electronContext;

    const validUUID = PLACEHOLDER_API_KEY;
    const input = page.locator('input[placeholder*="550e8400"]');

    await input.fill(validUUID);
    await page.getByRole('button', { name: /update/i }).click();

    await expect(
      page.getByText('Access key updated successfully'),
    ).toBeVisible();

    const storedKey = await page.evaluate(() =>
      localStorage.getItem('privatemode_api_key'),
    );
    expect(storedKey).toBe(validUUID);
  });

  test('should reject invalid API key format', async () => {
    const { page } = electronContext;

    const input = page.locator('input[placeholder*="550e8400"]');
    await input.fill('invalid-key');
    await page.getByRole('button', { name: /update/i }).click();

    await expect(page.getByText('Invalid access key format')).toBeVisible();
  });
});

test.describe('Chat storage', () => {
  test.beforeEach(async () => {
    electronContext = await electron.launchElectronApp();
    await electron.waitForAppReady(electronContext.page);
    await electron.clearStorage(electronContext.page);
    await electron.setupTestApiKey(electronContext.page, PLACEHOLDER_API_KEY);
    await electronContext.page.reload();
    await electron.waitForAppReady(electronContext.page);
  });

  test.afterEach(async () => {
    if (electronContext?.app) {
      await electron.closeElectronApp(electronContext.app);
    }
  });

  test('should persist chats across navigation', async () => {
    const { page } = electronContext;

    await page.evaluate(() => {
      const chat = {
        id: 'test chat',
        title: 'Test Chat',
        messages: [] as any[],
        createdAt: Date.now(),
        updatedAt: Date.now(),
        lastUserMessageAt: Date.now(),
        wordCount: 0,
      };
      localStorage.setItem('privatemode_chats', JSON.stringify([chat]));
    });

    await page.reload();
    await electron.waitForAppReady(page);

    await expect(
      page.getByRole('button', { name: /test chat/i }),
    ).toBeVisible();

    await page.getByRole('link', { name: /settings/i }).click();
    await expect(page.getByRole('heading', { name: 'Settings' })).toBeVisible();

    await page.getByRole('button', { name: /back/i }).click();

    await expect(
      page.getByRole('button', { name: /test chat/i }),
    ).toBeVisible();
  });

  test('should delete chat from sidebar', async () => {
    const { page } = electronContext;

    await page.evaluate(() => {
      const chat = {
        id: 'test chat',
        title: 'Test Chat',
        messages: [] as any[],
        createdAt: Date.now(),
        updatedAt: Date.now(),
        lastUserMessageAt: Date.now(),
        wordCount: 0,
      };
      localStorage.setItem('privatemode_chats', JSON.stringify([chat]));
    });

    await page.reload();
    await electron.waitForAppReady(page);

    page.on('dialog', (dialog) => dialog.accept());

    const chatItem = page.getByText('Test Chat').locator('..');
    await chatItem.hover();

    const deleteButton = chatItem.locator('button.delete-btn');
    await deleteButton.click();

    await expect(page.getByText('Test Chat')).not.toBeVisible();
  });

  test('should rename chat from sidebar', async () => {
    const { page } = electronContext;

    await page.evaluate(() => {
      const chat = {
        id: 'test chat',
        title: 'Test Chat',
        messages: [] as any[],
        createdAt: Date.now(),
        updatedAt: Date.now(),
        lastUserMessageAt: Date.now(),
        wordCount: 0,
      };
      localStorage.setItem('privatemode_chats', JSON.stringify([chat]));
    });

    await page.reload();
    await electron.waitForAppReady(page);

    const chatItem = page.getByText('Test Chat').locator('..');
    await chatItem.hover();

    const renameButton = chatItem.locator('button.rename-btn');
    await renameButton.click();

    const renameInput = page.locator('input.rename-input');
    await renameInput.fill('New Name');
    await renameInput.press('Enter');

    await expect(page.getByText('New Name')).toBeVisible();
    await expect(page.getByText('Test Chat')).not.toBeVisible();
  });
});

test.describe('Model interaction', () => {
  test.beforeEach(async () => {
    electronContext = await electron.launchElectronApp();
    await electron.waitForAppReady(electronContext.page);
    await electron.clearStorage(electronContext.page);
    await electron.setupTestApiKey(
      electronContext.page,
      process.env.PRIVATEMODE_API_KEY,
    );
    await electronContext.page.reload();
    await electron.waitForAppReady(electronContext.page);
  });

  test.afterEach(async () => {
    if (electronContext?.app) {
      await electron.closeElectronApp(electronContext.app);
    }
  });

  test('should fetch available models from API', async () => {
    const { page } = electronContext;

    const modelPicker = page.locator('.model-button');
    await expect(modelPicker).toBeVisible();
    await modelPicker.click();

    const modelOptions = page.locator('.model-option');
    await expect(modelOptions).not.toHaveCount(0, { timeout: 30000 });
  });

  test('should send a message and receive a response', async () => {
    const { page } = electronContext;

    const textarea = page.getByPlaceholder('Type a message...');
    await textarea.fill('Say "Smoke test completed" and nothing else.');

    const sendButton = page.locator('.send-button');

    await expect(sendButton).toBeEnabled({ timeout: 30000 }); // Wait until model is loaded
    await sendButton.click();

    await expect(
      page.getByText('Say "Smoke test completed" and nothing else.'),
    ).toBeVisible();

    await expect(
      page
        .locator('.message.assistant')
        .filter({ hasText: /Smoke test completed/i }),
    ).toBeVisible({ timeout: 30000 });
  });

  test('should handle stop generation', async () => {
    const { page } = electronContext;

    const textarea = page.getByPlaceholder('Type a message...');
    await textarea.fill('Write a long story about a journey.');

    const sendButton = page.locator('.send-button');
    await expect(sendButton).toBeEnabled({ timeout: 30000 }); // Wait until model is loaded
    await sendButton.click();

    const stopButton = page.locator('.stop');
    await expect(stopButton).toBeVisible({ timeout: 5000 });
    await stopButton.click();

    await expect(sendButton).toBeVisible({ timeout: 5000 });
  });

  test('should maintain chat history across sessions', async () => {
    const { page } = electronContext;

    const textarea = page.getByPlaceholder('Type a message...');
    await textarea.fill('Remember the number 42.');

    const sendButton = page.locator('.send-button');
    await expect(sendButton).toBeEnabled({ timeout: 30000 }); // Wait until model is loaded
    await sendButton.click();

    await electron.closeElectronApp(electronContext.app);

    electronContext = await electron.launchElectronApp();
    await electron.waitForAppReady(electronContext.page);

    await expect(
      electronContext.page
        .locator('.message.user')
        .filter({ hasText: /Remember the number 42./i }),
    ).toBeVisible();
  });
});

test.describe('Security page', () => {
  test.beforeEach(async () => {
    electronContext = await electron.launchElectronApp();
    await electron.waitForAppReady(electronContext.page);
    await electron.clearStorage(electronContext.page);
    await electron.setupTestApiKey(
      electronContext.page,
      process.env.PRIVATEMODE_API_KEY,
    );
    await electronContext.page.reload();
    await electron.waitForAppReady(electronContext.page);
  });

  test.afterEach(async () => {
    if (electronContext?.app) {
      await electron.closeElectronApp(electronContext.app);
    }
  });

  test('should display security status sections', async () => {
    const { page } = electronContext;

    const securityLink = page.getByRole('link', {
      name: /your session is secure/i,
    });
    await expect(securityLink).toBeVisible({ timeout: 30000 });
    await securityLink.click();

    await expect(
      page.getByRole('heading', { name: 'Security', exact: true }),
    ).toBeVisible();
    await expect(page.getByText('Remote attestation')).toBeVisible();
    await expect(page.getByText('Reproducible software')).toBeVisible();
    await expect(page.getByText('Hardware-based security')).toBeVisible();
  });

  test('should display manifest hash after loading', async () => {
    const { page } = electronContext;

    const securityLink = page.getByRole('link', {
      name: /your session is secure/i,
    });
    await expect(securityLink).toBeVisible({ timeout: 30000 });
    await securityLink.click();

    await expect(page.getByText('Manifest hash (SHA-256)')).toBeVisible({
      timeout: 10000,
    });
    await expect(page.locator('.data-value').first()).toBeVisible();
  });
});

test.describe('File upload', () => {
  test.beforeEach(async () => {
    electronContext = await electron.launchElectronApp();
    await electron.waitForAppReady(electronContext.page);
    await electron.clearStorage(electronContext.page);
    await electron.setupTestApiKey(
      electronContext.page,
      process.env.PRIVATEMODE_API_KEY,
    );
    await electronContext.page.reload();
    await electron.waitForAppReady(electronContext.page);
  });

  test.afterEach(async () => {
    if (electronContext?.app) {
      await electron.closeElectronApp(electronContext.app);
    }
  });

  test('should upload and process a text file', async () => {
    const { page } = electronContext;

    await page.waitForTimeout(2000);

    const fileContent = 'This is a test document with important information.';

    const fileInput = page.locator('input[type="file"]');

    await fileInput.setInputFiles({
      name: 'test.txt',
      mimeType: 'text/plain',
      buffer: Buffer.from(fileContent),
    });

    await expect(page.getByText('test.txt')).toBeVisible({ timeout: 30000 });

    const fileChip = page.locator('.file-chip').filter({ hasText: 'test.txt' });
    await expect(fileChip).toBeVisible();

    const removeButton = fileChip.locator('button');
    await removeButton.click();
    await expect(fileChip).not.toBeVisible();
  });
});
