import type { ExportedSecret, PrivatemodeAIOptions } from 'privatemode-ai';
import { PrivatemodeAI } from 'privatemode-ai';
import { get, writable } from 'svelte/store';
import { authIdentity, getAuthCredential } from './authStore';
import { translate } from './i18n';

export interface Model {
  id: string;
  object: string;
  tasks?: string[];
}

export type PrivatemodeClient = PrivatemodeAI;

export const privatemodeClient = writable<PrivatemodeClient | null>(null);
export const clientReady = writable<boolean>(false);
export const clientError = writable<string | null>(null);
export const clientVerifying = writable<boolean>(true);
export const modelsLoaded = writable<boolean>(false);
export const modelsLoading = writable<boolean>(false);
export const modelsError = writable<string | null>(null);
export const models = writable<Model[]>([]);

const SECRET_STORAGE_KEY = 'privatemode-secret';
const MANIFEST_STORAGE_KEY = 'privatemode-manifest';
const IDENTITY_STORAGE_KEY = 'privatemode-identity';

interface CachedSession {
  secret: ExportedSecret;
  manifestBase64: string;
}

function clearCachedSession(): void {
  sessionStorage.removeItem(SECRET_STORAGE_KEY);
  sessionStorage.removeItem(MANIFEST_STORAGE_KEY);
  sessionStorage.removeItem(IDENTITY_STORAGE_KEY);
}

function loadCachedSession(identity: string): CachedSession | null {
  try {
    const secretRaw = sessionStorage.getItem(SECRET_STORAGE_KEY);
    const manifestRaw = sessionStorage.getItem(MANIFEST_STORAGE_KEY);
    const cachedIdentity = sessionStorage.getItem(IDENTITY_STORAGE_KEY);
    if (!secretRaw || !manifestRaw) return null;
    if (cachedIdentity !== identity) {
      clearCachedSession();
      return null;
    }
    const secret = JSON.parse(secretRaw) as ExportedSecret;
    if (secret.expiresAtUnix * 1000 <= Date.now()) {
      clearCachedSession();
      return null;
    }
    return { secret, manifestBase64: manifestRaw };
  } catch {
    clearCachedSession();
    return null;
  }
}

function saveCachedSession(
  secret: ExportedSecret,
  manifestBase64: string,
  identity: string,
): void {
  try {
    sessionStorage.setItem(SECRET_STORAGE_KEY, JSON.stringify(secret));
    sessionStorage.setItem(MANIFEST_STORAGE_KEY, manifestBase64);
    sessionStorage.setItem(IDENTITY_STORAGE_KEY, identity);
  } catch {
    // sessionStorage may be unavailable; ignore.
  }
}

let refreshTimer: ReturnType<typeof setTimeout> | null = null;
let nextRefreshTime: number | null = null;
let currentClient: PrivatemodeAI | null = null;
let currentClientOpts: PrivatemodeAIOptions | null = null;
let currentIdentity: string | null = null;
let refreshInProgress = false;

function setClient(client: PrivatemodeAI | null): void {
  privatemodeClient.set(client);
}

async function performSecretRefresh(): Promise<void> {
  if (!currentClient || !currentClientOpts || currentIdentity === null) return;
  if (refreshInProgress) {
    console.log('Secret refresh already in progress, skipping...');
    return;
  }
  refreshInProgress = true;
  try {
    console.log('Refreshing Privatemode secret...');
    // If the client was initialized offline (from cache), a plain
    // refreshSecret() will fail because the secret manager was never
    // set up. Fall back to a full re-initialization: create a fresh
    // client so verify() fetches the current manifest instead of
    // reusing the (potentially stale) cached one.
    try {
      await currentClient.refreshSecret();
    } catch {
      const fresh = new PrivatemodeAI(currentClientOpts);
      await fresh.verify();
      await fresh.refreshSecret();
      currentClient = fresh;
      setClient(fresh);
    }
    const exported = currentClient.exportSecret();
    const manifestBytes = currentClient.manifestBytes!;
    saveCachedSession(
      exported,
      btoa(String.fromCharCode(...manifestBytes)),
      currentIdentity,
    );
    scheduleSecretRefresh(
      currentClient,
      exported.expiresAtUnix,
      currentClientOpts,
      currentIdentity,
    );
  } catch (e) {
    console.error('Background secret refresh failed:', e);
  } finally {
    refreshInProgress = false;
  }
}

