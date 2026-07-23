import { Manifest } from './manifest.js';
import {
  errManifestMismatch,
  errNoSecretForID,
  initWasm,
  initialize,
  updateSecret,
  fetchManifest,
  initializeOffline as wasmInitializeOffline,
  setAuth as wasmSetAuth,
  chatCompletions as wasmChatCompletions,
  streamChatCompletions as wasmStreamChatCompletions,
  unstructured as wasmUnstructured,
  transcribeAudio as wasmTranscribeAudio,
  listModels as wasmListModels,
  exportSecret as wasmExportSecret,
  importSecret as wasmImportSecret,
} from './wasm.js';

/**
 * An authentication credential for the Privatemode API. Privatemode supports
 * authenticating with an API key, a Clerk session JWT, or anonymously.
 */
export type AuthCredential =
  | { type: 'apiKey'; value: string }
  | { type: 'jwt'; value: string }
  | { type: 'anonymous'; value: string };

/**
 * A function that returns the credential to authenticate the next request with.
 * It is invoked before every request, so it can return short-lived credentials
 * (such as JWTs) that are refreshed over time.
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
   * credential to use. Use this to authenticate with a Clerk JWT or
   * anonymously, or to rotate short-lived credentials. Mutually exclusive
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
   * URL to load the Wasm module from in a browser environment.
   * Ignored in Node.js.
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
   * before instantiation.
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
  private isBrowser: boolean;
  private _manifestBytes: Uint8Array | null;
  private browserWasmURL: string;
  private enableWasmLogging: boolean;
  private expectedWasmHash: string | undefined;
  private onManifestUpdate?: (manifestBytes: Uint8Array) => void;
  private onSecretUpdate?: (secret: ExportedSecret) => void;
  private verified = false;
  private initialized = false;
  private hasSecret = false;

  constructor(options: PrivatemodeCoreOptions = {}) {
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
    this._manifestBytes = options.manifestBytes ?? null;
    this.browserWasmURL = options.browserWasmURL ?? './privatemode.wasm';
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
      this._manifestBytes = new TextEncoder().encode(await fetchManifest());
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
    wasmInitializeOffline(
      cred.type,
      cred.value,
      this.apiBaseURL,
      this.enableWasmLogging,
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
    wasmSetAuth(cred.type, cred.value);
    if (!this.verified) {
      await this.initializeWithManifestRetry(cred);
    }
    await updateSecret();
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
      const resp = await wasmChatCompletions(
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
      this.initiateStream(body, options),
    );
    yield* generator;
  }

  /**
   * Initiate a streaming request and return an async generator.
   * Waits for the stream to successfully start (first chunk, completion,
   * or error) before returning. This allows retryWithSecretRefresh to
   * catch "no secret for ID" errors that occur before streaming starts.
   */
  private async initiateStream(
    body: Record<string, unknown>,
    options?: PrivatemodeStreamOptions,
  ): Promise<AsyncGenerator<unknown>> {
    const chunks: string[] = [];
    let waiter: (() => void) | null = null;
    let done = false;
    let streamError: Error | null = null;

    const onChunk = (chunk: string) => {
      chunks.push(chunk);
      if (waiter) {
        waiter();
        waiter = null;
      }
    };

    const streamPromise = wasmStreamChatCompletions(
      JSON.stringify(body),
      onChunk,
      options?.signal,
      options?.onResponseHeaders,
      headersToRecord(options?.headers),
    );
    streamPromise
      .then(() => {
        done = true;
        if (waiter) {
          waiter();
          waiter = null;
        }
      })
      .catch((e: Error) => {
        streamError = e;
        if (waiter) {
          waiter();
          waiter = null;
        }
      });

    // Wait for first chunk, completion, or error to verify the stream started successfully.
    // "no secret for ID" errors occur before streaming starts, so they'll be caught here.
    while (chunks.length === 0 && streamError === null && !done) {
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

    // Stream has started successfully, return generator for remaining chunks
    return (async function* () {
      // Yield any chunks we already have
      while (chunks.length > 0) {
        yield JSON.parse(chunks.shift()!);
      }

      // Continue streaming
      while (true) {
        if (options?.signal?.aborted) {
          throw new DOMException('The operation was aborted.', 'AbortError');
        }

        if (chunks.length > 0) {
          yield JSON.parse(chunks.shift()!);
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
      const resp = await wasmUnstructured(wasmFiles, optsJSON);
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
      const wasmFile = {
        name: file.name,
        content:
          file.content instanceof Uint8Array
            ? file.content
            : new Uint8Array(file.content),
        ...(file.contentType ? { contentType: file.contentType } : {}),
      };
      const resp = await wasmTranscribeAudio(
        wasmFile,
        JSON.stringify(options),
        headersToRecord(requestOptions?.headers),
        requestOptions?.signal,
      );
      return { body: JSON.parse(resp.body), headers: resp.headers };
    });
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
    const resp = await wasmListModels(
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
    return JSON.parse(wasmExportSecret()) as ExportedSecret;
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
    wasmImportSecret(secret.id, secret.data, secret.expiresAtUnix);
    this.hasSecret = true;
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
   * as json encoding/decoding may alter the bytes and cause verification to fail.
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
   * Ensure the WASM module is loaded and initialized.
   */
  private async loadWasm(): Promise<void> {
    if (this.isBrowser) {
      await initWasm(fetch(this.browserWasmURL), this.expectedWasmHash);
      return;
    }
    const { readFile } = await import('node:fs/promises');
    const { resolve } = await import('node:path');
    const wasmPath = resolve(
      import.meta.dirname,
      '../../wasm/privatemode.wasm',
    ); // TODO(msanft): Distribute the Wasm blob via NPM?
    const wasmBuffer = await readFile(wasmPath);
    await initWasm(wasmBuffer, this.expectedWasmHash);
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
    wasmSetAuth(cred.type, cred.value);
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
      await initialize(
        manifestBase64,
        cred.type,
        cred.value,
        this.apiBaseURL,
        this.enableWasmLogging,
      );
    } catch (error) {
      if (
        error instanceof Error &&
        error.message.includes(errManifestMismatch())
      ) {
        // Fetch a new manifest and retry
        console.log('Manifest mismatch, trying again with new manifest...');
        const manifestBytes = new TextEncoder().encode(await fetchManifest());
        const newManifestBase64 = btoa(String.fromCharCode(...manifestBytes));

        await initialize(
          newManifestBase64,
          cred.type,
          cred.value,
          this.apiBaseURL,
          this.enableWasmLogging,
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

/** A file to send to the unstructured partition endpoint. */
export interface UnstructuredFile {
  /** Filename (e.g. "document.pdf"). */
  name: string;
  /** Raw file content as a Uint8Array or ArrayBuffer. */
  content: Uint8Array | ArrayBuffer;
  /** Optional MIME type hint (e.g. "application/pdf"). */
  contentType?: string;
}

/** Optional parameters for the unstructured partition endpoint. */
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
