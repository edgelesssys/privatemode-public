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

  it('lists the available models', async () => {
    const client = createClient();

    const models = await client.models.list();

    expect(models.data.length).toBeGreaterThan(0);
    expect(models.data.map((model) => model.id)).toContain('gpt-oss-120b');
  }, 120_000);

  it('retrieves a single model', async () => {
    const client = createClient();

    const model = await client.models.retrieve('gpt-oss-120b');

    expect(model.id).toBe('gpt-oss-120b');
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

  it('maps API errors to the official OpenAI error classes', async () => {
    const client = createClient();

    const completion = client.chat.completions.create({
      max_tokens: 16,
      messages: [{ role: 'user', content: 'Hello!' }],
      model: 'this-model-does-not-exist',
    });

    await expect(completion).rejects.toBeInstanceOf(OpenAI.NotFoundError);
  }, 120_000);
});