function scheduleSecretRefresh(
  client: PrivatemodeAI,
  expiresAtUnix: number,
  clientOpts: PrivatemodeAIOptions,
  identity: string,
): void {
  currentClient = client;
  currentClientOpts = clientOpts;
  currentIdentity = identity;
  if (refreshTimer !== null) clearTimeout(refreshTimer);
  // Refresh 60 seconds before expiration (the server-side buffer is 5min,
  // so this is well within the valid window).
  const msUntilRefresh = expiresAtUnix * 1000 - Date.now() - 60_000;
  nextRefreshTime = Date.now() + msUntilRefresh;
  refreshTimer = setTimeout(performSecretRefresh, msUntilRefresh);
}

// Handle visibility changes to refresh secret if timer was missed while tab was hidden
function handleVisibilityChange(): void {
  if (document.visibilityState === 'visible' && nextRefreshTime !== null) {
    // Check if we're past the scheduled refresh time
    if (Date.now() >= nextRefreshTime) {
      console.log(
        'Tab became visible and refresh is overdue, triggering now...',
      );
      performSecretRefresh();
    }
  }
}

// Set up visibility listener once
if (typeof document !== 'undefined') {
  document.removeEventListener('visibilitychange', handleVisibilityChange);
  document.addEventListener('visibilitychange', handleVisibilityChange);
}

let modelsRequest: Promise<Model[]> | null = null;

export async function loadModels(
  client: PrivatemodeClient | null = get(privatemodeClient),
): Promise<Model[]> {
  if (!client) return [];
  if (get(modelsLoaded)) return get(models);
  if (modelsRequest) return modelsRequest;

  modelsLoading.set(true);
  modelsError.set(null);
  modelsRequest = (async () => {
    const response = await client.models.list();
    const loadedModels: Model[] = response.data;
    models.set(loadedModels);
    modelsLoaded.set(true);
    return loadedModels;
  })();

  try {
    return await modelsRequest;
  } catch (e) {
    const message =
      e instanceof Error ? e.message : translate('model.loadFailed');
    modelsError.set(message);
    modelsLoaded.set(false);
    throw e;
  } finally {
    modelsLoading.set(false);
    modelsRequest = null;
  }
}

export async function initializeClient(): Promise<void> {
  clientError.set(null);
  clientReady.set(false);
  clientVerifying.set(true);
  modelsLoaded.set(false);
  modelsError.set(null);
  try {
    const identity = get(authIdentity);
    const configuredManifestBytes = import.meta.env
      .VITE_PRIVATEMODE_MANIFEST_BASE64
      ? Uint8Array.from(
          atob(import.meta.env.VITE_PRIVATEMODE_MANIFEST_BASE64),
          (c) => c.charCodeAt(0),
        )
      : undefined;
    const opts = {
      auth: getAuthCredential,
      openAIOptions: {
        defaultHeaders: { 'Privatemode-Client': 'WebApp' },
      },
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
          saveCachedSession(secret, manifestBase64, identity);
          scheduleSecretRefresh(client, secret.expiresAtUnix, opts, identity);
        }
      },
    };
    const client = new PrivatemodeAI(opts);
    const cached = loadCachedSession(identity);
    if (cached) {
      const manifestBytes =
        configuredManifestBytes ??
        Uint8Array.from(atob(cached.manifestBase64), (c) => c.charCodeAt(0));
      await client.initializeOffline(manifestBytes);
      client.importSecret(cached.secret);
      scheduleSecretRefresh(
        client,
        cached.secret.expiresAtUnix,
        opts,
        identity,
      );
    } else {
      await client.verify();
      await client.refreshSecret();
      const exported = client.exportSecret();
      const manifestBytes = client.manifestBytes!;
      saveCachedSession(
        exported,
        btoa(String.fromCharCode(...manifestBytes)),
        identity,
      );
      scheduleSecretRefresh(client, exported.expiresAtUnix, opts, identity);
    }

    setClient(client);
    clientReady.set(true);
    loadModels().catch((e) => {
      console.error('Error loading models:', e);
    });
  } catch (e) {
    setClient(null);
    clientError.set(
      e instanceof Error
        ? e.message
        : translate('connection.verificationFailed'),
    );
    console.error('Failed to verify Privatemode deployment:', e);
  } finally {
    clientVerifying.set(false);
  }
}
