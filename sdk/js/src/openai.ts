import OpenAI, { type ClientOptions } from 'openai';

import { PrivatemodeAPIError } from './errors.js';
import type { Manifest } from './manifest.js';
import {
  PrivatemodeCore,
  type AudioFile,
  type AudioTranscriptionOptions,
  type ExportedSecret,
  type PrivatemodeCoreOptions,
  type PrivatemodeRequestOptions,
  type PrivatemodeResponse,
  type UnstructuredFile,
  type UnstructuredOptions,
  type VerifyResult,
} from './privatemode-ai.js';

const transportBaseURL = 'https://privatemode.invalid/v1';
// Satisfies the OpenAI client's mandatory API key check. Requests are
// authenticated through the configured Privatemode auth provider instead,
// and this value never leaves the process: every request is handled by
// PrivatemodeOpenAITransport rather than sent over the network.
const placeholderAPIKey = 'privatemode-transport';
const unsupportedTranscriptionFields = new Set(['stream']);
const controlledOpenAIOptionNames = [
  'adminAPIKey',
  'apiKey',
  'baseURL',
  'defaultQuery',
  'fetch',
  'fetchOptions',
  'provider',
  'workloadIdentity',
];
const openAITransportHeaderNames = new Set([
  'accept',
  'authorization',
  'content-length',
  'content-type',
  'host',
  'openai-organization',
  'openai-project',
  'user-agent',
]);
const privatemodeState = Symbol('privatemodeState');

interface PrivatemodeAIState {
  core: PrivatemodeCore;
  ensureReady: () => Promise<void>;
}

type PrivatemodeAIInternalOptions = PrivatemodeAIOptions & {
  [privatemodeState]?: PrivatemodeAIState;
};

/** The Privatemode operations used by the OpenAI transport adapter. */
export type PrivatemodeTransportClient = Pick<
  PrivatemodeCore,
  'chatCompletions' | 'listModels' | 'streamChatCompletions' | 'transcribeAudio'
>;

/** OpenAI client options whose values are not controlled by Privatemode. */
export type PrivatemodeOpenAIClientOptions = Omit<
  ClientOptions,
  | 'adminAPIKey'
  | 'apiKey'
  | 'baseURL'
  | 'defaultQuery'
  | 'fetch'
  | 'fetchOptions'
  | 'provider'
  | 'workloadIdentity'
>;

/** Options for the OpenAI-compatible Privatemode client. */
export type PrivatemodeAIOptions = PrivatemodeCoreOptions & {
  /** Options forwarded to the official OpenAI client. */
  openAIOptions?: PrivatemodeOpenAIClientOptions;
};

/**
 * Secure OpenAI-compatible client backed by the encrypted Privatemode
 * transport. Attestation and secret establishment happen lazily before the
 * first OpenAI request.
 */
export class PrivatemodeAI extends OpenAI {
  private readonly privatemode: PrivatemodeCore;
  private readonly state: PrivatemodeAIState;

  public constructor(options: PrivatemodeAIOptions) {
    const {
      [privatemodeState]: existingState,
      openAIOptions,
      ...privatemodeOptions
    } = options as PrivatemodeAIInternalOptions;
    assertSupportedOpenAIOptions(openAIOptions);
    const dangerouslyAllowBrowser =
      openAIOptions?.dangerouslyAllowBrowser ??
      privatemodeOptions.dangerouslyAllowBrowser;
    const state =
      existingState ??
      createPrivatemodeState({
        ...privatemodeOptions,
        dangerouslyAllowBrowser,
      });
    const transport = new PrivatemodeOpenAITransport(
      state.core,
      state.ensureReady,
    );

    super({
      ...openAIOptions,
      apiKey: placeholderAPIKey,
      baseURL: transportBaseURL,
      dangerouslyAllowBrowser,
      fetch: transport.fetch,
    });

    this.privatemode = state.core;
    this.state = state;
  }

