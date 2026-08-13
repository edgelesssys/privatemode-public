import { copyFile, rm } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';

const source =
  process.env.PRIVATEMODE_WASM_PATH ??
  fileURLToPath(new URL('../../wasm/privatemode.wasm', import.meta.url));
const destination = fileURLToPath(
  new URL('../dist/privatemode.wasm', import.meta.url),
);

await rm(destination, { force: true });
await copyFile(source, destination);
