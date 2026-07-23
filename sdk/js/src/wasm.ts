import './wasm_exec.js';
import { PrivatemodeAPIError } from './errors.js';

// Go is provided by wasm_exec.js
// eslint-disable-next-line @typescript-eslint/no-explicit-any
declare const Go: any;

let initialized = false;

export interface WasmAPIResponse {
  body: string;
  headers: Headers;
}

export async function initWasm(
  wasmSource: BufferSource | Response | Promise<Response>,
  expectedHash?: string,
) {
  if (initialized) return;

  async function verifyHash(
    data: ArrayBuffer,
    expectedHash: string,
  ): Promise<void> {
    const hashBuffer = await crypto.subtle.digest('SHA-256', data);
    const hashHex = Array.from(new Uint8Array(hashBuffer))
      .map((b) => b.toString(16).padStart(2, '0'))
      .join('');
    if (hashHex !== expectedHash.toLowerCase()) {
      throw new Error(
        `WASM integrity check failed: expected SHA-256 ${expectedHash}, got ${hashHex}`,
      );
    }
  }

  const go = new Go();

  let result: WebAssembly.WebAssemblyInstantiatedSource;

  const source = await wasmSource;

  if (source instanceof Response) {
    if (expectedHash) {
      // Need the raw bytes to hash, so we can't use streaming instantiation.
      const wasmBytes = await source.arrayBuffer();
      await verifyHash(wasmBytes, expectedHash);
      result = await WebAssembly.instantiate(wasmBytes, go.importObject);
    } else {
      // Browser: streaming instantiation
      result = await WebAssembly.instantiateStreaming(source, go.importObject);
    }
  } else {
    // Node.js: from buffer
    if (expectedHash) {
      const bytes = new Uint8Array(
        source instanceof ArrayBuffer
          ? source
          : (source as ArrayBufferView).buffer,
      );
      await verifyHash(bytes.buffer as ArrayBuffer, expectedHash);
    }
    result = await WebAssembly.instantiate(source, go.importObject);
  }

  go.run(result.instance);
  initialized = true;
}

export async function initialize(
  manifest: string,
  authType: string,
  authValue: string,
  apiBaseURL: string,
  enableLogging: boolean,
): Promise<void> {
  ensureInitialized();

  const initializeWasm = (globalThis as Record<string, unknown>).initialize as (
    manifest: string,
    authType: string,
    authValue: string,
    apiBaseURL: string,
    enableLogging: boolean,
  ) => Promise<void>;

  await initializeWasm(
    manifest,
    authType,
    authValue,
    apiBaseURL,
    enableLogging,
  );
}

export async function updateSecret(): Promise<void> {
  ensureInitialized();

  const updateSecretWasm = (globalThis as Record<string, unknown>)
    .updateSecret as () => Promise<void>;

  await updateSecretWasm();
}

export async function fetchManifest(): Promise<string> {
  ensureInitialized();

  const fetchManifestWasm = (globalThis as Record<string, unknown>)
    .fetchManifest as () => Promise<string>;

  return fetchManifestWasm();
}

export async function chatCompletions(
  body: string,
  headers?: Record<string, string>,
  signal?: AbortSignal,
): Promise<WasmAPIResponse> {
  ensureInitialized();

  const chatCompletionsWasm = (globalThis as Record<string, unknown>)
    .chatCompletions as (
    body: string,
    headers?: Record<string, string>,
    signal?: AbortSignal,
  ) => Promise<unknown>;

  return normalizeWasmResponse(
    await callWasm(() => chatCompletionsWasm(body, headers, signal)),
  );
}

export async function streamChatCompletions(
  body: string,
  onChunk: (chunk: string) => void,
  signal?: AbortSignal,
  onHeaders?: (headers: Headers) => void,
  headers?: Record<string, string>,
): Promise<void> {
  ensureInitialized();

  const streamChatCompletionsWasm = (globalThis as Record<string, unknown>)
    .streamChatCompletions as (
    body: string,
    onChunk: (chunk: string) => void,
    signal?: AbortSignal,
    onHeaders?: (headers: Record<string, string>) => void,
    headers?: Record<string, string>,
  ) => Promise<void>;

  return callWasm(() =>
    streamChatCompletionsWasm(
      body,
      onChunk,
      signal,
      onHeaders
        ? (responseHeaders) => onHeaders(new Headers(responseHeaders))
        : undefined,
      headers,
    ),
  );
}

export async function unstructured(
  files: Array<{ name: string; content: Uint8Array; contentType?: string }>,
  optionsJSON: string,
): Promise<string> {
  ensureInitialized();

  const unstructuredWasm = (globalThis as Record<string, unknown>)
    .unstructured as (
    files: Array<{ name: string; content: Uint8Array; contentType?: string }>,
    optionsJSON: string,
  ) => Promise<string>;

  return callWasm(() => unstructuredWasm(files, optionsJSON));
}

