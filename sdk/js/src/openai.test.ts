import OpenAI, { toFile } from 'openai';
import { afterEach, describe, expect, expectTypeOf, it, vi } from 'vitest';

import { PrivatemodeAPIError } from './errors.js';
import {
  asOpenAI,
  PrivatemodeAI,
  PrivatemodeOpenAITransport,
  type PrivatemodeOpenAIClientOptions,
  type PrivatemodeTransportClient,
} from './openai.js';
import { PrivatemodeCore } from './privatemode-ai.js';

const isBrowser =
  typeof window !== 'undefined' && typeof window.document !== 'undefined';

afterEach(() => {
  vi.restoreAllMocks();
});

function apiResponse(body: unknown, headers?: HeadersInit) {
  return { body, headers: new Headers(headers) };
}

function createTransportClient(
  overrides: Partial<PrivatemodeTransportClient> = {},
): PrivatemodeTransportClient {
  return {
    chatCompletions: vi.fn(async () =>
      apiResponse({
        choices: [],
        created: 1,
        id: 'chatcmpl-default',
        model: 'test-model',
        object: 'chat.completion',
      }),
    ),
    streamChatCompletions: async function* () {},
    transcribeAudio: vi.fn(async () => apiResponse({ text: '' })),
    listModels: vi.fn(async () => apiResponse({ data: [], object: 'list' })),
    ...overrides,
  };
}

function createClient(privatemode: PrivatemodeTransportClient): OpenAI {
  return asOpenAI(privatemode, {
    dangerouslyAllowBrowser: isBrowser,
    maxRetries: 0,
  });
}

