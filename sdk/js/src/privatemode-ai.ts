import { Manifest } from './manifest.js';
import {
  createWasmClient,
  errManifestMismatch,
  errNoSecretForID,
  initWasm,
  type WasmClient,
} from './wasm.js';

/** An authentication credential for the Privatemode API. */
export type AuthCredential =
  { type: 'apiKey'; value: string } | { type: 'jwt'; value: string };

/**
 * A function that returns the credential to authenticate the next request with.
 * It's invoked before every request, so credentials can be refreshed over time.
 */
export type AuthProvider = () => AuthCredential | Promise<AuthCredential>;

/** Options for configuring {@link PrivatemodeCore}. */
export interface PrivatemodeCoreOptions {
  /**
   * Privatemode API key to authenticate with. Shorthand for an {@link auth}
   * provider that always returns this key. Mutually exclusive with `auth`.
   */
  apiKey?: string;

  /**
   * Authentication provider, invoked before every request to obtain the
   * credential to use. Use this to rotate credentials. Mutually exclusive
   * with {@link apiKey}.
   */
  auth?: AuthProvider;

  /**
   * API base URL.
   * @default 'https://api.privatemode.ai'
   */
  apiBaseURL?: string;

  /**
   * Override for the manifest used to verify the Privatemode
   * deployment. By default, the manifest is fetched from the
   * Privatemode CDN. The bytes must be valid JSON matching the {@link Manifest} schema.
   */
  manifestBytes?: Uint8Array;

  /**
   * Whether to allow usage in a browser, which exposes the API key
   * to the user and also increases vulnerability to cross-site
   * attacks that might try to steal the key.
   * Note that using Privatemode in a browser environment requires
   * careful consideration of the security implications and appropriate
   * mitigations (e.g. in response to cross-site attacks) to retain the
   * security guarantees of Privatemode.
   * @default false
   */
  dangerouslyAllowBrowser?: boolean;

  /**
   * URL or filesystem path of the Wasm module. In Node.js, the SDK loads the
   * `privatemode.wasm` file shipped beside its JavaScript output by default.
   *
   * The Wasm module is instantiated once per JavaScript realm: the first
   * client to load it determines the binary, and later clients reuse the
   * loaded module, ignoring their own {@link wasmURL}. Client state is
   * still isolated per instance.
   */
  wasmURL?: string | URL;

  /**
   * URL to load the Wasm module from in a browser environment.
   * Ignored in Node.js. Takes precedence over {@link wasmURL} in browsers.
   * @default './privatemode.wasm'
   */
  browserWasmURL?: string;

  /**
   * Surface the logs emitted by the Wasm module in the JavaScript console.
   * @default true
   */
  enableWasmLogging?: boolean;

  /**
   * Expected SHA-256 hash of the Wasm binary (hex-encoded).
   * If provided, the loaded Wasm module is verified against this hash
   * before instantiation. If the module was already loaded (see
   * {@link wasmURL}), the hash is compared against the hash of the first
   * load; it fails on a mismatch, and also if the module was loaded
   * without a hash.
   */
  expectedWasmHash?: string;

  /**
   * Optional callback invoked when the manifest is updated internally
   * (e.g., due to a manifest mismatch during verification).
   */
  onManifestUpdate?: (manifestBytes: Uint8Array) => void;

  /**
   * Optional callback invoked when the encryption secret is refreshed
   * or updated.
   */
  onSecretUpdate?: (secret: ExportedSecret) => void;
}

/** An exported secret that can be cached and restored. */
export interface ExportedSecret {
  /** Secret identifier. */
  id: string;
  /** Base64-encoded secret data. */
  data: string;
  /** Expiration as a Unix timestamp (seconds). */
  expiresAtUnix: number;
}

/** Result of verifying the Privatemode deployment. */
export interface VerifyResult {
  /** The manifest the verification has been performed against. */
  manifest: Manifest;
}

/** @internal Options applied to a low-level Privatemode API request. */
export interface PrivatemodeRequestOptions {
  headers?: HeadersInit;
  signal?: AbortSignal;
}

/** @internal A successful low-level Privatemode API response. */
export interface PrivatemodeResponse {
  body: unknown;
  headers: Headers;
}

/** @internal Options applied to a low-level streaming request. */
export interface PrivatemodeStreamOptions extends PrivatemodeRequestOptions {
  onResponseHeaders?: (headers: Headers) => void;
}

