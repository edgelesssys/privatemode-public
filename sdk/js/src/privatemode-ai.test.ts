/// <reference types="vite/client" />
import { describe, it, expect, beforeAll, afterAll, vi } from 'vitest';
import { PrivatemodeAI } from './privatemode-ai.js';
import * as wasm from './wasm.js';

const TEST_API_BASE_URL = 'https://api.privatemode.ai';
const RUN_INTEGRATION_TESTS = !!import.meta.env
  .PRIVATEMODE_RUN_INTEGRATION_TESTS;
const isNode = typeof process !== 'undefined' && !!process.versions?.node;
const browserWasmURL = isNode
  ? undefined
  : new URL('../../wasm/privatemode.wasm', import.meta.url).href;

describe('PrivatemodeAI', () => {
  let prevValue: string | undefined;

  beforeAll(() => {
    if (RUN_INTEGRATION_TESTS && isNode) {
      prevValue = process.env.NODE_TLS_REJECT_UNAUTHORIZED;
      process.env.NODE_TLS_REJECT_UNAUTHORIZED = '0';
    }
  });

  afterAll(() => {
    if (RUN_INTEGRATION_TESTS && isNode) {
      if (prevValue !== undefined) {
        process.env.NODE_TLS_REJECT_UNAUTHORIZED = prevValue;
      } else {
        delete process.env.NODE_TLS_REJECT_UNAUTHORIZED;
      }
    }
  });

  describe('constructor', () => {
    it('throws if no API key is provided', () => {
      if (isNode) {
        const originalEnv = process.env.PRIVATEMODE_API_KEY;
        delete process.env.PRIVATEMODE_API_KEY;
        try {
          expect(() => new PrivatemodeAI()).toThrow(
            /PRIVATEMODE_API_KEY environment variable is missing/,
          );
        } finally {
          if (originalEnv !== undefined) {
            process.env.PRIVATEMODE_API_KEY = originalEnv;
          }
        }
      } else {
        expect(
          () => new PrivatemodeAI({ dangerouslyAllowBrowser: true }),
        ).toThrow(/PRIVATEMODE_API_KEY environment variable is missing/);
      }
    });

    it('uses API key from options', () => {
      const client = new PrivatemodeAI({
        apiKey: 'test-key',
        dangerouslyAllowBrowser: !isNode,
      });
      expect(client).toBeInstanceOf(PrivatemodeAI);
    });

    it('uses custom baseURL', () => {
      const client = new PrivatemodeAI({
        apiKey: 'test-key',
        apiBaseURL: 'https://custom.example.com',
        dangerouslyAllowBrowser: !isNode,
      });
      expect(client).toBeInstanceOf(PrivatemodeAI);
    });
  });

  describe('verify', () => {
    it.skipIf(!RUN_INTEGRATION_TESTS)(
      'fetches manifest and verifies the Privatemode deployment',
      async () => {
        const client = new PrivatemodeAI({
          apiKey: import.meta.env.PRIVATEMODE_API_KEY,
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
        const client = new PrivatemodeAI({
          apiKey: import.meta.env.PRIVATEMODE_API_KEY,
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
        const client = new PrivatemodeAI({
          apiKey: import.meta.env.PRIVATEMODE_API_KEY,
          apiBaseURL: TEST_API_BASE_URL,
          browserWasmURL,
          dangerouslyAllowBrowser: !isNode,
        });

        await client.verify();
        await client.refreshSecret();

        const response = (await client.chatCompletions({
          model: 'gpt-oss-120b',
          messages: [
            {
              role: 'user',
              content:
                'Reply with exactly one word: hello. Do not include any other text.',
            },
          ],
          max_tokens: 500,
        })) as {
          choices: {
            message: {
              content: string | null;
            };
          }[];
        };
        expect(response.choices).toBeDefined();
        expect(response.choices.length).toBeGreaterThan(0);
        const msg = response.choices[0].message;
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
        const client = new PrivatemodeAI({
          apiKey: import.meta.env.PRIVATEMODE_API_KEY,
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
        const client = new PrivatemodeAI({
          apiKey: import.meta.env.PRIVATEMODE_API_KEY,
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

  describe('offline initialization with imported secret', () => {
    it.skipIf(!RUN_INTEGRATION_TESTS)(
      'exports a secret from a verified client and imports it into an offline-initialized client',
      async () => {
        // Export a secret from client a)
        const verifiedClient = new PrivatemodeAI({
          apiKey: import.meta.env.PRIVATEMODE_API_KEY,
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
        const offlineClient = new PrivatemodeAI({
          apiKey: import.meta.env.PRIVATEMODE_API_KEY,
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
        const response = (await offlineClient.chatCompletions({
          model: 'gpt-oss-120b',
          messages: [
            {
              role: 'user',
              content:
                'Reply with exactly one word: hello. Do not include any other text.',
            },
          ],
          max_tokens: 500,
        })) as {
          choices: {
            message: {
              content: string | null;
            };
          }[];
        };
        expect(response.choices).toBeDefined();
        expect(response.choices.length).toBeGreaterThan(0);
        const text = (response.choices[0].message.content ?? '').toLowerCase();
        expect(text).toContain('hello');
      },
      120_000,
    );
  });

  describe('unstructured', () => {
    it.skipIf(!RUN_INTEGRATION_TESTS)(
      'sends an encrypted unstructured request and returns a decrypted response',
      async () => {
        const client = new PrivatemodeAI({
          apiKey: import.meta.env.PRIVATEMODE_API_KEY,
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

  describe('automatic secret refresh retry logic', () => {
    /**
     * Helper function to mock common WASM functions used in all retry logic tests.
     * Returns the mocks so tests can customize or restore them.
     */
    function mockWasmFunctions() {
      const mockInitialize = vi.spyOn(wasm, 'initialize').mockResolvedValue();
      const mockUpdateSecret = vi
        .spyOn(wasm, 'updateSecret')
        .mockResolvedValue();
      const mockFetchManifest = vi
        .spyOn(wasm, 'fetchManifest')
        .mockResolvedValue(
          JSON.stringify({
            SeedShareHash: 'test-hash',
            Policies: {},
          }),
        );
      const mockErrManifestMismatch = vi
        .spyOn(wasm, 'errManifestMismatch')
        .mockReturnValue('manifest mismatch error');

      return {
        mockInitialize,
        mockUpdateSecret,
        mockFetchManifest,
        mockErrManifestMismatch,
        restoreAll: () => {
          mockInitialize.mockRestore();
          mockUpdateSecret.mockRestore();
          mockFetchManifest.mockRestore();
          mockErrManifestMismatch.mockRestore();
        },
      };
    }

    it.skipIf(!isNode)(
      'retries chatCompletions after refreshing expired secret',
      async () => {
        const client = new PrivatemodeAI({
          apiKey: 'test-key',
        });

        const wasmMocks = mockWasmFunctions();

        let callCount = 0;
        const mockChatCompletions = vi
          .spyOn(wasm, 'chatCompletions')
          .mockImplementation(async () => {
            callCount++;
            if (callCount === 1) {
              throw new Error(wasm.errNoSecretForID() + ' xyz');
            }
            return JSON.stringify({
              choices: [{ message: { content: 'Success' } }],
            });
          });

        await client.verify();

        const response = await client.chatCompletions({
          model: 'test-model',
          messages: [{ role: 'user', content: 'test' }],
        });

        expect(callCount).toBe(2);
        expect(wasmMocks.mockUpdateSecret).toHaveBeenCalledTimes(1);
        expect(response).toEqual({
          choices: [{ message: { content: 'Success' } }],
        });

        mockChatCompletions.mockRestore();
        wasmMocks.restoreAll();
      },
    );

    it.skipIf(!isNode)(
      'retries streamChatCompletions after refreshing expired secret',
      async () => {
        const client = new PrivatemodeAI({
          apiKey: 'test-key',
        });

        const wasmMocks = mockWasmFunctions();

        let callCount = 0;
        const mockStreamCompletions = vi
          .spyOn(wasm, 'streamChatCompletions')
          .mockImplementation(async (_body, onChunk) => {
            callCount++;
            if (callCount === 1) {
              throw new Error(wasm.errNoSecretForID() + ' xyz');
            }
            // Simulate successful streaming
            onChunk(
              JSON.stringify({
                choices: [{ delta: { content: 'Hello' } }],
              }),
            );
            onChunk(
              JSON.stringify({
                choices: [{ finish_reason: 'stop', delta: {} }],
              }),
            );
          });

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

        mockStreamCompletions.mockRestore();
        wasmMocks.restoreAll();
      },
    );

    it.skipIf(!isNode)(
      'retries unstructured after refreshing expired secret',
      async () => {
        const client = new PrivatemodeAI({
          apiKey: 'test-key',
        });

        const wasmMocks = mockWasmFunctions();

        let callCount = 0;
        const mockUnstructured = vi
          .spyOn(wasm, 'unstructured')
          .mockImplementation(async () => {
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

        mockUnstructured.mockRestore();
        wasmMocks.restoreAll();
      },
    );

    it.skipIf(!isNode)(
      'retries transcribeAudio after refreshing expired secret',
      async () => {
        const client = new PrivatemodeAI({
          apiKey: 'test-key',
        });

        const wasmMocks = mockWasmFunctions();

        let callCount = 0;
        const mockTranscribeAudio = vi
          .spyOn(wasm, 'transcribeAudio')
          .mockImplementation(async () => {
            callCount++;
            if (callCount === 1) {
              throw new Error(wasm.errNoSecretForID() + ' xyz');
            }
            return JSON.stringify({ text: 'Success' });
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
          expect(response).toEqual({ text: 'Success' });
        } finally {
          mockTranscribeAudio.mockRestore();
          wasmMocks.restoreAll();
        }
      },
    );

    it.skipIf(!isNode)('does not retry on non-secret errors', async () => {
      const client = new PrivatemodeAI({
        apiKey: 'test-key',
      });

      const wasmMocks = mockWasmFunctions();

      let callCount = 0;
      const mockChatCompletions = vi
        .spyOn(wasm, 'chatCompletions')
        .mockImplementation(async () => {
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

      mockChatCompletions.mockRestore();
      wasmMocks.restoreAll();
    });

    it.skipIf(!isNode)('throws error if refreshSecret fails', async () => {
      const client = new PrivatemodeAI({
        apiKey: 'test-key',
      });

      const wasmMocks = mockWasmFunctions();
      wasmMocks.mockUpdateSecret.mockRejectedValue(
        new Error('Failed to refresh secret'),
      );

      let callCount = 0;
      const mockChatCompletions = vi
        .spyOn(wasm, 'chatCompletions')
        .mockImplementation(async () => {
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

      mockChatCompletions.mockRestore();
      wasmMocks.restoreAll();
    });

    it.skipIf(!isNode)(
      'only retries once on repeated secret errors',
      async () => {
        const client = new PrivatemodeAI({
          apiKey: 'test-key',
        });

        const wasmMocks = mockWasmFunctions();

        let callCount = 0;
        const mockChatCompletions = vi
          .spyOn(wasm, 'chatCompletions')
          .mockImplementation(async () => {
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

        mockChatCompletions.mockRestore();
        wasmMocks.restoreAll();
      },
    );

    it.skipIf(!isNode)(
      'retries verify() with new manifest on manifest mismatch',
      async () => {
        const client = new PrivatemodeAI({
          apiKey: 'test-key',
        });

        const originalManifest = '{"content":"original-mnf"}';
        const newManifest = '{"content":"new-mnf"}';

        const mockWasm = mockWasmFunctions();

        let initializeCallCount = 0;
        mockWasm.mockInitialize = vi
          .spyOn(wasm, 'initialize')
          .mockImplementation(async () => {
            initializeCallCount++;
            if (initializeCallCount === 1) {
              throw new Error(wasm.errManifestMismatch());
            }
            // Second call succeeds
          });

        let fetchManifestCallCount = 0;
        mockWasm.mockFetchManifest = vi
          .spyOn(wasm, 'fetchManifest')
          .mockImplementation(async () => {
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
});
