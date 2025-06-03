package openai

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/edgelesssys/continuum/internal/gpl/constants"
	"github.com/edgelesssys/continuum/internal/gpl/forwarder"
	"github.com/edgelesssys/continuum/internal/gpl/openai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// TestSetBytes checks that a string is correctly inserted into a JSON string.
func TestSetBytes(t *testing.T) {
	replace := `demo-app:3a71ea7448791716e325146b:a03acc195834a9822d676e797381c035418dde3539cf46ae61d0ef2ff59b81f1d7d05cc5d8b79cf2ec08c9ce147c90c3`
	original := `{"model": "model","messages": [{"role": "user", "content": "Hi"}],"temperature": 1}`

	res, err := sjson.SetBytes([]byte(original), "messages", []byte(replace))
	assert.NoError(t, err)
	assert.Equal(t, replace, gjson.GetBytes(res, "messages").String())
}

// TestSetRawBytes checks that a marshalled JSON is correctly inserted into a JSON string.
func TestSetRawBytes(t *testing.T) {
	replace := `[{"role": "user", "content": "Write a haiku about the dust on my floor."}]`
	original := `{"model": "model","messages": [{"role": "user", "content": "Hi"}],"temperature": 1}`

	res, err := sjson.SetRawBytes([]byte(original), "messages", []byte(replace))
	assert.NoError(t, err)
	assert.Equal(t, replace, gjson.GetBytes(res, "messages").String())
}

func TestGetClientSemanticVersion(t *testing.T) {
	a := &Adapter{}
	makeReq := func(version string) *http.Request {
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, "/", nil)
		if version != "" {
			req.Header.Set(constants.PrivatemodeVersionHeader, version)
		}
		return req
	}

	testCases := map[string]struct {
		headerValue    string
		expectedOutput string
		expectError    bool
	}{
		"no header":          {"", "", false},
		"valid version":      {"v1.12.3", "v1.12.3", false},
		"with pseudo suffix": {"v1.12.3-beta", "v1.12.3", false},
		"0.0.0 old app":      {"0.0.0", "v0.0.0", false},
		"invalid version":    {"banana", "", true},
	}

	for name, tt := range testCases {
		t.Run(name, func(t *testing.T) {
			req := makeReq(tt.headerValue)
			version, err := a.getSemanticVersion(req.Header.Get(constants.PrivatemodeVersionHeader))
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedOutput, version)
			}
		})
	}
}

func TestForwardModelsRequest(t *testing.T) {
	testCases := map[string]struct {
		workloadTasks  []string
		path           string
		serverResponse string
		wantResponse   string
	}{
		"list models": {
			workloadTasks: []string{constants.WorkloadTaskGenerate},
			path:          "/v1/models",
			serverResponse: func() string {
				res, err := json.Marshal(openai.ModelsResponse{
					Object: "list",
					Data: []openai.Model{
						{
							ID:     "latest",
							Object: "model",
						},
						{
							ID:     "llama3",
							Object: "model",
						},
					},
				})
				require.NoError(t, err)
				return string(res)
			}(),
			wantResponse: func() string {
				res, err := json.Marshal(openai.ModelsResponse{
					Object: "list",
					Data: []openai.Model{
						{
							ID:     "latest",
							Object: "model",
							Tasks:  []string{constants.WorkloadTaskGenerate},
						},
						{
							ID:     "llama3",
							Object: "model",
							Tasks:  []string{constants.WorkloadTaskGenerate},
						},
					},
				})
				require.NoError(t, err)
				return string(res)
			}(),
		},
		"retrieve model": {
			workloadTasks: []string{constants.WorkloadTaskGenerate},
			path:          "/v1/models/latest",
			serverResponse: func() string {
				res, err := json.Marshal(openai.Model{
					ID:     "latest",
					Object: "model",
				})
				require.NoError(t, err)
				return string(res)
			}(),
			wantResponse: func() string {
				res, err := json.Marshal(openai.Model{
					ID:     "latest",
					Object: "model",
					Tasks:  []string{constants.WorkloadTaskGenerate},
				})
				require.NoError(t, err)
				return string(res)
			}(),
		},
		"list models with multiple tasks": {
			workloadTasks: []string{constants.WorkloadTaskGenerate, "custom-task"},
			path:          "/v1/models",
			serverResponse: func() string {
				res, err := json.Marshal(openai.ModelsResponse{
					Object: "list",
					Data: []openai.Model{
						{
							ID:     "latest",
							Object: "model",
						},
						{
							ID:     "llama3",
							Object: "model",
						},
					},
				})
				require.NoError(t, err)
				return string(res)
			}(),
			wantResponse: func() string {
				res, err := json.Marshal(openai.ModelsResponse{
					Object: "list",
					Data: []openai.Model{
						{
							ID:     "latest",
							Object: "model",
							Tasks:  []string{constants.WorkloadTaskGenerate, "custom-task"},
						},
						{
							ID:     "llama3",
							Object: "model",
							Tasks:  []string{constants.WorkloadTaskGenerate, "custom-task"},
						},
					},
				})
				require.NoError(t, err)
				return string(res)
			}(),
		},
		"retrieve model with multiple tasks": {
			workloadTasks: []string{constants.WorkloadTaskGenerate, constants.WorkloadTaskToolCalling},
			path:          "/v1/models/latest",
			serverResponse: func() string {
				res, err := json.Marshal(openai.Model{
					ID:     "latest",
					Object: "model",
				})
				require.NoError(t, err)
				return string(res)
			}(),
			wantResponse: func() string {
				res, err := json.Marshal(openai.Model{
					ID:     "latest",
					Object: "model",
					Tasks:  []string{constants.WorkloadTaskGenerate, constants.WorkloadTaskToolCalling},
				})
				require.NoError(t, err)
				return string(res)
			}(),
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(tc.serverResponse))
			}))
			defer srv.Close()

			log := slog.Default()
			forwarder := forwarder.New("tcp", srv.Listener.Addr().String(), log)
			adapter, err := New(tc.workloadTasks, nil, forwarder, log)
			require.NoError(err)

			request := httptest.NewRequest(http.MethodGet, tc.path, nil)
			responseRecorder := httptest.NewRecorder()

			adapter.forwardModelsRequest(responseRecorder, request)
			t.Log(responseRecorder.Body.String())
			assert.JSONEq(tc.wantResponse, responseRecorder.Body.String())
		})
	}
}