  /** Create a configured client that reuses this client's verified state. */
  public override withOptions(
    options: Partial<PrivatemodeOpenAIClientOptions>,
  ): this;
  public override withOptions(options: Partial<ClientOptions>): this {
    assertSupportedOpenAIOptions(options);
    const inheritedOptions: ClientOptions = {
      ...this._options,
      maxRetries: this.maxRetries,
      timeout: this.timeout,
      logger: this.logger,
      logLevel: this.logLevel,
    };
    for (const name of controlledOpenAIOptionNames) {
      delete inheritedOptions[name as keyof ClientOptions];
    }

    return new PrivatemodeAI({
      openAIOptions: {
        ...inheritedOptions,
        ...options,
      } as PrivatemodeOpenAIClientOptions,
      [privatemodeState]: this.state,
    } as PrivatemodeAIInternalOptions) as this;
  }

  /** Verify the Privatemode deployment. See {@link PrivatemodeCore.verify}. */
  public verify(): Promise<VerifyResult> {
    return this.privatemode.verify();
  }

  /** Initialize without attestation. See {@link PrivatemodeCore.initializeOffline}. */
  public initializeOffline(manifestBytes: Uint8Array): Promise<void> {
    return this.privatemode.initializeOffline(manifestBytes);
  }

  /** Refresh the encryption secret. See {@link PrivatemodeCore.refreshSecret}. */
  public refreshSecret(): Promise<void> {
    return this.privatemode.refreshSecret();
  }

  /** Partition documents. See {@link PrivatemodeCore.unstructured}. */
  public unstructured(
    files: UnstructuredFile[],
    options?: UnstructuredOptions,
  ): Promise<unknown> {
    return this.privatemode.unstructured(files, options);
  }

  /** Export the encryption secret. See {@link PrivatemodeCore.exportSecret}. */
  public exportSecret(): ExportedSecret {
    return this.privatemode.exportSecret();
  }

  /** Import an encryption secret. See {@link PrivatemodeCore.importSecret}. */
  public importSecret(secret: ExportedSecret): void {
    this.privatemode.importSecret(secret);
  }

  /** The current manifest. See {@link PrivatemodeCore.manifest}. */
  public get manifest(): Manifest | null {
    return this.privatemode.manifest;
  }

  /** The current manifest as raw bytes. See {@link PrivatemodeCore.manifestBytes}. */
  public get manifestBytes(): Uint8Array | null {
    return this.privatemode.manifestBytes;
  }
}

/**
 * Wrap an initialized Privatemode client in the official OpenAI client.
 *
 * The caller must verify the Privatemode deployment and establish or import an
 * encryption secret before making inference requests.
 */
export function asOpenAI(
  privatemode: PrivatemodeTransportClient,
  options: PrivatemodeOpenAIClientOptions = {},
): OpenAI {
  assertSupportedOpenAIOptions(options);
  const transport = new PrivatemodeOpenAITransport(privatemode);
  return new OpenAI({
    ...options,
    apiKey: placeholderAPIKey,
    baseURL: transportBaseURL,
    fetch: transport.fetch,
  });
}

/**
 * Returns a hook that verifies the deployment and establishes an encryption
 * secret once, before the first request that uses it. A failed initialization
 * is retried on the next request.
 */
function lazyInitialization(privatemode: PrivatemodeCore): () => Promise<void> {
  let initialization: Promise<void> | undefined;
  return (): Promise<void> => {
    if (privatemode.readyForInference) return Promise.resolve();
    initialization ??= (async () => {
      await privatemode.verify();
      await privatemode.refreshSecret();
    })().catch((error: unknown) => {
      initialization = undefined;
      throw error;
    });
    return initialization;
  };
}

function createPrivatemodeState(
  options: PrivatemodeCoreOptions,
): PrivatemodeAIState {
  const core = new PrivatemodeCore(options);
  return { core, ensureReady: lazyInitialization(core) };
}

/** A Fetch-compatible adapter from the OpenAI SDK to Privatemode. */
export class PrivatemodeOpenAITransport {
  public constructor(
    private readonly privatemode: PrivatemodeTransportClient,
    private readonly ensureReady?: () => Promise<void>,
  ) {}

