/// <reference types="vite/client" />
import { describe, expect, it, vi } from 'vitest';
import { PrivatemodeCore, type AuthProvider } from './privatemode-ai.js';
import * as wasm from './wasm.js';

/** Build an API-key {@link AuthProvider} for tests. */
const apiKeyAuth =
  (value: string): AuthProvider =>
  () => ({ type: 'apiKey', value });

const TEST_API_BASE_URL = 'https://api.privatemode.ai';
const RUN_INTEGRATION_TESTS = !!import.meta.env
  .PRIVATEMODE_RUN_INTEGRATION_TESTS;
const isNode = typeof process !== 'undefined' && !!process.versions?.node;
const browserWasmURL = isNode
  ? undefined
  : new URL('../../wasm/privatemode.wasm', import.meta.url).href;

describe('PrivatemodeCore', () => {
  describe('constructor', () => {
    it('throws if no authentication is provided', () => {
      expect(
        () => new PrivatemodeCore({ dangerouslyAllowBrowser: !isNode }),
      ).toThrow(/No authentication configured/);
    });

    it('uses the provided auth provider', () => {
      const client = new PrivatemodeCore({
        auth: apiKeyAuth('test-key'),
        dangerouslyAllowBrowser: !isNode,
      });
      expect(client).toBeInstanceOf(PrivatemodeCore);
    });

    it('uses the provided API key', () => {
      const client = new PrivatemodeCore({
        apiKey: 'test-key',
        dangerouslyAllowBrowser: !isNode,
      });
      expect(client).toBeInstanceOf(PrivatemodeCore);
    });

    it('treats an empty API key as missing authentication', () => {
      expect(
        () =>
          new PrivatemodeCore({ apiKey: '', dangerouslyAllowBrowser: !isNode }),
      ).toThrow(/No authentication configured/);
    });

    it('throws if both an API key and an auth provider are provided', () => {
      expect(
        () =>
          new PrivatemodeCore({
            apiKey: 'test-key',
            auth: apiKeyAuth('test-key'),
            dangerouslyAllowBrowser: !isNode,
          }),
      ).toThrow(/not both/);
    });

    it('uses custom baseURL', () => {
      const client = new PrivatemodeCore({
        auth: apiKeyAuth('test-key'),
        apiBaseURL: 'https://custom.example.com',
        dangerouslyAllowBrowser: !isNode,
      });
      expect(client).toBeInstanceOf(PrivatemodeCore);
    });
  });

  describe('verify', () => {
    it.skipIf(!RUN_INTEGRATION_TESTS)(
      'fetches manifest and verifies the Privatemode deployment',
      async () => {
        const client = new PrivatemodeCore({
          auth: apiKeyAuth(import.meta.env.PRIVATEMODE_API_KEY),
          apiBaseURL: TEST_API_BASE_URL,
          browserWasmURL,
          dangerouslyAllowBrowser: !isNode,
        });

        const result = await client.verify();

        expect(result.manifest).toBeDefined();
      },
      60_000,
    );
  });

  describe('updateSecret', () => {
    it.skipIf(!RUN_INTEGRATION_TESTS)(
      'refreshes the encryption secret',
      async () => {
        const client = new PrivatemodeCore({
          auth: apiKeyAuth(import.meta.env.PRIVATEMODE_API_KEY),
          apiBaseURL: TEST_API_BASE_URL,
          browserWasmURL,
          dangerouslyAllowBrowser: !isNode,
        });

        await client.verify();
        await expect(client.refreshSecret()).resolves.toBeUndefined();
      },
    );
  });

  describe('chatCompletions', () => {
    it.skipIf(!RUN_INTEGRATION_TESTS)(
      'sends an encrypted chat completions request and returns a decrypted response',
      async () => {
        const client = new PrivatemodeCore({
          auth: apiKeyAuth(import.meta.env.PRIVATEMODE_API_KEY),
          apiBaseURL: TEST_API_BASE_URL,
          browserWasmURL,
          dangerouslyAllowBrowser: !isNode,
        });

        await client.verify();
        await client.refreshSecret();

        const response = await client.chatCompletions({
          model: 'gpt-oss-120b',
          messages: [
            {
              role: 'user',
              content:
                'Reply with exactly one word: hello. Do not include any other text.',
            },
          ],
          max_tokens: 500,
        });
        const body = response.body as {
          choices: {
            message: {
              content: string | null;
            };
          }[];
        };
        expect(body.choices).toBeDefined();
        expect(body.choices.length).toBeGreaterThan(0);
        const msg = body.choices[0].message;
        const text = (msg.content ?? '').toLowerCase();
        expect(text).toContain('hello');
      },
      120_000,
    );
  });

  describe('streamChatCompletions', () => {
    it.skipIf(!RUN_INTEGRATION_TESTS)(
      'streams an encrypted chat completions request and returns decrypted chunks',
      async () => {
        const client = new PrivatemodeCore({
          auth: apiKeyAuth(import.meta.env.PRIVATEMODE_API_KEY),
          apiBaseURL: TEST_API_BASE_URL,
          browserWasmURL,
          dangerouslyAllowBrowser: !isNode,
        });

        await client.verify();
        await client.refreshSecret();

        const chunks: unknown[] = [];
        for await (const chunk of client.streamChatCompletions({
          model: 'gpt-oss-120b',
          stream: true,
          messages: [
            {
              role: 'user',
              content:
                'Reply with exactly one word: hello. Do not include any other text.',
            },
          ],
          max_tokens: 500,
        })) {
          chunks.push(chunk);
        }
        expect(chunks.length).toBeGreaterThan(0);

        type StreamChunk = {
          choices: {
            finish_reason: string | null;
            delta: { content?: string };
          }[];
        };

        const hasStop = chunks.some(
          (c) => (c as StreamChunk).choices?.[0]?.finish_reason === 'stop',
        );
        expect(hasStop).toBe(true);

        const content = chunks
          .map((c) => (c as StreamChunk).choices?.[0]?.delta?.content ?? '')
          .join('');
        expect(content.toLowerCase()).toContain('hello');
      },
      120_000,
    );

    it.skipIf(!RUN_INTEGRATION_TESTS)(
      'aborts a streaming request when the signal is triggered',
      async () => {
        const client = new PrivatemodeCore({
          auth: apiKeyAuth(import.meta.env.PRIVATEMODE_API_KEY),
          apiBaseURL: TEST_API_BASE_URL,
          browserWasmURL,
          dangerouslyAllowBrowser: !isNode,
        });

        await client.verify();
        await client.refreshSecret();

        const abortController = new AbortController();
        const chunks: unknown[] = [];

        // Abort after receiving the first chunk.
        try {
          for await (const chunk of client.streamChatCompletions(
            {
              model: 'gpt-oss-120b',
              stream: true,
              messages: [
                {
                  role: 'user',
                  content:
                    'Write a very long essay about the history of computing.',
                },
              ],
              max_tokens: 2000,
            },
            { signal: abortController.signal },
          )) {
            chunks.push(chunk);
            abortController.abort();
          }
          // If we reach here, the stream ended before the abort took effect.
          // That's acceptable but unlikely with max_tokens: 2000.
        } catch (error) {
          expect(error).toBeInstanceOf(DOMException);
          expect((error as DOMException).name).toBe('AbortError');
        }

        // We should have received at most a few chunks before aborting.
        expect(chunks.length).toBeGreaterThan(0);
        expect(chunks.length).toBeLessThan(100);
      },
      120_000,
    );
  });

  describe('streamTranscribeAudio', () => {
    it.skipIf(!RUN_INTEGRATION_TESTS || !isNode)(
      'streams an encrypted audio transcription from the live API',
      async () => {
        const { readFile } = await import('node:fs/promises');
        const { resolve } = await import('node:path');
        const audio = await readFile(
          resolve(import.meta.dirname, '../../../test/docs/test.mp3'),
        );

        const client = new PrivatemodeCore({
          auth: apiKeyAuth(import.meta.env.PRIVATEMODE_API_KEY),
          apiBaseURL: TEST_API_BASE_URL,
        });

        await client.verify();
        await client.refreshSecret();

        type TranscriptionChunk = {
          choices?: { delta?: { content?: string } }[];
          usage?: { total_tokens?: number };
        };
        const chunks: TranscriptionChunk[] = [];
        for await (const chunk of client.streamTranscribeAudio(
          {
            name: 'test.mp3',
            content: audio,
            contentType: 'audio/mpeg',
          },
          {
            model: 'whisper-large-v3',
            language: 'en',
            response_format: 'json',
          },
        )) {
          chunks.push(chunk as TranscriptionChunk);
        }

        expect(chunks.length).toBeGreaterThan(0);
        const transcript = chunks
          .flatMap((chunk) => chunk.choices ?? [])
          .map((choice) => choice.delta?.content ?? '')
          .join('');
        expect(transcript.length).toBeGreaterThan(0);
      },
      120_000,
    );
  });

  describe('offline initialization with imported secret', () => {
    it.skipIf(!RUN_INTEGRATION_TESTS)(
      'exports a secret from a verified client and imports it into an offline-initialized client',
      async () => {
        // Export a secret from client a)
        const verifiedClient = new PrivatemodeCore({
          auth: apiKeyAuth(import.meta.env.PRIVATEMODE_API_KEY),
          apiBaseURL: TEST_API_BASE_URL,
          browserWasmURL,
          dangerouslyAllowBrowser: !isNode,
        });

        const { manifest } = await verifiedClient.verify();
        await verifiedClient.refreshSecret();
        const exportedSecret = verifiedClient.exportSecret();

        expect(exportedSecret.id).toBeDefined();
        expect(exportedSecret.data).toBeDefined();
        expect(exportedSecret.expiresAtUnix).toBeGreaterThan(0);

        // Import that secret into client b), without exchanging
        // a secret with the deployment again.
        const offlineClient = new PrivatemodeCore({
          auth: apiKeyAuth(import.meta.env.PRIVATEMODE_API_KEY),
          apiBaseURL: TEST_API_BASE_URL,
          browserWasmURL,
          dangerouslyAllowBrowser: !isNode,
        });

        const manifestBytes = new TextEncoder().encode(
          JSON.stringify(manifest),
        );
        await offlineClient.initializeOffline(manifestBytes);
        offlineClient.importSecret(exportedSecret);

        // Check that inference works with client b)
        const response = await offlineClient.chatCompletions({
          model: 'gpt-oss-120b',
          messages: [
            {
              role: 'user',
              content:
                'Reply with exactly one word: hello. Do not include any other text.',
            },
          ],
          max_tokens: 500,
        });
        const body = response.body as {
          choices: {
            message: {
              content: string | null;
            };
          }[];
        };
        expect(body.choices).toBeDefined();
        expect(body.choices.length).toBeGreaterThan(0);
        const text = (body.choices[0].message.content ?? '').toLowerCase();
        expect(text).toContain('hello');
      },
      120_000,
    );
  });

  describe('unstructured', () => {
    it.skipIf(!RUN_INTEGRATION_TESTS)(
      'sends an encrypted unstructured request and returns a decrypted response',
      async () => {
        const client = new PrivatemodeCore({
          auth: apiKeyAuth(import.meta.env.PRIVATEMODE_API_KEY),
          apiBaseURL: TEST_API_BASE_URL,
          browserWasmURL,
          dangerouslyAllowBrowser: !isNode,
        });

        await client.verify();
        await client.refreshSecret();

        const fileContent = 'Hello from Privatemode SDK test';
        const response = (await client.unstructured(
          [
            {
              name: 'test.txt',
              content: new TextEncoder().encode(fileContent),
              contentType: 'text/plain',
            },
          ],
          { strategy: 'fast' },
        )) as { text: string }[];

        expect(response).toBeDefined();
        expect(response.length).toBeGreaterThan(0);
        expect(response[0].text).toBe(fileContent);
      },
      120_000,
    );
  });

  /**
   * Helper function to mock the per-instance Wasm client. Returns the mock
   * client so tests can customize methods, and a restore function for the
   * module-level spies.
   */
  function mockWasmFunctions() {
    const client = {
      initialize: vi.fn().mockResolvedValue(undefined),
      initializeOffline: vi.fn(),
      setAuth: vi.fn(),
      updateSecret: vi.fn().mockResolvedValue(undefined),
      fetchManifest: vi.fn().mockResolvedValue(
        JSON.stringify({
          SeedShareHash: 'test-hash',
          Policies: {},
        }),
      ),
      chatCompletions: vi.fn(),
      embeddings: vi.fn(),
      streamChatCompletions: vi.fn(),
      unstructured: vi.fn(),
      transcribeAudio: vi.fn(),
      streamTranscribeAudio: vi.fn(),
      listModels: vi.fn(),
      exportSecret: vi.fn(),
      importSecret: vi.fn(),
      close: vi.fn(),
    };
    const mockCreateWasmClient = vi
      .spyOn(wasm, 'createWasmClient')
      .mockReturnValue(client as unknown as wasm.WasmClient);
    const mockErrManifestMismatch = vi
      .spyOn(wasm, 'errManifestMismatch')
      .mockReturnValue('manifest mismatch error');

    return {
      client,
      mockUpdateSecret: client.updateSecret,
      restoreAll: () => {
        mockCreateWasmClient.mockRestore();
        mockErrManifestMismatch.mockRestore();
      },
    };
  }

  describe('automatic secret refresh retry logic', () => {
    it.skipIf(!isNode)(
      'retries chatCompletions after refreshing expired secret',
      async () => {
        const client = new PrivatemodeCore({
          auth: apiKeyAuth('test-key'),
        });

        const wasmMocks = mockWasmFunctions();

        let callCount = 0;
        wasmMocks.client.chatCompletions.mockImplementation(async () => {
          callCount++;
          if (callCount === 1) {
            throw new Error(wasm.errNoSecretForID() + ' xyz');
          }
          return {
            body: JSON.stringify({
              choices: [{ message: { content: 'Success' } }],
            }),
            headers: new Headers({ 'x-request-id': 'req_chat' }),
          };
        });

        await client.verify();

        const response = await client.chatCompletions({
          model: 'test-model',
          messages: [{ role: 'user', content: 'test' }],
        });

        expect(callCount).toBe(2);
        expect(wasmMocks.mockUpdateSecret).toHaveBeenCalledTimes(1);
        expect(response.body).toEqual({
          choices: [{ message: { content: 'Success' } }],
        });
        expect(response.headers.get('x-request-id')).toBe('req_chat');

        wasmMocks.restoreAll();
      },
    );

    it.skipIf(!isNode)(
      'retries embeddings after refreshing expired secret',
      async () => {
        const client = new PrivatemodeCore({ auth: apiKeyAuth('test-key') });
        const wasmMocks = mockWasmFunctions();
        let callCount = 0;
        wasmMocks.client.embeddings.mockImplementation(async () => {
          callCount++;
          if (callCount === 1) {
            throw new Error(wasm.errNoSecretForID() + ' xyz');
          }
          return {
            body: JSON.stringify({
              data: [{ embedding: [0.1, 0.2], index: 0, object: 'embedding' }],
              object: 'list',
            }),
            headers: new Headers({ 'x-request-id': 'req_embeddings' }),
          };
        });

        try {
          await client.verify();
          const response = await client.embeddings({
            input: ['hello', 'world'],
            model: 'qwen3-embedding-4b',
          });

          expect(callCount).toBe(2);
          expect(wasmMocks.mockUpdateSecret).toHaveBeenCalledTimes(1);
          expect(response.body).toMatchObject({ object: 'list' });
          expect(response.headers.get('x-request-id')).toBe('req_embeddings');
        } finally {
          wasmMocks.restoreAll();
        }
      },
    );

    it.skipIf(!isNode)(
      'retries streamChatCompletions after refreshing expired secret',
      async () => {
        const client = new PrivatemodeCore({
          auth: apiKeyAuth('test-key'),
        });

        const wasmMocks = mockWasmFunctions();

        let callCount = 0;
        wasmMocks.client.streamChatCompletions.mockImplementation(
          async (
            _body: string,
            onChunk: (chunk: string) => Promise<void> | void,
          ) => {
            callCount++;
            if (callCount === 1) {
              throw new Error(wasm.errNoSecretForID() + ' xyz');
            }
            // Simulate successful streaming
            await onChunk(
              JSON.stringify({
                choices: [{ delta: { content: 'Hello' } }],
              }),
            );
            await onChunk(
              JSON.stringify({
                choices: [{ finish_reason: 'stop', delta: {} }],
              }),
            );
          },
        );

        await client.verify();

        const chunks: unknown[] = [];
        for await (const chunk of client.streamChatCompletions({
          model: 'test-model',
          messages: [{ role: 'user', content: 'test' }],
          stream: true,
        })) {
          chunks.push(chunk);
        }

        expect(callCount).toBe(2);
        expect(wasmMocks.mockUpdateSecret).toHaveBeenCalledTimes(1);
        expect(chunks.length).toBeGreaterThan(0);

        wasmMocks.restoreAll();
      },
    );

    it.skipIf(!isNode)(
      'applies backpressure while streaming chat completions',
      async () => {
        const client = new PrivatemodeCore({ auth: apiKeyAuth('test-key') });
        const wasmMocks = mockWasmFunctions();
        let produced = 0;
        wasmMocks.client.streamChatCompletions.mockImplementation(
          async (
            _body: string,
            onChunk: (chunk: string) => Promise<void> | void,
          ) => {
            produced++;
            await onChunk(JSON.stringify({ id: 'first', choices: [] }));
            produced++;
            await onChunk(JSON.stringify({ id: 'second', choices: [] }));
            produced++;
          },
        );

        try {
          await client.verify();
          const iterator = client.streamChatCompletions({
            model: 'test-model',
            messages: [{ role: 'user', content: 'test' }],
            stream: true,
          });

          await expect(iterator.next()).resolves.toMatchObject({
            value: { id: 'first' },
            done: false,
          });
          expect(produced).toBe(1);

          await expect(iterator.next()).resolves.toMatchObject({
            value: { id: 'second' },
            done: false,
          });
          expect(produced).toBe(2);

          await expect(iterator.next()).resolves.toEqual({
            value: undefined,
            done: true,
          });
          expect(produced).toBe(3);
        } finally {
          wasmMocks.restoreAll();
        }
      },
    );

    it.skipIf(!isNode)(
      'unblocks a backpressured producer when the stream is aborted',
      async () => {
        const client = new PrivatemodeCore({ auth: apiKeyAuth('test-key') });
        const wasmMocks = mockWasmFunctions();
        let producerRejected = false;
        wasmMocks.client.streamChatCompletions.mockImplementation(
          async (
            _body: string,
            onChunk: (chunk: string) => Promise<void> | void,
          ) => {
            try {
              await onChunk(JSON.stringify({ id: 'first', choices: [] }));
            } catch (error) {
              producerRejected = true;
              throw error;
            }
          },
        );

        try {
          await client.verify();
          const controller = new AbortController();
          const iterator = client.streamChatCompletions(
            {
              model: 'test-model',
              messages: [{ role: 'user', content: 'test' }],
              stream: true,
            },
            { signal: controller.signal },
          );

          await expect(iterator.next()).resolves.toMatchObject({
            value: { id: 'first' },
            done: false,
          });
          controller.abort();
          await expect(iterator.next()).rejects.toMatchObject({
            name: 'AbortError',
          });
          await vi.waitFor(() => expect(producerRejected).toBe(true));
        } finally {
          wasmMocks.restoreAll();
        }
      },
    );

    it.skipIf(!isNode)(
      'rejects the producer when the consumer closes the stream early',
      async () => {
        const client = new PrivatemodeCore({ auth: apiKeyAuth('test-key') });
        const wasmMocks = mockWasmFunctions();
        let producerError: Error | null = null;
        wasmMocks.client.streamChatCompletions.mockImplementation(
          async (
            _body: string,
            onChunk: (chunk: string) => Promise<void> | void,
          ) => {
            try {
              await onChunk(JSON.stringify({ id: 'first', choices: [] }));
              await onChunk(JSON.stringify({ id: 'second', choices: [] }));
            } catch (error) {
              producerError = error as Error;
              throw error;
            }
          },
        );

        try {
          await client.verify();
          const iterator = client.streamChatCompletions({
            model: 'test-model',
            messages: [{ role: 'user', content: 'test' }],
            stream: true,
          });

          await expect(iterator.next()).resolves.toMatchObject({
            value: { id: 'first' },
            done: false,
          });
          await iterator.return(undefined);
          await vi.waitFor(() => expect(producerError).not.toBeNull());
          expect(producerError!.message).toMatch(/consumer closed/i);
        } finally {
          wasmMocks.restoreAll();
        }
      },
    );

    it.skipIf(!isNode)(
      'retries unstructured after refreshing expired secret',
      async () => {
        const client = new PrivatemodeCore({
          auth: apiKeyAuth('test-key'),
        });

        const wasmMocks = mockWasmFunctions();

        let callCount = 0;
        wasmMocks.client.unstructured.mockImplementation(async () => {
          callCount++;
          if (callCount === 1) {
            throw new Error(wasm.errNoSecretForID() + ' xyz');
          }
          return JSON.stringify([{ text: 'Success' }]);
        });

        await client.verify();

        const response = await client.unstructured([
          {
            name: 'test.txt',
            content: new Uint8Array([1, 2, 3]),
          },
        ]);

        expect(callCount).toBe(2);
        expect(wasmMocks.mockUpdateSecret).toHaveBeenCalledTimes(1);
        expect(response).toEqual([{ text: 'Success' }]);

        wasmMocks.restoreAll();
      },
    );

    it.skipIf(!isNode)(
      'retries transcribeAudio after refreshing expired secret',
      async () => {
        const client = new PrivatemodeCore({
          auth: apiKeyAuth('test-key'),
        });

        const wasmMocks = mockWasmFunctions();

        let callCount = 0;
        wasmMocks.client.transcribeAudio.mockImplementation(async () => {
          callCount++;
          if (callCount === 1) {
            throw new Error(wasm.errNoSecretForID() + ' xyz');
          }
          return {
            body: JSON.stringify({ text: 'Success' }),
            headers: new Headers({ 'x-request-id': 'req_audio' }),
          };
        });

        try {
          await client.verify();

          const response = await client.transcribeAudio(
            {
              name: 'test.wav',
              content: new Uint8Array([1, 2, 3]),
              contentType: 'audio/wav',
            },
            { model: 'voxtral-mini-3b' },
          );

          expect(callCount).toBe(2);
          expect(wasmMocks.mockUpdateSecret).toHaveBeenCalledTimes(1);
          expect(response.body).toEqual({ text: 'Success' });
          expect(response.headers.get('x-request-id')).toBe('req_audio');
        } finally {
          wasmMocks.restoreAll();
        }
      },
    );

    it.skipIf(!isNode)(
      'retries streamTranscribeAudio after refreshing expired secret',
      async () => {
        const client = new PrivatemodeCore({
          auth: apiKeyAuth('test-key'),
        });

        const wasmMocks = mockWasmFunctions();
        let callCount = 0;
        wasmMocks.client.streamTranscribeAudio.mockImplementation(
          async (file, options, onChunk) => {
            callCount++;
            expect(file.name).toBe('test.wav');
            expect(JSON.parse(options)).toEqual({
              model: 'whisper-large-v3',
              language: 'en',
            });
            if (callCount === 1) {
              throw new Error(wasm.errNoSecretForID() + ' xyz');
            }
            await onChunk(
              JSON.stringify({ type: 'transcript.text.delta', delta: 'Hello' }),
            );
            await onChunk(
              JSON.stringify({ type: 'transcript.text.done', text: 'Hello' }),
            );
          },
        );

        try {
          await client.verify();
          const events: unknown[] = [];
          for await (const event of client.streamTranscribeAudio(
            {
              name: 'test.wav',
              content: new Uint8Array([1, 2, 3]),
              contentType: 'audio/wav',
            },
            { model: 'whisper-large-v3', language: 'en' },
          )) {
            events.push(event);
          }

          expect(callCount).toBe(2);
          expect(wasmMocks.mockUpdateSecret).toHaveBeenCalledTimes(1);
          expect(events).toEqual([
            { type: 'transcript.text.delta', delta: 'Hello' },
            { type: 'transcript.text.done', text: 'Hello' },
          ]);
        } finally {
          wasmMocks.restoreAll();
        }
      },
    );

    it.skipIf(!isNode)('does not retry on non-secret errors', async () => {
      const client = new PrivatemodeCore({
        auth: apiKeyAuth('test-key'),
      });

      const wasmMocks = mockWasmFunctions();

      let callCount = 0;
      wasmMocks.client.chatCompletions.mockImplementation(async () => {
        callCount++;
        throw new Error('Some other error');
      });

      await client.verify();

      await expect(
        client.chatCompletions({
          model: 'test-model',
          messages: [{ role: 'user', content: 'test' }],
        }),
      ).rejects.toThrow('Some other error');

      expect(callCount).toBe(1);
      expect(wasmMocks.mockUpdateSecret).not.toHaveBeenCalled();

      wasmMocks.restoreAll();
    });

    it.skipIf(!isNode)('throws error if refreshSecret fails', async () => {
      const client = new PrivatemodeCore({
        auth: apiKeyAuth('test-key'),
      });

      const wasmMocks = mockWasmFunctions();
      wasmMocks.mockUpdateSecret.mockRejectedValue(
        new Error('Failed to refresh secret'),
      );

      let callCount = 0;
      wasmMocks.client.chatCompletions.mockImplementation(async () => {
        callCount++;
        throw new Error(wasm.errNoSecretForID() + ' xyz');
      });

      await client.verify();

      await expect(
        client.chatCompletions({
          model: 'test-model',
          messages: [{ role: 'user', content: 'test' }],
        }),
      ).rejects.toThrow('Failed to refresh secret');

      expect(callCount).toBe(1);
      expect(wasmMocks.mockUpdateSecret).toHaveBeenCalledTimes(1);

      wasmMocks.restoreAll();
    });

    it.skipIf(!isNode)(
      'only retries once on repeated secret errors',
      async () => {
        const client = new PrivatemodeCore({
          auth: apiKeyAuth('test-key'),
        });

        const wasmMocks = mockWasmFunctions();

        let callCount = 0;
        wasmMocks.client.chatCompletions.mockImplementation(async () => {
          callCount++;
          // Always throw secret error
          throw new Error(wasm.errNoSecretForID() + ' xyz');
        });

        await client.verify();

        await expect(
          client.chatCompletions({
            model: 'test-model',
            messages: [{ role: 'user', content: 'test' }],
          }),
        ).rejects.toThrow(wasm.errNoSecretForID() + ' xyz');

        // Should be called twice: once initially, once after refresh
        expect(callCount).toBe(2);
        expect(wasmMocks.mockUpdateSecret).toHaveBeenCalledTimes(1);

        wasmMocks.restoreAll();
      },
    );

    it.skipIf(!isNode)(
      'retries verify() with new manifest on manifest mismatch',
      async () => {
        const client = new PrivatemodeCore({
          auth: apiKeyAuth('test-key'),
        });

        const originalManifest = '{"content":"original-mnf"}';
        const newManifest = '{"content":"new-mnf"}';

        const mockWasm = mockWasmFunctions();

        let initializeCallCount = 0;
        mockWasm.client.initialize.mockImplementation(async () => {
          initializeCallCount++;
          if (initializeCallCount === 1) {
            throw new Error(wasm.errManifestMismatch());
          }
          // Second call succeeds
        });

        let fetchManifestCallCount = 0;
        mockWasm.client.fetchManifest.mockImplementation(async () => {
          fetchManifestCallCount++;
          if (fetchManifestCallCount === 1) {
            return originalManifest;
          }
          return newManifest;
        });

        await client.verify();

        // Should have called initialize twice (original fails, retry succeeds)
        expect(initializeCallCount).toBe(2);
        // Should have fetched manifest twice (initial + retry)
        expect(fetchManifestCallCount).toBe(2);
        // Manifest should be updated to the new one
        expect(new TextDecoder().decode(client.manifestBytes!)).toBe(
          newManifest,
        );

        mockWasm.restoreAll();
      },
    );
  });

  describe('close', () => {
    it.skipIf(!isNode)('prevents use after close', async () => {
      const client = new PrivatemodeCore({ auth: apiKeyAuth('test-key') });
      const wasmMocks = mockWasmFunctions();

      try {
        await client.verify();
        client.close();
        client.close();
        expect(wasmMocks.client.close).toHaveBeenCalledTimes(1);

        await expect(client.verify()).rejects.toThrow(/closed/);
        await expect(
          client.chatCompletions({
            model: 'test-model',
            messages: [{ role: 'user', content: 'test' }],
          }),
        ).rejects.toThrow(/closed/);
        expect(() => client.exportSecret()).toThrow(/closed/);
      } finally {
        wasmMocks.restoreAll();
      }
    });
  });
});