/** @internal Low-level implementation used by the OpenAI transport. */
export class PrivatemodeCore {
  private authProvider: AuthProvider;
  private apiBaseURL: string;
  private extraHTTPHeaders: Record<string, string>;
  private isBrowser: boolean;
  private _manifestBytes: Uint8Array | null;
  private wasmURL: string | URL;
  private browserWasmURL: string;
  private enableWasmLogging: boolean;
  private expectedWasmHash: string | undefined;
  private onManifestUpdate?: (manifestBytes: Uint8Array) => void;
  private onSecretUpdate?: (secret: ExportedSecret) => void;
  private verified = false;
  private initialized = false;
  private hasSecret = false;
  private closed = false;
  private wasmClient: WasmClient | null = null;

  constructor(
    options: PrivatemodeCoreOptions & {
      extraHTTPHeaders?: HeadersInit;
    } = {},
  ) {
    this.isBrowser =
      typeof window !== 'undefined' &&
      typeof window.document !== 'undefined' &&
      typeof navigator !== 'undefined';
    if (this.isBrowser && !options.dangerouslyAllowBrowser) {
      throw new Error(
        "It looks like you're running in a browser-like environment.\n\n" +
          'This is disabled by default, as it risks exposing your secret API credentials to attackers.\n' +
          'If you understand the risks and have appropriate mitigations in place,\n' +
          'you can set the `dangerouslyAllowBrowser` option to `true`.',
      );
    }

    if (options.apiKey !== undefined && options.auth !== undefined) {
      throw new Error(
        'Pass either `apiKey` or an `auth` provider to the PrivatemodeAI ' +
          'client, not both.',
      );
    }
    const { apiKey } = options;
    const auth =
      options.auth ??
      (apiKey
        ? (): AuthCredential => ({ type: 'apiKey', value: apiKey })
        : undefined);
    if (!auth) {
      throw new Error(
        'No authentication configured. Pass an `apiKey` or an `auth` ' +
          'provider to the PrivatemodeAI client.',
      );
    }
    this.authProvider = auth;

    this.apiBaseURL = options.apiBaseURL ?? 'https://api.privatemode.ai';
    const extraHTTPHeaders = new Headers(options.extraHTTPHeaders);
    if (!extraHTTPHeaders.has('Privatemode-Client')) {
      extraHTTPHeaders.set('Privatemode-Client', 'SDK');
    }
    this.extraHTTPHeaders = Object.fromEntries(extraHTTPHeaders);
    this._manifestBytes = options.manifestBytes ?? null;
    this.wasmURL = options.wasmURL ?? defaultWasmURL();
    this.browserWasmURL =
      options.browserWasmURL ??
      options.wasmURL?.toString() ??
      './privatemode.wasm';
    this.enableWasmLogging = options.enableWasmLogging ?? true;
    this.expectedWasmHash = options.expectedWasmHash;
    this.onManifestUpdate = options.onManifestUpdate;
    this.onSecretUpdate = options.onSecretUpdate;
  }

  /**
   * Verify the Privatemode deployment by fetching and verifying the
   * attestation document of the coordinator, and initialize the
   * encryption secret. If the encryption secret has already been
   * initialized, the verification is still performed, keeping the
   * existing secret.
   *
   * @returns The verification result.
   * @throws If verification fails.
   */
  public async verify(): Promise<VerifyResult> {
    await this.loadWasm();

    if (!this._manifestBytes) {
      this._manifestBytes = new TextEncoder().encode(
        await this.wasm.fetchManifest(),
      );
    }
    this.initialized = true;

    await this.initializeWithManifestRetry(await this.resolveCredential());
    return {
      manifest: JSON.parse(
        new TextDecoder().decode(this._manifestBytes!),
      ) as Manifest,
    };
  }

  /**
   * Initialize the client offline without performing attestation.
   * This loads the Wasm module and sets up the API key and base URL,
   * but skips remote attestation. Use this together with
   * {@link importSecret} to restore a previously cached secret.
   *
   * @param manifestBytes - Manifest bytes to set on the
   * client (e.g. from a previous session's cache).
   */
  public async initializeOffline(manifestBytes: Uint8Array): Promise<void> {
    await this.loadWasm();
    const cred = await this.resolveCredential();
    this.wasm.initializeOffline(
      cred.type,
      cred.value,
      this.apiBaseURL,
      this.enableWasmLogging,
      this.extraHTTPHeaders,
    );
    this._manifestBytes = manifestBytes;
    this.initialized = true;
  }

