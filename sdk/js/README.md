# Privatemode TypeScript / JavaScript SDK

This is the TypeScript / Javascript client SDK for
[Privatemode](https://www.privatemode.ai/).

It allows to securely verify and connect to Privatemode from a JS / TS
client application with an OpenAI-compatible client.

## Installation

```sh
npm i privatemode-ai
```

The official `openai` package is a peer dependency, shared with your
application so that both use the same OpenAI SDK types and classes. Recent
package managers (npm 7+, pnpm 8+) install it automatically; add it explicitly
(`npm i openai`) if you import from `openai` directly or use an older package
manager.

The SDK is compatible with browsers as well as other JS runtimes
supporting ECMAScript modules.

## Quick Start

```ts
import { PrivatemodeAI } from 'privatemode-ai';

const client = new PrivatemodeAI({
  apiKey: process.env.PRIVATEMODE_API_KEY!,
});

const completion = await client.chat.completions.create({
  model: 'gpt-oss-120b',
  messages: [{ role: 'user', content: 'Hello!' }],
});

console.log(completion.choices[0]?.message.content);
```

`PrivatemodeAI` verifies the Privatemode deployment and
establishes an encryption secret lazily before the first request.

To authenticate with rotating credentials (e.g. short-lived JWTs) or
anonymously, pass an `auth` provider instead of `apiKey`. It is invoked
before every request to obtain the current credential.

### Streaming

```ts
const stream = await client.chat.completions.create({
  model: 'gpt-oss-120b',
  messages: [{ role: 'user', content: 'Write a short poem.' }],
  stream: true,
});

for await (const chunk of stream) {
  process.stdout.write(chunk.choices[0]?.delta.content ?? '');
}
```

Errors received before streaming begins use the official OpenAI error classes.
Once a stream has begun, transport errors are surfaced directly because an HTTP
status can no longer represent them.

### Supported OpenAI Resources

| OpenAI SDK method                       | Privatemode endpoint         |
| --------------------------------------- | ---------------------------- |
| `client.chat.completions.create()`      | `/v1/chat/completions`       |
| `client.embeddings.create()`            | `/v1/embeddings`             |
| `client.audio.transcriptions.create()`  | `/v1/audio/transcriptions`   |
| `client.models.list()`                  | `/v1/models`                 |
| `client.models.retrieve()`              | `/v1/models` (from the list) |

Unsupported OpenAI resources fail closed with a non-retryable
`OpenAI.InternalServerError` whose `code` is `unsupported_endpoint`. Requests
are never forwarded through the platform's ordinary `fetch` implementation.

Transcriptions currently support `json` and `verbose_json` response formats.

The OpenAI SDK's standard retry policy remains enabled for retryable API
responses. Privatemode separately retries once only when an encryption secret
has expired, after refreshing that secret; it does not independently retry
rate-limit or server errors. Set `maxRetries` in `openAIOptions` to override the
OpenAI retry policy.

## Advanced Usage

Applications that manage attestation, persisted secrets, or Privatemode-specific
endpoints such as the unstructured document API can use those operations on the
same client:

```ts
import { PrivatemodeAI } from 'privatemode-ai';

const client = new PrivatemodeAI({
  // Rotating credentials, as an alternative to a static `apiKey`.
  auth: async () => ({ type: 'jwt', value: await fetchSessionToken() }),
  // Required when running in a browser.
  dangerouslyAllowBrowser: true,
});

await client.verify();
await client.refreshSecret();
```

Advanced lifecycle and Privatemode-specific operations are available alongside
the OpenAI-compatible chat completions, transcriptions, and model APIs.

Browser use must be enabled explicitly with `dangerouslyAllowBrowser: true`, as
shown above. Only enable it after considering how credentials and decrypted
data are protected in the application.
