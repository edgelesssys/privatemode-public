import { PrivatemodeAPIError } from './errors.js';
import './wasm_exec.js';

// Go is provided by wasm_exec.js
// eslint-disable-next-line @typescript-eslint/no-explicit-any
declare const Go: any;

let initialized = false;
let initializing: Promise<void> | null = null;
/** SHA-256 the loaded module was verified with, or null if unverified. */
let loadedWasmHash: string | null = null;

export interface WasmAPIResponse {
  body: string;
  headers: Headers;
}

function runGo(go: InstanceType<typeof Go>, instance: WebAssembly.Instance) {
  // Go disables its Fetch-based HTTP transport when process.argv0 starts with
  // "node", leaving js/wasm's fake network unable to reach external hosts.
  // Go.run enters the module synchronously, so hide argv0 while package
  // initializers select the transport. See https://go.dev/issue/60810.
  const globals = globalThis as unknown as {
    process?: { argv0?: unknown };
  };
  const originalProcess = globals.process;
  const enableNodeFetch =
    typeof globalThis.fetch === 'function' &&
    typeof originalProcess?.argv0 === 'string' &&
    originalProcess.argv0.startsWith('node');

  if (enableNodeFetch) {
    globals.process = Object.create(originalProcess, {
      argv0: { value: 'wasm' },
    });
  }
  try {
    void go.run(instance);
  } finally {
    if (enableNodeFetch) {
      globals.process = originalProcess;
    }
  }
}

/** The raw client object exported by the Go Wasm module. */
interface WasmClientExports {
  initialize(
    manifest: string,
    authType: string,
    authValue: string,
    apiBaseURL: string,
    enableLogging: boolean,
    extraHTTPHeaders: Record<string, string>,
  ): Promise<void>;
  initializeOffline(
    authType: string,
    authValue: string,
    apiBaseURL: string,
    enableLogging: boolean,
    extraHTTPHeaders: Record<string, string>,
  ): void;
  setAuth(authType: string, authValue: string): void;
  updateSecret(): Promise<void>;
  fetchManifest(): Promise<string>;
  chatCompletions(
    body: string,
    headers?: Record<string, string>,
    signal?: AbortSignal,
  ): Promise<unknown>;
  embeddings(
    body: string,
    headers?: Record<string, string>,
    signal?: AbortSignal,
  ): Promise<unknown>;
  streamChatCompletions(
    body: string,
    onChunk: (chunk: string) => Promise<void> | void,
    signal?: AbortSignal,
    onHeaders?: (headers: Record<string, string>) => void,
    headers?: Record<string, string>,
  ): Promise<void>;
  unstructured(
    files: Array<{ name: string; content: Uint8Array; contentType?: string }>,
    optionsJSON: string,
  ): Promise<string>;
  transcribeAudio(
    file: { name: string; content: Uint8Array; contentType?: string },
    optionsJSON: string,
    headers?: Record<string, string>,
    signal?: AbortSignal,
  ): Promise<unknown>;
  streamTranscribeAudio(
    file: { name: string; content: Uint8Array; contentType?: string },
    optionsJSON: string,
    onChunk: (chunk: string) => Promise<void> | void,
    signal?: AbortSignal,
    onHeaders?: (headers: Record<string, string>) => void,
    headers?: Record<string, string>,
  ): Promise<void>;
  listModels(
    headers?: Record<string, string>,
    signal?: AbortSignal,
  ): Promise<unknown>;
  exportSecret(): string;
  importSecret(id: string, base64Data: string, expiresAtUnix: number): void;
  close(): void;
}

export async function initWasm(
  wasmSource: BufferSource | Response | Promise<Response>,
  expectedHash?: string,
): Promise<void> {
  if (initialized) {
    ensureHashMatchesLoaded(expectedHash);
    return;
  }
  if (initializing) {
    await initializing;
    ensureHashMatchesLoaded(expectedHash);
    return;
  }

  initializing = (async () => {
    async function verifyHash(
      data: BufferSource,
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
        result = await WebAssembly.instantiateStreaming(
          source,
          go.importObject,
        );
      }
    } else {
      // Node.js: from buffer
      if (expectedHash) {
        // Hash exactly the bytes that are instantiated: for typed-array
        // views, the view's slice of the backing buffer, not the whole
        // backing buffer.
        const bytes =
          source instanceof ArrayBuffer
            ? source
            : new Uint8Array(
                (source as ArrayBufferView).buffer as ArrayBuffer,
                (source as ArrayBufferView).byteOffset,
                (source as ArrayBufferView).byteLength,
              );
        await verifyHash(bytes, expectedHash);
      }
      result = await WebAssembly.instantiate(source, go.importObject);
    }

    runGo(go, result.instance);
    initialized = true;
    loadedWasmHash = expectedHash?.toLowerCase() ?? null;
  })();

  try {
    await initializing;
  } finally {
    initializing = null;
  }
}

/**
 * Check an expected hash against the hash the already-loaded module was
 * verified with. The module is loaded once per realm, so a later caller's
 * hash can only be honored by comparing it to the first load: a mismatch
 * fails, and so does providing a hash when the module was loaded without
 * integrity verification, instead of silently accepting any hash.
 */