  /** Route an OpenAI SDK request through the encrypted Privatemode client. */
  public readonly fetch: NonNullable<ClientOptions['fetch']> = async (
    input,
    init,
  ) => {
    const request = new Request(input, init);
    throwIfAborted(request.signal);

    const method = request.method.toUpperCase();
    const path = normalizePath(new URL(request.url).pathname);

    try {
      if (method === 'POST' && path === '/v1/chat/completions') {
        await this.ensureReadyFor(request);
        return await this.chatCompletions(request);
      }
      if (method === 'POST' && path === '/v1/audio/transcriptions') {
        await this.ensureReadyFor(request);
        return await this.transcribeAudio(request);
      }
      if (method === 'GET' && path === '/v1/models') {
        await this.ensureReadyFor(request);
        const models = await this.listModels(request);
        return jsonResponse(models.body, { headers: models.headers });
      }
      const modelID = retrievedModelID(path);
      if (method === 'GET' && modelID !== undefined) {
        await this.ensureReadyFor(request);
        return await this.retrieveModel(request, modelID);
      }
    } catch (error) {
      const response = apiErrorResponse(error);
      if (response) return response;
      throw error;
    }

    return unsupportedEndpointResponse(method, path);
  };

  /** Run the lazy-initialization hook, honoring the request's abort signal. */
  private async ensureReadyFor(request: Request): Promise<void> {
    if (!this.ensureReady) return;
    await withAbort(this.ensureReady(), request.signal);
  }

  private async listModels(request: Request): Promise<PrivatemodeResponse> {
    return withAbort(
      this.privatemode.listModels(requestOptions(request)),
      request.signal,
    );
  }

  /** Serve a single-model lookup from the model list. */
  private async retrieveModel(
    request: Request,
    modelID: string,
  ): Promise<Response> {
    const models = await this.listModels(request);
    const data = (models.body as { data?: unknown }).data;
    const model = Array.isArray(data)
      ? (data as Array<{ id?: unknown }>).find((m) => m?.id === modelID)
      : undefined;
    if (!model) {
      return jsonResponse(
        {
          error: {
            code: 'model_not_found',
            message: `The model '${modelID}' does not exist.`,
            param: 'model',
            type: 'invalid_request_error',
          },
        },
        { status: 404 },
      );
    }
    return jsonResponse(model, { headers: models.headers });
  }

  private async chatCompletions(request: Request): Promise<Response> {
    const body = await readJSONObject(request);
    if (body.stream === true) {
      return this.streamingResponse(body, request);
    }

    const completion = await withAbort(
      this.privatemode.chatCompletions(body, requestOptions(request)),
      request.signal,
    );
    return jsonResponse(completion.body, { headers: completion.headers });
  }

  private async streamingResponse(
    body: Record<string, unknown>,
    request: Request,
  ): Promise<Response> {
    const signal = request.signal;
    const abortController = new AbortController();
    let responseHeaders = new Headers();
    const forwardAbort = () => abortController.abort(abortReason(signal));
    signal.addEventListener('abort', forwardAbort, { once: true });
    const cleanup = () => signal.removeEventListener('abort', forwardAbort);

    if (signal.aborted) forwardAbort();

    const generator = this.privatemode.streamChatCompletions(body, {
      headers: requestOptions(request).headers,
      onResponseHeaders: (headers) => {
        responseHeaders = headers;
      },
      signal: abortController.signal,
    });
    const iterator = generator[Symbol.asyncIterator]();

    let first: IteratorResult<unknown>;
    try {
      first = await withAbort(iterator.next(), abortController.signal);
    } catch (error) {
      cleanup();
      throw error;
    }

    let pending: IteratorResult<unknown> | undefined = first;
    let finished = false;
    const encoder = new TextEncoder();

    const stream = new ReadableStream<Uint8Array>({
      pull: async (controller) => {
        try {
          throwIfAborted(abortController.signal);
          const next = pending ?? (await iterator.next());
          pending = undefined;

          if (next.done) {
            finished = true;
            controller.enqueue(encoder.encode('data: [DONE]\n\n'));
            controller.close();
            cleanup();
            return;
          }

          controller.enqueue(
            encoder.encode(`data: ${JSON.stringify(next.value)}\n\n`),
          );
        } catch (error) {
          finished = true;
          cleanup();
          controller.error(error);
        }
      },
      cancel: async (reason) => {
        if (!finished) abortController.abort(reason);
        cleanup();
        await iterator.return?.(undefined);
      },
    });

    responseHeaders.set('cache-control', 'no-cache');
    responseHeaders.set('content-type', 'text/event-stream; charset=utf-8');
    responseHeaders.delete('content-encoding');
    responseHeaders.delete('content-length');
    responseHeaders.delete('transfer-encoding');
    return new Response(stream, { headers: responseHeaders });
  }

