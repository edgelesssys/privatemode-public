package forwarder

import (
	"bufio"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestForwardStreaming(t *testing.T) {
	assert := assert.New(t)

	mutator := &stubMutator{
		mutateResponse: `"plainText"`,
	}
	responseMutator := WithFullJSONResponseMutation(mutator.mutate, nil)

	stubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")

		for range 20 {
			_, _ = w.Write([]byte("data: {\"field\": \"encryptedData\"}\n\n"))
			time.Sleep(5 * time.Millisecond)
		}
	}))
	defer stubServer.Close()

	forwarder := New("tcp", stubServer.Listener.Addr().String(), slog.Default())

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	resp := httptest.NewRecorder()

	forwarder.Forward(
		resp,
		req,
		NoRequestMutation,
		responseMutator,
		NoHeaderMutation,
	)

	assert.Equal(http.StatusOK, resp.Code)
	assert.Equal("text/event-stream", resp.Header().Get("Content-Type"))
	var expectedResponse string
	for range 20 {
		expectedResponse += `data: {"field": "plainText"}` + "\n\n"
	}
	assert.Equal(expectedResponse, resp.Body.String())
}

func TestForwardNonStreaming(t *testing.T) {
	assert := assert.New(t)

	mutator := &stubMutator{
		mutateResponse: `"plainText"`,
	}
	responseMutator := WithFullJSONResponseMutation(mutator.mutate, nil)

	stubServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var encryptedData string
		for range bufio.MaxScanTokenSize {
			encryptedData += "AB"
		}
		_, _ = w.Write(fmt.Appendf(nil, "{\"field\": \"%s\"}", encryptedData))
	}))
	defer stubServer.Close()

	forwarder := New("tcp", stubServer.Listener.Addr().String(), slog.Default())

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	resp := httptest.NewRecorder()

	forwarder.Forward(
		resp,
		req,
		NoRequestMutation,
		responseMutator,
		NoHeaderMutation,
	)

	assert.Equal(http.StatusOK, resp.Code)
	assert.Equal("application/json", resp.Header().Get("Content-Type"))

	expectedResponse := `{"field": "plainText"}`
	assert.Equal(expectedResponse, resp.Body.String())
}
