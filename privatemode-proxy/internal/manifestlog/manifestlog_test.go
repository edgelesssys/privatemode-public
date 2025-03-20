// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: GPL-3.0-only

package manifestlog

import (
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteLogEntry(t *testing.T) {
	newManifest := []byte("schema_version = 1")
	assert := assert.New(t)
	fs := afero.NewMemMapFs()

	// Log manifest
	assert.NoError(WriteEntry(fs, "workspace", newManifest, "policyhash1"))

	// Assert
	expectedManifestPath := "workspace/manifests/1" + fileSuffix
	assertManifestFileIsLogged(fs, expectedManifestPath, newManifest, assert)

	// Update manifest and log
	newManifest = []byte("schema_version = 2")
	assert.NoError(WriteEntry(fs, "workspace", newManifest, "policyhash2"))

	// Assert
	expectedManifestPath2 := "workspace/manifests/2" + fileSuffix
	assertManifestFileIsLogged(fs, expectedManifestPath2, newManifest, assert)

	expectedLogPath := "workspace/manifests/log.txt"
	assertLogFile(fs, t, expectedLogPath, []string{expectedManifestPath, expectedManifestPath2}, []string{"policyhash1", "policyhash2"})
}

func TestTOMLFileWithoutLogFile(t *testing.T) {
	assert := assert.New(t)
	fs := afero.NewMemMapFs()

	alreadyExistingManifestPath := "workspace/manifests/1" + fileSuffix
	require.NoError(t, afero.WriteFile(fs, alreadyExistingManifestPath, []byte("2021-09-01T12:00:00Z workspace/manifests/1"+fileSuffix+"\n"), 0o644))
	assert.Error(WriteEntry(fs, "workspace", []byte("schema_version = 1"), "policyhash"))
}

func TestAppendToExistingLogs(t *testing.T) {
	newManifest := []byte("schema_version = 1")
	assert := assert.New(t)
	fs := afero.NewMemMapFs()

	// Existing log and manifest
	alreadyExistingManifestPath := "workspace/manifests/1" + fileSuffix
	require.NoError(t, afero.WriteFile(fs, "workspace/manifests/log.txt", []byte("2021-09-01T12:00:00Z policyhash1 workspace/manifests/1"+fileSuffix+"\n"), 0o644))
	require.NoError(t, afero.WriteFile(fs, alreadyExistingManifestPath, []byte(""), 0o644))

	// Act
	assert.NoError(WriteEntry(fs, "workspace", newManifest, "policyhash2"))

	// Assert
	expectedManifestPath := "workspace/manifests/2" + fileSuffix
	assertManifestFileIsLogged(fs, expectedManifestPath, newManifest, assert)

	expectedLogPath := "workspace/manifests/log.txt"
	assertLogFile(fs, t, expectedLogPath, []string{alreadyExistingManifestPath, expectedManifestPath}, []string{"policyhash1", "policyhash2"})
}

func assertLogFile(fs afero.Fs, t *testing.T, expectedLogPath string, expectedManifestPaths, expectedPolicyHashes []string) {
	assert := assert.New(t)
	require := require.New(t)

	bt, err := afero.ReadFile(fs, expectedLogPath)
	require.NoError(err)
	lines := strings.Split(string(bt), "\n")
	require.Len(lines, len(expectedManifestPaths)+1) // +1 because the last line is empty

	for i, expectedManifestPath := range expectedManifestPaths {
		fields := strings.Fields(lines[i])
		assert.Equal(expectedPolicyHashes[i], fields[1])
		assert.Equal(expectedManifestPath, fields[2])
	}
}

func assertManifestFileIsLogged(fs afero.Fs, expectedManifestPath string, newMf []byte, assert *assert.Assertions) {
	bt, err := afero.ReadFile(fs, expectedManifestPath)
	assert.NoError(err)
	assert.Equal(newMf, bt)
}
