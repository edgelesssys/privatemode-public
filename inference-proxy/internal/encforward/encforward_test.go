package encforward

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/edgelesssys/continuum/inference-proxy/internal/cipher"
	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/edgelesssys/continuum/internal/oss/encbody"
	"github.com/edgelesssys/continuum/internal/oss/forwarder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestForwardEncryptedLegacy(t *testing.T) {
	cases := map[string]forwarder.Response{
		"unary": &forwarder.UnaryResponse{
			StatusCode: http.StatusOK,
			Header:     http.Header{},
			Body:       []byte(`{"data":"plain"}`),
		},
		"streaming": &forwarder.StreamingResponse{
			StatusCode: http.StatusOK,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader(`{"data":"plain"}`)),
		},
	}

	for name, mappedResponse := range cases {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			assert := assert.New(t)

			plainForwarder := &fakePlainForwarder{}
			f := New(plainForwarder, &fakeCipherCreator{})

			decryptCalled := false
			legacyDecrypt := func(cipher.ResponseCipher) forwarder.RequestMutator {
				return func(*http.Request) error {
					decryptCalled = true
					return nil
				}
			}
			postDecryptCalled := false
			postDecrypt := func(*http.Request) error {
				postDecryptCalled = true
				return nil
			}

			r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/chat/completions", http.NoBody)
			f.ForwardEncrypted(httptest.NewRecorder(), r, legacyDecrypt, postDecrypt, nil,
				func(*http.Response) (forwarder.Response, error) { return mappedResponse, nil })

			// Run request mutator, as [forwarder.Forwarder] would
			require.NoError(plainForwarder.reqMutator(r))
			assert.True(decryptCalled)
			assert.True(postDecryptCalled)

			// Run response mapper, as [forwarder.Forwarder] would
			out, err := plainForwarder.mapper(&http.Response{Header: http.Header{}})
			require.NoError(err)
			require.IsType(mappedResponse, out)
			body, err := io.ReadAll(responseBody(t, out))
			require.NoError(err)
			assert.Contains(string(body), "ENCRYPTED")
		})
	}
}

func TestForwardEncryptedFBE(t *testing.T) {
	unaryResponse := []byte(`{"answer":42}`)
	sseResponse := []byte("data: hello\n\ndata: world\n\n")
	cases := map[string]struct {
		mappedResponse    forwarder.Response
		expectedPlaintext []byte
	}{
		"unary": {
			mappedResponse: &forwarder.UnaryResponse{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"application/json"},
				},
				Body: unaryResponse,
			},
			expectedPlaintext: unaryResponse,
		},
		"streaming": {
			mappedResponse: &forwarder.StreamingResponse{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"text/event-stream"},
				},
				Body: io.NopCloser(bytes.NewReader(sseResponse)),
			},
			expectedPlaintext: sseResponse,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			assert := assert.New(t)

			secret := bytes.Repeat([]byte{0x01}, 32)
			r, client := buildEncryptedRequest(t, secret, `{"model":"gpt"}`)

			plainForwarder := &fakePlainForwarder{}
			f := New(plainForwarder, &fakeCipherCreator{secret: secret})

			postDecryptCalled := false
			postDecrypt := func(*http.Request) error {
				postDecryptCalled = true
				return nil
			}

			preEncrypt := func(*http.Response) (forwarder.Response, error) {
				return tc.mappedResponse, nil
			}

			failIfCalled := func(cipher.ResponseCipher) forwarder.RequestMutator {
				t.Fatal("legacy scheme selected; FBE path expected")
				return nil
			}

			f.ForwardEncrypted(
				httptest.NewRecorder(), r,
				failIfCalled, // legacyDecryptMutator should not be called
				postDecrypt,
				nil,
				preEncrypt,
			)
			require.True(plainForwarder.called)

			// Run request mutator, as [forwarder.Forwarder] would
			require.NoError(plainForwarder.reqMutator(r))
			assert.True(postDecryptCalled)
			plainReq, err := io.ReadAll(r.Body)
			require.NoError(err)
			assert.JSONEq(`{"model":"gpt"}`, string(plainReq))

			// Run response mapper, as [forwarder.Forwarder] would
			out, err := plainForwarder.mapper(&http.Response{Header: http.Header{}})
			require.NoError(err)

			httpResp := &http.Response{Header: out.GetHeader().Clone(), Body: responseBody(t, out)}
			ds, err := client.DecryptResponse(httpResp, constants.MaxEncryptedFrameSize)
			require.NoError(err)
			plainResp, err := io.ReadAll(ds.Body)
			require.NoError(err)
			require.NoError(ds.Body.Close())
			assert.Equal(tc.expectedPlaintext, plainResp)
		})
	}
}

