//go:build js && wasm

package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"syscall/js"
	"time"

	"github.com/edgelesssys/continuum/internal/oss/attest"
	"github.com/edgelesssys/continuum/internal/oss/auth"
	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/edgelesssys/continuum/internal/oss/privatemode"
	"github.com/edgelesssys/continuum/internal/oss/secretmanager"
)

// Authentication method identifiers exchanged with the JavaScript SDK. They must be
// kept in sync with the AuthCredential types in sdk/js/src/privatemode-ai.ts.
const (
	authTypeAPIKey    = "apiKey"
	authTypeJWT       = "jwt"
	authTypeAnonymous = "anonymous"
)

// credentialFromArgs builds an [auth.Credential] from the (type, value) pair passed by
// the JavaScript SDK.
func credentialFromArgs(authType, authValue string) (auth.Credential, error) {
	switch authType {
	case authTypeAPIKey:
		return auth.APIKey(authValue), nil
	case authTypeJWT:
		return auth.JWT(authValue), nil
	case authTypeAnonymous:
		return auth.Anonymous(authValue), nil
	default:
		return auth.Credential{}, fmt.Errorf("unknown auth type %q", authType)
	}
}

type privatemodeWasmWrapper struct {
	client *privatemode.Client
}

func newWasmWrapper() *privatemodeWasmWrapper {
	return &privatemodeWasmWrapper{
		client: privatemode.New(auth.Credential{}),
	}
}

// initialize returns a JS Promise that resolves when the Privatemode
// client has been initialized. This verifies the deployment by attesting
// the Contrast coordinator and sets up the secret for encryption.
// Arguments are structured as follows:
//   - args[0]: base64-encoded manifest bytes (string)
//   - args[1]: authentication method (string: "apiKey", "jwt", or "anonymous")
//   - args[2]: authentication value, e.g. the API key, JWT, or fingerprint (string)
//   - args[3]: API base URL for the Privatemode API (string)
//   - args[4]: whether to enable logging (bool)
//   - args[5]: optional additional HTTP headers (object)
//
// The returned Promise resolves with undefined on success, or rejects
// with a JS Error on failure.
func (w *privatemodeWasmWrapper) initialize(this js.Value, args []js.Value) any {
	base64Manifest := args[0].String()
	authType := args[1].String()
	authValue := args[2].String()
	apiBaseURL := args[3].String()
	enableLogging := args[4].Bool()

	promiseConstructor := js.Global().Get("Promise")

	cred, err := credentialFromArgs(authType, authValue)
	if err != nil {
		return promiseConstructor.Call("reject",
			js.Global().Get("Error").New(fmt.Sprintf("building credential: %s", err)))
	}

	client := privatemode.New(cred).WithAPIBaseURL(apiBaseURL)
	if len(args) > 5 {
		client = client.WithExtraHTTPHeaders(requestOptionsFromArg(args, 5).Header)
	}
	if enableLogging {
		client = client.WithLogger(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	}
	w.client = client

	expectedMfBytes, err := base64.StdEncoding.DecodeString(base64Manifest)
	if err != nil {
		return promiseConstructor.Call("reject",
			js.Global().Get("Error").New(fmt.Sprintf("decoding base64 manifest: %s", err)))
	}

	promiseFunc := func(_ js.Value, promiseArgs []js.Value) any {
		resolve := promiseArgs[0]
		reject := promiseArgs[1]

		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := w.client.Initialize(ctx, expectedMfBytes); err != nil {
				reject.Invoke(js.Global().Get("Error").New(fmt.Sprintf("initializing client: %s", err)))
				return
			}
			resolve.Invoke(js.Undefined())
		}()

		return nil
	}

	fn := js.FuncOf(promiseFunc)
	defer fn.Release()
	return promiseConstructor.New(fn)
}

