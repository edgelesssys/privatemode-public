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
	"os"
	"slices"
	"sync"
	"time"

	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/edgelesssys/continuum/internal/oss/httputil"
	"github.com/edgelesssys/continuum/internal/oss/openai"
	"github.com/edgelesssys/continuum/internal/oss/requestid"
)

//go:embed heartbeat.mp3
var heartbeatAudio []byte

const (
	probeInterval       = 1 * time.Minute
	probeTimeout        = 10 * time.Minute
	requestIDPrefix     = "probe_"
	reachabilityTimeout = 2 * time.Second
	timeoutUnreachable  = 1 * time.Hour
	timeoutReachable    = 15 * time.Minute // must cover at least probeInterval + probeTimeout
)

type workloadState struct {
	reachable bool
	// generation is the last observed content of the marker file, or empty if the marker has not
	// been read successfully yet.
	generation string
	// lastProgressAt is the anchor for the timeouts. It is set by [New], a probe success, a
	// workload container start, and the workload becoming reachable.
	lastProgressAt time.Time
	// lastErr is the last probe error, or nil if successful.
	lastErr error
}

// Prober runs inference probes against the workload in a background loop.
type Prober struct {
	baseURL         string
	reachabilityURL string
	readMarker      func() ([]byte, error)
	tasks           []string
	modelName       string
	client          httpClient
	cacheSalt       string
	// mu guards workload.
	mu       sync.Mutex
	workload workloadState
	log      *slog.Logger
}

// New creates a new workload [Prober]. Call [*Prober.Run] to start the probe loop. reachabilityURL
// must answer exactly while the workload serves. Probe errors on an unreachable workload are
// suppressed. startMarkerPath is the file the workload container writes a fresh token to on every
// start, letting the prober detect workload restarts.
func New(
	baseURL, reachabilityURL, startMarkerPath string,
	modelName string, tasks []string,
	log *slog.Logger,
) *Prober {
	return &Prober{
		baseURL:         baseURL,
		reachabilityURL: reachabilityURL,
		readMarker:      func() ([]byte, error) { return os.ReadFile(startMarkerPath) },
		tasks:           tasks,
		modelName:       modelName,
		client:          httputil.NewClient(),
		log:             log,
		cacheSalt:       openai.RandomPromptCacheSalt(),
		workload: workloadState{
			lastProgressAt: time.Now(), // arms [timeoutUnreachable] for startup
		},
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

// CheckLiveness reports whether the workload is alive. Probes reachability, so it must be
// called with a request context. The workload must succeed a probe within [timeoutUnreachable]
// while it is not reachable, and within [timeoutReachable] once it is reachable.
func (p *Prober) CheckLiveness(ctx context.Context) error {
	genErr := p.refreshGeneration(ctx) // error expected before the first workload container start

	if err := p.refreshReachability(ctx); err != nil {
		p.log.ErrorContext(ctx, "Failed setup for refreshing reachability", "error", err)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	p.logRefreshGenerationErrorWithLock(ctx, genErr)

	timeout := timeoutReachable
	if !p.workload.reachable {
		timeout = timeoutUnreachable
	}
	if !time.Now().After(p.workload.lastProgressAt.Add(timeout)) {
		return nil
	}
	return p.reportNotAliveWithLock(ctx, timeout)
}

// reportNotAliveWithLock builds a message why the workload is not alive, which is logged and
// returned as an error. timeout is the limit the workload exceeded.
func (p *Prober) reportNotAliveWithLock(ctx context.Context, timeout time.Duration) error {
	generation := "none observed"
	if p.workload.generation != "" {
		generation = p.workload.generation
	}
	noProgressFor := time.Since(p.workload.lastProgressAt).Truncate(time.Second)

	msg := fmt.Sprintf(
		"no workload progress for %v, exceeding the limit of %v, start generation: %s",
		noProgressFor, timeout, generation,
	)
	err := errors.New(msg)
	if p.workload.lastErr != nil {
		err = fmt.Errorf("%s, last probe error: %w", msg, p.workload.lastErr)
	}

	p.log.ErrorContext(ctx, "Workload liveness check failed, reporting it as not-alive for restart",
		"workloadNoProgressFor", noProgressFor,
		"workloadProgressTimeout", timeout,
		"workloadReachable", p.workload.reachable,
		"workloadGeneration", generation,
		"workloadProbeError", p.workload.lastErr,
	)
	return err
}

// refreshGeneration reads the workload's container start marker and renews the timeouts whenever it
// holds a token other than the one observed before. Returns an error on read failure.
func (p *Prober) refreshGeneration(ctx context.Context) error {
	marker, err := p.readMarker()
	if err != nil {
		return fmt.Errorf("reading marker: %w", err)
	}
	generation := string(bytes.TrimSpace(marker))
	if generation == "" {
		return errors.New("marker is empty")
	}

	p.mu.Lock()
	started := p.workload.generation != generation
	p.workload.generation = generation
	if started {
		p.workload.lastProgressAt = time.Now()
	}
	p.mu.Unlock()

	if started {
		p.log.InfoContext(ctx, "Workload started, renewing its timeouts", "workloadGeneration", generation)
	}

	return nil
}

func (p *Prober) logRefreshGenerationErrorWithLock(ctx context.Context, err error) {
	if err == nil {
		return
	}
	if p.workload.reachable {
		p.log.ErrorContext(ctx, "Failed refreshing generation marker for reachable workload", "error", err)
	} else {
		p.log.InfoContext(ctx, "Failed refreshing generation marker", "error", err)
	}
}

// refreshReachability calls the reachability URL and renews the timeouts when the workload just
// became reachable. Returns an error only on setup failure, not reachability failure.
func (p *Prober) refreshReachability(ctx context.Context) error {
	reachabilityCtx, cancel := context.WithTimeout(ctx, reachabilityTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reachabilityCtx, http.MethodGet, p.reachabilityURL, http.NoBody)
	if err != nil {
		// No information about reachability, conservative fallback: not reachable
		p.mu.Lock()
		p.workload.reachable = false
		p.mu.Unlock()
		return err
	}

	resp, requestErr := p.client.Do(req)
	if resp != nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
	// Any response counts: vLLM registers its routes only once ready.
	reachable := requestErr == nil

	p.mu.Lock()
	wasReachable := p.workload.reachable
	p.workload.reachable = reachable
	if reachable && !wasReachable {
		p.workload.lastProgressAt = time.Now()
	}
	p.mu.Unlock()

	log := p.log.With("workloadReachabilityURL", p.reachabilityURL)
	if reachable && !wasReachable {
		log.InfoContext(ctx, "Workload became reachable")
	} else if !reachable && wasReachable {
		log.WarnContext(ctx, "Workload became unreachable", "error", requestErr)
	}
	return nil
}

func (p *Prober) probe(ctx context.Context) {
	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	err := p.checkOnce(probeCtx)

	p.mu.Lock()
	p.workload.lastErr = err
	if err == nil {
		p.workload.lastProgressAt = time.Now()
	}
	p.mu.Unlock()
	if err != nil {
		p.log.WarnContext(ctx, "Workload probe failed", "error", err)
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
		Model: modelName, N: 1, MaxCompletionTokens: 10,
		Messages:  []openai.Message{{Role: "user", Content: "heartbeat"}},
		CacheSalt: cacheSalt,
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
		Model: modelName,
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
