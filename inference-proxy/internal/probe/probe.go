// Package probe sends minimal inference requests to verify a workload is ready to serve traffic.
package probe

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"slices"
	"sync/atomic"
	"time"

	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/edgelesssys/continuum/internal/oss/httputil"
	"github.com/edgelesssys/continuum/internal/oss/openai"
	"github.com/edgelesssys/continuum/internal/oss/requestid"
)

//go:embed heartbeat.mp3
var heartbeatAudio []byte

const (
	probeInterval   = 1 * time.Minute
	probeTimeout    = 10 * time.Minute
	requestIDPrefix = "probe_"
)

type probeResult struct {
	err error
}

// Prober runs inference probes against the workload in a background loop.
type Prober struct {
	baseURL   string
	tasks     []string
	modelName string
	client    httpClient
	cacheSalt string
	last      atomic.Pointer[probeResult]
	log       *slog.Logger
}

// New creates a new workload [Prober]. Call [*Prober.Run] to start the probe loop.
func New(baseURL string, modelName string, tasks []string, log *slog.Logger) *Prober {
	return &Prober{
		baseURL:   baseURL,
		tasks:     tasks,
		modelName: modelName,
		client:    httputil.NewClient(),
		log:       log,
		cacheSalt: openai.RandomPromptCacheSalt(),
	}
}

// Run probes the workload every probeInterval until ctx is cancelled.
func (p *Prober) Run(ctx context.Context) {
	ticker := time.NewTicker(probeInterval)
	defer ticker.Stop()
	for {
		p.probe(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Err returns the error from the last probe, or an error if no probe has run yet.
func (p *Prober) Err() error {
	r := p.last.Load()
	if r == nil {
		return errors.New("probe not yet run")
	}
	return r.err
}

func (p *Prober) probe(ctx context.Context) {
	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	err := p.checkOnce(probeCtx)
	p.last.Store(&probeResult{err: err})
	if err != nil {
		p.log.ErrorContext(ctx, "Workload probe failed", "error", err)
	} else {
		p.log.InfoContext(ctx, "Workload probe succeeded")
	}
}

func (p *Prober) checkOnce(ctx context.Context) error {
	req, err := p.buildRequest(ctx)
	if err != nil {
		return fmt.Errorf("building probe request: %w", err)
	}
	req.Header.Set(requestid.Header, requestIDPrefix+requestid.New())
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("sending probe request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading probe response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("probe returned %s: %s", resp.Status, body)
	}
	return validateResponse(deriveProbeKind(p.tasks), body)
}

func (p *Prober) buildRequest(ctx context.Context) (*http.Request, error) {
	switch deriveProbeKind(p.tasks) {
	case constants.WorkloadTaskGenerate:
		return buildGenerateRequest(ctx, p.baseURL, p.modelName, p.cacheSalt)
	case constants.WorkloadTaskTranscribe:
		return buildTranscribeRequest(ctx, p.baseURL, p.modelName)
	case constants.WorkloadTaskEmbed:
		return buildEmbedRequest(ctx, p.baseURL, p.modelName)
	case constants.WorkloadTaskUnstructured:
		return buildUnstructuredRequest(ctx, p.baseURL)
	default:
		return nil, fmt.Errorf("no known probe task in %v", p.tasks)
	}
}

// deriveProbeKind selects a single task for probing based on fixed precedence.
func deriveProbeKind(tasks []string) string {
	for _, t := range []string{
		constants.WorkloadTaskGenerate,
		constants.WorkloadTaskTranscribe,
		constants.WorkloadTaskEmbed,
		constants.WorkloadTaskUnstructured,
	} {
		if slices.Contains(tasks, t) {
			return t
		}
	}
	return ""
}

func buildGenerateRequest(ctx context.Context, baseURL, modelName, cacheSalt string) (*http.Request, error) {
	body, err := json.Marshal(openai.ChatRequest{
		ChatRequestPlainData: openai.ChatRequestPlainData{Model: modelName, N: 1, MaxCompletionTokens: 10},
		Messages:             []openai.Message{{Role: "user", Content: "heartbeat"}},
		CacheSalt:            cacheSalt,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+openai.ChatCompletionsEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

func buildEmbedRequest(ctx context.Context, baseURL, modelName string) (*http.Request, error) {
	body, err := json.Marshal(openai.EmbeddingsRequest{
		EmbeddingsRequestPlainData: openai.EmbeddingsRequestPlainData{Model: modelName},
		Input: []string{
			"Instruct: Retrieve semantically similar text.",
			"heartbeat 1",
			"heartbeat 2",
		},
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+openai.EmbeddingsEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

func buildTranscribeRequest(ctx context.Context, baseURL, modelName string) (*http.Request, error) {
	buf := &bytes.Buffer{}
	writer := multipart.NewWriter(buf)
	_ = writer.WriteField("model", modelName)
	_ = writer.WriteField("language", "en")
	fileForm, _ := writer.CreateFormFile("file", "file.mp3")
	_, _ = fileForm.Write(heartbeatAudio)
	_ = writer.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+openai.TranscriptionsEndpoint, buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Accept", "application/json")
	return req, nil
}

func buildUnstructuredRequest(ctx context.Context, baseURL string) (*http.Request, error) {
	buf := &bytes.Buffer{}
	writer := multipart.NewWriter(buf)
	part, _ := writer.CreateFormFile("files", "file.md")
	_, _ = part.Write([]byte("# heartbeat test\n"))
	_ = writer.WriteField("strategy", "fast")
	_ = writer.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/general/v0/general", buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Accept", "application/json")
	return req, nil
}

func validateResponse(probeKind string, body []byte) error {
	if probeKind == constants.WorkloadTaskUnstructured {
		if !bytes.Contains(body, []byte("heartbeat test")) {
			return fmt.Errorf("unexpected content in unstructured response: %q", body)
		}
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return fmt.Errorf("unmarshalling response: %w", err)
	}
	if len(m) == 0 {
		return fmt.Errorf("empty response object")
	}
	return nil
}

type httpClient interface {
	Do(*http.Request) (*http.Response, error)
}