// initializeOffline sets up the Privatemode client without performing
// attestation. This is useful when restoring a previously cached
// secret with [importSecret].
// Arguments are structured as follows:
//   - args[0]: authentication method (string: "apiKey", "jwt", or "anonymous")
//   - args[1]: authentication value, e.g. the API key, JWT, or fingerprint (string)
//   - args[2]: API base URL for the Privatemode API (string)
//   - args[3]: whether to enable logging (bool)
//   - args[4]: optional additional HTTP headers (object)
//
// Throws a JS Error if the authentication method is unknown.
func (w *privatemodeWasmWrapper) initializeOffline(this js.Value, args []js.Value) any {
	authType := args[0].String()
	authValue := args[1].String()
	apiBaseURL := args[2].String()
	enableLogging := args[3].Bool()

	cred, err := credentialFromArgs(authType, authValue)
	if err != nil {
		panic(js.Global().Get("Error").New(fmt.Sprintf("building credential: %s", err)))
	}

	client := privatemode.New(cred).WithAPIBaseURL(apiBaseURL)
	if len(args) > 4 {
		client = client.WithExtraHTTPHeaders(requestOptionsFromArg(args, 4).Header)
	}
	if enableLogging {
		client = client.WithLogger(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	}
	w.client = client
	return js.Undefined()
}

// setAuth updates the authentication credential used for subsequent requests.
// This allows rotating short-lived credentials (e.g. JWTs) without
// re-initializing the client.
// Arguments are structured as follows:
//   - args[0]: authentication method (string: "apiKey", "jwt", or "anonymous")
//   - args[1]: authentication value, e.g. the API key, JWT, or fingerprint (string)
//
// Throws a JS Error if the authentication method is unknown.
func (w *privatemodeWasmWrapper) setAuth(this js.Value, args []js.Value) any {
	authType := args[0].String()
	authValue := args[1].String()

	cred, err := credentialFromArgs(authType, authValue)
	if err != nil {
		panic(js.Global().Get("Error").New(fmt.Sprintf("building credential: %s", err)))
	}
	w.client.SetCredential(cred)
	return js.Undefined()
}

// updateSecret returns a JS Promise that resolves when the client's
// secret has been updated. [initialize] must have been called before
// calling this.
// It takes no JS arguments.
//
// The returned Promise resolves with undefined on success, or rejects
// with a JS Error on failure.
func (w *privatemodeWasmWrapper) updateSecret(this js.Value, args []js.Value) any {
	promiseConstructor := js.Global().Get("Promise")

	promiseFunc := func(_ js.Value, promiseArgs []js.Value) any {
		resolve := promiseArgs[0]
		reject := promiseArgs[1]

		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second) // TODO: Consider making this timeout configurable
			defer cancel()
			if err := w.client.UpdateSecret(ctx); err != nil {
				reject.Invoke(js.Global().Get("Error").New(fmt.Sprintf("updating secret: %s", err)))
				return
			}
			resolve.Invoke(js.Undefined())
		}()

		return nil
	}

	fn := js.FuncOf(promiseFunc)
	defer fn.Release()
	return promiseConstructor.New(fn)
}

// fetchManifest returns a JS Promise that resolves with the current
// manifest from the CDN as a string. It takes no JS arguments.
func (w *privatemodeWasmWrapper) fetchManifest(this js.Value, args []js.Value) any {
	promiseConstructor := js.Global().Get("Promise")

	promiseFunc := func(_ js.Value, promiseArgs []js.Value) any {
		resolve := promiseArgs[0]
		reject := promiseArgs[1]

		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second) // TODO: Consider making this timeout configurable
			defer cancel()
			manifest, err := w.client.FetchManifest(ctx)
			if err != nil {
				reject.Invoke(js.Global().Get("Error").New(fmt.Sprintf("fetching manifest: %s", err)))
				return
			}
			resolve.Invoke(js.ValueOf(string(manifest)))
		}()

		return nil
	}

	fn := js.FuncOf(promiseFunc)
	defer fn.Release()
	return promiseConstructor.New(fn)
}