func TestForwardEncryptedValidation(t *testing.T) {
	testCases := map[string]struct {
		encrypted    bool
		secretID     string
		secretErr    error
		expectedBody string
	}{
		"not encrypted": {
			encrypted:    false,
			expectedBody: "request must be encrypted",
		},
		"missing secret ID": {
			encrypted:    true,
			secretID:     "",
			expectedBody: "request must contain secret ID",
		},
		"secret lookup fails": {
			encrypted:    true,
			secretID:     "some-id",
			secretErr:    fmt.Errorf("%s %q", constants.ErrorNoSecretForID, "some-id"),
			expectedBody: constants.ErrorNoSecretForID,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)

			plainForwarder := &fakePlainForwarder{}
			f := New(plainForwarder, &fakeCipherCreator{secretErr: tc.secretErr})

			r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/chat/completions", http.NoBody)
			r.Header.Set(constants.PrivatemodeVersionHeader, constants.Version())
			if tc.encrypted {
				r.Header.Set("Content-Type", encbody.ContentType)
			}
			if tc.secretID != "" {
				r.Header.Set(constants.PrivatemodeSecretIDHeader, tc.secretID)
			}

			w := httptest.NewRecorder()
			f.ForwardEncrypted(
				w, r,
				func(cipher.ResponseCipher) forwarder.RequestMutator { return forwarder.NoRequestMutation },
				forwarder.NoRequestMutation,
				nil,
				func(*http.Response) (forwarder.Response, error) { return nil, nil },
			)

			assert.Equal(http.StatusBadRequest, w.Code)
			assert.Contains(w.Body.String(), tc.expectedBody)
			assert.False(plainForwarder.called, "plain forwarder must not be called when preflight fails")
		})
	}
}

// buildEncryptedRequest builds an encrypted request together with the client exchange that can
// decrypt its response.
func buildEncryptedRequest(t *testing.T, secret []byte, body string) (*http.Request, *encbody.ClientExchange) {
	t.Helper()
	client, err := encbody.InitializeClientExchange(secret)
	require.NoError(t, err)

	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set(constants.PrivatemodeVersionHeader, constants.Version())
	r.Header.Set(constants.PrivatemodeSecretIDHeader, "some-id")
	require.NoError(t, client.EncryptRequest(r))

	return r, client
}

// responseBody extracts a body for [http.Response] from a [forwarder.Response].
func responseBody(t *testing.T, resp forwarder.Response) io.ReadCloser {
	t.Helper()
	switch r := resp.(type) {
	case *forwarder.UnaryResponse:
		return io.NopCloser(bytes.NewReader(r.Body))
	case *forwarder.StreamingResponse:
		return r.Body
	default:
		t.Fatalf("unexpected response type %T", resp)
		return nil
	}
}

type fakePlainForwarder struct {
	called     bool
	reqMutator forwarder.RequestMutator
	mapper     forwarder.ResponseMapper
}

func (f *fakePlainForwarder) Forward(
	_ http.ResponseWriter,
	_ *http.Request,
	rm forwarder.RequestMutator,
	mapper forwarder.ResponseMapper,
	_ ...forwarder.Opts,
) {
	f.called = true
	f.reqMutator = rm
	f.mapper = mapper
}

type fakeCipherCreator struct {
	secret    []byte
	secretErr error
}

func (f *fakeCipherCreator) NewResponseCipher() cipher.ResponseCipher {
	return markerCipher{}
}

func (f *fakeCipherCreator) Secret(context.Context, string) ([]byte, error) {
	return f.secret, f.secretErr
}

// markerCipher encrypts every field to the constant "ENCRYPTED" and decrypts to the identity.
type markerCipher struct{}

func (markerCipher) DecryptRequest(context.Context) func(string) (string, error) {
	return func(in string) (string, error) { return in, nil }
}

func (markerCipher) EncryptResponse(context.Context) func(string) (string, error) {
	return func(string) (string, error) { return `"ENCRYPTED"`, nil }
}
