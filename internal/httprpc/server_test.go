package httprpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/edgelesssys/continuum/internal/oss/logging"
	"github.com/edgelesssys/continuum/internal/oss/requestid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type echoReq struct {
	Value string `json:"value"`
}

type echoResp struct {
	Echo string `json:"echo"`
}

func TestMakeHandlerTransportValidation(t *testing.T) {
	bigBody := `{"value":"` + strings.Repeat("x", maxBodyBytes+1) + `"}`

	tests := map[string]struct {
		method      string
		contentType string
		body        string
		wantStatus  int
		wantErrCode string
	}{
		"accepts POST with JSON": {
			method:      http.MethodPost,
			contentType: "application/json",
			body:        `{"value":"hello"}`,
			wantStatus:  http.StatusOK,
		},
		"rejects GET": {
			method:      http.MethodGet,
			contentType: "application/json",
			body:        `{}`,
			wantStatus:  http.StatusMethodNotAllowed,
			wantErrCode: "httprpc.MethodNotAllowed",
		},
		"rejects non-JSON content type": {
			method:      http.MethodPost,
			contentType: "text/plain",
			body:        `...`,
			wantStatus:  http.StatusUnsupportedMediaType,
			wantErrCode: "httprpc.UnsupportedMediaType",
		},
		"rejects malformed JSON": {
			method:      http.MethodPost,
			contentType: "application/json",
			body:        `{not json`,
			wantStatus:  http.StatusBadRequest,
			wantErrCode: "httprpc.InvalidRequest",
		},
		"rejects multiple JSON values": {
			method:      http.MethodPost,
			contentType: "application/json",
			body:        `{"value":"a"}{"value":"b"}`,
			wantStatus:  http.StatusBadRequest,
			wantErrCode: "httprpc.InvalidRequest",
		},
		"rejects unknown fields": {
			method:      http.MethodPost,
			contentType: "application/json",
			body:        `{"value":"a","unknown":"b"}`,
			wantStatus:  http.StatusBadRequest,
			wantErrCode: "httprpc.InvalidRequest",
		},
		"rejects body exceeding max size": {
			method:      http.MethodPost,
			contentType: "application/json",
			body:        bigBody,
			wantStatus:  http.StatusRequestEntityTooLarge,
			wantErrCode: "httprpc.RequestEntityTooLarge",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			assert := assert.New(t)

			echoFn := func(_ context.Context, req echoReq) (echoResp, error) {
				return echoResp{Echo: req.Value}, nil
			}

			handler := MakeHandler(slog.Default(), echoFn)
			req := httptest.NewRequestWithContext(context.Background(), tc.method, "/test", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", tc.contentType)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			require.Equal(tc.wantStatus, rec.Code)
			assert.Equal("application/json", rec.Header().Get("Content-Type"))
			assert.Equal("nosniff", rec.Header().Get("X-Content-Type-Options"))
			assert.NotEmpty(rec.Header().Get(requestid.Header))

			if tc.wantStatus == http.StatusOK {
				assert.JSONEq(`{"echo":"hello"}`, rec.Body.String())
			} else if tc.wantErrCode != "" {
				var errResp ErrorResponse
				require.NoError(json.Unmarshal(rec.Body.Bytes(), &errResp))
				assert.Equal(tc.wantErrCode, errResp.Code)
			}
		})
	}
}

func TestMakeHandlerRPCExecution(t *testing.T) {
	baseErr := errors.New("underlying error")

	tests := map[string]struct {
		fn            func(context.Context, echoReq) (echoResp, error)
		wantStatus    int
		wantResp      string
		wantErrorResp ErrorResponse
	}{
		"success": {
			fn:         func(_ context.Context, req echoReq) (echoResp, error) { return echoResp{Echo: req.Value}, nil },
			wantStatus: http.StatusOK,
			wantResp:   `{"echo":"hello"}`,
		},
		"client fault FuncError": {
			fn: func(_ context.Context, _ echoReq) (echoResp, error) {
				return echoResp{}, &FuncError{ClientMessage: "bad request", Code: "test.Bad", ServerFault: false, Err: baseErr}
			},
			wantStatus:    http.StatusBadRequest,
			wantErrorResp: ErrorResponse{Message: "bad request", Code: "test.Bad", ServerFault: false},
		},
		"server fault FuncError": {
			fn: func(_ context.Context, _ echoReq) (echoResp, error) {
				return echoResp{}, &FuncError{ClientMessage: "server error", Code: "test.Internal", ServerFault: true, Err: baseErr}
			},
			wantStatus:    http.StatusInternalServerError,
			wantErrorResp: ErrorResponse{Message: "server error", Code: "test.Internal", ServerFault: true},
		},
		"wrapped FuncError": {
			fn: func(_ context.Context, _ echoReq) (echoResp, error) {
				funcErr := &FuncError{ClientMessage: "wrapped error", Code: "test.Wrapped", ServerFault: false, Err: baseErr}
				return echoResp{}, fmt.Errorf("additional context: %w", funcErr)
			},
			wantStatus:    http.StatusBadRequest,
			wantErrorResp: ErrorResponse{Message: "wrapped error", Code: "test.Wrapped", ServerFault: false},
		},
		"non FuncError returns internal": {
			fn: func(_ context.Context, _ echoReq) (echoResp, error) {
				return echoResp{}, errors.New("raw error")
			},
			wantStatus:    http.StatusInternalServerError,
			wantErrorResp: ErrorResponse{Message: "internal server error", Code: "httprpc.Internal", ServerFault: true},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			assert := assert.New(t)

			handler := MakeHandler(slog.Default(), tc.fn)

			req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/test", strings.NewReader(`{"value":"hello"}`))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			require.Equal(tc.wantStatus, rec.Code)

			if tc.wantStatus == http.StatusOK {
				assert.JSONEq(tc.wantResp, rec.Body.String())
			} else {
				var errResp ErrorResponse
				require.NoError(json.Unmarshal(rec.Body.Bytes(), &errResp))
				assert.Equal(tc.wantErrorResp, errResp)
			}
		})
	}
}

func TestMakeHandlerContextPropagation(t *testing.T) {
	var capturedCtx context.Context
	fn := func(ctx context.Context, req echoReq) (echoResp, error) {
		capturedCtx = ctx
		return echoResp{Echo: req.Value}, nil
	}

	handler := MakeHandler(slog.Default(), fn)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/test", strings.NewReader(`{"value":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	reqID, ok := requestid.FromContext(capturedCtx)
	require.True(t, ok, "request ID should be in context")
	assert.NotEmpty(t, reqID)

	assert.Equal(t, reqID, rec.Header().Get(requestid.Header))

	ctxLogger := logging.FromContext(capturedCtx)
	assert.NotNil(t, ctxLogger)
}

func TestFuncErrorUnwrap(t *testing.T) {
	inner := errors.New("inner error")
	fe := &FuncError{ClientMessage: "problem occurred", Err: inner}
	assert.ErrorIs(t, fe, inner)
}