function ensureHashMatchesLoaded(expectedHash?: string): void {
  if (!expectedHash) return;
  if (!loadedWasmHash) {
    throw new Error(
      'WASM module was already loaded without integrity verification; the expected hash cannot be checked.',
    );
  }
  if (loadedWasmHash !== expectedHash.toLowerCase()) {
    throw new Error(
      `WASM integrity check failed: module already loaded with SHA-256 ${loadedWasmHash}, expected ${expectedHash}`,
    );
  }
}

/**
 * Create a new state-isolated Wasm client. Each client owns its
 * authentication, endpoint, manifest, and secret state, so multiple SDK
 * instances cannot overwrite each other's configuration.
 */
export function createWasmClient(): WasmClient {
  ensureInitialized();
  const factory = (globalThis as Record<string, unknown>)
    .createPrivatemodeClient as () => WasmClientExports;
  return new WasmClient(factory());
}

/** A state-isolated client handle exported by the Go Wasm module. */
export class WasmClient {
  private closed = false;

  public constructor(private readonly exports: WasmClientExports) {}

  /**
   * Release the Go function references backing this client. The client
   * must not be used afterwards. Closing an already-closed client is a
   * no-op.
   */
  public close(): void {
    if (this.closed) return;
    this.closed = true;
    this.exports.close();
  }

  public initialize(
    manifest: string,
    authType: string,
    authValue: string,
    apiBaseURL: string,
    enableLogging: boolean,
    extraHTTPHeaders: Record<string, string> = {},
  ): Promise<void> {
    return this.exports.initialize(
      manifest,
      authType,
      authValue,
      apiBaseURL,
      enableLogging,
      extraHTTPHeaders,
    );
  }

  public initializeOffline(
    authType: string,
    authValue: string,
    apiBaseURL: string,
    enableLogging: boolean,
    extraHTTPHeaders: Record<string, string> = {},
  ): void {
    this.exports.initializeOffline(
      authType,
      authValue,
      apiBaseURL,
      enableLogging,
      extraHTTPHeaders,
    );
  }

  public setAuth(authType: string, authValue: string): void {
    this.exports.setAuth(authType, authValue);
  }

  public updateSecret(): Promise<void> {
    return this.exports.updateSecret();
  }

  public fetchManifest(): Promise<string> {
    return this.exports.fetchManifest();
  }

  public async chatCompletions(
    body: string,
    headers?: Record<string, string>,
    signal?: AbortSignal,
  ): Promise<WasmAPIResponse> {
    return normalizeWasmResponse(
      await callWasm(() => this.exports.chatCompletions(body, headers, signal)),
    );
  }

  public async embeddings(
    body: string,
    headers?: Record<string, string>,
    signal?: AbortSignal,
  ): Promise<WasmAPIResponse> {
    return normalizeWasmResponse(
      await callWasm(() => this.exports.embeddings(body, headers, signal)),
    );
  }

  /**
   * Stream chat completions. If `onChunk` returns a Promise, the next chunk
   * is not read from the network until it settles, so a slow consumer
   * backpressures the response instead of buffering it in memory.
   */
  public streamChatCompletions(
    body: string,
    onChunk: (chunk: string) => Promise<void> | void,
    signal?: AbortSignal,
    onHeaders?: (headers: Headers) => void,
    headers?: Record<string, string>,
  ): Promise<void> {
    return callWasm(() =>
      this.exports.streamChatCompletions(
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

  public unstructured(
    files: Array<{ name: string; content: Uint8Array; contentType?: string }>,
    optionsJSON: string,
  ): Promise<string> {
    return callWasm(() => this.exports.unstructured(files, optionsJSON));
  }

  public async transcribeAudio(
    file: { name: string; content: Uint8Array; contentType?: string },
    optionsJSON: string,
    headers?: Record<string, string>,
    signal?: AbortSignal,
  ): Promise<WasmAPIResponse> {
    return normalizeWasmResponse(
      await callWasm(() =>
        this.exports.transcribeAudio(file, optionsJSON, headers, signal),
      ),
    );
  }

  /**
   * Stream audio transcription events. If `onChunk` returns a Promise, the
   * next event is not read until it settles, so a slow consumer backpressures
   * the response instead of buffering it in memory.
   */
  public streamTranscribeAudio(
    file: { name: string; content: Uint8Array; contentType?: string },
    optionsJSON: string,
    onChunk: (chunk: string) => Promise<void> | void,
    signal?: AbortSignal,
    onHeaders?: (headers: Headers) => void,
    headers?: Record<string, string>,
  ): Promise<void> {
    return callWasm(() =>
      this.exports.streamTranscribeAudio(
        file,
        optionsJSON,
        onChunk,
        signal,
        onHeaders
          ? (responseHeaders) => onHeaders(new Headers(responseHeaders))
          : undefined,
        headers,
      ),
    );
  }

  public async listModels(
    headers?: Record<string, string>,
    signal?: AbortSignal,
  ): Promise<WasmAPIResponse> {
    return normalizeWasmResponse(
      await callWasm(() => this.exports.listModels(headers, signal)),
    );
  }

  public exportSecret(): string {
    return this.exports.exportSecret();
  }

  public importSecret(
    id: string,
    base64Data: string,
    expiresAtUnix: number,
  ): void {
    this.exports.importSecret(id, base64Data, expiresAtUnix);
  }
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
