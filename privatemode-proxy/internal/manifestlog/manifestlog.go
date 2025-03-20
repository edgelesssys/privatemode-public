// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: GPL-3.0-only

// package manifestlog contains the functionality to log manifest updates. This ensures traceability of the manifest history.
package manifestlog

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/edgelesssys/continuum/internal/gpl/constants"
	"github.com/spf13/afero"
)

const (
	logFileName = "log.txt"
	fileSuffix  = ".json"
)

// WriteEntry create a log entry for a new manifest and stores a versioned TOML file of the manifest.
func WriteEntry(fs afero.Fs, workspacePath string, mf []byte, coordinatorPolicyHash string) error {
	logFilePath := filepath.Join(workspacePath, constants.ManifestDir, logFileName)

	if err := validateThatLogFileAndManifestsCoexist(fs, logFilePath, workspacePath); err != nil {
		return err
	}

	// Ensure the directory exists
	err := fs.MkdirAll(filepath.Dir(logFilePath), os.ModePerm)
	if err != nil {
		return fmt.Errorf("failed to create manifest directory: %w", err)
	}

	logFile, err := fs.OpenFile(logFilePath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer logFile.Close()
	nbrOfLines, err := countLines(logFile)
	if err != nil {
		return fmt.Errorf("counting lines: %w", err)
	}
	version := nbrOfLines + 1
	manifestPath := filepath.Join(workspacePath, constants.ManifestDir, fmt.Sprint(version)+fileSuffix)
	timestamp := time.Now().Format(time.RFC3339)
	logEntry := fmt.Sprintf("%s %s %s\n", timestamp, coordinatorPolicyHash, manifestPath)

	_, err = logFile.WriteString(logEntry)
	if err != nil {
		return err
	}

	return afero.WriteFile(fs, manifestPath, mf, 0o644)
}

// validateThatLogFileAndManifestsCoexist checks that a log.txt file exists when there are versioned manifest files.
func validateThatLogFileAndManifestsCoexist(fs afero.Fs, logFilePath string, workspacePath string) error {
	logFileExists, err := afero.Exists(fs, logFilePath)
	if err != nil {
		return fmt.Errorf("checking if log file exists: %w", err)
	}

	filesExist, err := afero.Glob(fs, filepath.Join(workspacePath, constants.ManifestDir, "*"+fileSuffix))
	if err != nil {
		return fmt.Errorf("checking if manifest files exist: %w", err)
	}

	if len(filesExist) > 0 && !logFileExists {
		return fmt.Errorf("versioned manifest files exist but log.txt does not. Please remove the manifest files in the %s directory and try again", constants.ManifestDir)
	}
	return nil
}

func countLines(logFile afero.File) (int, error) {
	lineCount := 0
	scanner := bufio.NewScanner(logFile)
	for scanner.Scan() {
		lineCount++
	}

	if err := scanner.Err(); err != nil {
		return 0, err
	}
	return lineCount, nil
}