  /**
   * Update the encryption secret. {@link verify} must have been called
   * before calling this. Most callers will want to call this in some
   * sort of loop to keep the secret up-to-date.
   *
   * @throws If the secret update fails or verify hasn't been called.
   */
  public async refreshSecret(): Promise<void> {
    if (!this.initialized) {
      // Security-wise, this is not a safeguard against users who
      // *really* want to shoot themselves in the foot, but it still
      // provides some protection against using this function before
      // properly initializing the client.
      throw new Error(
        'verify() or initializeOffline() must be called before refreshSecret().',
      );
    }
    const cred = await this.resolveCredential();
    this.wasm.setAuth(cred.type, cred.value);
    if (!this.verified) {
      await this.initializeWithManifestRetry(cred);
    }
    await this.wasm.updateSecret();
    this.hasSecret = true;
    if (this.onSecretUpdate) {
      this.onSecretUpdate(this.exportSecret());
    }
  }

  /**
   * Send a chat completions request. The request body is encrypted
   * and the response is decrypted transparently.
   *
   * {@link verify} and {@link refreshSecret} must have been called
   * first.
   *
   * @param body - The JSON request body (OpenAI chat completions
   * format).
   * @param options - Optional request headers and abort signal.
   * @returns The parsed response body and response headers.
   * @throws If encryption, sending, or decryption fails.
   */
  public async chatCompletions(
    body: Record<string, unknown>,
    options?: PrivatemodeRequestOptions,
  ): Promise<PrivatemodeResponse> {
    await this.applyCredential();
    return this.retryWithSecretRefresh(async () => {
      const resp = await this.wasm.chatCompletions(
        JSON.stringify(body),
        headersToRecord(options?.headers),
        options?.signal,
      );
      return { body: JSON.parse(resp.body), headers: resp.headers };
    });
  }

  /**
   * Send an embeddings request. The request body is encrypted and the
   * response is decrypted transparently.
   *
   * {@link verify} and {@link refreshSecret} must have been called first.
   *
   * @param body - The JSON request body (OpenAI embeddings format).
   * @param options - Optional request headers and abort signal.
   * @returns The parsed response body and response headers.
   * @throws If encryption, sending, or decryption fails.
   */
  public async embeddings(
    body: Record<string, unknown>,
    options?: PrivatemodeRequestOptions,
  ): Promise<PrivatemodeResponse> {
    await this.applyCredential();
    return this.retryWithSecretRefresh(async () => {
      const resp = await this.wasm.embeddings(
        JSON.stringify(body),
        headersToRecord(options?.headers),
        options?.signal,
      );
      return { body: JSON.parse(resp.body), headers: resp.headers };
    });
  }

  /**
   * Stream chat completions. The request body is encrypted and
   * response chunks are decrypted transparently.
   *
   * {@link verify} and {@link refreshSecret} must have been called
   * first.
   *
   * @param body - The JSON request body (OpenAI chat completions
   * format). Should include `stream: true`.
   * @param options - Optional parameters.
   * @param options.signal - An AbortSignal to cancel the stream.
   * @param options.headers - Additional request headers.
   * @param options.onResponseHeaders - Called with the response headers.
   * @returns An async generator yielding decrypted response chunks.
   * @throws If encryption, streaming, or decryption fails.
   */
  public async *streamChatCompletions(
    body: Record<string, unknown>,
    options?: PrivatemodeStreamOptions,
  ): AsyncGenerator<unknown> {
    await this.applyCredential();
    const generator = await this.retryWithSecretRefresh(async () =>
      this.initiateStream(
        (onChunk) =>
          this.wasm.streamChatCompletions(
            JSON.stringify(body),
            onChunk,
            options?.signal,
            options?.onResponseHeaders,
            headersToRecord(options?.headers),
          ),
        options,
      ),
    );
    yield* generator;
  }