// chatCompletions returns a JS Promise that resolves with the decrypted
// response from the /v1/chat/completions endpoint.
// Arguments:
//   - args[0]: JSON request body (string)
//   - args[1]: (optional) additional request headers.
//   - args[2]: (optional) JS AbortSignal for cancellation.
//
// The returned Promise resolves with the decrypted response body and headers,
// or rejects with a JS Error on failure. If the AbortSignal is triggered,
// the request is cancelled and the Promise rejects with an AbortError.
func (w *privatemodeWasmWrapper) chatCompletions(this js.Value, args []js.Value) any {
	body := args[0].String()
	opts := requestOptionsFromArg(args, 1)
	abortSignal := abortSignalFromArg(args, 2)

	promiseConstructor := js.Global().Get("Promise")
	promiseFunc := func(_ js.Value, promiseArgs []js.Value) any {
		resolve := promiseArgs[0]
		reject := promiseArgs[1]

		go func() {
			ctx, release, aborted := contextWithAbortSignal(context.Background(), abortSignal)
			defer release()
			if aborted {
				reject.Invoke(newDOMAbortError())
				return
			}
			resp, err := w.client.ChatCompletionsWithOptions(ctx, []byte(body), opts)
			if err != nil {
				if ctx.Err() != nil {
					reject.Invoke(newDOMAbortError())
					return
				}
				reject.Invoke(newJSError(err, "chat completions: %s", err))
				return
			}
			resolve.Invoke(newJSResponse(resp))
		}()

		return nil
	}

	fn := js.FuncOf(promiseFunc)
	defer fn.Release()
	return promiseConstructor.New(fn)
}

// embeddings returns a JS Promise that resolves with the decrypted
// response from the /v1/embeddings endpoint.
// Arguments:
//   - args[0]: JSON request body (string)
//   - args[1]: (optional) additional request headers.
//   - args[2]: (optional) JS AbortSignal for cancellation.
//
// The returned Promise resolves with the decrypted response body and headers,
// or rejects with a JS Error on failure. If the AbortSignal is triggered,
// the request is cancelled and the Promise rejects with an AbortError.
func (w *privatemodeWasmWrapper) embeddings(this js.Value, args []js.Value) any {
	body := args[0].String()
	opts := requestOptionsFromArg(args, 1)
	abortSignal := abortSignalFromArg(args, 2)

	promiseConstructor := js.Global().Get("Promise")
	promiseFunc := func(_ js.Value, promiseArgs []js.Value) any {
		resolve := promiseArgs[0]
		reject := promiseArgs[1]

		go func() {
			ctx, release, aborted := contextWithAbortSignal(context.Background(), abortSignal)
			defer release()
			if aborted {
				reject.Invoke(newDOMAbortError())
				return
			}
			resp, err := w.client.EmbeddingsWithOptions(ctx, []byte(body), opts)
			if err != nil {
				if ctx.Err() != nil {
					reject.Invoke(newDOMAbortError())
					return
				}
				reject.Invoke(newJSError(err, "embeddings: %s", err))
				return
			}
			resolve.Invoke(newJSResponse(resp))
		}()

		return nil
	}

	fn := js.FuncOf(promiseFunc)
	defer fn.Release()
	return promiseConstructor.New(fn)
}

