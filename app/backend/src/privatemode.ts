import * as path from 'path';
import * as os from 'os';
import * as fs from 'fs';

export type StartProxyResult = {
  success: boolean;
  port: string;
  error?: string;
};

interface PrivatemodeAddon {
  startProxy(): StartProxyResult;
}

let libPath: string;
let addonPath: string;

if (import.meta.env.DEV || process.env.PRIVATEMODE_IS_PLAYWRIGHT_TEST === '1') {
  libPath = path.join(__dirname, '../../../../build-libprivatemode/lib');
  if (!fs.existsSync(libPath)) {
    throw new Error(
      `libprivatemode not found at ${libPath}. Did you forget to build it via 'nix build .#libprivatemode --out-link build-libprivatemode'?`,
    );
  }
  addonPath = path.join(
    __dirname,
    '../../build/Release/privatemode_addon.node',
  );
} else {
  libPath = path.join(process.resourcesPath, 'lib');
  addonPath = path.join(
    process.resourcesPath,
    'build/Release/privatemode_addon.node',
  );
}

const platform = os.platform();
let libFilename: string;
if (platform === 'win32') {
  libFilename = 'libprivatemode.dll';
} else if (platform === 'darwin') {
  libFilename = 'libprivatemode.dylib';
} else {
  libFilename = 'libprivatemode.so';
}

process.env.LIBPRIVATEMODE_PATH = path.join(libPath, libFilename);
if (!fs.existsSync(process.env.LIBPRIVATEMODE_PATH)) {
  throw new Error(
    `Dynamic library not found at ${process.env.LIBPRIVATEMODE_PATH}`,
  );
}

console.log('Loading privatemode addon from', addonPath);
console.log('Library path set to', process.env.LIBPRIVATEMODE_PATH);

const addon: PrivatemodeAddon = require(addonPath);

export function startProxy(): StartProxyResult {
  return addon.startProxy();
}