  /**
   * Initiate a streaming request and return an async generator.
   * Waits for the stream to successfully start (first chunk, completion,
   * or error) before returning. This allows retryWithSecretRefresh to
   * catch "no secret for ID" errors that occur before streaming starts.
   *
   * The chunk callback resolves only when the consumer advances the
   * iterator, so the bridge holds at most one undelivered chunk and a slow
   * consumer backpressures the response instead of buffering it in memory.
   */
  private async initiateStream(
    start: (onChunk: (chunk: string) => Promise<void> | void) => Promise<void>,
    options?: { signal?: AbortSignal },
  ): Promise<AsyncGenerator<unknown>> {
    type PendingChunk = {
      value: string;
      acknowledge: () => void;
      reject: (error: Error) => void;
    };

    let pending: PendingChunk | null = null;
    let waiter: (() => void) | null = null;
    let done = false;
    let streamError: Error | null = null;
    let closed = false;

    const notify = () => {
      waiter?.();
      waiter = null;
    };

    const takePending = (): PendingChunk | null => {
      const chunk = pending;
      pending = null;
      return chunk;
    };

    const rejectPending = (error: Error): void => {
      pending?.reject(error);
      pending = null;
    };

    const onChunk = (chunk: string): Promise<void> =>
      new Promise<void>((acknowledge, reject) => {
        if (closed) {
          reject(new Error('Stream consumer closed.'));
          return;
        }
        if (options?.signal?.aborted) {
          reject(
            options.signal.reason ??
              new DOMException('The operation was aborted.', 'AbortError'),
          );
          return;
        }
        if (pending) {
          reject(new Error('Stream backpressure invariant violated.'));
          return;
        }

        const onAbort = () => {
          pending = null;
          reject(
            options?.signal?.reason ??
              new DOMException('The operation was aborted.', 'AbortError'),
          );
          notify();
        };
        const cleanup = () =>
          options?.signal?.removeEventListener('abort', onAbort);
        options?.signal?.addEventListener('abort', onAbort, { once: true });
        pending = {
          value: chunk,
          acknowledge: () => {
            cleanup();
            acknowledge();
          },
          reject: (error) => {
            cleanup();
            reject(error);
          },
        };
        notify();
      });

    const streamPromise = start(onChunk);
    streamPromise
      .then(() => {
        done = true;
        notify();
      })
      .catch((e: Error) => {
        streamError = e;
        notify();
      });

    // Wait for first chunk, completion, or error to verify the stream started successfully.
    // "no secret for ID" errors occur before streaming starts, so they'll be caught here.
    while (pending === null && streamError === null && !done) {
      if (options?.signal?.aborted) {
        throw new DOMException('The operation was aborted.', 'AbortError');
      }
      await new Promise<void>((r) => {
        waiter = r;
      });
    }

    // If there's an error at this point, throw it so retryWithSecretRefresh can catch it
    if (streamError !== null) {
      throw streamError;
    }

    // Stream has started successfully. A chunk is acknowledged only when the
    // consumer advances the iterator, keeping the bridge bounded to one chunk.
    return (async function* () {
      let inFlight: PendingChunk | null = null;
      try {
        while (true) {
          if (options?.signal?.aborted) {
            throw new DOMException('The operation was aborted.', 'AbortError');
          }

          const next = takePending();
          if (next) {
            inFlight = next;
            yield JSON.parse(inFlight.value);
            inFlight.acknowledge();
            inFlight = null;
          } else if (streamError !== null) {
            throw streamError;
          } else if (done) {
            return;
          } else {
            await new Promise<void>((r) => {
              waiter = r;
            });
          }
        }
      } finally {
        closed = true;
        const error = new Error('Stream consumer closed.');
        if (inFlight) inFlight.reject(error);
        rejectPending(error);
      }
    })();
  }

  /**
   * Send a request to the unstructured partition endpoint. The request
   * is encrypted and the response is decrypted transparently.
   *
   * {@link verify} and {@link refreshSecret} must have been called
   * first.
   *
   * @param files - One or more files to process.
   * @param options - Optional partitioning parameters.
   * @returns The decrypted response body as a parsed object.
   * @throws If encryption, sending, or decryption fails or the
   * response is invalid JSON.
   */
  public async unstructured(
    files: UnstructuredFile[],
    options?: UnstructuredOptions,
  ): Promise<unknown> {
    await this.applyCredential();
    return this.retryWithSecretRefresh(async () => {
      const wasmFiles = files.map((f) => ({
        name: f.name,
        content:
          f.content instanceof Uint8Array
            ? f.content
            : new Uint8Array(f.content),
        ...(f.contentType ? { contentType: f.contentType } : {}),
      }));
      const optsJSON = options ? JSON.stringify(options) : '';
      const resp = await this.wasm.unstructured(wasmFiles, optsJSON);
      return JSON.parse(resp);
    });
  }

