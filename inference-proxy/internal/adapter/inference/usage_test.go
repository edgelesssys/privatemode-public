package inference

import (
	"encoding/json"
	"errors"
	"io"
	"testing"

	"github.com/edgelesssys/continuum/internal/oss/sse"
	"github.com/edgelesssys/continuum/internal/oss/usage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockSSEReader yields a fixed sequence of SSE events.
type mockSSEReader struct {
	events []sse.Event
	pos    int
}

func (m *mockSSEReader) Next() (sse.Event, error) {
	if m.pos >= len(m.events) {
		return sse.Event{}, io.EOF
	}
	e := m.events[m.pos]
	m.pos++
	return e, nil
}

func events(raws ...string) []sse.Event {
	parsed := make([]sse.Event, 0, len(raws))
	for _, raw := range raws {
		parsed = append(parsed, sse.ParseEvent([]byte(raw)))
	}
	return parsed
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
		events    []sse.Event
		wantUsage usage.Stats
	}{
		"empty stream": {
			events:    nil,
			wantUsage: usage.Stats{},
		},
		"single data line": {
			events:    events("data: {\"n\":7}\n\n"),
			wantUsage: usage.Stats{PromptTokens: 7},
		},
		"later event overwrites earlier usage": {
			events:    events("data: {\"n\":1}\n\n", "data: {\"n\":2}\n\n"),
			wantUsage: usage.Stats{PromptTokens: 2},
		},
		"no extractable usage": {
			events:    events("data: not json\n\n"),
			wantUsage: usage.Stats{},
		},
		"events without usage are ignored": {
			events:    events("data: no usage\n\n", "data: {\"n\":6}\n\n", "data: no usage\n\n"),
			wantUsage: usage.Stats{PromptTokens: 6},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			mock := &mockSSEReader{events: tc.events}
			reader := NewUsageSSEReader(mock, testExtractor)

			var got []sse.Event
			for {
				event, err := reader.Next()
				if errors.Is(err, io.EOF) {
					break
				}
				require.NoError(t, err)
				got = append(got, event)
			}

			assert.Equal(t, tc.events, got)
			assert.Equal(t, tc.wantUsage, reader.LatestUsage())
		})
	}
}
