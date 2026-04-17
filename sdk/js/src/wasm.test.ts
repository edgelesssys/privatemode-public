import { describe, it, expect, beforeAll, vi } from 'vitest';
import {
  initWasm,
  initialize,
  updateSecret,
  privatemodeVersion,
  fetchManifest,
} from './wasm.js';

const isNode = typeof process !== 'undefined' && !!process.versions?.node;
const manifest = btoa('{}');

const originalFetch = globalThis.fetch;
const fetchMock = vi.fn();
globalThis.fetch = fetchMock;

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
  beforeAll(async () => {
    fetchMock.mockImplementation(() =>
      Promise.resolve(new Response('', { status: 200 })),
    );
    const wasmSource = await loadWasm();
    await initWasm(wasmSource);
  });

  describe('privatemodeVersion', () => {
    it('returns a semver', () => {
      expect(privatemodeVersion()).toMatch(/^v\d+\.\d+\.\d+(-.+)?$/);
    });
  });

  describe('initialize', () => {
    it('fails with illegal base64 data', async () => {
      const invalidBase64 = '!!!';
      await expect(initialize(invalidBase64, '', '', true)).rejects.toThrow(
        'illegal base64 data at input byte 0',
      );
    });

    it('fails with unauthorized API key', async () => {
      fetchMock.mockImplementation(() =>
        Promise.resolve(new Response('Unauthorized', { status: 401 })),
      );
      await expect(
        initialize(manifest, 'bad-key', 'https://example.com', true),
      ).rejects.toThrow('Unauthorized');
    });
  });

  describe('updateSecret', () => {
    it('fails when not initialized', async () => {
      await expect(updateSecret()).rejects.toThrow();
    });
  });

  describe('fetchManifest', () => {
    it('returns manifest string', async () => {
      const manifestData = JSON.stringify({ name: 'test' });
      fetchMock.mockImplementation(() =>
        Promise.resolve(new Response(manifestData, { status: 200 })),
      );
      const res = await fetchManifest();
      expect(res).toBe(manifestData);
    });
  });
});