  /**
   * Send an audio file to the OpenAI-compatible transcription endpoint.
   * The request is encrypted and the response is decrypted transparently.
   *
   * {@link verify} and {@link refreshSecret} must have been called
   * first.
   *
   * @param file - The audio file to transcribe.
   * @param options - Transcription options, including the STT model.
   * @param requestOptions - Optional request headers and abort signal.
   * @returns The parsed response body and response headers.
   * @throws If encryption, sending, or decryption fails or the
   * response is invalid JSON.
   */
  public async transcribeAudio(
    file: AudioFile,
    options: AudioTranscriptionOptions,
    requestOptions?: PrivatemodeRequestOptions,
  ): Promise<PrivatemodeResponse> {
    await this.applyCredential();
    return this.retryWithSecretRefresh(async () => {
      const wasmFile = this.toWasmAudioFile(file);
      const resp = await this.wasm.transcribeAudio(
        wasmFile,
        JSON.stringify(options),
        headersToRecord(requestOptions?.headers),
        requestOptions?.signal,
      );
      return { body: JSON.parse(resp.body), headers: resp.headers };
    });
  }

  /**
   * Stream an audio transcription. The request is encrypted and each response
   * event is decrypted transparently.
   *
   * {@link verify} and {@link refreshSecret} must have been called first.
   *
   * @param file - The audio file to transcribe.
   * @param options - Transcription options, including the STT model.
   * @param streamOptions - Optional request headers, response-header callback,
   * and abort signal.
   * @returns An async generator yielding parsed transcription events.
   */
  public async *streamTranscribeAudio(
    file: AudioFile,
    options: AudioTranscriptionOptions,
    streamOptions?: PrivatemodeStreamOptions,
  ): AsyncGenerator<unknown> {
    await this.applyCredential();
    const wasmFile = this.toWasmAudioFile(file);
    const generator = await this.retryWithSecretRefresh(async () =>
      this.initiateStream(
        (onChunk) =>
          this.wasm.streamTranscribeAudio(
            wasmFile,
            JSON.stringify(options),
            onChunk,
            streamOptions?.signal,
            streamOptions?.onResponseHeaders,
            headersToRecord(streamOptions?.headers),
          ),
        streamOptions,
      ),
    );
    yield* generator;
  }

  private toWasmAudioFile(file: AudioFile): {
    name: string;
    content: Uint8Array;
    contentType?: string;
  } {
    return {
      name: file.name,
      content:
        file.content instanceof Uint8Array
          ? file.content
          : new Uint8Array(file.content),
      ...(file.contentType ? { contentType: file.contentType } : {}),
    };
  }

  /**
   * List available models. The response is not encrypted and only
   * requires authentication.
   *
   * {@link verify} must have been called first.
   *
   * @param options - Optional request headers and abort signal.
   * @returns The parsed response body and response headers from /v1/models.
   * @throws If the request fails.
   */
  public async listModels(
    options?: PrivatemodeRequestOptions,
  ): Promise<PrivatemodeResponse> {
    await this.applyCredential();
    const resp = await this.wasm.listModels(
      headersToRecord(options?.headers),
      options?.signal,
    );
    return { body: JSON.parse(resp.body), headers: resp.headers };
  }

  /**
   * Export the current encryption secret so it can be cached and
   * restored later with {@link importSecret}, avoiding a full HPKE
   * handshake on reload.
   *
   * {@link verify} and {@link refreshSecret} must have been called
   * first.
   *
   * @returns The exported secret.
   * @throws If no secret has been established yet.
   */
  public exportSecret(): ExportedSecret {
    return JSON.parse(this.wasm.exportSecret()) as ExportedSecret;
  }

  /**
   * Import a previously exported secret, restoring the encryption
   * state without performing a new HPKE handshake.
   *
   * {@link verify} must have been called first.
   *
   * @param secret - A secret previously obtained from
   * {@link exportSecret}.
   * @throws If the import fails.
   */
  public importSecret(secret: ExportedSecret): void {
    this.wasm.importSecret(secret.id, secret.data, secret.expiresAtUnix);
    this.hasSecret = true;
  }

  /**
   * Release the Wasm resources backing this client. Any further use of the
   * client throws. Closing an already-closed client is a no-op.
   */
  public close(): void {
    this.closed = true;
    this.wasmClient?.close();
    this.wasmClient = null;
    this.verified = false;
    this.initialized = false;
    this.hasSecret = false;
  }

