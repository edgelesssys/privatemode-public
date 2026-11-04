/// <reference types="vite/client" />
import OpenAI, { toFile } from 'openai';
import { describe, expect, it } from 'vitest';

import { PrivatemodeAI } from './openai.js';

const isBrowser =
  typeof window !== 'undefined' && typeof window.document !== 'undefined';
const isNode = typeof process !== 'undefined' && !!process.versions?.node;
const runIntegrationTests = !!import.meta.env.PRIVATEMODE_RUN_INTEGRATION_TESTS;
const browserWasmURL = isNode
  ? undefined
  : new URL('../../wasm/privatemode.wasm', import.meta.url).href;

function createClient(): PrivatemodeAI {
  return new PrivatemodeAI({
    apiKey: import.meta.env.PRIVATEMODE_API_KEY,
    apiBaseURL: 'https://api.privatemode.ai',
    browserWasmURL,
    dangerouslyAllowBrowser: isBrowser,
  });
}

/** Load the spoken-audio MP3 fixture (a copy of test/docs/test.mp3). */
async function testAudio(): Promise<Uint8Array> {
  const url = new URL('./testdata/test.mp3', import.meta.url);
  if (isNode) {
    const { readFile } = await import('node:fs/promises');
    const { fileURLToPath } = await import('node:url');
    return new Uint8Array(await readFile(fileURLToPath(url)));
  }
  const response = await fetch(url);
  return new Uint8Array(await response.arrayBuffer());
}