// streamChatCompletions returns a JS Promise that resolves when
// streaming is complete. It calls the provided JS callback for each
// decrypted chunk.
// Arguments:
//   - args[0]: JSON request body (string). Should include "stream": true.
//   - args[1]: JS callback function called with each decrypted chunk (string).
//     If the callback returns a Promise, the next chunk is not read until the
//     Promise settles, propagating consumer backpressure to the response body.
//   - args[2]: (optional) JS AbortSignal for cancellation.
//   - args[3]: (optional) JS callback called with the response headers.
//   - args[4]: (optional) additional request headers.
//
// The returned Promise resolves with undefined when the stream ends,
// or rejects with a JS Error on failure. If the AbortSignal is
// triggered, the stream is cancelled and the Promise rejects with
// an AbortError.
func (w *privatemodeWasmWrapper) streamChatCompletions(this js.Value, args []js.Value) any {
	body := args[0].String()
	onChunk := args[1]
	var onHeaders js.Value
	if len(args) > 3 && !args[3].IsUndefined() && !args[3].IsNull() {
		onHeaders = args[3]
	}
	opts := requestOptionsFromArg(args, 4)
	abortSignal := abortSignalFromArg(args, 2)

	promiseConstructor := js.Global().Get("Promise")
	promiseFunc := func(_ js.Value, promiseArgs []js.Value) any {
		resolve := promiseArgs[0]
		reject := promiseArgs[1]

		go func() {
			ctx, release, aborted := contextWithAbortSignal(context.Background(), abortSignal)
			defer release()
			if aborted {
				reject.Invoke(newDOMAbortError())
				return
			}

			stream, err := w.client.StreamChatCompletionsWithOptions(ctx, []byte(body), opts)
			if err != nil {
				if ctx.Err() != nil {
					reject.Invoke(newDOMAbortError())
					return
				}
				reject.Invoke(newJSError(err, "stream chat completions: %s", err))
				return
			}
			defer stream.Close()
			if onHeaders.Truthy() {
				onHeaders.Invoke(js.ValueOf(headerToJS(stream.Header)))
			}

			for chunk, err := range stream.All() {
				if err != nil {
					if ctx.Err() != nil {
						reject.Invoke(newDOMAbortError())
						return
					}
					reject.Invoke(newJSError(err, "reading stream chunk: %s", err))
					return
				}
				if err := awaitJSPromise(onChunk.Invoke(js.ValueOf(string(chunk)))); err != nil {
					if ctx.Err() != nil {
						reject.Invoke(newDOMAbortError())
						return
					}
					reject.Invoke(js.Global().Get("Error").New(fmt.Sprintf("stream consumer rejected chunk: %s", err)))
					return
				}
			}

			resolve.Invoke(js.Undefined())
		}()

		return nil
	}

	fn := js.FuncOf(promiseFunc)
	defer fn.Release()
	return promiseConstructor.New(fn)
}

// newDOMAbortError creates a JS DOMException with name "AbortError",
// matching what the browser's AbortController produces.
func newDOMAbortError() js.Value {
	return js.Global().Get("DOMException").New("The operation was aborted.", "AbortError")
}

// awaitJSPromise waits for a Promise returned by a JavaScript callback. Stream
// callbacks use this acknowledgement to prevent Go from reading the next
// response chunk before JavaScript has consumed the current one.
func awaitJSPromise(promise js.Value) error {
	if promise.Type() != js.TypeObject || promise.Get("then").Type() != js.TypeFunction {
		return nil
	}

	result := make(chan error, 1)
	onFulfilled := js.FuncOf(func(_ js.Value, _ []js.Value) any {
		result <- nil
		return nil
	})
	defer onFulfilled.Release()
	onRejected := js.FuncOf(func(_ js.Value, args []js.Value) any {
		message := "callback rejected"
		if len(args) > 0 {
			// The rejection reason can be any value, including null or
			// undefined; only objects can be inspected for a message.
			switch reason := args[0]; reason.Type() {
			case js.TypeObject:
				if value := reason.Get("message"); value.Type() == js.TypeString {
					message = value.String()
				} else {
					message = reason.String()
				}
			case js.TypeNull, js.TypeUndefined:
			default:
				message = reason.String()
			}
		}
		result <- fmt.Errorf("%s", message)
		return nil
	})
	defer onRejected.Release()

	promise.Call("then", onFulfilled, onRejected)
	return <-result
}

// abortSignalFromArg returns the optional JS AbortSignal at index, or the
// zero js.Value if the argument is absent.
func abortSignalFromArg(args []js.Value, index int) js.Value {
	if len(args) <= index || args[index].IsUndefined() || args[index].IsNull() {
		return js.Value{}
	}
	return args[index]
}