  private async transcribeAudio(request: Request): Promise<Response> {
    const form = await request.formData();

    const fileValue = form.get('file');
    if (
      typeof fileValue === 'string' ||
      !fileValue ||
      typeof fileValue.arrayBuffer !== 'function'
    ) {
      return badRequestResponse('Audio transcription requires a file.');
    }

    const model = requiredString(form, 'model');
    if (model instanceof Response) return model;

    const responseFormat = optionalString(form, 'response_format');
    if (
      responseFormat !== undefined &&
      responseFormat !== 'json' &&
      responseFormat !== 'verbose_json'
    ) {
      return badRequestResponse(
        'Privatemode supports only json and verbose_json transcription responses.',
      );
    }

    const temperature = optionalNumber(form, 'temperature');
    if (temperature instanceof Response) return temperature;
    const language = optionalString(form, 'language');
    const prompt = optionalString(form, 'prompt');
    const additionalFields: Record<string, string[]> = {};
    for (const [name, value] of form) {
      if (
        name === 'file' ||
        name === 'language' ||
        name === 'model' ||
        name === 'prompt' ||
        name === 'response_format' ||
        name === 'temperature'
      ) {
        continue;
      }
      if (
        unsupportedTranscriptionFields.has(name) &&
        value.toString() === 'true'
      ) {
        return badRequestResponse(
          `Privatemode does not support the ${name} audio transcription parameter.`,
        );
      }
      if (typeof value !== 'string') {
        return badRequestResponse(
          `Privatemode cannot forward the binary audio transcription parameter ${name}.`,
        );
      }
      (additionalFields[name] ??= []).push(value);
    }

    const file: AudioFile = {
      name: fileValue.name || 'audio',
      content: new Uint8Array(await fileValue.arrayBuffer()),
      ...(fileValue.type ? { contentType: fileValue.type } : {}),
    };
    const options: AudioTranscriptionOptions = {
      model,
      ...(language !== undefined ? { language } : {}),
      ...(prompt !== undefined ? { prompt } : {}),
      ...(responseFormat !== undefined
        ? { response_format: responseFormat }
        : {}),
      ...(temperature !== undefined ? { temperature } : {}),
      ...(Object.keys(additionalFields).length > 0
        ? { additional_fields: additionalFields }
        : {}),
    };

    const transcription = await withAbort(
      this.privatemode.transcribeAudio(file, options, requestOptions(request)),
      request.signal,
    );
    return jsonResponse(transcription.body, {
      headers: transcription.headers,
    });
  }
}

function assertSupportedOpenAIOptions(
  options: Partial<ClientOptions> | undefined,
): void {
  if (!options) return;
  const unsupported = controlledOpenAIOptionNames.filter((name) =>
    Object.hasOwn(options, name),
  );
  if (unsupported.length > 0) {
    throw new Error(
      `Privatemode controls or does not support these OpenAI client options: ${unsupported.join(', ')}.`,
    );
  }
}

