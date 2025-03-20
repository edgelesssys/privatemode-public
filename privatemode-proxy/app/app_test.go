package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadAPIKey(t *testing.T) {
	require := require.New(t)
	logger := slog.Default()

	testCases := map[string]struct {
		configData string
		expectKey  string
		wantErr    bool
	}{
		"no config file": {
			expectKey: "",
		},
		"config file with invalid JSON": {
			configData: `{invalid-json}`,
			expectKey:  "",
			wantErr:    true,
		},
		"config file without APP key": {
			configData: `{}`,
			expectKey:  "",
		},
		"config file with valid APP key": {
			configData: `{"app_key": "test-key"}`,
			expectKey:  "test-key",
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			tempDir := t.TempDir()

			if tc.configData != "" {
				err := os.WriteFile(filepath.Join(tempDir, "config.json"), []byte(tc.configData), 0o644)
				require.NoError(err)
			}

			apiKey, err := loadAPIKey(tempDir, logger)
			if tc.wantErr {
				assert.Error(err)
			} else {
				assert.NoError(err)
				assert.Equal(tc.expectKey, apiKey)
			}
		})
	}
}
