import './wasm_exec.js';

// Go is provided by wasm_exec.js
// eslint-disable-next-line @typescript-eslint/no-explicit-any
declare const Go: any;

let initialized = false;

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
  apiKey: string,
  apiBaseURL: string,
  enableLogging: boolean,
): Promise<void> {
  ensureInitialized();

  const initializeWasm = (globalThis as Record<string, unknown>).initialize as (
    manifest: string,
    apiKey: string,
    apiBaseURL: string,
    enableLogging: boolean,
  ) => Promise<void>;

  await initializeWasm(manifest, apiKey, apiBaseURL, enableLogging);
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

export async function chatCompletions(body: string): Promise<string> {
  ensureInitialized();

  const chatCompletionsWasm = (globalThis as Record<string, unknown>)
    .chatCompletions as (body: string) => Promise<string>;

  return chatCompletionsWasm(body);
}

export async function streamChatCompletions(
  body: string,
  onChunk: (chunk: string) => void,
  signal?: AbortSignal,
): Promise<void> {
  ensureInitialized();

  const streamChatCompletionsWasm = (globalThis as Record<string, unknown>)
    .streamChatCompletions as (
    body: string,
    onChunk: (chunk: string) => void,
    signal?: AbortSignal,
  ) => Promise<void>;

  return streamChatCompletionsWasm(body, onChunk, signal);
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

  return unstructuredWasm(files, optionsJSON);
}

export async function listModels(): Promise<string> {
  ensureInitialized();

  const listModelsWasm = (globalThis as Record<string, unknown>)
    .listModels as () => Promise<string>;

  return listModelsWasm();
}

export function initializeOffline(
  apiKey: string,
  apiBaseURL: string,
  enableLogging: boolean,
): void {
  ensureInitialized();

  const initializeOfflineWasm = (globalThis as Record<string, unknown>)
    .initializeOffline as (
    apiKey: string,
    apiBaseURL: string,
    enableLogging: boolean,
  ) => void;

  initializeOfflineWasm(apiKey, apiBaseURL, enableLogging);
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