// contextWithAbortSignal derives a context that is cancelled when the given
// JS AbortSignal fires. It returns the context, a release function that must
// be deferred, and whether the signal had already been aborted.
func contextWithAbortSignal(ctx context.Context, abortSignal js.Value) (_ context.Context, release func(), aborted bool) {
	ctx, cancel := context.WithCancel(ctx)
	if !abortSignal.Truthy() {
		return ctx, cancel, false
	}
	if abortSignal.Get("aborted").Bool() {
		cancel()
		return ctx, cancel, true
	}
	onAbort := js.FuncOf(func(_ js.Value, _ []js.Value) any {
		cancel()
		return nil
	})
	abortSignal.Call("addEventListener", "abort", onAbort, map[string]any{"once": true})
	return ctx, func() {
		cancel()
		onAbort.Release()
	}, false
}

func requestOptionsFromArg(args []js.Value, index int) privatemode.RequestOptions {
	if len(args) <= index || args[index].IsUndefined() || args[index].IsNull() {
		return privatemode.RequestOptions{}
	}

	value := args[index]
	keys := js.Global().Get("Object").Call("keys", value)
	header := make(http.Header, keys.Length())
	for i := range keys.Length() {
		name := keys.Index(i).String()
		header.Set(name, value.Get(name).String())
	}
	return privatemode.RequestOptions{Header: header}
}

func headerToJS(header http.Header) map[string]any {
	headers := make(map[string]any, len(header))
	for name, values := range header {
		headers[name] = strings.Join(values, ", ")
	}
	return headers
}

func newJSResponse(resp *privatemode.Response) js.Value {
	return js.ValueOf(map[string]any{
		"body":    string(resp.Body),
		"headers": headerToJS(resp.Header),
	})
}

// newJSError creates a JS Error for err with a message built from format and
// args. If err wraps a [privatemode.ResponseError], the API response details
// are attached to the JS error.
func newJSError(err error, format string, args ...any) js.Value {
	jsErr := js.Global().Get("Error").New(fmt.Sprintf(format, args...))
	var responseErr *privatemode.ResponseError
	if errors.As(err, &responseErr) {
		jsErr.Set("name", "PrivatemodeAPIError")
		jsErr.Set("status", responseErr.StatusCode)
		jsErr.Set("body", string(responseErr.Body))
		jsErr.Set("headers", js.ValueOf(headerToJS(responseErr.Header)))
	}
	return jsErr
}

// unstructured returns a JS Promise that resolves with the decrypted
// response from the /unstructured/ endpoint.
// Arguments:
//   - args[0]: JS array of file objects, each with "name" (string),
//     "content" (Uint8Array), and optional "contentType" (string).
//   - args[1]: JSON-encoded options string (may be empty for defaults).
//
// The returned Promise resolves with the decrypted response body as a
// string, or rejects with a JS Error on failure.
func (w *privatemodeWasmWrapper) unstructured(this js.Value, args []js.Value) any {
	jsFiles := args[0]
	optsJSON := args[1].String()

	numFiles := jsFiles.Length()
	files := make([]privatemode.UnstructuredFile, 0, numFiles)
	for i := range numFiles {
		jsFile := jsFiles.Index(i)
		jsContent := jsFile.Get("content")
		content := make([]byte, jsContent.Length())
		js.CopyBytesToGo(content, jsContent)
		f := privatemode.UnstructuredFile{
			Name:    jsFile.Get("name").String(),
			Content: content,
		}
		ct := jsFile.Get("contentType")
		if ct.Truthy() {
			f.ContentType = ct.String()
		}
		files = append(files, f)
	}

	var opts *privatemode.UnstructuredOptions
	if optsJSON != "" {
		opts = &privatemode.UnstructuredOptions{}
		if err := json.Unmarshal([]byte(optsJSON), opts); err != nil {
			promiseConstructor := js.Global().Get("Promise")
			return promiseConstructor.Call("reject",
				js.Global().Get("Error").New(fmt.Sprintf("unstructured: parsing options: %s", err)))
		}
	}

	promiseConstructor := js.Global().Get("Promise")
	promiseFunc := func(_ js.Value, promiseArgs []js.Value) any {
		resolve := promiseArgs[0]
		reject := promiseArgs[1]

		go func() {
			resp, err := w.client.Unstructured(context.Background(), files, opts)
			if err != nil {
				reject.Invoke(newJSError(err, "unstructured: %s", err))
				return
			}
			resolve.Invoke(js.ValueOf(string(resp)))
		}()

		return nil
	}

	fn := js.FuncOf(promiseFunc)
	defer fn.Release()
	return promiseConstructor.New(fn)
}

