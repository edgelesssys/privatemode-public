import { writable, derived } from 'svelte/store';
import type { Clerk as ClerkType } from '@clerk/clerk-js';
import { ui as clerkUI } from '@clerk/ui';

// Fallback API key for unauthenticated users.
const FALLBACK_API_KEY = '15785f3d-e0b3-4c59-8b58-edaa77606084';

const API_KEY_STORAGE_KEY = 'privatemode-user-api-key';

export interface OrgMembership {
  id: string;
  name: string;
  imageUrl: string | null;
}

export const clerkInstance = writable<ClerkType | null>(null);
export const clerkLoaded = writable(false);
export const isSignedIn = writable(false);
export const userDisplayName = writable<string | null>(null);
export const userImageUrl = writable<string | null>(null);
export const orgName = writable<string | null>(null);
export const orgId = writable<string | null>(null);
export const userOrganizations = writable<OrgMembership[]>([]);

export const activeApiKey = writable<string>(FALLBACK_API_KEY);
export const apiKeyError = writable<string | null>(null);
export const isLimitedMode = derived(
  [isSignedIn, activeApiKey],
  ([$isSignedIn, $activeApiKey]) =>
    !$isSignedIn || $activeApiKey === FALLBACK_API_KEY,
);

function loadCachedApiKey(): string | null {
  try {
    return sessionStorage.getItem(API_KEY_STORAGE_KEY);
  } catch {
    return null;
  }
}

function saveCachedApiKey(key: string): void {
  try {
    sessionStorage.setItem(API_KEY_STORAGE_KEY, key);
  } catch {
    console.warn('Unable to cache API key in sessionStorage');
  }
}

function clearCachedApiKey(): void {
  try {
    sessionStorage.removeItem(API_KEY_STORAGE_KEY);
  } catch {
    console.warn('Unable to clear cached API key from sessionStorage');
  }
}

function keysUrl(): string {
  const baseUrl = 'https://api-key-service-403278544087.europe-west1.run.app';
  const pathPrefix = import.meta.env.VITE_API_KEY_SERVICE_PATH_PREFIX || '';
  return `${baseUrl}${pathPrefix}/api/v2/keys`;
}

function authHeaders(token: string): Record<string, string> {
  return {
    Authorization: `Bearer ${token}`,
    'Content-Type': 'application/json',
  };
}

async function createWebAppKey(token: string): Promise<string> {
  const response = await fetch(keysUrl(), {
    method: 'POST',
    headers: authHeaders(token),
    body: JSON.stringify({
      name: 'chat.privatemode.ai',
      comment: "API key that's used by the Privatemode web app",
      is_webapp_key: true,
    }),
  });

  if (!response.ok) {
    throw new Error(`Failed to create web app API key: ${response.statusText}`);
  }

  const key = await response.json();
  return key.license_key;
}

async function fetchApiKeyFromService(token: string): Promise<string> {
  const response = await fetch(keysUrl(), {
    headers: authHeaders(token),
  });

  if (!response.ok) {
    throw new Error(`Failed to fetch API keys: ${response.statusText}`);
  }

  const keys = await response.json();

  const webAppKey = (Array.isArray(keys) ? keys : []).find(
    (k: { name: string }) => k.name === 'chat.privatemode.ai',
  );
  if (webAppKey) {
    return webAppKey.license_key;
  }

  // Auto-create the web app key if none exists for this org.
  return createWebAppKey(token);
}

export async function initializeAuth(): Promise<void> {
  const publishableKey = import.meta.env.VITE_CLERK_PUBLISHABLE_KEY;
  if (!publishableKey) {
    console.warn('No Clerk publishable key configured; auth disabled');
    clerkLoaded.set(true);
    return;
  }

  try {
    const { Clerk } = await import('@clerk/clerk-js');
    const clerk = new Clerk(publishableKey);
    await clerk.load({
      ui: clerkUI,
      signInUrl: 'https://portal.privatemode.ai/sign-in',
      signUpUrl: 'https://portal.privatemode.ai/sign-up',
    });

    clerkInstance.set(clerk);
    clerkLoaded.set(true);

    const updateAuthState = () => {
      const session = clerk.session;
      const user = clerk.user;
      const org = clerk.organization;

      if (session && user) {
        isSignedIn.set(true);
        userDisplayName.set(
          user.fullName || user.primaryEmailAddress?.emailAddress || 'User',
        );
        userImageUrl.set(user.imageUrl || null);
        orgName.set(org?.name || null);
        orgId.set(org?.id || null);

        user
          .getOrganizationMemberships()
          .then(({ data }) => {
            userOrganizations.set(
              data.map((m) => ({
                id: m.organization.id,
                name: m.organization.name,
                imageUrl: m.organization.imageUrl || null,
              })),
            );
          })
          .catch((err: unknown) => {
            console.error('Failed to fetch organization memberships:', err);
            userOrganizations.set([]);
          });

        const cached = loadCachedApiKey();
        if (cached) {
          activeApiKey.set(cached);
        } else {
          session
            .getToken()
            .then((token: string | null) => {
              if (token) return fetchApiKeyFromService(token);
              throw new Error('No session token');
            })
            .then((key: string) => {
              activeApiKey.set(key);
              saveCachedApiKey(key);
              apiKeyError.set(null);
            })
            .catch((err: unknown) => {
              console.error('Failed to fetch API key:', err);
              const message =
                err instanceof Error ? err.message : 'Failed to fetch API key';
              apiKeyError.set(message);
              activeApiKey.set(FALLBACK_API_KEY);
            });
        }
      } else {
        isSignedIn.set(false);
        userDisplayName.set(null);
        userImageUrl.set(null);
        orgName.set(null);
        orgId.set(null);
        userOrganizations.set([]);
        activeApiKey.set(FALLBACK_API_KEY);
        clearCachedApiKey();
      }
    };

    clerk.addListener(updateAuthState);
  } catch (err) {
    console.error('Failed to initialize Clerk:', err);
    clerkLoaded.set(true);
  }
}

export async function switchOrganization(
  organizationId: string,
): Promise<void> {
  let clerk: ClerkType | null = null;
  clerkInstance.subscribe((c) => (clerk = c))();
  if (clerk) {
    await (clerk as ClerkType).setActive({ organization: organizationId });
    clearCachedApiKey();
    sessionStorage.removeItem('privatemode-secret');
    sessionStorage.removeItem('privatemode-manifest');
  }
}

export async function signIn(): Promise<void> {
  let clerk: ClerkType | null = null;
  clerkInstance.subscribe((c) => (clerk = c))();
  if (clerk) {
    await (clerk as ClerkType).openSignIn();
  }
}

export async function signOut(): Promise<void> {
  let clerk: ClerkType | null = null;
  clerkInstance.subscribe((c) => (clerk = c))();
  if (clerk) {
    clearCachedApiKey();
    // Clear the privatemode session cache so a new session is created with the new key
    sessionStorage.removeItem('privatemode-secret');
    sessionStorage.removeItem('privatemode-manifest');
    await (clerk as ClerkType).signOut();
  }
}
