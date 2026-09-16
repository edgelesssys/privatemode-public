package statsfeed

import (
	"bytes"
	"context"
	"encoding/json"
	"maps"
	"strings"
	"testing"

	"github.com/edgelesssys/continuum/internal/oss/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	msgQueueState      = "Received queue state event"
	msgRequestFinished = "Received request finished event"
	msgUnrecognized    = "Ignoring unrecognized stats event type"
	msgBatch           = "Received stats events"
)

// testPID is the PID every batch reports; the value carries no meaning to the handler.
const testPID = 42

// testRequestID is the request ID our infrastructure tracks a request by.
const testRequestID = "0199a0f1-2b3c-7def-8123-456789abcdef"

const (
	eventQueueState = `{"type":"queue_state","num_waiting_capacity":3,"num_waiting_deferred":1,
		"num_running":2,"kv_cache_usage":0.5}`
	eventQueueStateIdle = `{"type":"queue_state","num_waiting_capacity":0,"num_waiting_deferred":0,
		"num_running":1,"kv_cache_usage":0.1}`
	eventRequestFinished = `{"type":"request_finished","event_id":"e1",
		"request_id":"chatcmpl-` + testRequestID + `","finish_reason":"stop",
		"num_prompt_tokens":10,"num_generation_tokens":20,"num_cached_tokens":5,"max_tokens":100,
		"queued_time_engine_seconds":0.1,"prefill_time_engine_seconds":0.2,
		"decode_time_engine_seconds":0.3,"e2e_latency_frontend_seconds":0.7}`
	// The nullable fields unset, as vLLM reports them when it has no value.
	eventRequestFinishedNulls = `{"type":"request_finished","event_id":"e2",
		"request_id":null,"finish_reason":"stop",
		"num_prompt_tokens":10,"num_generation_tokens":20,"num_cached_tokens":5,"max_tokens":null,
		"queued_time_engine_seconds":0.1,"prefill_time_engine_seconds":0.2,
		"decode_time_engine_seconds":0.3,"e2e_latency_frontend_seconds":0.7}`
	eventUnknownType = `{"type":"something_new","whatever":{"nested":true}}`

	// Erroneous event shapes.

	eventNoType            = `{"num_running":2}`
	eventInvalidQueueState = `{"type":"queue_state","num_running":"two"}`
)

func TestSubmitStatsEventsLogsDecodedFields(t *testing.T) {
	records, err := submit(t.Context(), t, 1, eventQueueState, eventRequestFinished, eventRequestFinishedNulls)

	require.NoError(t, err)

	assertAttrs(t, findOnlyRecordWithMsg(t, records, msgQueueState), map[string]any{
		"frontendIndex": float64(1),
		"pid":           float64(testPID),
		"event": map[string]any{
			"type":                 "queue_state",
			"num_waiting_capacity": float64(3),
			"num_waiting_deferred": float64(1),
			"num_running":          float64(2),
			"kv_cache_usage":       0.5,
		},
	})

	finished := findRecordsWithMsg(records, msgRequestFinished)
	require.Len(t, finished, 2)

	assertAttrs(t, finished[0], map[string]any{
		"frontendIndex": float64(1),
		"pid":           float64(testPID),
		"requestID":     testRequestID,
		"event": map[string]any{
			"type":                         "request_finished",
			"event_id":                     "e1",
			"request_id":                   "chatcmpl-" + testRequestID,
			"finish_reason":                "stop",
			"num_prompt_tokens":            float64(10),
			"num_generation_tokens":        float64(20),
			"num_cached_tokens":            float64(5),
			"max_tokens":                   float64(100),
			"queued_time_engine_seconds":   0.1,
			"prefill_time_engine_seconds":  0.2,
			"decode_time_engine_seconds":   0.3,
			"e2e_latency_frontend_seconds": 0.7,
		},
	})

	// nil values must be preserved, not set to zero
	assertAttrs(t, finished[1], map[string]any{
		"frontendIndex": float64(1),
		"pid":           float64(testPID),
		"event": map[string]any{
			"type":                         "request_finished",
			"event_id":                     "e2",
			"request_id":                   nil,
			"finish_reason":                "stop",
			"num_prompt_tokens":            float64(10),
			"num_generation_tokens":        float64(20),
			"num_cached_tokens":            float64(5),
			"max_tokens":                   nil,
			"queued_time_engine_seconds":   0.1,
			"prefill_time_engine_seconds":  0.2,
			"decode_time_engine_seconds":   0.3,
			"e2e_latency_frontend_seconds": 0.7,
		},
	})
}