// transcribeAudio returns a JS Promise that resolves with the decrypted
// response from the /v1/audio/transcriptions endpoint.
// Arguments:
//   - args[0]: JS file object with "name" (string), "content"
//     (Uint8Array), and optional "contentType" (string).
//   - args[1]: JSON-encoded options string.
//   - args[2]: (optional) additional request headers.
//   - args[3]: (optional) JS AbortSignal for cancellation.
//
// The returned Promise resolves with the decrypted response body and headers,
// or rejects with a JS Error on failure. If the AbortSignal is triggered,
// the request is cancelled and the Promise rejects with an AbortError.
func (w *privatemodeWasmWrapper) transcribeAudio(this js.Value, args []js.Value) any {
	file := audioFileFromJS(args[0])
	opts, err := audioTranscriptionOptionsFromJSON(args[1].String())
	if err != nil {
		promiseConstructor := js.Global().Get("Promise")
		return promiseConstructor.Call("reject",
			js.Global().Get("Error").New(fmt.Sprintf("transcribe audio: %s", err)))
	}
	requestOpts := requestOptionsFromArg(args, 2)
	abortSignal := abortSignalFromArg(args, 3)

	promiseConstructor := js.Global().Get("Promise")
	promiseFunc := func(_ js.Value, promiseArgs []js.Value) any {
		resolve := promiseArgs[0]
		reject := promiseArgs[1]

		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			ctx, release, aborted := contextWithAbortSignal(ctx, abortSignal)
			defer release()
			if aborted {
				reject.Invoke(newDOMAbortError())
				return
			}
			resp, err := w.client.TranscribeAudioWithOptions(ctx, file, opts, requestOpts)
			if err != nil {
				if errors.Is(ctx.Err(), context.Canceled) {
					reject.Invoke(newDOMAbortError())
					return
				}
				reject.Invoke(newJSError(err, "transcribe audio: %s", err))
				return
			}
			resolve.Invoke(newJSResponse(resp))
		}()

		return nil
	}

	fn := js.FuncOf(promiseFunc)
	defer fn.Release()
	return promiseConstructor.New(fn)
}

