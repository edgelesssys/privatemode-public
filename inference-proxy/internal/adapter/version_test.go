package adapter_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/edgelesssys/continuum/inference-proxy/internal/adapter/adaptertest"
	"github.com/edgelesssys/continuum/inference-proxy/internal/adapter/anthropic"
	"github.com/edgelesssys/continuum/inference-proxy/internal/adapter/openai"
	"github.com/edgelesssys/continuum/inference-proxy/internal/encforward"
	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/edgelesssys/continuum/internal/oss/encbody"
	"github.com/edgelesssys/continuum/internal/oss/forwarder"
	"github.com/edgelesssys/continuum/internal/oss/mutators"
	"github.com/edgelesssys/continuum/internal/oss/ocspheader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Exercise version-dependent behavior through real encrypted exchanges, including
// the forwarder's request cloning and the response's use of decrypted metadata.
func TestAuthenticatedClientVersion(t *testing.T) {
	const salt = "Jp3c/Hn9Q2tWkx8aVb4ZrL6mYe0sTfUg1iOqP7dNwE5="
	digest := sha256.Sum256([]byte(salt))
	hashedSalt := base64.URLEncoding.EncodeToString(digest[:])
	policy, err := ocspheader.NewHeader([]ocspheader.AllowStatus{ocspheader.AllowStatusGood}, time.Time{}).Marshal()
	require.NoError(t, err)
	ocspFile := filepath.Join(t.TempDir(), "ocsp.json")
	require.NoError(t, os.WriteFile(ocspFile, []byte("[]"), 0o600))

	for name, tc := range map[string]struct {
		outerVersion     string
		innerVersion     *string
		modern           bool
		wantVersionError bool
	}{
		"authenticated newer":                             {outerVersion: "v1.46.0", innerVersion: new("v1.63.0"), modern: true},
		"authenticated older":                             {outerVersion: "v1.63.0", innerVersion: new("v1.46.0")},
		"no outer version":                                {innerVersion: new("v1.63.0"), modern: true},
		"fallback v1.61":                                  {outerVersion: "v1.61.9", modern: true},
		"fallback v1.62":                                  {outerVersion: "v1.62.0", modern: true},
		"fallback v1.62 patch":                            {outerVersion: "v1.62.9", modern: true},
		"fallback short v1.62":                            {outerVersion: "v1.62", modern: true},
		"fallback v1.62 prerelease":                       {outerVersion: "v1.62.0-pre", modern: true},
		"v1.63 requires authenticated version":            {outerVersion: "v1.63.0", wantVersionError: true},
		"short v1.63 requires authenticated version":      {outerVersion: "v1.63", wantVersionError: true},
		"v1.63 prerelease requires authenticated version": {outerVersion: "v1.63.0-pre", wantVersionError: true},
		"newer minor requires authenticated version":      {outerVersion: "v1.64.0", wantVersionError: true},
		"newer major requires authenticated version":      {outerVersion: "v2.0.0", wantVersionError: true},
		"fallback older":                                  {outerVersion: "v1.46.0"},
		"neither version":                                 {},
		"invalid HTTP version":                            {outerVersion: "invalid"},
		"empty authenticated":                             {outerVersion: "v1.63.0", innerVersion: new("")},
		"invalid authenticated":                           {outerVersion: "v1.63.0", innerVersion: new("invalid")},
	} {
		for _, endpoint := range []string{
			constants.OpenAIChatCompletionsEndpoint,
			constants.AnthropicMessagesEndpoint,
			constants.OpenAITranscriptionsEndpoint,
			constants.OpenAIEmbeddingsEndpoint,
		} {
			for _, mismatch := range []bool{false, true} {
				suffix := "/matching tokens"
				if mismatch {
					suffix = "/mismatched tokens"
				}
				t.Run(name+endpoint+suffix, func(t *testing.T) {
					upstreamBodies := make(chan []byte, 1)
					upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						body, err := io.ReadAll(r.Body)
						assert.NoError(t, err)
						upstreamBodies <- body
						w.Header().Set("Content-Type", "application/json")
						_, _ = w.Write([]byte(`{"duration":42.3}`))
					}))
					defer upstream.Close()
					log := slog.New(slog.DiscardHandler)
					secret := bytes.Repeat([]byte{1}, 32)
					secrets := &adaptertest.Secrets{SecretMap: map[string][]byte{"test": secret}}
					plain := forwarder.New(upstream.Client(), upstream.Listener.Addr().String(), forwarder.SchemeHTTP, forwarder.UpstreamModel, log)
					f := encforward.New(plain, secrets)
					tasks := []string{constants.WorkloadTaskGenerate}
					openaiAdapter, err := openai.New(tasks, secrets, ocspFile, f, nil, log)
					require.NoError(t, err)
					anthropicAdapter, err := anthropic.New(tasks, secrets, ocspFile, f, nil, log)
					require.NoError(t, err)
					mux := http.NewServeMux()
					openaiAdapter.RegisterRoutes(mux)
					anthropicAdapter.RegisterRoutes(mux)

					body := `{"model":"test","cache_salt":"` + salt + `","messages":[{"role":"user","content":"abcdefgh"}],"input":"abcdefgh"}`
					contentType := "application/json"
					if endpoint == constants.OpenAITranscriptionsEndpoint {
						var form bytes.Buffer
						writer := multipart.NewWriter(&form)
						require.NoError(t, writer.WriteField("model", "test"))
						require.NoError(t, writer.Close())
						body, contentType = form.String(), writer.FormDataContentType()
					}
					r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, endpoint, strings.NewReader(body))
					r.Header.Set("Content-Type", contentType)
					r.Header.Set(constants.PrivatemodeSecretIDHeader, "test")
					r.Header.Set(constants.PrivatemodeVersionHeader, tc.outerVersion)
					require.NoError(t, mutators.ContentLengthTokensInjector(r))
					if mismatch {
						r.Header.Set(constants.PrivatemodeContentLengthTokensHeader, "999999")
					}
					headers := map[string]string{constants.PrivatemodeNvidiaOCSPPolicyHeader: policy}
					if tc.innerVersion != nil {
						headers[constants.PrivatemodeVersionHeader] = *tc.innerVersion
					}
					client, err := encbody.InitializeClientExchange(secret)
					require.NoError(t, err)
					require.NoError(t, client.EncryptRequest(r, headers))
					w := httptest.NewRecorder()
					mux.ServeHTTP(w, r)
					response, err := client.DecryptResponse(w.Result(), constants.MaxEncryptedFrameSize)
					require.NoError(t, err)
					defer response.Body.Close()
					responseBody, err := io.ReadAll(response.Body)
					require.NoError(t, err)
					if tc.wantVersionError {
						require.Equal(t, http.StatusBadRequest, w.Code, string(responseBody))
						assert.Contains(t, string(responseBody), "missing authenticated client version")
						assert.Empty(t, upstreamBodies)
						return
					}
					if mismatch && tc.modern {
						require.Equal(t, http.StatusBadRequest, w.Code, string(responseBody))
						assert.Contains(t, string(responseBody), "does not match the estimated inference usage")
						assert.Empty(t, upstreamBodies)
						return
					}
					require.Equal(t, http.StatusOK, w.Code, string(responseBody))
					require.Len(t, upstreamBodies, 1)
					forwardedBody := <-upstreamBodies
					switch endpoint {
					case constants.OpenAIChatCompletionsEndpoint, constants.AnthropicMessagesEndpoint:
						wantSalt := hashedSalt
						if tc.modern {
							wantSalt = salt
						}
						assert.Equal(t, wantSalt, gjson.GetBytes(forwardedBody, "cache_salt").String())
					case constants.OpenAITranscriptionsEndpoint:
						want := `{"duration":"42.3"}`
						if tc.modern {
							want = `{"duration":42.3}`
						}
						assert.JSONEq(t, want, string(responseBody))
					}
				})
			}
		}
	}
}
