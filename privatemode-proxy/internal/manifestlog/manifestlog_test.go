// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package manifestlog

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestWriteEntry(t *testing.T) {
	const (
		manifestA = `{"Policies":{"a":{}}}`
		manifestB = `{"Policies":{"b":{}}}`
	)
	testCases := map[string]struct {
		files     map[string]string
		manifests []string
		versions  []int
	}{
		"first manifest": {
			manifests: []string{manifestA},
			versions:  []int{1},
		},
		"consecutive identical manifests": {
			manifests: []string{manifestA, manifestA},
			versions:  []int{1, 1},
		},
		"changed manifest": {
			manifests: []string{manifestA, manifestB},
			versions:  []int{1, 2},
		},
		"return to an earlier manifest": {
			manifests: []string{manifestA, manifestB, manifestA},
			versions:  []int{1, 2, 3},
		},
		"existing manifest history": {
			files: map[string]string{
				"2.json":  manifestB,
				"7.json":  manifestA,
				"log.txt": "previous entry\n",
			},
			manifests: []string{manifestA, manifestA, manifestB},
			versions:  []int{8, 8, 9},
		},
	}
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			fs := afero.NewMemMapFs()
			workspace := "workspace"
			manifestDir := filepath.Join(workspace, constants.ManifestDir)
			require.NoError(fs.MkdirAll(manifestDir, os.ModePerm))
			for name, content := range tc.files {
				require.NoError(afero.WriteFile(fs, filepath.Join(manifestDir, name), []byte(content), 0o644))
			}
			logger, err := New(fs, workspace)
			require.NoError(err)

			expectedFiles := map[string]string{}
			for name, content := range tc.files {
				expectedFiles[name] = content
			}
			expectedLog := tc.files[logFileName]
			for i, manifest := range tc.manifests {
				secretID := "secret-" + strconv.Itoa(i)
				before := time.Now().Truncate(time.Second)
				require.NoError(logger.WriteEntry([]byte(manifest), secretID))
				after := time.Now()

				manifestName := strconv.Itoa(tc.versions[i]) + fileSuffix
				expectedFiles[manifestName] = manifest
				log, err := afero.ReadFile(fs, filepath.Join(manifestDir, logFileName))
				require.NoError(err)
				require.True(strings.HasPrefix(string(log), expectedLog), "previous log entries must remain intact")
				entry := strings.TrimPrefix(string(log), expectedLog)
				fields := strings.SplitN(entry, " ", 2)
				require.Len(fields, 2)
				timestamp, err := time.Parse(time.RFC3339, fields[0])
				require.NoError(err)
				require.WithinRange(timestamp, before, after)
				require.Equal(filepath.Join(manifestDir, manifestName)+" "+secretID+"\n", fields[1])
				expectedLog = string(log)
				expectedFiles[logFileName] = expectedLog

				files, err := afero.ReadDir(fs, manifestDir)
				require.NoError(err)
				require.Len(files, len(expectedFiles))
				for name, expected := range expectedFiles {
					content, err := afero.ReadFile(fs, filepath.Join(manifestDir, name))
					require.NoError(err)
					require.Equal(expected, string(content))
				}
			}
		})
	}

	t.Run("caller reuses manifest buffer", func(t *testing.T) {
		require := require.New(t)
		fs := afero.NewMemMapFs()
		logger, err := New(fs, "workspace")
		require.NoError(err)
		manifest := []byte(manifestA)
		require.NoError(logger.WriteEntry(manifest, "secret-0"))
		copy(manifest, manifestB)
		require.NoError(logger.WriteEntry(manifest, "secret-1"))
		for i, expected := range []string{manifestA, manifestB} {
			content, err := afero.ReadFile(fs, filepath.Join(logger.manifestDir, strconv.Itoa(i+1)+fileSuffix))
			require.NoError(err)
			require.Equal(expected, string(content))
		}
	})

	for _, blockedFile := range []string{logFileName, "1.json"} {
		t.Run("cannot write "+blockedFile, func(t *testing.T) {
			require := require.New(t)
			fs := afero.NewOsFs()
			logger, err := New(fs, t.TempDir())
			require.NoError(err)
			blockedPath := filepath.Join(logger.manifestDir, blockedFile)
			require.NoError(fs.Mkdir(blockedPath, os.ModePerm))

			require.Error(logger.WriteEntry([]byte(manifestA), "failed-secret"))
			require.NoError(fs.Remove(blockedPath))
			require.NoError(logger.WriteEntry([]byte(manifestA), "successful-secret"))

			content, err := afero.ReadFile(fs, filepath.Join(logger.manifestDir, "1.json"))
			require.NoError(err)
			require.Equal(manifestA, string(content)) //nolint:testifylint
			log, err := afero.ReadFile(fs, filepath.Join(logger.manifestDir, logFileName))
			require.NoError(err)
			require.NotContains(string(log), "failed-secret")
			require.Contains(string(log), filepath.Join(logger.manifestDir, "1.json")+" successful-secret\n")
			require.Equal(1, strings.Count(string(log), "\n"))
		})
	}
}
