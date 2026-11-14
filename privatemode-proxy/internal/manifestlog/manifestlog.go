// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

// Package manifestlog contains the functionality to log manifest updates. This ensures traceability of the manifest history.
package manifestlog

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/spf13/afero"
)

const (
	logFileName = "log.txt"
	fileSuffix  = ".json"
)

// Logger logs manifest updates. It is not thread-safe.
type Logger struct {
	fs              afero.Fs
	manifestDir     string
	manifestVersion int
	lastManifest    []byte
}

// New creates a logger.
func New(fs afero.Fs, workspacePath string) (*Logger, error) {
	l := &Logger{
		fs:          fs,
		manifestDir: filepath.Join(workspacePath, constants.ManifestDir),
	}
	if err := fs.MkdirAll(l.manifestDir, os.ModePerm); err != nil {
		return nil, fmt.Errorf("creating manifest directory: %w", err)
	}
	var err error
	l.manifestVersion, err = getStartManifestVersion(fs, l.manifestDir)
	if err != nil {
		return nil, err
	}
	return l, nil
}

// WriteEntry logs a successfully exchanged secret's ID and stores its attested manifest in a versioned file.
func (l *Logger) WriteEntry(mf []byte, secretID string) error {
	manifestExists := bytes.Equal(l.lastManifest, mf)
	version := l.manifestVersion
	if !manifestExists {
		version++
	}
	manifestPath := filepath.Join(l.manifestDir, strconv.Itoa(version)+fileSuffix)

	logFilePath := filepath.Join(l.manifestDir, logFileName)
	logFile, err := l.fs.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer logFile.Close()

	if !manifestExists {
		if err := afero.WriteFile(l.fs, manifestPath, mf, 0o644); err != nil {
			return err
		}
	}

	timestamp := time.Now().Format(time.RFC3339)
	logEntry := fmt.Sprintf("%s %s %s\n", timestamp, manifestPath, secretID)
	_, err = logFile.WriteString(logEntry)
	if err != nil {
		return err
	}
	if err := logFile.Close(); err != nil {
		return err
	}

	if !manifestExists {
		l.lastManifest = bytes.Clone(mf)
		l.manifestVersion = version
	}
	return nil
}

func getStartManifestVersion(fs afero.Fs, manifestDir string) (int, error) {
	files, err := afero.ReadDir(fs, manifestDir)
	if err != nil {
		return 0, fmt.Errorf("reading manifest directory: %w", err)
	}

	version := 0
	for _, file := range files {
		if !strings.HasSuffix(file.Name(), fileSuffix) {
			continue
		}
		v, err := strconv.Atoi(strings.TrimSuffix(file.Name(), fileSuffix))
		if err != nil {
			return 0, fmt.Errorf("manifest directory contains unexpected file %q", file.Name())
		}
		version = max(version, v)
	}
	return version, nil
}
