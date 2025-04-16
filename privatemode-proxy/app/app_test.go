package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig(t *testing.T) {
	require := require.New(t)
	logger := slog.Default()

	testCases := map[string]struct {
		configData string
		expectCfg  jsonConfig
		wantErr    bool
	}{
		"no config file": {
			expectCfg: jsonConfig{},
		},
		"config file with invalid JSON": {
			configData: `{invalid-json}`,
			expectCfg:  jsonConfig{},
			wantErr:    true,
		},
		"config file without APP key": {
			configData: `{}`,
			expectCfg:  jsonConfig{},
		},
		"config file with valid APP key": {
			configData: `{"app_key": "test-key"}`,
			expectCfg:  jsonConfig{APIKey: "test-key"},
		},
		"complete config file": {
			configData: `{"app_key": "test-key", "deployment_uid": "test-uid", "manifest_path": "test-manifest"}`,
			expectCfg:  jsonConfig{APIKey: "test-key", DeploymentUID: "test-uid", ManifestPath: "test-manifest"},
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

			config, err := loadRuntimeConfig(tempDir, logger)
			if tc.wantErr {
				assert.Error(err)
			} else {
				assert.NoError(err)
				assert.Equal(tc.expectCfg, config)
			}
		})
	}
}