describe('OpenAI transport', () => {
  it('makes PrivatemodeAI the OpenAI-compatible public client', () => {
    const client = new PrivatemodeAI({
      auth: () => ({ type: 'apiKey', value: 'test-key' }),
      dangerouslyAllowBrowser: isBrowser,
    });

    expect(client).toBeInstanceOf(OpenAI);
    expect(client).toMatchObject({
      exportSecret: expect.any(Function),
      initializeOffline: expect.any(Function),
      importSecret: expect.any(Function),
      refreshSecret: expect.any(Function),
      unstructured: expect.any(Function),
      verify: expect.any(Function),
    });
    expectTypeOf(client).not.toHaveProperty('chatCompletions');
    expectTypeOf(client).not.toHaveProperty('transcribeAudio');
    expectTypeOf(client).not.toHaveProperty('listModels');
    expectTypeOf<PrivatemodeOpenAIClientOptions>().not.toHaveProperty(
      'defaultQuery',
    );
    expectTypeOf<PrivatemodeOpenAIClientOptions>().not.toHaveProperty(
      'fetchOptions',
    );
  });

  it('exposes advanced Privatemode operations on the compatible client', async () => {
    const client = new PrivatemodeAI({
      auth: () => ({ type: 'apiKey', value: 'test-key' }),
      dangerouslyAllowBrowser: isBrowser,
    });
    const verify = vi
      .spyOn(PrivatemodeCore.prototype, 'verify')
      .mockResolvedValue({ manifest: {} as never });

    await client.verify();

    expect(verify).toHaveBeenCalledOnce();
  });

  it('initializes Privatemode lazily before the first OpenAI request', async () => {
    const client = new PrivatemodeAI({
      auth: () => ({ type: 'apiKey', value: 'test-key' }),
      dangerouslyAllowBrowser: isBrowser,
      openAIOptions: { maxRetries: 0 },
    });
    const verify = vi
      .spyOn(PrivatemodeCore.prototype, 'verify')
      .mockResolvedValue({ manifest: {} as never });
    const refreshSecret = vi
      .spyOn(PrivatemodeCore.prototype, 'refreshSecret')
      .mockResolvedValue();
    const chatCompletions = vi
      .spyOn(PrivatemodeCore.prototype, 'chatCompletions')
      .mockResolvedValue(
        apiResponse({
          choices: [
            {
              finish_reason: 'stop',
              index: 0,
              message: { content: 'Hello', role: 'assistant' },
            },
          ],
          created: 1,
          id: 'chatcmpl-1',
          model: 'test-model',
          object: 'chat.completion',
        }),
      );

    expect(verify).not.toHaveBeenCalled();
    const completion = await client.chat.completions.create({
      messages: [{ content: 'Hi', role: 'user' }],
      model: 'test-model',
    });

    expect(completion.choices[0]?.message.content).toBe('Hello');
    expect(verify).toHaveBeenCalledOnce();
    expect(refreshSecret).toHaveBeenCalledOnce();
    expect(chatCompletions).toHaveBeenCalledOnce();
  });

  it('reuses the verified Privatemode core with OpenAI option overrides', async () => {
    const client = new PrivatemodeAI({
      auth: () => ({ type: 'apiKey', value: 'test-key' }),
      dangerouslyAllowBrowser: isBrowser,
      openAIOptions: { maxRetries: 0 },
    });
    const verify = vi
      .spyOn(PrivatemodeCore.prototype, 'verify')
      .mockResolvedValue({ manifest: {} as never });
    const refreshSecret = vi
      .spyOn(PrivatemodeCore.prototype, 'refreshSecret')
      .mockResolvedValue();
    const chatCompletions = vi
      .spyOn(PrivatemodeCore.prototype, 'chatCompletions')
      .mockResolvedValue(
        apiResponse({
          choices: [],
          created: 1,
          id: 'chatcmpl-1',
          model: 'test-model',
          object: 'chat.completion',
        }),
      );
    const configured = client.withOptions({
      defaultHeaders: { 'x-custom-header': 'custom-value' },
      timeout: 1234,
    });

    await client.chat.completions.create({
      messages: [{ content: 'Hi', role: 'user' }],
      model: 'test-model',
    });
    await configured.chat.completions.create({
      messages: [{ content: 'Hi again', role: 'user' }],
      model: 'test-model',
    });

    expect(configured).toBeInstanceOf(PrivatemodeAI);
    expect(configured.timeout).toBe(1234);
    expect(verify).toHaveBeenCalledOnce();
    expect(refreshSecret).toHaveBeenCalledOnce();
    expect(chatCompletions).toHaveBeenCalledTimes(2);
    const requestHeaders = chatCompletions.mock.calls[1]?.[1]?.headers;
    expect(new Headers(requestHeaders).get('x-custom-header')).toBe(
      'custom-value',
    );
    expect(new Headers(requestHeaders).has('authorization')).toBe(false);
    expect(new Headers(requestHeaders).has('content-type')).toBe(false);
  });

  it('routes chat completions through Privatemode', async () => {
    const chatCompletions = vi.fn(async () =>
      apiResponse(
        {
          choices: [
            {
              finish_reason: 'stop',
              index: 0,
              message: { content: 'Hello', role: 'assistant' },
            },
          ],
          created: 1,
          id: 'chatcmpl-1',
          model: 'test-model',
          object: 'chat.completion',
        },
        { 'x-request-id': 'req_chat' },
      ),
    );
    const client = createClient(createTransportClient({ chatCompletions }));

    const {
      data: completion,
      response,
      request_id: requestID,
    } = await client.chat.completions
      .create({
        messages: [{ content: 'Hi', role: 'user' }],
        model: 'test-model',
      })
      .withResponse();

    expect(completion.choices[0]?.message.content).toBe('Hello');
    expect(completion._request_id).toBe('req_chat');
    expect(response.headers.get('x-request-id')).toBe('req_chat');
    expect(requestID).toBe('req_chat');
    expect(chatCompletions).toHaveBeenCalledWith(
      {
        messages: [{ content: 'Hi', role: 'user' }],
        model: 'test-model',
      },
      { headers: expect.any(Headers), signal: expect.any(AbortSignal) },
    );
  });

  it('converts Privatemode chunks into an OpenAI stream', async () => {
    const streamChatCompletions = vi.fn(async function* (
      _body: Record<string, unknown>,
      options?: {
        onResponseHeaders?: (headers: Headers) => void;
        signal?: AbortSignal;
      },
    ) {
      options?.onResponseHeaders?.(
        new Headers({ 'x-request-id': 'req_stream' }),
      );
      yield {
        choices: [
          {
            delta: { content: 'Hel', role: 'assistant' },
            finish_reason: null,
            index: 0,
          },
        ],
        created: 1,
        id: 'chatcmpl-1',
        model: 'test-model',
        object: 'chat.completion.chunk',
      };
      yield {
        choices: [
          {
            delta: { content: 'lo' },
            finish_reason: 'stop',
            index: 0,
          },
        ],
        created: 1,
        id: 'chatcmpl-1',
        model: 'test-model',
        object: 'chat.completion.chunk',
      };
    });
    const client = createClient(
      createTransportClient({ streamChatCompletions }),
    );

    const { data: stream, response } = await client.chat.completions
      .create({
        messages: [{ content: 'Hi', role: 'user' }],
        model: 'test-model',
        stream: true,
      })
      .withResponse();
    const chunks = [];
    for await (const chunk of stream) chunks.push(chunk);

    expect(
      chunks.map((chunk) => chunk.choices[0]?.delta.content ?? '').join(''),
    ).toBe('Hello');
    expect(response.headers.get('x-request-id')).toBe('req_stream');
    expect(streamChatCompletions).toHaveBeenCalledWith(
      {
        messages: [{ content: 'Hi', role: 'user' }],
        model: 'test-model',
        stream: true,
      },
      {
        headers: expect.any(Headers),
        onResponseHeaders: expect.any(Function),
        signal: expect.any(AbortSignal),
      },
    );
  });

  it('surfaces errors directly after streaming has begun', async () => {
    const streamError = new PrivatemodeAPIError('stream interrupted', 429, {
      error: {
        code: 'rate_limit',
        message: 'Slow down',
        type: 'rate_limit_error',
      },
    });
    const streamChatCompletions = vi.fn(async function* () {
      yield {
        choices: [
          {
            delta: { content: 'Hello', role: 'assistant' },
            finish_reason: null,
            index: 0,
          },
        ],
        created: 1,
        id: 'chatcmpl-1',
        model: 'test-model',
        object: 'chat.completion.chunk',
      };
      throw streamError;
    });
    const client = createClient(
      createTransportClient({ streamChatCompletions }),
    );
    const stream = await client.chat.completions.create({
      messages: [{ content: 'Hi', role: 'user' }],
      model: 'test-model',
      stream: true,
    });

    const readStream = async () => {
      const chunks = [];
      // Continue until the transport reports the mid-stream failure.
      for await (const chunk of stream) chunks.push(chunk);
      return chunks;
    };

    await expect(readStream()).rejects.toBe(streamError);
  });

  it('routes model listing through Privatemode', async () => {
    const listModels = vi.fn(async () =>
      apiResponse(
        {
          data: [
            {
              created: 1,
              id: 'test-model',
              object: 'model',
              owned_by: 'privatemode',
            },
          ],
          object: 'list',
        },
        { 'x-request-id': 'req_models' },
      ),
    );
    const client = createClient(createTransportClient({ listModels }));

    const { data: models, response } = await client.models
      .list()
      .withResponse();

    expect(models.data[0]?.id).toBe('test-model');
    expect(response.headers.get('x-request-id')).toBe('req_models');
    expect(listModels).toHaveBeenCalledWith({
      headers: expect.any(Headers),
      signal: expect.any(AbortSignal),
    });
  });

  it('serves model retrieval from the model list', async () => {
    const listModels = vi.fn(async () =>
      apiResponse(
        {
          data: [
            {
              created: 1,
              id: 'test-model',
              object: 'model',
              owned_by: 'privatemode',
            },
          ],
          object: 'list',
        },
        { 'x-request-id': 'req_model' },
      ),
    );
    const client = createClient(createTransportClient({ listModels }));

    const { data: model, response } = await client.models
      .retrieve('test-model')
      .withResponse();

    expect(model.id).toBe('test-model');
    expect(response.headers.get('x-request-id')).toBe('req_model');
  });

  it('returns an OpenAI error when retrieving an unknown model', async () => {
    const client = createClient(createTransportClient());

    await expect(client.models.retrieve('unknown-model')).rejects.toMatchObject(
      {
        code: 'model_not_found',
        status: 404,
        type: 'invalid_request_error',
      },
    );
  });

  it('converts OpenAI multipart transcription requests', async () => {
    const transcribeAudio = vi.fn(async () =>
      apiResponse({ text: 'Hello' }, { 'x-request-id': 'req_audio' }),
    );
    const client = createClient(createTransportClient({ transcribeAudio }));
    const file = await toFile(new Uint8Array([1, 2, 3]), 'audio.wav', {
      type: 'audio/wav',
    });

    const { data: transcription, response } = await client.audio.transcriptions
      .create({
        file,
        language: 'en',
        model: 'voxtral-mini-3b',
        temperature: 0.2,
      })
      .withResponse();

    expect(transcription.text).toBe('Hello');
    expect(response.headers.get('x-request-id')).toBe('req_audio');
    expect(transcribeAudio).toHaveBeenCalledWith(
      {
        content: new Uint8Array([1, 2, 3]),
        contentType: 'audio/wav',
        name: 'audio.wav',
      },
      {
        language: 'en',
        model: 'voxtral-mini-3b',
        temperature: 0.2,
      },
      { headers: expect.any(Headers), signal: expect.any(AbortSignal) },
    );
  });

  it('returns an OpenAI error for unsupported transcription formats', async () => {
    const client = createClient(createTransportClient());
    const file = await toFile(new Uint8Array([1]), 'audio.wav');

    await expect(
      client.audio.transcriptions.create({
        file,
        model: 'voxtral-mini-3b',
        response_format: 'text',
      }),
    ).rejects.toMatchObject({
      code: 'invalid_request',
      status: 400,
      type: 'invalid_request_error',
    });
  });

  it('returns an OpenAI error for streaming transcriptions', async () => {
    const client = createClient(createTransportClient());
    const file = await toFile(new Uint8Array([1]), 'audio.wav');

    await expect(
      client.audio.transcriptions.create({
        file,
        model: 'voxtral-mini-3b',
        stream: true,
      }),
    ).rejects.toMatchObject({
      code: 'invalid_request',
      status: 400,
      type: 'invalid_request_error',
    });
  });

  it('propagates request aborts while lazy initialization is pending', async () => {
    const transport = new PrivatemodeOpenAITransport(
      createTransportClient(),
      () => new Promise<never>(() => {}),
    );
    const abortController = new AbortController();

    const response = transport.fetch('https://privatemode.invalid/v1/models', {
      method: 'GET',
      signal: abortController.signal,
    });
    abortController.abort();

    await expect(response).rejects.toMatchObject({ name: 'AbortError' });
  });

  it('forwards additional transcription parameters', async () => {
    const transcribeAudio = vi.fn(async () => apiResponse({ text: 'Hello' }));
    const client = createClient(createTransportClient({ transcribeAudio }));
    const file = await toFile(new Uint8Array([1]), 'audio.wav');

    await client.audio.transcriptions.create({
      file,
      model: 'voxtral-mini-3b',
      response_format: 'verbose_json',
      timestamp_granularities: ['word', 'segment'],
    });

    expect(transcribeAudio).toHaveBeenCalledWith(
      expect.any(Object),
      expect.objectContaining({
        additional_fields: {
          'timestamp_granularities[]': ['word', 'segment'],
        },
      }),
      { headers: expect.any(Headers), signal: expect.any(AbortSignal) },
    );
  });

  it('rejects an empty transcription temperature', async () => {
    const form = new FormData();
    form.append('file', new Blob([new Uint8Array([1])]), 'audio.wav');
    form.append('model', 'voxtral-mini-3b');
    form.append('temperature', '');
    const transport = new PrivatemodeOpenAITransport(createTransportClient());

    const response = await transport.fetch(
      'https://privatemode.invalid/v1/audio/transcriptions',
      { body: form, method: 'POST' },
    );

    expect(response.status).toBe(400);
    await expect(response.json()).resolves.toMatchObject({
      error: { message: 'temperature must be a finite number.' },
    });
  });

  it('surfaces structured API failures as OpenAI errors', async () => {
    const error = new PrivatemodeAPIError('rate limited', 429, {
      error: {
        code: 'rate_limit',
        message: 'Slow down',
        type: 'rate_limit_error',
      },
    });
    const client = createClient(
      createTransportClient({
        chatCompletions: vi.fn(async () => Promise.reject(error)),
      }),
    );

    await expect(
      client.chat.completions.create({
        messages: [{ content: 'Hi', role: 'user' }],
        model: 'test-model',
      }),
    ).rejects.toBeInstanceOf(OpenAI.RateLimitError);
  });

  it('keeps the OpenAI retry policy enabled by default', async () => {
    const error = new PrivatemodeAPIError(
      'rate limited',
      429,
      {
        error: {
          code: 'rate_limit',
          message: 'Slow down',
          type: 'rate_limit_error',
        },
      },
      new Headers({ 'retry-after-ms': '0' }),
    );
    const chatCompletions = vi.fn(async () => Promise.reject(error));
    const client = asOpenAI(createTransportClient({ chatCompletions }), {
      dangerouslyAllowBrowser: isBrowser,
    });

    await expect(
      client.chat.completions.create({
        messages: [{ content: 'Hi', role: 'user' }],
        model: 'test-model',
      }),
    ).rejects.toBeInstanceOf(OpenAI.RateLimitError);
    expect(chatCompletions).toHaveBeenCalledTimes(3);
  });

  it('fails closed for unsupported OpenAI endpoints', async () => {
    const client = createClient(createTransportClient());

    await expect(
      client.embeddings.create({ input: 'hello', model: 'test-model' }),
    ).rejects.toMatchObject({
      code: 'unsupported_endpoint',
      status: 501,
      type: 'unsupported_endpoint_error',
    });
  });

  it('forwards cancellation to in-flight non-streaming chat completions', async () => {
    let requestStarted!: () => void;
    const started = new Promise<void>((resolve) => {
      requestStarted = resolve;
    });
    const chatCompletions = vi.fn(
      (_body: Record<string, unknown>, options?: { signal?: AbortSignal }) =>
        new Promise<never>((_resolve, reject) => {
          requestStarted();
          options?.signal?.addEventListener(
            'abort',
            () => reject(options.signal?.reason),
            { once: true },
          );
        }),
    );
    const client = createClient(createTransportClient({ chatCompletions }));
    const abortController = new AbortController();

    const completion = client.chat.completions.create(
      {
        messages: [{ content: 'Hi', role: 'user' }],
        model: 'test-model',
      },
      { signal: abortController.signal },
    );
    completion.catch(() => {}); // Avoid an unhandled rejection while waiting.
    await started;
    abortController.abort();

    await expect(completion).rejects.toBeInstanceOf(OpenAI.APIUserAbortError);
    expect(chatCompletions).toHaveBeenCalledWith(expect.any(Object), {
      headers: expect.any(Headers),
      signal: expect.any(AbortSignal),
    });
  });

  it('forwards cancellation while starting a stream', async () => {
    const streamChatCompletions = vi.fn(async function* (
      _body: Record<string, unknown>,
      options?: { signal?: AbortSignal },
    ) {
      await new Promise<void>((_resolve, reject) => {
        options?.signal?.addEventListener(
          'abort',
          () => reject(options.signal?.reason),
          { once: true },
        );
      });
      yield undefined;
    });
    const client = createClient(
      createTransportClient({ streamChatCompletions }),
    );
    const abortController = new AbortController();

    const stream = client.chat.completions.create(
      {
        messages: [{ content: 'Hi', role: 'user' }],
        model: 'test-model',
        stream: true,
      },
      { signal: abortController.signal },
    );
    abortController.abort();

    await expect(stream).rejects.toBeInstanceOf(OpenAI.APIUserAbortError);
  });
});
