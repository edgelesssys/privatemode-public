package probe

import (
	"context"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckLiveness(t *testing.T) {
	const reachabilityURL = "http://127.0.0.1:8000/v1/models"

	reachable := fakeHTTPClient{status: http.StatusOK}
	unreachable := fakeHTTPClient{err: assert.AnError}

	testCases := map[string]struct {
		state           proberState
		wantErr         bool
		wantErrContains string
	}{
		"unreachable inside its timeout": {
			state: proberState{
				client:         unreachable,
				lastProgressAt: time.Now().Add(-timeoutUnreachable + time.Minute),
			},
		},
		"unreachable inside its timeout with probe error": {
			state: proberState{
				client:         unreachable,
				lastProgressAt: time.Now().Add(-timeoutUnreachable + time.Minute),
				lastErr:        assert.AnError,
			},
		},
		"unreachable past its timeout": {
			state: proberState{
				client:         unreachable,
				lastProgressAt: time.Now().Add(-timeoutUnreachable - time.Minute),
			},
			wantErr: true,
		},
		"becoming unreachable does not renew the timeout": {
			state: proberState{
				client:         unreachable,
				reachable:      true,
				lastProgressAt: time.Now().Add(-timeoutUnreachable - time.Minute),
			},
			wantErr: true,
		},
		"becoming reachable renews the timeout": {
			state: proberState{
				client:         reachable,
				lastProgressAt: time.Now().Add(-timeoutUnreachable + time.Minute),
				lastErr:        assert.AnError,
			},
		},
		"reachable with probe error inside its timeout": {
			state: proberState{
				client:         reachable,
				reachable:      true,
				lastProgressAt: time.Now(),
				lastErr:        assert.AnError,
			},
		},
		"reachable with probe error past its timeout": {
			state: proberState{
				client:         reachable,
				reachable:      true,
				lastProgressAt: time.Now().Add(-timeoutReachable - time.Minute),
				lastErr:        assert.AnError,
			},
			wantErr:         true,
			wantErrContains: assert.AnError.Error(),
		},
		"reachable inside its timeout": {
			state: proberState{
				client:         reachable,
				reachable:      true,
				lastProgressAt: time.Now().Add(-timeoutReachable + time.Minute),
			},
		},
		"a restart renews the timeout": {
			state: proberState{
				client:         unreachable,
				generation:     "gen-1",
				marker:         staticMarker("gen-2"),
				lastProgressAt: time.Now().Add(-timeoutUnreachable - time.Minute),
			},
		},
		"an unchanged marker does not renew the timeout": {
			state: proberState{
				client:         unreachable,
				generation:     "gen-1",
				marker:         staticMarker("gen-1"),
				lastProgressAt: time.Now().Add(-timeoutUnreachable - time.Minute),
			},
			wantErr: true,
		},
		"the first marker observed renews the timeout": {
			state: proberState{
				client:         unreachable,
				marker:         staticMarker("gen-1"),
				lastProgressAt: time.Now().Add(-timeoutUnreachable - time.Minute),
			},
		},
		"an unreadable marker does not renew the timeout": {
			state: proberState{
				client:         unreachable,
				generation:     "gen-1",
				lastProgressAt: time.Now().Add(-timeoutUnreachable - time.Minute),
			},
			wantErr: true,
		},
		"an empty marker does not renew the timeout": {
			state: proberState{
				client:         unreachable,
				generation:     "gen-1",
				marker:         staticMarker(""),
				lastProgressAt: time.Now().Add(-timeoutUnreachable - time.Minute),
			},
			wantErr: true,
		},
		"an unusable reachability URL falls back to unreachable": {
			state: proberState{
				reachabilityURL: "http://\x7f",
				client:          reachable,
				reachable:       true,
				lastProgressAt:  time.Now().Add(-timeoutReachable - time.Minute),
			},
		},
		"an unusable reachability URL does not renew the timeout": {
			state: proberState{
				reachabilityURL: "http://\x7f",
				client:          reachable,
				reachable:       true,
				lastProgressAt:  time.Now().Add(-timeoutUnreachable - time.Minute),
			},
			wantErr: true,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			if tc.state.reachabilityURL == "" {
				tc.state.reachabilityURL = reachabilityURL
			}
			p := newTestProber(tc.state)

			err := p.CheckLiveness(context.Background())

			if tc.wantErr {
				assert.Error(t, err)
				if tc.wantErrContains != "" {
					assert.ErrorContains(t, err, tc.wantErrContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestProbeRecordsOutcome(t *testing.T) {
	testCases := map[string]struct {
		client      httpClient
		wantAnchor  bool
		wantLastErr bool
	}{
		"success moves the anchor": {
			client:     fakeHTTPClient{status: http.StatusOK, body: `{"id":"probe"}`},
			wantAnchor: true,
		},
		"failure keeps the anchor": {
			client:      fakeHTTPClient{err: assert.AnError},
			wantLastErr: true,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			anchor := time.Now().Add(-time.Hour)
			p := newTestProber(proberState{client: tc.client, lastProgressAt: anchor})
			p.tasks = []string{constants.WorkloadTaskGenerate}

			p.probe(context.Background())

			if tc.wantAnchor {
				assert.True(t, p.workload.lastProgressAt.After(anchor))
			} else {
				assert.Equal(t, anchor, p.workload.lastProgressAt)
			}
			if tc.wantLastErr {
				assert.Error(t, p.workload.lastErr)
			} else {
				assert.NoError(t, p.workload.lastErr)
			}
		})
	}
}

// TestRestartRenewsStartupTimeout covers a workload that dies during startup: without restart
// detection, its startup timeout expires once and never recovers, so it is killed forever.
func TestRestartRenewsStartupTimeout(t *testing.T) {
	generation := "gen-1"
	p := newTestProber(proberState{
		client: fakeHTTPClient{err: assert.AnError}, // unreachable: the workload is starting up
		marker: func() ([]byte, error) { return []byte(generation), nil },
	})

	require.NoError(t, p.CheckLiveness(context.Background()))

	// The workload does not come up within its startup timeout and is killed.
	p.workload.lastProgressAt = time.Now().Add(-timeoutUnreachable - time.Minute)
	require.Error(t, p.CheckLiveness(context.Background()))

	// It restarts and gets the full startup timeout again, ...
	generation = "gen-2"
	require.NoError(t, p.CheckLiveness(context.Background()))

	// ... which is still enforced.
	p.workload.lastProgressAt = time.Now().Add(-timeoutUnreachable - time.Minute)
	require.Error(t, p.CheckLiveness(context.Background()))
}

// TestBecomingReachableRenewsTimeout covers a slow startup: the time a workload spends starting
// up must not count against the shorter timeout that applies once it is reachable.
func TestBecomingReachableRenewsTimeout(t *testing.T) {
	client := &fakeHTTPClient{status: http.StatusOK, err: assert.AnError}
	p := newTestProber(proberState{
		client:         client,
		lastProgressAt: time.Now().Add(-timeoutReachable - time.Minute),
	})

	require.NoError(t, p.CheckLiveness(context.Background()))

	// The workload becomes reachable, but its first probe has not succeeded yet.
	client.err = nil
	require.NoError(t, p.CheckLiveness(context.Background()))

	// From here on the shorter timeout applies.
	p.workload.lastProgressAt = time.Now().Add(-timeoutReachable - time.Minute)
	require.Error(t, p.CheckLiveness(context.Background()))
}

type proberState struct {
	reachabilityURL string
	client          httpClient
	marker          func() ([]byte, error)
	reachable       bool
	generation      string
	lastProgressAt  time.Time
	lastErr         error
}

func newTestProber(s proberState) *Prober {
	p := &Prober{
		reachabilityURL: s.reachabilityURL,
		client:          s.client,
		readMarker:      s.marker,
		log:             slog.New(slog.DiscardHandler),
		workload: workloadState{
			reachable:      s.reachable,
			generation:     s.generation,
			lastProgressAt: s.lastProgressAt,
			lastErr:        s.lastErr,
		},
	}
	if p.readMarker == nil {
		// The workload has not written its marker yet.
		p.readMarker = func() ([]byte, error) { return nil, fs.ErrNotExist }
	}
	if p.workload.lastProgressAt.IsZero() {
		p.workload.lastProgressAt = time.Now()
	}
	return p
}

func staticMarker(generation string) func() ([]byte, error) {
	return func() ([]byte, error) { return []byte(generation), nil }
}

type fakeHTTPClient struct {
	status int
	body   string
	err    error
}

func (c fakeHTTPClient) Do(*http.Request) (*http.Response, error) {
	if c.err != nil {
		return nil, c.err
	}
	return &http.Response{StatusCode: c.status, Body: io.NopCloser(strings.NewReader(c.body))}, nil
}
