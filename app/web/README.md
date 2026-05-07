# Privatemode web app

This is the web app for Privatemode. It's built on top of the
[JS SDK](/sdk/js) and offers a ChatGPT-like UI for using Privatemode
with only a browser. (No Privatemode proxy required)

## Develop

To get an interactive development loop with hot reloading, run:

```sh
just dev-web-app
```

## Test

The Playwright-based [tests](./tests) test the app from a user's
perspective, using a real Chromium browser.

To run them, first ensure that `BROWSER_PATH` is set (explained below),
or that you've installed Playwright's bundled Chromium:

```sh
pnpm install
pnpm exec playwright install chromium --with-deps
```

Then run the tests with:

```sh
just test-web-app
```

Optionally, these environment variables can be specified:

- `BROWSER_PATH`: Use an existing Chrome / Chromium executable to run
  the tests with.
- `BASE_URL`: Run the tests against an already-deployed version of the
  web app. (e.g. `BASE_URL=https://chat.privatemode.ai`)
- `IGNORE_HTTPS_ERRORS=1`: Ignore HTTPS certificate errors in Playwright.
  Useful when testing against custom deployments with non-publicly-trusted
  certificates.
- `VITE_PRIVATEMODE_URL`: Point the web app at a custom Privatemode API
  deployment instead of `https://api.privatemode.ai`. Use the full base
  URL, for example `https://<namespace>.api.privatemode.ai`, without a
  trailing slash.
- `VITE_PRIVATEMODE_MANIFEST_BASE64`: Override the manifest used for
  verification so it is not fetched from the CDN. Set this to the
  base64-encoded manifest bytes.

### Running against a custom Privatemode deployment

To run the web app against a custom deployment, e.g., to add a new
model to the app that's only available in development deployments,
you can run:

```bash
VITE_PRIVATEMODE_MANIFEST_BASE64="$(openssl base64 -A < workspace/manifest.json)" VITE_PRIVATEMODE_URL="https://<namespace>.api.privatemode.ai" just dev-web-app
```

Replace `<namespace>` with your deployment namespace, for example `ms`.

You'll also need to configure your browser to trust the self-signed certificate of the deployment.

The certificate can be downloaded in PEM format with:

```bash
openssl s_client -connect <namespace>.api.privatemode.ai:443 \
  -showcerts </dev/null 2>/dev/null \
  | openssl x509 -outform PEM > cert.pem
```

Then, the certificate must be trusted in the browser. For Chrome, this
is done by navigating to `chrome://certificate-manager/localcerts/usercerts`,
adding the certificate to the list of trusted certificates.

## Build

Build the web app with:

```sh
just build-web-app
```

The artifacts will then be in `result/share`.

## CI

When making changes to the web app in the PR, a preview version is
deployed to Cloudflare pages, using test credentials for integrated
services such as Clerk. On releases, a live version of the app is
deployed.

After the deployment is live, the aforementioned tests are run against
it.

The preview deployments are auto-removed when the corresponding PR is
closed.
