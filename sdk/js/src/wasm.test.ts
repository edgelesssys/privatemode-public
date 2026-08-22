import { describe, it, expect, beforeAll, vi } from 'vitest';
import {
  createWasmClient,
  initWasm,
  privatemodeVersion,
  WasmClient,
} from './wasm.js';
import { PrivatemodeAPIError } from './errors.js';

const isNode = typeof process !== 'undefined' && !!process.versions?.node;
const manifest = btoa('{}');

const originalFetch = globalThis.fetch;
const fetchMock = vi.fn();
globalThis.fetch = fetchMock;

type WasmClientExports = ConstructorParameters<typeof WasmClient>[0];

async function loadWasm(): Promise<BufferSource | Response> {
  if (isNode) {
    const { readFile } = await import('node:fs/promises');
    const { resolve } = await import('node:path');
    const wasmPath = resolve(
      import.meta.dirname,
      '../../wasm/privatemode.wasm',
    );
    return readFile(wasmPath);
  }
  return originalFetch(new URL('../../wasm/privatemode.wasm', import.meta.url));
}

async function initWasmAsStandaloneNode(
  wasmSource: BufferSource | Response,
): Promise<void> {
  if (!isNode) {
    await initWasm(wasmSource);
    return;
  }

  // Vitest's Nix-provided worker has an absolute argv0, while `node app.js`
  // exposes "node" and triggers Go's Node-specific HTTP transport selection.
  const originalProcess = globalThis.process;
  const standaloneNodeProcess = Object.create(originalProcess, {
    argv0: { value: 'node' },
  }) as NodeJS.Process;
  globalThis.process = standaloneNodeProcess;
  try {
    await initWasm(wasmSource);
    expect(globalThis.process).toBe(standaloneNodeProcess);
  } finally {
    globalThis.process = originalProcess;
  }
}

describe('initWasm', () => {
  it('rejects when expectedWasmHash does not match', async () => {
    const fakeWasm = new Uint8Array([0, 1, 2, 3]);
    await expect(
      initWasm(
        fakeWasm,
        '0000000000000000000000000000000000000000000000000000000000000000',
      ),
    ).rejects.toThrow(/WASM integrity check failed/);
  });
});

