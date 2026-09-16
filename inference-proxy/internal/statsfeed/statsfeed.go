// Package statsfeed implements the inference-proxy's receiver for the serving statistics
// reported by the vLLM stat-logger plugin running in the same pod.
package statsfeed

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"time"

	"github.com/edgelesssys/continuum/internal/httprpc"
	"github.com/edgelesssys/continuum/internal/oss/logging"
	"github.com/edgelesssys/continuum/internal/oss/process"
	"github.com/edgelesssys/continuum/internal/oss/requestid"
)

// SubmitStatsEventsRPC submits a batch of statistics events observed by vLLM. The request is
// [SubmitStatsEventsRequest], the response is [SubmitStatsEventsResponse].
const SubmitStatsEventsRPC = "SubmitStatsEvents"

const (
	eventTypeQueueState      = "queue_state"
	eventTypeRequestFinished = "request_finished"
)

// SubmitStatsEventsRequest specifies the request structure for [SubmitStatsEventsRPC]. Events are
// kept undecoded so that events can be decoded based on event type. Events must be processed in
// array order representing arrival order.
type SubmitStatsEventsRequest struct {
	FrontendIndex int               `json:"frontend_index"`
	PID           int               `json:"pid"`
	Events        []json.RawMessage `json:"events"`
}

// SubmitStatsEventsResponse specifies the response structure for [SubmitStatsEventsRPC].
type SubmitStatsEventsResponse struct{}

// eventType extracts an event's discriminator, which the concrete event types repeat.
type eventType struct {
	Type string `json:"type"`
}

// queueStateEvent reports the current state of the engine's queue.
type queueStateEvent struct {
	Type               string  `json:"type"`
	NumWaitingCapacity int     `json:"num_waiting_capacity"`
	NumWaitingDeferred int     `json:"num_waiting_deferred"`
	NumRunning         int     `json:"num_running"`
	KVCacheUsage       float64 `json:"kv_cache_usage"`
}

// requestFinishedEvent reports one finished request.
type requestFinishedEvent struct {
	Type                      string  `json:"type"`
	EventID                   string  `json:"event_id"`
	RequestID                 *string `json:"request_id"`
	FinishReason              string  `json:"finish_reason"`
	NumPromptTokens           int     `json:"num_prompt_tokens"`
	NumGenerationTokens       int     `json:"num_generation_tokens"`
	NumCachedTokens           int     `json:"num_cached_tokens"`
	MaxTokens                 *int    `json:"max_tokens"`
	QueuedTimeEngineSeconds   float64 `json:"queued_time_engine_seconds"`
	PrefillTimeEngineSeconds  float64 `json:"prefill_time_engine_seconds"`
	DecodeTimeEngineSeconds   float64 `json:"decode_time_engine_seconds"`
	E2ELatencyFrontendSeconds float64 `json:"e2e_latency_frontend_seconds"`
}

// Server is the HTTP server exposing the stats feed RPCs.
type Server struct {
	log *slog.Logger
}

// New returns a new [Server].
func New(log *slog.Logger) *Server {
	return &Server{log: log}
}

// Serve starts the server on lis.
func (s *Server) Serve(ctx context.Context, lis net.Listener) error {
	server := &http.Server{
		Addr:              lis.Addr().String(),
		Handler:           s.handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		ErrorLog:          logging.NewHTTPErrorLogger(s.log),
	}
	return process.HTTPServeContext(ctx, server, lis, s.log)
}

func (s *Server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/"+SubmitStatsEventsRPC, httprpc.MakeHandler(
		s.log, s.submitStatsEvents, httprpc.WithAccessLogLevel(slog.LevelDebug),
	))
	return mux
}

func (s *Server) submitStatsEvents(ctx context.Context, req SubmitStatsEventsRequest) (SubmitStatsEventsResponse, error) {
	log := s.log.With("frontendIndex", req.FrontendIndex, "pid", req.PID)

	var numQueueState, numRequestFinished, numUnrecognized, numDecodeFailed int
	skipEvent := func(err error) {
		numDecodeFailed++
		log.DebugContext(ctx, "Skipping undecodable stats event", "error", err)
	}
	for i, raw := range req.Events {
		typ, err := decodeEventType(raw, i)
		if err != nil {
			skipEvent(err)
			continue
		}

		switch typ {
		case eventTypeQueueState:
			event, err := decodeEvent[queueStateEvent](raw, i)
			if err != nil {
				skipEvent(err)
				continue
			}
			numQueueState++
			s.handleQueueState(ctx, log, event)
		case eventTypeRequestFinished:
			event, err := decodeEvent[requestFinishedEvent](raw, i)
			if err != nil {
				skipEvent(err)
				continue
			}
			numRequestFinished++
			s.handleRequestFinished(ctx, log, event)
		default:
			numUnrecognized++
			log.DebugContext(ctx, "Ignoring unrecognized stats event type", "type", typ)
		}
	}

	log.DebugContext(
		ctx, "Received stats events",
		"numEvents", len(req.Events),
		"numQueueState", numQueueState,
		"numRequestFinished", numRequestFinished,
		"numUnrecognized", numUnrecognized,
		"numDecodeFailed", numDecodeFailed,
	)

	return SubmitStatsEventsResponse{}, nil
}

func (s *Server) handleQueueState(ctx context.Context, log *slog.Logger, event queueStateEvent) {
	log.DebugContext(ctx, "Received queue state event", "event", event)
}

func (s *Server) handleRequestFinished(ctx context.Context, log *slog.Logger, event requestFinishedEvent) {
	// Attach the reported request's ID to the context for logging in its context, rather than the
	// context of the RPC call.
	if id := requestIDFromVLLM(event.RequestID); id != "" {
		ctx = requestid.WithContext(ctx, id)
	}
	log.InfoContext(ctx, "Received request finished event", "event", event)
}

// requestIDPattern matches a canonical UUID within a vLLM-composited request ID.
// E.g. "chatcmpl-{UUID}".
var requestIDPattern = regexp.MustCompile(`(?:^|-)([0-9a-fA-F]{8}-(?:[0-9a-fA-F]{4}-){3}[0-9a-fA-F]{12})(?:$|-)`)

// requestIDFromVLLM extracts our request ID from the one vLLM reports. E.g. "chatcmpl-{UUID}" or
// "transcribe-{UUID}-2" (chunked transcription). Returns "" if it holds no ID of ours.
func requestIDFromVLLM(reported *string) string {
	if reported == nil {
		return ""
	}
	if match := requestIDPattern.FindStringSubmatch(*reported); match != nil {
		return match[1]
	}
	return ""
}

// decodeEvent decodes the type discriminator from a JSON message. index is only passed for error
// reporting.
func decodeEventType(raw json.RawMessage, index int) (string, error) {
	var typed eventType
	if err := json.Unmarshal(raw, &typed); err != nil {
		return "", fmt.Errorf("event %d: %s", index, err.Error())
	}
	if typed.Type == "" {
		return "", fmt.Errorf("event %d: no event type", index)
	}
	return typed.Type, nil
}

// decodeEvent decodes a single JSON message, ignorning unknown fields. index is only passed for
// error reporting.
func decodeEvent[T any](raw json.RawMessage, index int) (T, error) {
	var event T
	if err := json.Unmarshal(raw, &event); err != nil {
		return event, fmt.Errorf("event %d: %s", index, err.Error())
	}
	return event, nil
}
