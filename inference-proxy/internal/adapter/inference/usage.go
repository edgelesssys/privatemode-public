package inference

import (
	"errors"
	"log/slog"

	"github.com/edgelesssys/continuum/internal/oss/sse"
	"github.com/edgelesssys/continuum/internal/oss/usage"
)

// ErrNoUsage is returned by [UsageExtractor] if a body is valid but contains no extractable usage.
var ErrNoUsage = errors.New("no usage in response")

// UsageExtractor extracts usage stats from a JSON body.
// Returns [ErrNoUsage] if the body does not contain extractable usage.
type UsageExtractor func(body []byte) (usage.Stats, error)

type sseReader interface {
	Next() (sse.Line, error)
}

// UsageSSEReader extracts usage stats "online" from an SSE stream and yields lines unchanged.
//
// It performs optimistic parsing of the first line of an event (which is sufficient for vLLM),
// and only buffers data internally if that fails. There are theoretical edge cases which lead to
// incorrect results.
type UsageSSEReader struct {
	sse          sseReader
	log          *slog.Logger
	extractUsage UsageExtractor

	// Cross-event state:

	latestUsage usage.Stats

	// Per-event state:

	// firstLineParsed is true when the first line in the current event was successfully parsed.
	firstLineParsed bool
	// multiLineData accumulates the full data field across lines, except if firstLineParsed.
	multiLineData []byte
}

// NewUsageSSEReader creates a new UsageSSEReader.
func NewUsageSSEReader(sseReader sseReader, extractUsage UsageExtractor, log *slog.Logger) *UsageSSEReader {
	return &UsageSSEReader{
		sse:          sseReader,
		log:          log,
		extractUsage: extractUsage,
	}
}

// Next returns the next SSE line while extracting usage stats as a side effect.
// Errors are only returned from the underlying sseReader, parsing errors are ignored.
// [io.EOF] marks the end of the stream.
func (r *UsageSSEReader) Next() (sse.Line, error) {
	line, err := r.sse.Next()
	if err != nil {
		return line, err
	}

	switch {
	case line.IsField(sse.FieldData):
		switch {
		case r.firstLineParsed:
			// First data line already parsed successfully
			// Only incorrect behaviour possible here, because the first line has already been
			// thrown away. chosen: keep extracted usage stats but skip subsequent data lines.
			r.log.Warn("Skipping unexpected additional data line in SSE event after successful usage extraction")
		case len(r.multiLineData) == 0:
			// First data line in this event: try optimistic single-line parse.
			stats, err := r.extractUsage(line.Value)
			if err == nil {
				r.latestUsage = stats
				r.firstLineParsed = true
				break
			}
			// Optimistic parse failed: begin multi-line accumulation.
			fallthrough
		default:
			// Accumulating multi-line data
			r.multiLineData = append(r.multiLineData, line.Value...)
			r.multiLineData = append(r.multiLineData, '\n')
		}

	case line.Type == sse.LineEnd:
		if len(r.multiLineData) > 0 {
			stats, err := r.extractUsage(r.multiLineData)
			if err == nil {
				r.latestUsage = stats
			}
		}

		// Reset per-event state.
		r.firstLineParsed = false
		r.multiLineData = r.multiLineData[:0]
	}

	return line, nil
}

// LatestUsage returns the last successfully extracted usage stats.
// If no stats have been extracted yet, it returns the zero value.
func (r *UsageSSEReader) LatestUsage() usage.Stats {
	return r.latestUsage
}
