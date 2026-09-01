package health

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHandler(t *testing.T) {
	s := New(slog.Default())

	resp := httptest.NewRecorder()
	s.handler(resp, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody))
	assert.Equal(t, http.StatusOK, resp.Code)

	s.SetHealthy(false)
	resp = httptest.NewRecorder()
	s.handler(resp, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody))
	assert.Equal(t, http.StatusServiceUnavailable, resp.Code)

	s.SetHealthy(true)
	resp = httptest.NewRecorder()
	s.handler(resp, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody))
	assert.Equal(t, http.StatusOK, resp.Code)
}