describe.skipIf(!runIntegrationTests)('OpenAI-compatible client', () => {
  it('performs encrypted inference through the public OpenAI-compatible client', async () => {
    const client = createClient();

    const completion = await client.chat.completions.create({
      max_tokens: 500,
      messages: [
        {
          role: 'user',
          content:
            'Reply with exactly one word: hello. Do not include any other text.',
        },
      ],
      model: 'gpt-oss-120b',
    });

    expect(completion.choices[0]?.message.content?.toLowerCase()).toContain(
      'hello',
    );
  }, 120_000);

  it('streams encrypted inference through the public OpenAI-compatible client', async () => {
    const client = createClient();

    const stream = await client.chat.completions.create({
      max_tokens: 500,
      messages: [
        {
          role: 'user',
          content:
            'Reply with exactly one word: hello. Do not include any other text.',
        },
      ],
      model: 'gpt-oss-120b',
      stream: true,
    });

    let content = '';
    for await (const chunk of stream) {
      content += chunk.choices[0]?.delta.content ?? '';
    }

    expect(content.toLowerCase()).toContain('hello');
  }, 120_000);

  it('lists available models and retrieves a model matching its list entry', async () => {
    const client = createClient();

    const models = await client.models.list();

    expect(models.data.length).toBeGreaterThan(0);
    expect(models.data.map((model) => model.id)).toContain('gpt-oss-120b');

    const listedModel = models.data[0];
    const model = await client.models.retrieve(listedModel.id);

    expect(model).toStrictEqual(listedModel);
  }, 120_000);

  it('preserves OpenAI error details when retrieving an unknown model', async () => {
    const client = createClient();
    const model = client.models.retrieve('this-model-does-not-exist');

    await expect(model).rejects.toBeInstanceOf(OpenAI.NotFoundError);
    await expect(model).rejects.toMatchObject({
      status: 404,
      code: 'model_not_found',
      message: expect.stringContaining(
        "The model 'this-model-does-not-exist' does not exist.",
      ),
      param: 'model',
      type: 'invalid_request_error',
    });
  }, 120_000);

  it('creates embeddings through the public OpenAI-compatible client', async () => {
    const client = createClient();

    const response = await client.embeddings.create({
      dimensions: 1024,
      input: ['Represent this document', 'Represent this query'],
      model: 'qwen3-embedding-4b',
    });

    expect(response.object).toBe('list');
    expect(response.data).toHaveLength(2);
    expect(response.data[0]?.embedding).toHaveLength(1024);
    expect(response.data[1]?.embedding).toHaveLength(1024);
  }, 120_000);

  it('transcribes audio through the public OpenAI-compatible client', async () => {
    const client = createClient();
    const file = await toFile(await testAudio(), 'test.mp3', {
      type: 'audio/mpeg',
    });

    const transcription = await client.audio.transcriptions.create({
      file,
      model: 'voxtral-mini-3b',
    });

    expect(transcription.text.length).toBeGreaterThan(0);
  }, 120_000);

  // One error per component that answers it.
  const chatCompletionErrors = {
    'the apigateway': {
      request: { model: 'this-model-does-not-exist' },
      errorClass: OpenAI.NotFoundError,
      details: {
        status: 404,
        message: expect.stringContaining(
          'model "this-model-does-not-exist" not found',
        ),
      },
    },
    'the inference-proxy': {
      request: {
        messages: [
          {
            role: 'user',
            content: [
              { type: 'text', text: 'Describe the image.' },
              {
                type: 'image_url',
                image_url: { url: 'https://example.com/sample.png' },
              },
            ],
          },
        ],
      },
      errorClass: OpenAI.BadRequestError,
      details: {
        status: 400,
        message: expect.stringContaining(
          'remote resource URLs are not allowed',
        ),
      },
    },
    vLLM: {
      request: { tool_choice: 'required' },
      errorClass: OpenAI.BadRequestError,
      details: {
        status: 400,
        message: expect.stringContaining('`tools` must be set'),
        param: 'tool_choice',
        type: 'BadRequestError',
      },
    },
  } satisfies Record<
    string,
    {
      request: Partial<OpenAI.ChatCompletionCreateParamsNonStreaming>;
      errorClass: unknown;
      details: object;
    }
  >;

  for (const [origin, { request, errorClass, details }] of Object.entries(
    chatCompletionErrors,
  )) {
    for (const stream of [false, true]) {
      it(`surfaces a ${stream ? 'streaming' : 'unary'} chat completion error from ${origin} as an OpenAI error`, async () => {
        const client = createClient();

        const completion = client.chat.completions.create({
          max_tokens: 16,
          messages: [{ role: 'user', content: 'Hello!' }],
          model: 'gpt-oss-120b',
          ...request,
          stream,
        });

        await expect(completion).rejects.toBeInstanceOf(errorClass);
        await expect(completion).rejects.toMatchObject(details);
      }, 120_000);
    }
  }

  it('surfaces an embeddings error from the apigateway as an OpenAI error', async () => {
    const client = createClient();

    const response = client.embeddings.create({
      input: 'Represent this document',
      model: 'this-model-does-not-exist',
    });

    await expect(response).rejects.toBeInstanceOf(OpenAI.NotFoundError);
    await expect(response).rejects.toMatchObject({
      status: 404,
      message: expect.stringContaining(
        'model "this-model-does-not-exist" not found',
      ),
    });
  }, 120_000);

  it('surfaces an embeddings error from vLLM as an OpenAI error', async () => {
    const client = createClient();

    const response = client.embeddings.create({
      dimensions: 999_999,
      input: 'Represent this document',
      model: 'qwen3-embedding-4b',
    });

    await expect(response).rejects.toBeInstanceOf(OpenAI.BadRequestError);
    await expect(response).rejects.toMatchObject({
      status: 400,
      message: expect.stringContaining('only supports dimensions'),
      type: 'BadRequestError',
    });
  }, 120_000);

  it('surfaces a transcription error from the apigateway as an OpenAI error', async () => {
    const client = createClient();
    const file = await toFile(await testAudio(), 'test.mp3', {
      type: 'audio/mpeg',
    });

    const transcription = client.audio.transcriptions.create({
      file,
      model: 'this-model-does-not-exist',
    });

    await expect(transcription).rejects.toBeInstanceOf(OpenAI.NotFoundError);
    await expect(transcription).rejects.toMatchObject({
      status: 404,
      message: expect.stringContaining(
        'model "this-model-does-not-exist" not found',
      ),
    });
  }, 120_000);

  it('surfaces a transcription error from vLLM as an OpenAI error', async () => {
    const client = createClient();
    const file = await toFile(await testAudio(), 'test.mp3', {
      type: 'audio/mpeg',
    });

    const transcription = client.audio.transcriptions.create({
      file,
      model: 'voxtral-mini-3b',
      temperature: 7,
    });

    await expect(transcription).rejects.toBeInstanceOf(OpenAI.BadRequestError);
    await expect(transcription).rejects.toMatchObject({
      status: 400,
      message: expect.stringContaining('temperature must be in'),
      param: 'temperature',
      type: 'BadRequestError',
    });
  }, 120_000);
});
