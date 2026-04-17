import { test, expect } from '@playwright/test';
import * as web from './helpers/web';

test.describe('General functionality', () => {
  test.beforeEach(async ({ page }) => {
    await web.resetApp(page);
  });

  test('should display main page with chat input', async ({ page }) => {
    await expect(page.getByPlaceholder('Type a message...')).toBeVisible();
  });

  test('should have sidebar with navigation', async ({ page }) => {
    await expect(page.locator('.new-chat-btn')).toBeVisible();
    await expect(page.locator('a[href="/settings"]')).toBeVisible();
  });

  test('should navigate to settings page', async ({ page }) => {
    await page.locator('a[href="/settings"]').click();
    await expect(page.getByRole('heading', { name: 'Settings' })).toBeVisible();
  });
});

test.describe('Settings page', () => {
  test.beforeEach(async ({ page }) => {
    await web.resetApp(page);
    await page.locator('a[href="/settings"]').click();
  });

  test('should display settings with theme picker', async ({ page }) => {
    await expect(page.getByRole('heading', { name: 'Settings' })).toBeVisible();
    await expect(
      page.getByRole('heading', { name: 'Appearance' }),
    ).toBeVisible();
    await expect(page.getByText('System')).toBeVisible();
    await expect(page.getByText('Light')).toBeVisible();
    await expect(page.getByText('Dark')).toBeVisible();
  });

  test('should switch theme', async ({ page }) => {
    const darkButton = page.locator('.theme-option', { hasText: 'Dark' });
    await darkButton.click();
    await expect(darkButton).toHaveClass(/active/);
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
    await expect(page.locator('meta[name="theme-color"]')).toHaveAttribute(
      'content',
      '#18181B',
    );
  });

  test('should navigate back to chat', async ({ page }) => {
    await page.locator('.back-btn').click();
    await expect(page.getByPlaceholder('Type a message...')).toBeVisible();
  });

  test('should delete all chats', async ({ page }) => {
    // Go back to inject a chat first
    await page.goto('/');
    await web.waitForAppReady(page);

    const chat = web.createTestChat();
    await web.injectTestChat(page, chat);
    await page.reload();
    await web.waitForAppReady(page);

    await expect(
      page.locator('.chat-item-wrapper').filter({ hasText: 'Test Chat' }),
    ).toBeVisible();

    // Navigate to settings and delete all chats
    await page.locator('a[href="/settings"]').click();
    await page.locator('.danger-btn').click();
    await page.locator('.confirm-danger-btn').click();

    // Should redirect to chat page with no chats
    await web.waitForAppReady(page);
    await expect(
      page.locator('.chat-item-wrapper').filter({ hasText: 'Test Chat' }),
    ).not.toBeVisible();
  });
});

test.describe('Chat storage', () => {
  test.beforeEach(async ({ page }) => {
    await web.resetApp(page);
  });

  test('should persist chats across navigation', async ({ page }) => {
    const chat = web.createTestChat();
    await web.injectTestChat(page, chat);
    await page.reload();
    await web.waitForAppReady(page);

    await expect(
      page.locator('.chat-item-wrapper').filter({ hasText: 'Test Chat' }),
    ).toBeVisible();

    await page.locator('a[href="/settings"]').click();
    await expect(page.getByRole('heading', { name: 'Settings' })).toBeVisible();

    await page.locator('.back-btn').click();

    await expect(
      page.locator('.chat-item-wrapper').filter({ hasText: 'Test Chat' }),
    ).toBeVisible();
  });

  test('should delete chat from sidebar', async ({ page }) => {
    const chat = web.createTestChat();
    await web.injectTestChat(page, chat);
    await page.reload();
    await web.waitForAppReady(page);

    page.on('dialog', (dialog) => dialog.accept());

    const chatItem = page
      .locator('.chat-item-wrapper')
      .filter({ hasText: 'Test Chat' });
    await chatItem.hover();

    const deleteButton = chatItem.locator('button.delete-btn');
    await deleteButton.click();

    await expect(
      page.locator('.chat-item-wrapper').filter({ hasText: 'Test Chat' }),
    ).not.toBeVisible();
  });

  test('should rename chat from sidebar', async ({ page }) => {
    const chat = web.createTestChat();
    await web.injectTestChat(page, chat);
    await page.reload();
    await web.waitForAppReady(page);

    const chatItem = page
      .locator('.chat-item-wrapper')
      .filter({ hasText: 'Test Chat' });
    await chatItem.hover();

    const renameButton = chatItem.locator('button.rename-btn');
    await renameButton.click();

    const renameInput = page.locator('input.rename-input');
    await renameInput.fill('New Name');
    await renameInput.press('Enter');

    await expect(
      page.locator('.chat-item-wrapper').filter({ hasText: 'New Name' }),
    ).toBeVisible();
    await expect(
      page.locator('.chat-item-wrapper').filter({ hasText: 'Test Chat' }),
    ).not.toBeVisible();
  });
});