  /** @throws If {@link close} has been called on this client. */
  private ensureNotClosed(): void {
    if (this.closed) {
      throw new Error('Client is closed and can no longer be used.');
    }
  }

  /** @internal Whether inference can proceed without automatic setup. */
  public get readyForInference(): boolean {
    return this.initialized && this.hasSecret;
  }

  // TODO(msanft): Expose manifest fetching on the client?

  /**
   * The current manifest in use. If no manifest has been set or
   * fetched yet, this returns null.
   * To save/restore the manifest use {@link manifestBytes} instead of this function,
   * as JSON encoding/decoding may alter the bytes and cause verification to fail.
   */
  public get manifest(): Manifest | null {
    if (!this._manifestBytes) return null;
    return JSON.parse(
      new TextDecoder().decode(this._manifestBytes),
    ) as Manifest;
  }

  /**
   * The current manifest in use as raw bytes.
   * Use this for caching the manifest or initializing the client offline with {@link initializeOffline}.
   */
  public get manifestBytes(): Uint8Array | null {
    return this._manifestBytes;
  }

  /**
   * Ensure the WASM module is loaded and this instance's client is created.
   * The module itself is a per-realm singleton: only the first loader's
   * wasmURL takes effect, while client state remains isolated per instance.
   * A later expectedWasmHash is checked against the hash of the first load.
   */
  private async loadWasm(): Promise<void> {
    this.ensureNotClosed();
    if (this.isBrowser) {
      await initWasm(fetch(this.browserWasmURL), this.expectedWasmHash);
      this.wasmClient ??= createWasmClient();
      return;
    }
    const { readFile } = await import('node:fs/promises');
    const wasmBuffer = await readFile(this.wasmURL);
    await initWasm(wasmBuffer, this.expectedWasmHash);
    this.wasmClient ??= createWasmClient();
  }

  /** The state-isolated Wasm client backing this SDK instance. */
  private get wasm(): WasmClient {
    this.ensureNotClosed();
    if (!this.wasmClient) {
      throw new Error(
        'Client not initialized. Call verify() or initializeOffline() first.',
      );
    }
    return this.wasmClient;
  }

  /**
   * Resolve the current authentication credential via the configured provider.
   */
  private async resolveCredential(): Promise<AuthCredential> {
    return this.authProvider();
  }

  /**
   * Resolve the current credential and push it into the WASM client so the next
   * request is authenticated with it. Used before network operations to keep
   * short-lived credentials (e.g. JWTs) fresh.
   */
  private async applyCredential(): Promise<void> {
    const cred = await this.resolveCredential();
    this.wasm.setAuth(cred.type, cred.value);
  }

  /**
   * Initialize with automatic manifest retry if the active manifest doesn't match.
   * If initialization fails with a manifest mismatch error, fetches a new manifest
   * and retries once.
   *
   * @throws If initialization fails for reasons other than manifest mismatch, or if retry fails.
   */
  private async initializeWithManifestRetry(
    cred: AuthCredential,
  ): Promise<void> {
    const manifestBase64 = btoa(String.fromCharCode(...this._manifestBytes!));

    try {
      await this.wasm.initialize(
        manifestBase64,
        cred.type,
        cred.value,
        this.apiBaseURL,
        this.enableWasmLogging,
        this.extraHTTPHeaders,
      );
    } catch (error) {
      if (
        error instanceof Error &&
        error.message.includes(errManifestMismatch())
      ) {
        // Fetch a new manifest and retry
        console.log('Manifest mismatch, trying again with new manifest...');
        const manifestBytes = new TextEncoder().encode(
          await this.wasm.fetchManifest(),
        );
        const newManifestBase64 = btoa(String.fromCharCode(...manifestBytes));

        await this.wasm.initialize(
          newManifestBase64,
          cred.type,
          cred.value,
          this.apiBaseURL,
          this.enableWasmLogging,
          this.extraHTTPHeaders,
        );
        this._manifestBytes = manifestBytes;
        if (this.onManifestUpdate) {
          this.onManifestUpdate(manifestBytes);
        }
      } else {
        throw error;
      }
    }

    this.verified = true;
  }