export async function transcribeAudio(
  file: { name: string; content: Uint8Array; contentType?: string },
  optionsJSON: string,
  headers?: Record<string, string>,
  signal?: AbortSignal,
): Promise<WasmAPIResponse> {
  ensureInitialized();

  const transcribeAudioWasm = (globalThis as Record<string, unknown>)
    .transcribeAudio as (
    file: { name: string; content: Uint8Array; contentType?: string },
    optionsJSON: string,
    headers?: Record<string, string>,
    signal?: AbortSignal,
  ) => Promise<unknown>;

  return normalizeWasmResponse(
    await callWasm(() =>
      transcribeAudioWasm(file, optionsJSON, headers, signal),
    ),
  );
}

export async function listModels(
  headers?: Record<string, string>,
  signal?: AbortSignal,
): Promise<WasmAPIResponse> {
  ensureInitialized();

  const listModelsWasm = (globalThis as Record<string, unknown>).listModels as (
    headers?: Record<string, string>,
    signal?: AbortSignal,
  ) => Promise<unknown>;

  return normalizeWasmResponse(
    await callWasm(() => listModelsWasm(headers, signal)),
  );
}

export function initializeOffline(
  authType: string,
  authValue: string,
  apiBaseURL: string,
  enableLogging: boolean,
): void {
  ensureInitialized();

  const initializeOfflineWasm = (globalThis as Record<string, unknown>)
    .initializeOffline as (
    authType: string,
    authValue: string,
    apiBaseURL: string,
    enableLogging: boolean,
  ) => void;

  initializeOfflineWasm(authType, authValue, apiBaseURL, enableLogging);
}

export function setAuth(authType: string, authValue: string): void {
  ensureInitialized();

  const setAuthWasm = (globalThis as Record<string, unknown>).setAuth as (
    authType: string,
    authValue: string,
  ) => void;

  setAuthWasm(authType, authValue);
}

export function exportSecret(): string {
  ensureInitialized();

  const exportSecretWasm = (globalThis as Record<string, unknown>)
    .exportSecret as () => string;

  return exportSecretWasm();
}

export function importSecret(
  id: string,
  base64Data: string,
  expiresAtUnix: number,
): void {
  ensureInitialized();

  const importSecretWasm = (globalThis as Record<string, unknown>)
    .importSecret as (
    id: string,
    base64Data: string,
    expiresAtUnix: number,
  ) => void;

  importSecretWasm(id, base64Data, expiresAtUnix);
}

export function privatemodeVersion(): string {
  ensureInitialized();
  return (globalThis as Record<string, unknown>).privatemodeVersion as string;
}

export function errManifestMismatch(): string {
  ensureInitialized();
  return (globalThis as Record<string, unknown>).errManifestMismatch as string;
}

export function errNoSecretForID(): string {
  ensureInitialized();
  return (globalThis as Record<string, unknown>).errNoSecretForID as string;
}

function ensureInitialized() {
  if (!initialized)
    throw new Error('WASM not initialized. Call initWasm() first.');
}

async function callWasm<T>(operation: () => Promise<T>): Promise<T> {
  try {
    return await operation();
  } catch (error) {
    throw normalizeWasmError(error);
  }
}

function normalizeWasmError(error: unknown): unknown {
  if (!(error instanceof Error) || error.name !== 'PrivatemodeAPIError') {
    return error;
  }

  const raw = error as Error & {
    body?: unknown;
    headers?: unknown;
    status?: unknown;
  };
  if (typeof raw.status !== 'number' || typeof raw.body !== 'string') {
    return error;
  }

  let body: unknown = raw.body;
  try {
    body = JSON.parse(raw.body) as unknown;
  } catch {
    // Preserve non-JSON response bodies as text.
  }
  const headers =
    raw.headers && typeof raw.headers === 'object'
      ? new Headers(raw.headers as HeadersInit)
      : undefined;
  return new PrivatemodeAPIError(error.message, raw.status, body, headers, {
    cause: error,
  });
}

function normalizeWasmResponse(value: unknown): WasmAPIResponse {
  if (!value || typeof value !== 'object') {
    throw new TypeError('WASM API response must be an object.');
  }
  const raw = value as { body?: unknown; headers?: unknown };
  if (typeof raw.body !== 'string') {
    throw new TypeError('WASM API response body must be a string.');
  }
  if (!raw.headers || typeof raw.headers !== 'object') {
    throw new TypeError('WASM API response headers must be an object.');
  }
  return {
    body: raw.body,
    headers: new Headers(raw.headers as HeadersInit),
  };
}
