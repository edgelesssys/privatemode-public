// Vite plugin that adds Subresource Integrity (SRI) to the SvelteKit build
// output via an importmap with "integrity" entries for all JS modules.
// https://jspm.org/js-integrity-with-import-maps
// https://developer.mozilla.org/en-US/docs/Web/HTML/Reference/Elements/script/type/importmap
//
// This only works with production builds to to Vite's HMR in dev mode.

import { readdir, readFile, writeFile } from 'node:fs/promises';
import { createHash } from 'node:crypto';
import { join } from 'node:path';
import type { Plugin, ResolvedConfig } from 'vite';

async function collectFiles(dir: string): Promise<string[]> {
  const entries = await readdir(dir, { withFileTypes: true, recursive: true });
  return entries
    .filter((e) => !e.isDirectory())
    .map((e) => join(e.parentPath, e.name));
}

function computeHash(content: Buffer): string {
  const hash = createHash('sha384').update(content).digest('base64');
  return `sha384-${hash}`;
}

function toAbsoluteURL(immutableDir: string, filePath: string): string {
  const rel = filePath.slice(immutableDir.length + 1).replaceAll('\\', '/');
  return `/_app/immutable/${rel}`;
}

async function patchBuildFiles(buildDir: string): Promise<void> {
  const immutableDir = join(buildDir, '_app', 'immutable');

  // 1. Collect all immutable assets and compute their hashes.
  const files = await collectFiles(immutableDir);
  const integrityMap = new Map<string, string>();

  await Promise.all(
    files.map(async (file) => {
      const content = await readFile(file);
      const url = toAbsoluteURL(immutableDir, file);
      integrityMap.set(url, computeHash(content));
    }),
  );

  // 2. Build the import map (JS modules only, sorted for deterministic output).
  const jsIntegrity: Record<string, string> = {};
  for (const url of [...integrityMap.keys()]
    .filter((k) => k.endsWith('.js'))
    .sort()) {
    jsIntegrity[url] = integrityMap.get(url)!;
  }

  const importMap = JSON.stringify({ integrity: jsIntegrity }, null, '\t');
  const importMapTag = `<script type="importmap">\n${importMap}\n\t</script>`;

  // 3. Process each HTML file in the build directory.
  const buildEntries = await readdir(buildDir);
  const htmlFiles = buildEntries
    .filter((f) => f.endsWith('.html'))
    .map((f) => join(buildDir, f));

  let totalPatched = 0;
  for (const htmlFile of htmlFiles) {
    let html = await readFile(htmlFile, 'utf-8');

    // Strip any existing import maps (idempotency).
    while (html.includes('<script type="importmap">')) {
      html = html.replace(
        /[ \t]*<script type="importmap">[\s\S]*?<\/script>\n?/,
        '',
      );
    }

    // Add (or update) integrity/crossorigin on modulepreload links so the
    // browser's preload cache matches what the import map declares.
    html = html.replace(
      /<link href="([^"]+\.js)" rel="modulepreload"(?: integrity="[^"]*")?(?: crossorigin)?>/g,
      (_, href: string) => {
        const hash = integrityMap.get(href);
        if (hash) {
          return `<link href="${href}" rel="modulepreload" integrity="${hash}" crossorigin>`;
        }
        return `<link href="${href}" rel="modulepreload">`;
      },
    );

    // Inject the import map as the first child of <head>.
    html = html.replace(/(<head[^>]*>\n)/, `$1\t${importMapTag}\n`);

    await writeFile(htmlFile, html);
    totalPatched++;
  }

  const jsCount = Object.keys(jsIntegrity).length;
  console.log(
    `SRI: ${jsCount} JS modules in import map, ${integrityMap.size} total assets hashed, ${totalPatched} HTML files patched`,
  );
}

export function sriPlugin(): Plugin {
  let rootDir: string;

  return {
    name: 'sveltekit-sri',
    apply: 'build',

    configResolved(config: ResolvedConfig) {
      rootDir = config.root;
    },

    async closeBundle() {
      const buildDir = join(rootDir, 'build');
      // SvelteKit runs two Vite builds (client + server). The HTML files
      // only exist after the adapter runs at the end of the server build,
      // so we need to wait until then to patch the files.
      try {
        const entries = await readdir(buildDir);
        if (!entries.some((f) => f.endsWith('.html'))) return;
      } catch {
        return; // build directory doesn't exist yet (client build phase)
      }
      await patchBuildFiles(buildDir);
    },
  };
}