  /**
   * Retry an operation with automatic secret refresh if a "no secret for ID" error occurs.
   * This handles expired secrets transparently without requiring manual retry logic in consumers.
   */
  private async retryWithSecretRefresh<T>(
    operation: () => Promise<T>,
  ): Promise<T> {
    let retryCount = 0;
    const MAX_RETRIES = 1;

    while (retryCount <= MAX_RETRIES) {
      try {
        return await operation();
      } catch (error) {
        // Check if this is a "no secret for ID" error and we haven't retried yet
        if (
          error instanceof Error &&
          error.message.includes(errNoSecretForID()) &&
          retryCount < MAX_RETRIES
        ) {
          console.log('Secret expired, refreshing and retrying...');
          try {
            await this.refreshSecret();
            retryCount++;
            continue; // Retry the operation
          } catch (refreshError) {
            console.error('Failed to refresh secret:', refreshError);
            throw refreshError; // Throw refresh error
          }
        } else {
          throw error; // Re-throw if it's not a secret error or max retries reached
        }
      }
    }
    throw new Error('Retry loop exhausted'); // Should never reach here
  }
}

/** @internal A file to send to the unstructured partition endpoint. */
export interface UnstructuredFile {
  /** Filename (e.g. "document.pdf"). */
  name: string;
  /** Raw file content as a Uint8Array or ArrayBuffer. */
  content: Uint8Array | ArrayBuffer;
  /** Optional MIME type hint (e.g. "application/pdf"). */
  contentType?: string;
}

/** @internal Optional parameters for the unstructured partition endpoint. */
export interface UnstructuredOptions {
  /** Partitioning strategy: "hi_res", "fast", "ocr_only", "auto", or "vlm". */
  strategy?: string;
  /** Chunking strategy: "basic", "by_title", "by_page", or "by_similarity". */
  chunking_strategy?: string;
  /** Return bounding box coordinates. */
  coordinates?: boolean;
  /** Text encoding (default "utf-8"). */
  encoding?: string;
  /** Element types to extract as Base64-encoded images. */
  extract_image_block_types?: string[];
  /** Model for "hi_res" strategy. */
  hi_res_model_name?: string;
  /** Include PageBreak elements. */
  include_page_breaks?: boolean;
  /** Languages for OCR. */
  languages?: string[];
  /** Response format (e.g. "application/json"). */
  output_format?: string;
  /** Document types to skip table extraction for. */
  skip_infer_table_types?: string[];
  /** Page number for the first page. */
  starting_page_number?: number;
  /** Use random UUIDs for element IDs. */
  unique_element_ids?: boolean;
  /** Keep XML tags in output. */
  xml_keep_tags?: boolean;
}

/** An audio file to send to the transcription endpoint. */
export interface AudioFile {
  /** Filename (e.g. "recording.mp3"). */
  name: string;
  /** Raw audio content as a Uint8Array or ArrayBuffer. */
  content: Uint8Array | ArrayBuffer;
  /** Optional MIME type hint (e.g. "audio/mpeg"). */
  contentType?: string;
}

/** Optional parameters for the audio transcription endpoint. */
export interface AudioTranscriptionOptions {
  /** Speech-to-text model ID, e.g. "voxtral-mini-3b" or "whisper-large-v3". */
  model: string;
  /** Optional ISO-639-1 language code. */
  language?: string;
  /** Optional text to guide transcription style. */
  prompt?: string;
  /** JSON response format. Defaults to "json". */
  response_format?: 'json' | 'verbose_json';
  /** Sampling temperature. */
  temperature?: number;
  /** @internal Additional flattened multipart fields. */
  additional_fields?: Record<string, string[]>;
}

function headersToRecord(
  headers: HeadersInit | undefined,
): Record<string, string> | undefined {
  if (!headers) return undefined;
  return Object.fromEntries(new Headers(headers));
}

/** The location of the Wasm binary shipped with the SDK (Node.js loading). */
function defaultWasmURL(): URL {
  // Vitest executes the TypeScript source directly. Published builds execute
  // from dist, where the Wasm binary is copied beside this module.
  // The path is kept out of the `new URL` literal: bundlers statically
  // analyze `new URL('...', import.meta.url)` and would otherwise emit the
  // multi-megabyte binary as an asset of every browser build. Browsers load
  // the Wasm from `browserWasmURL` instead.
  const wasmPath = import.meta.url.endsWith('/src/privatemode-ai.ts')
    ? '../../wasm/privatemode.wasm'
    : './privatemode.wasm';
  return new URL(wasmPath, import.meta.url);
}