// streamTranscribeAudio returns a JS Promise that resolves when streaming is complete.
// It calls the provided JS callback for each decrypted transcription event.
// Arguments:
//   - args[0]: JS audio file object.
//   - args[1]: JSON-encoded transcription options.
//   - args[2]: JS callback invoked with each event as a JSON string. If the
//     callback returns a Promise, the next event is not read until it settles.
//   - args[3]: (optional) JS AbortSignal for cancellation.
//   - args[4]: (optional) JS callback called with the response headers.
//   - args[5]: (optional) additional request headers.
func (w *privatemodeWasmWrapper) streamTranscribeAudio(this js.Value, args []js.Value) any {
	file := audioFileFromJS(args[0])
	opts, err := audioTranscriptionOptionsFromJSON(args[1].String())
	if err != nil {
		return js.Global().Get("Promise").Call("reject",
			js.Global().Get("Error").New(fmt.Sprintf("stream transcribe audio: %s", err)))
	}
	onChunk := args[2]
	var onHeaders js.Value
	if len(args) > 4 && !args[4].IsUndefined() && !args[4].IsNull() {
		onHeaders = args[4]
	}
	requestOpts := requestOptionsFromArg(args, 5)
	abortSignal := abortSignalFromArg(args, 3)

	promiseConstructor := js.Global().Get("Promise")
	promiseFunc := func(_ js.Value, promiseArgs []js.Value) any {
		resolve := promiseArgs[0]
		reject := promiseArgs[1]

		go func() {
			ctx, release, aborted := contextWithAbortSignal(context.Background(), abortSignal)
			defer release()
			if aborted {
				reject.Invoke(newDOMAbortError())
				return
			}

			stream, err := w.client.StreamTranscribeAudioWithOptions(ctx, file, opts, requestOpts)
			if err != nil {
				if ctx.Err() != nil {
					reject.Invoke(newDOMAbortError())
					return
				}
				reject.Invoke(newJSError(err, "stream transcribe audio: %s", err))
				return
			}
			defer stream.Close()
			if onHeaders.Truthy() {
				onHeaders.Invoke(js.ValueOf(headerToJS(stream.Header)))
			}

			for chunk, err := range stream.All() {
				if err != nil {
					if ctx.Err() != nil {
						reject.Invoke(newDOMAbortError())
						return
					}
					reject.Invoke(newJSError(err, "reading transcription event: %s", err))
					return
				}
				if err := awaitJSPromise(onChunk.Invoke(js.ValueOf(string(chunk)))); err != nil {
					if ctx.Err() != nil {
						reject.Invoke(newDOMAbortError())
						return
					}
					reject.Invoke(js.Global().Get("Error").New(fmt.Sprintf("stream consumer rejected transcription event: %s", err)))
					return
				}
			}

			resolve.Invoke(js.Undefined())
		}()

		return nil
	}

	fn := js.FuncOf(promiseFunc)
	defer fn.Release()
	return promiseConstructor.New(fn)
}

func audioFileFromJS(jsFile js.Value) privatemode.AudioFile {
	jsContent := jsFile.Get("content")
	content := make([]byte, jsContent.Length())
	js.CopyBytesToGo(content, jsContent)
	file := privatemode.AudioFile{
		Name:    jsFile.Get("name").String(),
		Content: content,
	}
	if ct := jsFile.Get("contentType"); ct.Truthy() {
		file.ContentType = ct.String()
	}
	return file
}

func audioTranscriptionOptionsFromJSON(optsJSON string) (privatemode.AudioTranscriptionOptions, error) {
	opts := privatemode.AudioTranscriptionOptions{}
	if optsJSON != "" {
		if err := json.Unmarshal([]byte(optsJSON), &opts); err != nil {
			return opts, fmt.Errorf("parsing options: %w", err)
		}
	}
	return opts, nil
}

// listModels returns a JS Promise that resolves with the JSON response
// from the /v1/models endpoint.
// Arguments:
//   - args[0]: (optional) additional request headers.
//   - args[1]: (optional) JS AbortSignal for cancellation.
//
// [initialize] must have been called before calling this, as the
// API key and base URL are set during initialization.
//
// The returned Promise resolves with the response body and headers, or rejects
// with a JS Error on failure. If the AbortSignal is triggered, the request
// is cancelled and the Promise rejects with an AbortError.
func (w *privatemodeWasmWrapper) listModels(this js.Value, args []js.Value) any {
	opts := requestOptionsFromArg(args, 0)
	abortSignal := abortSignalFromArg(args, 1)
	promiseConstructor := js.Global().Get("Promise")

	promiseFunc := func(_ js.Value, promiseArgs []js.Value) any {
		resolve := promiseArgs[0]
		reject := promiseArgs[1]

		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			ctx, release, aborted := contextWithAbortSignal(ctx, abortSignal)
			defer release()
			if aborted {
				reject.Invoke(newDOMAbortError())
				return
			}
			resp, err := w.client.ListModelsWithOptions(ctx, opts)
			if err != nil {
				if errors.Is(ctx.Err(), context.Canceled) {
					reject.Invoke(newDOMAbortError())
					return
				}
				reject.Invoke(newJSError(err, "listing models: %s", err))
				return
			}
			resolve.Invoke(newJSResponse(resp))
		}()

		return nil
	}

	fn := js.FuncOf(promiseFunc)
	defer fn.Release()
	return promiseConstructor.New(fn)
}

