//go:build js && wasm

package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"syscall/js"
	"time"

	"github.com/edgelesssys/continuum/internal/oss/attest"
	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/edgelesssys/continuum/internal/oss/privatemode"
	"github.com/edgelesssys/continuum/internal/oss/secretmanager"
)

type privatemodeWasmWrapper struct {
	client *privatemode.Client
}

func newWasmWrapper() *privatemodeWasmWrapper {
	return &privatemodeWasmWrapper{
		client: privatemode.New(""),
	}
}

// initialize returns a JS Promise that resolves when the Privatemode
// client has been initialized. This verifies the deployment by attesting
// the Contrast coordinator and sets up the secret for encryption.
// Arguments are structured as follows:
//   - args[0]: base64-encoded manifest bytes (string)
//   - args[1]: API key for the Privatemode API (string)
//   - args[2]: API base URL for the Privatemode API (string)
//   - args[3]: whether to enable logging (bool)
//
// The returned Promise resolves with undefined on success, or rejects
// with a JS Error on failure.
func (w *privatemodeWasmWrapper) initialize(this js.Value, args []js.Value) any {
	base64Manifest := args[0].String()
	apiKey := args[1].String()
	apiBaseURL := args[2].String()
	enableLogging := args[3].Bool()

	client := privatemode.New(apiKey).WithAPIBaseURL(apiBaseURL)
	if enableLogging {
		client = client.WithLogger(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	}
	w.client = client

	promiseConstructor := js.Global().Get("Promise")

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
//   - args[0]: API key for the Privatemode API (string)
//   - args[1]: API base URL for the Privatemode API (string)
//   - args[2]: whether to enable logging (bool)
func (w *privatemodeWasmWrapper) initializeOffline(this js.Value, args []js.Value) any {
	apiKey := args[0].String()
	apiBaseURL := args[1].String()
	enableLogging := args[2].Bool()

	client := privatemode.New(apiKey).WithAPIBaseURL(apiBaseURL)
	if enableLogging {
		client = client.WithLogger(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	}
	w.client = client
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
//
// The returned Promise resolves with the decrypted response body as a
// string, or rejects with a JS Error on failure.
func (w *privatemodeWasmWrapper) chatCompletions(this js.Value, args []js.Value) any {
	body := args[0].String()

	promiseConstructor := js.Global().Get("Promise")
	promiseFunc := func(_ js.Value, promiseArgs []js.Value) any {
		resolve := promiseArgs[0]
		reject := promiseArgs[1]

		go func() {
			resp, err := w.client.ChatCompletions(context.Background(), []byte(body))
			if err != nil {
				reject.Invoke(js.Global().Get("Error").New(fmt.Sprintf("chat completions: %s", err)))
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

// streamChatCompletions returns a JS Promise that resolves when
// streaming is complete. It calls the provided JS callback for each
// decrypted chunk.
// Arguments:
//   - args[0]: JSON request body (string). Should include "stream": true.
//   - args[1]: JS callback function called with each decrypted chunk (string).
//   - args[2]: (optional) JS AbortSignal for cancellation.
//
// The returned Promise resolves with undefined when the stream ends,
// or rejects with a JS Error on failure. If the AbortSignal is
// triggered, the stream is cancelled and the Promise rejects with
// an AbortError.
func (w *privatemodeWasmWrapper) streamChatCompletions(this js.Value, args []js.Value) any {
	body := args[0].String()
	onChunk := args[1]

	var abortSignal js.Value
	if len(args) > 2 && !args[2].IsUndefined() && !args[2].IsNull() {
		abortSignal = args[2]
	}

	promiseConstructor := js.Global().Get("Promise")
	promiseFunc := func(_ js.Value, promiseArgs []js.Value) any {
		resolve := promiseArgs[0]
		reject := promiseArgs[1]

		go func() {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// If an AbortSignal was provided, listen for the abort event
			// and cancel the Go context when it fires.
			if abortSignal.Truthy() {
				if abortSignal.Get("aborted").Bool() {
					reject.Invoke(newDOMAbortError())
					return
				}
				onAbort := js.FuncOf(func(_ js.Value, _ []js.Value) any {
					cancel()
					return nil
				})
				defer onAbort.Release()
				abortSignal.Call("addEventListener", "abort", onAbort, map[string]any{"once": true})
			}

			stream, err := w.client.StreamChatCompletions(ctx, []byte(body))
			if err != nil {
				if ctx.Err() != nil {
					reject.Invoke(newDOMAbortError())
					return
				}
				reject.Invoke(js.Global().Get("Error").New(fmt.Sprintf("stream chat completions: %s", err)))
				return
			}
			defer stream.Close()

			for chunk, err := range stream.All() {
				if err != nil {
					if ctx.Err() != nil {
						reject.Invoke(newDOMAbortError())
						return
					}
					reject.Invoke(js.Global().Get("Error").New(fmt.Sprintf("reading stream chunk: %s", err)))
					return
				}
				onChunk.Invoke(js.ValueOf(string(chunk)))
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
				reject.Invoke(js.Global().Get("Error").New(fmt.Sprintf("unstructured: %s", err)))
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

// listModels returns a JS Promise that resolves with the JSON response
// from the /v1/models endpoint.
// It takes no JS arguments.
//
// [initialize] must have been called before calling this, as the
// API key and base URL are set during initialization.
//
// The returned Promise resolves with the response body as a string,
// or rejects with a JS Error on failure.
func (w *privatemodeWasmWrapper) listModels(this js.Value, args []js.Value) any {
	promiseConstructor := js.Global().Get("Promise")

	promiseFunc := func(_ js.Value, promiseArgs []js.Value) any {
		resolve := promiseArgs[0]
		reject := promiseArgs[1]

		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			resp, err := w.client.ListModels(ctx)
			if err != nil {
				reject.Invoke(js.Global().Get("Error").New(fmt.Sprintf("listing models: %s", err)))
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

func main() {
	w := newWasmWrapper()

	js.Global().Set("privatemodeVersion", constants.Version())
	js.Global().Set("errManifestMismatch", attest.ErrManifestMismatch.Error())
	js.Global().Set("errNoSecretForID", constants.ErrorNoSecretForID)
	js.Global().Set("initialize", js.FuncOf(w.initialize))
	js.Global().Set("initializeOffline", js.FuncOf(w.initializeOffline))
	js.Global().Set("updateSecret", js.FuncOf(w.updateSecret))
	js.Global().Set("fetchManifest", js.FuncOf(w.fetchManifest))
	js.Global().Set("chatCompletions", js.FuncOf(w.chatCompletions))
	js.Global().Set("streamChatCompletions", js.FuncOf(w.streamChatCompletions))
	js.Global().Set("unstructured", js.FuncOf(w.unstructured))
	js.Global().Set("listModels", js.FuncOf(w.listModels))
	js.Global().Set("exportSecret", js.FuncOf(w.exportSecret))
	js.Global().Set("importSecret", js.FuncOf(w.importSecret))

	<-make(chan struct{})
}
