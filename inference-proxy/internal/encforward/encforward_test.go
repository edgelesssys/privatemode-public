package encforward

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/edgelesssys/continuum/inference-proxy/internal/secrets"
	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/edgelesssys/continuum/internal/oss/encbody"
	"github.com/edgelesssys/continuum/internal/oss/forwarder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestForwardEncrypted(t *testing.T) {
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
			f := New(plainForwarder, &fakeSecretGetter{secret: secret})

			postDecryptCalled := false
			postDecrypt := func(*http.Request) error {
				postDecryptCalled = true
				return nil
			}

			preEncrypt := func(*http.Response) (forwarder.Response, error) {
				return tc.mappedResponse, nil
			}

			f.ForwardEncrypted(httptest.NewRecorder(), r, postDecrypt, preEncrypt)
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
			secretErr:    secrets.ErrSecretNotFound,
			expectedBody: constants.ErrorNoSecretForID,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)

			plainForwarder := &fakePlainForwarder{}
			f := New(plainForwarder, &fakeSecretGetter{secretErr: tc.secretErr})

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
				forwarder.NoRequestMutation,
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

type fakeSecretGetter struct {
	secret    []byte
	secretErr error
}

func (f *fakeSecretGetter) Get(context.Context, string) ([]byte, error) {
	return f.secret, f.secretErr
}