// exportSecret returns the current secret as a JSON string of the form
// {"id": "...", "data": "base64...", "expiresAtUnix": 1234567890}.
// [initialize] or [initializeOffline] must have been called before
// calling this.
// It takes no JS arguments.
//
// Returns the JSON string on success, or throws a JS Error on failure.
func (w *privatemodeWasmWrapper) exportSecret(this js.Value, args []js.Value) any {
	secret, err := w.client.ExportSecret()
	if err != nil {
		panic(js.Global().Get("Error").New(fmt.Sprintf("exporting secret: %s", err)))
	}
	result := struct {
		ID            string `json:"id"`
		Data          string `json:"data"`
		ExpiresAtUnix int64  `json:"expiresAtUnix"`
	}{
		ID:            secret.ID,
		Data:          base64.StdEncoding.EncodeToString(secret.Data),
		ExpiresAtUnix: secret.ExpirationDate.Unix(),
	}
	jsonBytes, err := json.Marshal(result)
	if err != nil {
		panic(js.Global().Get("Error").New(fmt.Sprintf("marshaling secret: %s", err)))
	}
	return js.ValueOf(string(jsonBytes))
}

// importSecret sets the client's secret from exported values.
// [initialize] or [initializeOffline] must have been called before
// calling this.
// Arguments:
//   - args[0]: secret ID (string)
//   - args[1]: base64-encoded secret data (string)
//   - args[2]: expiration Unix timestamp (int)
//
// Throws a JS Error if base64 decoding fails.
func (w *privatemodeWasmWrapper) importSecret(this js.Value, args []js.Value) any {
	id := args[0].String()
	base64Data := args[1].String()
	expiresAtUnix := int64(args[2].Int())

	data, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		panic(js.Global().Get("Error").New(fmt.Sprintf("decoding base64 secret data: %s", err)))
	}

	if err := w.client.ImportSecret(secretmanager.Secret{
		ID:             id,
		Data:           data,
		ExpirationDate: time.Unix(expiresAtUnix, 0),
	}); err != nil {
		panic(js.Global().Get("Error").New(fmt.Sprintf("importing secret: %s", err)))
	}
	return js.Undefined()
}

// newClient returns a JavaScript object whose methods are bound to an
// independent Privatemode client. Keeping the wrapper in the method closures
// ensures that authentication, endpoint, manifest, and secret state cannot be
// overwritten by another JavaScript SDK client.
func newClient(_ js.Value, _ []js.Value) any {
	w := newWasmWrapper()
	client := js.Global().Get("Object").New()
	var funcs []js.Func
	register := func(name string, fn func(js.Value, []js.Value) any) {
		f := js.FuncOf(fn)
		funcs = append(funcs, f)
		client.Set(name, f)
	}
	register("initialize", w.initialize)
	register("initializeOffline", w.initializeOffline)
	register("setAuth", w.setAuth)
	register("updateSecret", w.updateSecret)
	register("fetchManifest", w.fetchManifest)
	register("chatCompletions", w.chatCompletions)
	register("embeddings", w.embeddings)
	register("streamChatCompletions", w.streamChatCompletions)
	register("unstructured", w.unstructured)
	register("transcribeAudio", w.transcribeAudio)
	register("streamTranscribeAudio", w.streamTranscribeAudio)
	register("listModels", w.listModels)
	register("exportSecret", w.exportSecret)
	register("importSecret", w.importSecret)
	// close releases the Go function references backing this client so that
	// short-lived clients don't accumulate them. The client, including close
	// itself, must not be called afterwards.
	register("close", func(_ js.Value, _ []js.Value) any {
		for _, f := range funcs {
			f.Release()
		}
		return js.Undefined()
	})
	return client
}

func main() {
	js.Global().Set("privatemodeVersion", constants.Version())
	js.Global().Set("errManifestMismatch", attest.ErrManifestMismatch.Error())
	js.Global().Set("errNoSecretForID", constants.ErrorNoSecretForID)
	js.Global().Set("createPrivatemodeClient", js.FuncOf(newClient))

	<-make(chan struct{})
}
