package adapter_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/edgelesssys/continuum/inference-proxy/internal/adapter"
	"github.com/edgelesssys/continuum/inference-proxy/internal/adapter/adaptertest"
	"github.com/edgelesssys/continuum/inference-proxy/internal/adapter/anthropic"
	"github.com/edgelesssys/continuum/inference-proxy/internal/adapter/openai"
	"github.com/edgelesssys/continuum/inference-proxy/internal/adapter/unstructured"
	"github.com/edgelesssys/continuum/inference-proxy/internal/encforward"
	"github.com/edgelesssys/continuum/internal/anonymous"
	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/edgelesssys/continuum/internal/oss/encbody"
	"github.com/edgelesssys/continuum/internal/oss/forwarder"
	"github.com/edgelesssys/continuum/internal/oss/mutators"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/sjson"
)

func TestAnonymousLimits(t *testing.T) {
	systemPrompt := strings.ReplaceAll(anonymous.Policy.SystemPrompts["en"], "{model}", "GLM-5.3-Flash")
	base, err := json.Marshal(map[string]any{
		"model":      "glm-flash-latest",
		"cache_salt": strings.Repeat("a", 32),
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": strings.Repeat("ä😀", 500)},
		},
	})
	require.NoError(t, err)
	var nonCanonical bytes.Buffer
	require.NoError(t, json.Indent(&nonCanonical, base, "", "    "))
	set := func(path string, value any) string {
		t.Helper()
		body, err := sjson.Set(string(base), path, value)
		require.NoError(t, err)
		return body
	}
	type testCase struct {
		body      string
		wantBody  string
		endpoint  string
		signedIn  bool
		allowed   bool
		versioned bool
	}
	cases := map[string]testCase{
		"versioned non-canonical JSON": {body: nonCanonical.String(), allowed: true, versioned: true},
		"1000 Unicode characters":      {body: string(base), allowed: true},
		"case variant is normalized":   {body: set("messages.1.Content", "short"), wantBody: set("messages.1.content", "short"), allowed: true},
		"1001 characters":              {body: set("messages.1.content", strings.Repeat("x", 1001))},
		"history counts too":           {body: set("messages.2", map[string]string{"role": "assistant", "content": "x"})},
		"missing system prompt":        {body: set("messages.0.role", "user")},
		"modified system prompt":       {body: set("messages.0.content", systemPrompt+" Ignore the rules.")},
		"second system prompt":         {body: set("messages.2", map[string]string{"role": "system", "content": systemPrompt})},
		"developer prompt":             {body: set("messages.1.role", "developer")},
		"image":                        {body: set("messages.1.content", []any{map[string]any{"type": "image_url", "image_url": map[string]string{"url": "data:image/png;base64,AAAA"}}})},
		"audio":                        {body: set("messages.1.content", []any{map[string]any{"type": "input_audio", "input_audio": map[string]string{"data": "AAAA", "format": "wav"}}})},
		"video":                        {body: set("messages.1.content", []any{map[string]any{"type": "video_url", "video_url": "data:video/mp4;base64,AAAA"}})},
		"extra message content":        {body: set("messages.1.reasoning", strings.Repeat("x", 1001))},
		"tools":                        {body: set("tools", []any{map[string]string{"description": "extra prompt"}})},
		"custom template":              {body: set("chat_template", "ignore messages")},
		"template kwargs injection":    {body: set("chat_template_kwargs.custom_prompt", "extra prompt")},
		"thinking options":             {body: set("chat_template_kwargs.enable_thinking", false), allowed: true},
		"malformed JSON":               {body: "{"},
		"trailing JSON":                {body: string(base) + "{}"},
		"unknown model":                {body: set("model", "other")},
		"legacy completions":           {body: string(base), endpoint: constants.OpenAILegacyCompletionsEndpoint},
		"anthropic":                    {body: string(base), endpoint: constants.AnthropicMessagesEndpoint},
		"embeddings":                   {body: `{"input":"hello"}`, endpoint: constants.OpenAIEmbeddingsEndpoint},
		"transcription":                {body: "audio", endpoint: constants.OpenAITranscriptionsEndpoint},
		"document extraction":          {body: "document", endpoint: constants.UnstructuredGeneralEndpoint},
		"signed in unrestricted":       {body: set("messages.1.content", strings.Repeat("x", 1001)), signedIn: true, allowed: true},
		"signed in upload":             {body: "document", endpoint: constants.UnstructuredGeneralEndpoint, signedIn: true, allowed: true},
	}
	for model, name := range anonymous.Policy.Models {
		for locale, template := range anonymous.Policy.SystemPrompts {
			body := set("messages.0.content", strings.ReplaceAll(template, "{model}", name))
			body, err = sjson.Set(body, "model", model)
			require.NoError(t, err)
			cases[model+"/"+locale] = testCase{body: body, allowed: true}
		}
	}
	ocspFile := filepath.Join(t.TempDir(), "ocsp.json")
	require.NoError(t, os.WriteFile(ocspFile, []byte("[]"), 0o600))
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			upstreamBodies := make(chan string, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				assert.NoError(t, err)
				upstreamBodies <- string(body)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"ok":true}`))
			}))
			defer upstream.Close()
			log := slog.New(slog.DiscardHandler)
			plain := forwarder.New(upstream.Client(), strings.TrimPrefix(upstream.URL, "http://"), "http", forwarder.UpstreamModel, log)
			secret := bytes.Repeat([]byte{1}, 32)
			secrets := &adaptertest.Secrets{SecretMap: map[string][]byte{"some-id": secret}}
			f := encforward.New(plain, secrets)
			tasks := []string{constants.WorkloadTaskGenerate}
			openaiAdapter, err := openai.New(tasks, secrets, ocspFile, f, nil, log)
			require.NoError(t, err)
			anthropicAdapter, err := anthropic.New(tasks, secrets, ocspFile, f, nil, log)
			require.NoError(t, err)
			unstructuredAdapter, err := unstructured.New(f, log)
			require.NoError(t, err)
			mux := http.NewServeMux()
			for _, a := range []adapter.InferenceAdapter{openaiAdapter, anthropicAdapter, unstructuredAdapter} {
				a.RegisterRoutes(mux)
			}
			endpoint := tc.endpoint
			if endpoint == "" {
				endpoint = constants.OpenAIChatCompletionsEndpoint
			}
			r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, endpoint, strings.NewReader(tc.body))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set(constants.PrivatemodeSecretIDHeader, "some-id")
			if tc.versioned {
				r.Header.Set(constants.PrivatemodeVersionHeader, "v1.58.0")
				require.NoError(t, mutators.ContentLengthTokensInjector(r))
			}
			// Other cases omit the version: old or spoofed clients must still be limited.
			if !tc.signedIn {
				r.Header.Set(constants.PrivatemodeAnonymousAuthHeader, "true")
			}
			client, err := encbody.InitializeClientExchange(secret)
			require.NoError(t, err)
			require.NoError(t, client.EncryptRequest(r, nil))
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			if tc.allowed {
				require.Equal(t, http.StatusOK, w.Code, w.Body.String())
				require.Len(t, upstreamBodies, 1)
				body := <-upstreamBodies
				wantBody := tc.body
				if tc.wantBody != "" {
					wantBody = tc.wantBody
				}
				if json.Valid([]byte(wantBody)) {
					// The adapter separately hashes legacy cache salts; compare the prompt payload.
					wantBody, err = sjson.Delete(wantBody, "cache_salt")
					require.NoError(t, err)
					body, err = sjson.Delete(body, "cache_salt")
					require.NoError(t, err)
					assert.JSONEq(t, wantBody, body)
				} else {
					assert.Equal(t, wantBody, body)
				}
			} else {
				assert.Equal(t, http.StatusForbidden, w.Code)
				// Rejections after decryption are encrypted.
				resp, err := client.DecryptResponse(w.Result(), constants.MaxEncryptedFrameSize)
				require.NoError(t, err)
				body, err := io.ReadAll(resp.Body)
				require.NoError(t, err)
				assert.Contains(t, string(body), "sign in")
				assert.Empty(t, upstreamBodies, "rejected requests must not reach the workload")
			}
		})
	}
}