test.describe('Model interaction', () => {
  test.beforeEach(async ({ page }) => {
    await web.resetApp(page);
  });

  test('should fetch available models', async ({ page }) => {
    const modelPicker = page.locator('.model-button');
    await expect(modelPicker).toBeVisible();
    await modelPicker.click();

    const modelOptions = page.locator('.model-option');
    await expect(modelOptions).not.toHaveCount(0, { timeout: 30000 });
  });

  test('should send a message and receive a response', async ({ page }) => {
    const textarea = page.getByPlaceholder('Type a message...');
    await textarea.fill('Say "Smoke test completed" and nothing else.');

    const sendButton = page.locator('.send-button');
    await expect(sendButton).toBeEnabled({ timeout: 30000 });
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

  test('should handle stop generation', async ({ page }) => {
    const textarea = page.getByPlaceholder('Type a message...');
    await textarea.fill('Write a long story about a journey.');

    const sendButton = page.locator('.send-button:not(.stop)');
    await expect(sendButton).toBeEnabled({ timeout: 30000 });
    await sendButton.click();

    const stopButton = page.locator('.send-button.stop');
    await expect(stopButton).toBeVisible({ timeout: 5000 });
    await stopButton.click();

    await expect(stopButton).toBeHidden({ timeout: 5000 });
    await expect(sendButton).toBeVisible({ timeout: 5000 });
  });

  test('should maintain chat history across sessions', async ({ page }) => {
    const textarea = page.getByPlaceholder('Type a message...');
    await textarea.fill('Remember the number 42.');

    const sendButton = page.locator('.send-button');
    await expect(sendButton).toBeEnabled({ timeout: 30000 });
    await sendButton.click();

    await expect(
      page
        .locator('.message.user')
        .filter({ hasText: /Remember the number 42./i }),
    ).toBeVisible({ timeout: 30000 });

    await page.reload();
    await web.waitForAppReady(page);

    await expect(
      page
        .locator('.message.user')
        .filter({ hasText: /Remember the number 42./i }),
    ).toBeVisible();
  });
});

test.describe('Security page', () => {
  test.beforeEach(async ({ page }) => {
    await web.resetApp(page);
  });

  test('should display security status sections', async ({ page }) => {
    const securityLink = page.locator('a.security-info[href="/security"]');
    await expect(securityLink).toBeVisible({ timeout: 30000 });
    await securityLink.click();

    await expect(
      page.getByRole('heading', { name: 'Security', exact: true }),
    ).toBeVisible();
    await expect(page.getByText('Remote attestation')).toBeVisible();
    await expect(page.getByText('Reproducible software')).toBeVisible();
    await expect(page.getByText('Hardware-based security')).toBeVisible();
  });

  test('should display manifest hash after loading', async ({ page }) => {
    const securityLink = page.locator('a.security-info[href="/security"]');
    await expect(securityLink).toBeVisible({ timeout: 30000 });
    await securityLink.click();

    await expect(page.getByText('Manifest hash (SHA-256)')).toBeVisible({
      timeout: 10000,
    });
    const manifestBlock = page.locator('.data-block', {
      hasText: 'Manifest hash (SHA-256)',
    });
    await expect(manifestBlock.locator('.data-value')).toBeVisible();
  });
});

test.describe('File upload', () => {
  test.beforeEach(async ({ page }) => {
    await web.resetApp(page);
  });

  test('should upload and process a text file', async ({ page }) => {
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