func TestSubmitStatsEventsSummarizesBatch(t *testing.T) {
	testCases := map[string]struct {
		frontendIndex int
		events        []string
		wantAll       map[string]any
	}{
		"mixed batch": {
			events: []string{eventQueueState, eventUnknownType, eventRequestFinished, eventQueueStateIdle},
			wantAll: map[string]any{
				"frontendIndex": float64(0), "pid": float64(testPID),
				"numEvents": float64(4), "numQueueState": float64(2),
				"numRequestFinished": float64(1), "numUnrecognized": float64(1),
				"numDecodeFailed": float64(0),
			},
		},
		"undecodable events are skipped": {
			events: []string{
				eventNoType, eventQueueState, eventInvalidQueueState, eventRequestFinished,
			},
			wantAll: map[string]any{
				"frontendIndex": float64(0), "pid": float64(testPID),
				"numEvents": float64(4), "numQueueState": float64(1),
				"numRequestFinished": float64(1), "numUnrecognized": float64(0),
				"numDecodeFailed": float64(2),
			},
		},
		"empty batch": {
			frontendIndex: 3,
			wantAll: map[string]any{
				"frontendIndex": float64(3), "pid": float64(testPID),
				"numEvents": float64(0), "numQueueState": float64(0),
				"numRequestFinished": float64(0), "numUnrecognized": float64(0),
				"numDecodeFailed": float64(0),
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			records, err := submit(t.Context(), t, tc.frontendIndex, tc.events...)

			require.NoError(t, err)

			batch := findOnlyRecordWithMsg(t, records, msgBatch)
			assert.Equal(t, "DEBUG", batch["level"])
			assertAttrs(t, batch, tc.wantAll)
		})
	}
}

func TestRequestIDFromVLLM(t *testing.T) {
	testCases := map[string]struct {
		reported *string
		want     string
	}{
		"chat completion":   {reported: new("chatcmpl-" + testRequestID), want: testRequestID},
		"completion":        {reported: new("cmpl-" + testRequestID), want: testRequestID},
		"embedding":         {reported: new("embd-" + testRequestID), want: testRequestID},
		"transcription":     {reported: new("transcribe-" + testRequestID), want: testRequestID},
		"audio chunk":       {reported: new("transcribe-" + testRequestID + "-3"), want: testRequestID},
		"uppercase":         {reported: new("chatcmpl-" + strings.ToUpper(testRequestID)), want: strings.ToUpper(testRequestID)},
		"unprefixed":        {reported: new(testRequestID), want: testRequestID},
		"header never sent": {reported: new("chatcmpl-6b1e1a06f5b34e0e")},
		"leading hex run":   {reported: new("chatcmpl-dead" + testRequestID)},
		"trailing hex run":  {reported: new("chatcmpl-" + testRequestID + "beef")},
		"no leading dash":   {reported: new("chatcmpl" + testRequestID)},
		"not reported":      {reported: nil},
		"empty":             {reported: new("")},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, requestIDFromVLLM(tc.reported))
		})
	}
}

// submit calls the RPC handler with events, returning the log records it emitted, in order.
func submit(ctx context.Context, t *testing.T, frontendIndex int, events ...string) ([]map[string]any, error) {
	t.Helper()

	req := SubmitStatsEventsRequest{FrontendIndex: frontendIndex, PID: testPID}
	for _, event := range events {
		req.Events = append(req.Events, json.RawMessage(event))
	}

	var logOutput bytes.Buffer
	s := New(logging.NewWriterLogger("debug", &logOutput))

	_, err := s.submitStatsEvents(ctx, req)
	return parseRecords(t, logOutput.String()), err
}

// parseRecords parses JSON log output into one map per record, in emission order.
func parseRecords(t *testing.T, logOutput string) []map[string]any {
	t.Helper()

	var records []map[string]any
	for line := range strings.SplitSeq(strings.TrimSpace(logOutput), "\n") {
		if line == "" {
			continue
		}
		var record map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &record))
		records = append(records, record)
	}
	return records
}

// findRecordsWithMsg returns the records carrying msg, in emission order.
func findRecordsWithMsg(records []map[string]any, msg string) []map[string]any {
	var found []map[string]any
	for _, record := range records {
		if record["msg"] == msg {
			found = append(found, record)
		}
	}
	return found
}

// findOnlyRecordWithMsg returns the one record carrying msg. It errors if there are multiple.
func findOnlyRecordWithMsg(t *testing.T, records []map[string]any, msg string) map[string]any {
	t.Helper()

	found := findRecordsWithMsg(records, msg)
	require.Len(t, found, 1)
	return found[0]
}

// assertAttrs asserts that record carries exactly want, ignoring slog's own keys.
func assertAttrs(t *testing.T, record map[string]any, want map[string]any) {
	t.Helper()

	got := maps.Clone(record)
	delete(got, "time")
	delete(got, "level")
	delete(got, "msg")
	assert.Equal(t, want, got)
}