describe('wasm', () => {
  let client: WasmClient;

  beforeAll(async () => {
    fetchMock.mockImplementation(() =>
      Promise.resolve(new Response('', { status: 200 })),
    );
    const wasmSource = await loadWasm();
    await initWasmAsStandaloneNode(wasmSource);
    client = createWasmClient();
  });

  describe('privatemodeVersion', () => {
    it('returns a semver', () => {
      expect(privatemodeVersion()).toMatch(/^v\d+\.\d+\.\d+(-.+)?$/);
    });
  });

  describe('initialize', () => {
    it('fails with illegal base64 data', async () => {
      const invalidBase64 = '!!!';
      await expect(
        client.initialize(
          invalidBase64,
          'apiKey',
          'test-key',
          'https://example.com',
          true,
        ),
      ).rejects.toThrow('illegal base64 data at input byte 0');
    });

    it('fails with unauthorized API key', async () => {
      fetchMock.mockImplementation(() =>
        Promise.resolve(new Response('Unauthorized', { status: 401 })),
      );
      await expect(
        client.initialize(
          manifest,
          'apiKey',
          'bad-key',
          'https://example.com',
          true,
        ),
      ).rejects.toThrow('Unauthorized');
    });
  });

  describe('client isolation', () => {
    it('keeps secrets scoped to their client handle', () => {
      const first = createWasmClient();
      const second = createWasmClient();
      const expiresAt = Math.floor(Date.now() / 1000) + 60;

      first.initializeOffline(
        'apiKey',
        'first-key',
        'https://first.test',
        false,
      );
      second.initializeOffline(
        'apiKey',
        'second-key',
        'https://second.test',
        false,
      );
      first.importSecret('first', btoa('first-secret'), expiresAt);
      second.importSecret('second', btoa('second-secret'), expiresAt);

      expect(JSON.parse(first.exportSecret())).toMatchObject({ id: 'first' });
      expect(JSON.parse(second.exportSecret())).toMatchObject({ id: 'second' });
    });
  });

  describe('close', () => {
    it('releases the client and tolerates double close', () => {
      const closable = createWasmClient();
      closable.initializeOffline('apiKey', 'key', 'https://close.test', false);
      expect(() => closable.close()).not.toThrow();
      expect(() => closable.close()).not.toThrow();
    });

    it('calls the Wasm close export only once', () => {
      const close = vi.fn();
      const mockClient = new WasmClient({
        close,
      } as unknown as WasmClientExports);
      mockClient.close();
      mockClient.close();
      expect(close).toHaveBeenCalledTimes(1);
    });
  });

  describe('initWasm after the module is loaded', () => {
    it('accepts further loads without a hash', async () => {
      await expect(initWasm(new Uint8Array([0]))).resolves.toBeUndefined();
    });

    it('rejects a hash when the module was loaded without one', async () => {
      await expect(
        initWasm(
          new Uint8Array([0]),
          '0000000000000000000000000000000000000000000000000000000000000000',
        ),
      ).rejects.toThrow(/already loaded without integrity verification/);
    });
  });

  describe('updateSecret', () => {
    it('fails when not initialized', async () => {
      await expect(client.updateSecret()).rejects.toThrow();
    });
  });

  describe('fetchManifest', () => {
    it('uses Fetch when Node argv0 is node', async () => {
      const manifestData = JSON.stringify({ name: 'test' });
      fetchMock.mockClear();
      fetchMock.mockImplementation(() =>
        Promise.resolve(new Response(manifestData, { status: 200 })),
      );
      const res = await client.fetchManifest();
      expect(res).toBe(manifestData);
      expect(fetchMock).toHaveBeenCalledOnce();
      expect(fetchMock.mock.calls[0]?.[0]).toMatch(
        /^https:\/\/cdn\.confidential\.cloud\/privatemode\/v2\/manifest\.json\?t=\d+$/,
      );
    });
  });

  describe('inference exports', () => {
    it('exposes streaming transcriptions', async () => {
      await expect(
        client.streamTranscribeAudio(
          { name: 'empty.wav', content: new Uint8Array() },
          JSON.stringify({ model: 'test-model' }),
          () => undefined,
        ),
      ).rejects.toThrow('audio file content must not be empty');
    });
  });

  describe('API errors', () => {
    it('normalizes successful responses exported by Wasm', async () => {
      const mock = vi.fn(async () => ({
        body: JSON.stringify({ id: 'chatcmpl-1' }),
        headers: { 'x-request-id': 'req_chat' },
      }));
      const mockClient = new WasmClient({
        chatCompletions: mock,
      } as unknown as WasmClientExports);

      const response = await mockClient.chatCompletions('{}', {
        'x-custom-header': 'custom-value',
      });

      expect(response.body).toBe(JSON.stringify({ id: 'chatcmpl-1' }));
      expect(response.headers.get('x-request-id')).toBe('req_chat');
      expect(mock).toHaveBeenCalledWith(
        '{}',
        { 'x-custom-header': 'custom-value' },
        undefined,
      );
    });

    it('normalizes embeddings responses exported by Wasm', async () => {
      const embeddings = vi.fn(async () => ({
        body: JSON.stringify({ data: [], object: 'list' }),
        headers: { 'x-request-id': 'req_embeddings' },
      }));
      const mockClient = new WasmClient({
        embeddings,
      } as unknown as WasmClientExports);

      const response = await mockClient.embeddings('{}');

      expect(response.body).toBe(JSON.stringify({ data: [], object: 'list' }));
      expect(response.headers.get('x-request-id')).toBe('req_embeddings');
      expect(embeddings).toHaveBeenCalledWith('{}', undefined, undefined);
    });

    it('normalizes structured errors exported by Wasm', async () => {
      const error = Object.assign(new Error('chat completions: Slow down'), {
        body: JSON.stringify({
          error: {
            code: 'rate_limit',
            message: 'Slow down',
            type: 'rate_limit_error',
          },
        }),
        headers: { 'retry-after': '2' },
        name: 'PrivatemodeAPIError',
        status: 429,
      });
      const mockClient = new WasmClient({
        chatCompletions: vi.fn(async () => Promise.reject(error)),
      } as unknown as WasmClientExports);

      await expect(mockClient.chatCompletions('{}')).rejects.toMatchObject({
        body: {
          error: {
            code: 'rate_limit',
            message: 'Slow down',
            type: 'rate_limit_error',
          },
        },
        status: 429,
      });
      await expect(mockClient.chatCompletions('{}')).rejects.toBeInstanceOf(
        PrivatemodeAPIError,
      );
      await expect(mockClient.chatCompletions('{}')).rejects.toMatchObject({
        headers: expect.any(Headers),
      });
    });
  });
});
