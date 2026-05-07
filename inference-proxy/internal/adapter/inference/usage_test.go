package inference

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/edgelesssys/continuum/internal/oss/sse"
	"github.com/edgelesssys/continuum/internal/oss/usage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(&testLogWriter{t: t}, nil))
}

type testLogWriter struct{ t *testing.T }

func (w *testLogWriter) Write(p []byte) (int, error) {
	w.t.Helper()
	w.t.Log(strings.TrimRight(string(p), "\n"))
	return len(p), nil
}

// mockSSEReader yields a fixed sequence of SSE lines.
type mockSSEReader struct {
	lines []sse.Line
	pos   int
}

func (m *mockSSEReader) Next() (sse.Line, error) {
	if m.pos >= len(m.lines) {
		return sse.Line{}, io.EOF
	}
	l := m.lines[m.pos]
	m.pos++
	return l, nil
}

func dataLine(value string) sse.Line {
	return sse.Line{Type: sse.LineField, Name: sse.FieldData, Value: []byte(value)}
}

func endLine() sse.Line {
	return sse.Line{Type: sse.LineEnd}
}

// testExtractor parses {"n": <int>} and maps it to PromptTokens.
func testExtractor(body []byte) (usage.Stats, error) {
	var parsed struct {
		N *int64 `json:"n"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return usage.Stats{}, err
	}
	if parsed.N == nil {
		return usage.Stats{}, errors.New("no usage")
	}
	return usage.Stats{PromptTokens: *parsed.N}, nil
}

func TestUsageSSEReader(t *testing.T) {
	testCases := map[string]struct {
		lines     []sse.Line
		wantUsage usage.Stats
	}{
		"empty stream": {
			lines:     nil,
			wantUsage: usage.Stats{},
		},
		"optimistic single-line parse": {
			lines: []sse.Line{
				dataLine(`{"n":7}`),
				endLine(),
			},
			wantUsage: usage.Stats{PromptTokens: 7},
		},
		"multi-line accumulation": {
			lines: []sse.Line{
				dataLine(`{"n"`),
				dataLine(`:5}`),
				endLine(),
			},
			wantUsage: usage.Stats{PromptTokens: 5},
		},
		"later event overwrites earlier usage": {
			lines: []sse.Line{
				dataLine(`{"n":1}`),
				endLine(),
				dataLine(`{"n":2}`),
				endLine(),
			},
			wantUsage: usage.Stats{PromptTokens: 2},
		},
		"no extractable usage": {
			lines: []sse.Line{
				dataLine(`not json`),
				endLine(),
			},
			wantUsage: usage.Stats{},
		},
		"event without usage preserves previous usage": {
			lines: []sse.Line{
				dataLine(`{"n":6}`),
				endLine(),
				dataLine(`no usage here`),
				endLine(),
			},
			wantUsage: usage.Stats{PromptTokens: 6},
		},
		"event without usage preserves later usage": {
			lines: []sse.Line{
				dataLine(`no usage here`),
				endLine(),
				dataLine(`{"n":6}`),
				endLine(),
			},
			wantUsage: usage.Stats{PromptTokens: 6},
		},
		"optimistic parse + erroneous line does not cause an error": {
			lines: []sse.Line{
				dataLine(`{"n":4}`),
				dataLine(`erroneous second line`),
				endLine(),
			},
			// The result is really an implementation detail
			wantUsage: usage.Stats{PromptTokens: 4},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			mock := &mockSSEReader{lines: tc.lines}
			reader := NewUsageSSEReader(mock, testExtractor, testLogger(t))

			for {
				_, err := reader.Next()
				if errors.Is(err, io.EOF) {
					break
				}
				require.NoError(t, err)
			}

			assert.Equal(t, tc.wantUsage, reader.LatestUsage())
		})
	}
}
