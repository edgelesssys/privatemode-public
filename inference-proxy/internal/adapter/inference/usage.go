package inference

import (
	"errors"

	"github.com/edgelesssys/continuum/internal/oss/sse"
	"github.com/edgelesssys/continuum/internal/oss/usage"
)

// ErrNoUsage is returned by [UsageExtractor] if a body is valid but contains no extractable usage.
var ErrNoUsage = errors.New("no usage in response")

// UsageExtractor extracts usage stats from a JSON body.
// Returns [ErrNoUsage] if the body does not contain extractable usage.
type UsageExtractor func(body []byte) (usage.Stats, error)

// UsageSSEReader extracts usage stats "online" from an SSE stream and yields events unchanged.
type UsageSSEReader struct {
	sse          sseEventReader
	extractUsage UsageExtractor

	latestUsage usage.Stats
}

var _ sseEventReader = (*UsageSSEReader)(nil)

// NewUsageSSEReader creates a new UsageSSEReader.
func NewUsageSSEReader(sseReader sseEventReader, extractUsage UsageExtractor) *UsageSSEReader {
	return &UsageSSEReader{
		sse:          sseReader,
		extractUsage: extractUsage,
	}
}

// Next returns the next SSE event while extracting usage stats as a side effect.
// Errors are only returned from the underlying sseReader, parsing errors are ignored.
// [io.EOF] marks the end of the stream.
func (r *UsageSSEReader) Next() (sse.Event, error) {
	event, err := r.sse.Next()
	if err != nil {
		return event, err
	}

	if data := event.FullData(); data != nil {
		if stats, err := r.extractUsage(data); err == nil {
			r.latestUsage = stats
		}
	}

	return event, nil
}

// LatestUsage returns the last successfully extracted usage stats.
// If no stats have been extracted yet, it returns the zero value.
func (r *UsageSSEReader) LatestUsage() usage.Stats {
	return r.latestUsage
}
