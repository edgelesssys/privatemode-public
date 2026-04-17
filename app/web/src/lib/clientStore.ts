import { writable, get } from 'svelte/store';
import { PrivatemodeAI } from 'privatemode-ai';
import type { ExportedSecret, PrivatemodeAIOptions } from 'privatemode-ai';
import { activeApiKey } from './authStore';

export interface Model {
  id: string;
  object: string;
  tasks?: string[];
}

export const privatemodeClient = writable<PrivatemodeAI | null>(null);
export const clientReady = writable<boolean>(false);
export const clientError = writable<string | null>(null);
export const clientVerifying = writable<boolean>(true);
export const modelsLoaded = writable<boolean>(false);
export const models = writable<Model[]>([]);

const SECRET_STORAGE_KEY = 'privatemode-secret';
const MANIFEST_STORAGE_KEY = 'privatemode-manifest';

interface CachedSession {
  secret: ExportedSecret;
  manifestBase64: string;
}

function loadCachedSession(): CachedSession | null {
  try {
    const secretRaw = sessionStorage.getItem(SECRET_STORAGE_KEY);
    const manifestRaw = sessionStorage.getItem(MANIFEST_STORAGE_KEY);
    if (!secretRaw || !manifestRaw) return null;
    const secret = JSON.parse(secretRaw) as ExportedSecret;
    if (secret.expiresAtUnix * 1000 <= Date.now()) {
      sessionStorage.removeItem(SECRET_STORAGE_KEY);
      sessionStorage.removeItem(MANIFEST_STORAGE_KEY);
      return null;
    }
    return { secret, manifestBase64: manifestRaw };
  } catch {
    sessionStorage.removeItem(SECRET_STORAGE_KEY);
    sessionStorage.removeItem(MANIFEST_STORAGE_KEY);
    return null;
  }
}

function saveCachedSession(
  secret: ExportedSecret,
  manifestBase64: string,
): void {
  try {
    sessionStorage.setItem(SECRET_STORAGE_KEY, JSON.stringify(secret));
    sessionStorage.setItem(MANIFEST_STORAGE_KEY, manifestBase64);
  } catch {
    // sessionStorage may be unavailable; ignore.
  }
}

let refreshTimer: ReturnType<typeof setTimeout> | null = null;

function scheduleSecretRefresh(
  client: PrivatemodeAI,
  expiresAtUnix: number,
  clientOpts: PrivatemodeAIOptions,
): void {
  if (refreshTimer !== null) clearTimeout(refreshTimer);
  // Refresh 60 seconds before expiration (the server-side buffer is 5min,
  // so this is well within the valid window).
  const msUntilRefresh = expiresAtUnix * 1000 - Date.now() - 60_000;
  if (msUntilRefresh <= 0) return;
  refreshTimer = setTimeout(async () => {
    try {
      // If the client was initialized offline (from cache), a plain
      // refreshSecret() will fail because the secret manager was never
      // set up. Fall back to a full re-initialization: create a fresh
      // client so verify() fetches the current manifest instead of
      // reusing the (potentially stale) cached one.
      try {
        await client.refreshSecret();
      } catch {
        const fresh = new PrivatemodeAI(clientOpts);
        await fresh.verify();
        await fresh.refreshSecret();
        client = fresh;
        privatemodeClient.set(client);
      }
      const exported = client.exportSecret();
      const manifestBytes = client.manifestBytes!;
      saveCachedSession(exported, btoa(String.fromCharCode(...manifestBytes)));
      scheduleSecretRefresh(client, exported.expiresAtUnix, clientOpts);
    } catch (e) {
      console.error('Background secret refresh failed:', e);
    }
  }, msUntilRefresh);
}

export async function initializeClient(): Promise<void> {
  const apiKey = get(activeApiKey);
  clientError.set(null);
  clientReady.set(false);
  clientVerifying.set(true);
  modelsLoaded.set(false);
  try {
    const configuredManifestBytes = import.meta.env
      .VITE_PRIVATEMODE_MANIFEST_BASE64
      ? Uint8Array.from(
          atob(import.meta.env.VITE_PRIVATEMODE_MANIFEST_BASE64),
          (c) => c.charCodeAt(0),
        )
      : undefined;
    const opts = {
      apiKey,
      apiBaseURL: import.meta.env.VITE_PRIVATEMODE_URL || undefined,
      manifestBytes: configuredManifestBytes,
      dangerouslyAllowBrowser: true,
      browserWasmURL: import.meta.env.VITE_WASM_URL || undefined,
      expectedWasmHash: import.meta.env.VITE_WASM_HASH || undefined,
      onManifestUpdate: (manifestBytes: Uint8Array) => {
        // Update cached manifest when it changes internally
        const manifestBase64 = btoa(String.fromCharCode(...manifestBytes));
        sessionStorage.setItem(MANIFEST_STORAGE_KEY, manifestBase64);
      },
      onSecretUpdate: (secret: ExportedSecret) => {
        // Update cached secret and reschedule refresh when it changes
        const client = get(privatemodeClient);
        if (!client) return;
        const manifestBytes = client.manifestBytes;
        if (manifestBytes) {
          const manifestBase64 = btoa(String.fromCharCode(...manifestBytes));
          saveCachedSession(secret, manifestBase64);
          scheduleSecretRefresh(client, secret.expiresAtUnix, opts);
        }
      },
    };
    const client = new PrivatemodeAI(opts);
    const cached = loadCachedSession();
    if (cached) {
      const manifestBytes =
        configuredManifestBytes ??
        Uint8Array.from(atob(cached.manifestBase64), (c) => c.charCodeAt(0));
      await client.initializeOffline(manifestBytes);
      client.importSecret(cached.secret);
      scheduleSecretRefresh(client, cached.secret.expiresAtUnix, opts);
    } else {
      await client.verify();
      await client.refreshSecret();
      const exported = client.exportSecret();
      const manifestBytes = client.manifestBytes!;
      saveCachedSession(exported, btoa(String.fromCharCode(...manifestBytes)));
      scheduleSecretRefresh(client, exported.expiresAtUnix, opts);
    }

    privatemodeClient.set(client);
    clientReady.set(true);
  } catch (e) {
    privatemodeClient.set(null);
    clientError.set(e instanceof Error ? e.message : 'Verification failed');
    console.error('Failed to verify Privatemode deployment:', e);
  } finally {
    clientVerifying.set(false);
  }
}