function requestOptions(request: Request): PrivatemodeRequestOptions {
  const headers = new Headers();
  for (const [name, value] of request.headers) {
    if (
      openAITransportHeaderNames.has(name) ||
      name.startsWith('x-stainless-')
    ) {
      continue;
    }
    headers.append(name, value);
  }
  return { headers, signal: request.signal };
}

function normalizePath(path: string): string {
  if (path.length > 1 && path.endsWith('/')) return path.slice(0, -1);
  return path;
}

/** The model ID of a /v1/models/{model} path, or undefined for other paths. */
function retrievedModelID(path: string): string | undefined {
  const prefix = '/v1/models/';
  if (!path.startsWith(prefix)) return undefined;
  const modelID = path.slice(prefix.length);
  if (modelID.length === 0 || modelID.includes('/')) return undefined;
  return decodeURIComponent(modelID);
}

async function readJSONObject(
  request: Request,
): Promise<Record<string, unknown>> {
  let value: unknown;
  try {
    value = await request.json();
  } catch (error) {
    throw new TypeError('Request body must be valid JSON.', { cause: error });
  }
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    throw new TypeError('Request body must be a JSON object.');
  }
  return value as Record<string, unknown>;
}

function jsonResponse(body: unknown, init: ResponseInit = {}): Response {
  const headers = new Headers(init.headers);
  headers.delete('content-encoding');
  headers.delete('content-length');
  headers.delete('transfer-encoding');
  if (!headers.has('content-type')) {
    headers.set('content-type', 'application/json; charset=utf-8');
  }
  return new Response(JSON.stringify(body), { ...init, headers });
}

function badRequestResponse(message: string): Response {
  return jsonResponse(
    {
      error: {
        code: 'invalid_request',
        message,
        type: 'invalid_request_error',
      },
    },
    { status: 400 },
  );
}

function unsupportedEndpointResponse(method: string, path: string): Response {
  return jsonResponse(
    {
      error: {
        code: 'unsupported_endpoint',
        message: `Privatemode does not support ${method} ${path} through the OpenAI client.`,
        type: 'unsupported_endpoint_error',
      },
    },
    {
      status: 501,
      headers: { 'x-should-retry': 'false' },
    },
  );
}

function apiErrorResponse(error: unknown): Response | undefined {
  if (!(error instanceof PrivatemodeAPIError)) return undefined;
  if (error.status < 400 || error.status > 599) return undefined;

  const init = { headers: error.headers, status: error.status };
  if (typeof error.body === 'string') {
    return new Response(error.body, init);
  }
  return jsonResponse(error.body, init);
}

function requiredString(form: FormData, name: string): string | Response {
  const value = form.get(name);
  if (typeof value !== 'string' || value.length === 0) {
    return badRequestResponse(`Audio transcription requires ${name}.`);
  }
  return value;
}

function optionalString(form: FormData, name: string): string | undefined {
  const value = form.get(name);
  return typeof value === 'string' ? value : undefined;
}

function optionalNumber(
  form: FormData,
  name: string,
): number | Response | undefined {
  const value = optionalString(form, name);
  if (value === undefined) return undefined;
  if (value.trim() === '') {
    return badRequestResponse(`${name} must be a finite number.`);
  }
  const number = Number(value);
  if (!Number.isFinite(number)) {
    return badRequestResponse(`${name} must be a finite number.`);
  }
  return number;
}

function throwIfAborted(signal: AbortSignal): void {
  if (signal.aborted) throw abortReason(signal);
}

function abortReason(signal: AbortSignal): unknown {
  return (
    signal.reason ??
    new DOMException('The operation was aborted.', 'AbortError')
  );
}

function withAbort<T>(operation: Promise<T>, signal: AbortSignal): Promise<T> {
  if (signal.aborted) return Promise.reject(abortReason(signal));

  return new Promise<T>((resolve, reject) => {
    const onAbort = () => reject(abortReason(signal));
    signal.addEventListener('abort', onAbort, { once: true });
    operation.then(resolve, reject).finally(() => {
      signal.removeEventListener('abort', onAbort);
    });
  });
}
