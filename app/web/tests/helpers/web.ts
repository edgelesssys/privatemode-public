import type { Page } from '@playwright/test';
import type { Chat as AppChat } from '../../src/lib/chatStore';

export type Chat = AppChat & { wordCount: number };

export async function resetApp(page: Page): Promise<void> {
  await page.goto('/');
  await waitForAppReady(page);
  await clearStorage(page);
  await page.reload();
  await waitForAppReady(page);
}

export async function clearStorage(page: Page): Promise<void> {
  await page.evaluate(() => {
    localStorage.clear();
    sessionStorage.clear();
  });
}

export async function waitForAppReady(page: Page): Promise<void> {
  await page.getByPlaceholder('Type a message...').waitFor({
    state: 'visible',
    timeout: 60000,
  });
}

export function createTestChat(overrides?: Partial<Chat>): Chat {
  const now = Date.now();
  return {
    id: crypto.randomUUID(),
    title: 'Test Chat',
    messages: [],
    createdAt: now,
    updatedAt: now,
    lastUserMessageAt: now,
    wordCount: 0,
    ...overrides,
  };
}

export async function injectTestChat(page: Page, chat: Chat): Promise<void> {
  await page.evaluate((c) => {
    const existing = localStorage.getItem('privatemode_chats');
    const chats = existing ? JSON.parse(existing) : [];
    chats.push(c);
    localStorage.setItem('privatemode_chats', JSON.stringify(chats));
  }, chat);
}
