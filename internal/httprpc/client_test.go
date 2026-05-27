package httprpc

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientCall(t *testing.T) {
	tests := map[string]struct {
		setupHandler    func(t *testing.T, w http.ResponseWriter, r *http.Request)
		wantErr         bool
		wantResp        echoResp
		wantErrCode     string
		wantServerFault bool
		wantErrMsgPart  string
	}{
		"successful call": {
			setupHandler: func(t *testing.T, w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
				assert.Equal(t, "application/json", r.Header.Get("Accept"))

				var req echoReq
				require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
				assert.Equal(t, "hello", req.Value)

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				require.NoError(t, json.NewEncoder(w).Encode(echoResp{Echo: req.Value}))
			},
			wantErr:  false,
			wantResp: echoResp{Echo: "hello"},
		},
		"404 not found": {
			setupHandler: func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				_, err := w.Write([]byte("not found"))
				require.NoError(t, err)
			},
			wantErr:         true,
			wantServerFault: false,
			wantErrMsgPart:  "not found",
		},
		"RPC client fault error": {
			setupHandler: func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				require.NoError(t, json.NewEncoder(w).Encode(ErrorResponse{Message: "bad request", Code: "test.Bad"}))
			},
			wantErr:         true,
			wantErrCode:     "test.Bad",
			wantServerFault: false,
			wantErrMsgPart:  "bad request",
		},
		"RPC server fault error": {
			setupHandler: func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				require.NoError(t, json.NewEncoder(w).Encode(ErrorResponse{Message: "server failed", Code: "test.Fail", ServerFault: true}))
			},
			wantErr:         true,
			wantErrCode:     "test.Fail",
			wantServerFault: true,
			wantErrMsgPart:  "server failed",
		},
		"Non-JSON 4xx": {
			setupHandler: func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusForbidden)
				_, err := w.Write([]byte("plain text error"))
				require.NoError(t, err)
			},
			wantErr:         true,
			wantServerFault: false,
			wantErrMsgPart:  "plain text error",
		},
		"Non-JSON 5xx": {
			setupHandler: func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadGateway)
				_, err := w.Write([]byte("plain text error"))
				require.NoError(t, err)
			},
			wantErr:         true,
			wantServerFault: true,
			wantErrMsgPart:  "plain text error",
		},
		"success with non-JSON content type": {
			setupHandler: func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/plain")
				w.WriteHeader(http.StatusOK)
				_, err := w.Write([]byte(`{"echo":"hello"}`))
				require.NoError(t, err)
			},
			wantErr:        true,
			wantErrMsgPart: "unexpected content type",
		},
		"success with invalid JSON": {
			setupHandler: func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, err := w.Write([]byte(`{invalid`))
				require.NoError(t, err)
			},
			wantErr:        true,
			wantErrMsgPart: "decoding response",
		},
		"response body too large": {
			setupHandler: func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				// Write more than maxBodyBytes
				_, err := w.Write(make([]byte, maxBodyBytes+1))
				require.NoError(t, err)
			},
			wantErr:        true,
			wantErrMsgPart: "reading response body",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			assert := assert.New(t)

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				tc.setupHandler(t, w, r)
			}))
			defer server.Close()

			client, err := NewClient(server.URL, http.DefaultClient)
			require.NoError(err)
			resp, err := ClientCall[echoReq, echoResp](context.Background(), client, "/test", echoReq{Value: "hello"})

			if tc.wantErr {
				require.Error(err)
				assert.Contains(err.Error(), tc.wantErrMsgPart)

				if respErr, ok := errors.AsType[*ResponseError](err); ok {
					assert.Equal(tc.wantErrCode, respErr.Code)
					assert.Equal(tc.wantServerFault, respErr.ServerFault)
				}
			} else {
				require.NoError(err)
				assert.Equal(tc.wantResp, resp)
			}
		})
	}
}
