// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package middleware

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"os"
	"path/filepath"
)

// dumpRequestToFile writes the HTTP request (including body) to the given file path.
func dumpRequestToFile(req *http.Request, dumpRequestFilePath string) error {
	if err := os.MkdirAll(filepath.Dir(dumpRequestFilePath), 0o755); err != nil {
		return fmt.Errorf("creating dump directory: %w", err)
	}

	// Dump the request (includeBody = true)
	data, err := httputil.DumpRequest(req, true)
	if err != nil {
		return fmt.Errorf("dumping request: %w", err)
	}

	if err := os.WriteFile(dumpRequestFilePath, data, 0o644); err != nil {
		return fmt.Errorf("writing request dump file: %w", err)
	}
	return nil
}

// dumpResponseRecorderToFile writes the HTTP response captured by a ResponseRecorder
// to the given file path.
func dumpResponseRecorderToFile(rec *ResponseRecorder, dumpResponseFilePath string) error {
	if err := os.MkdirAll(filepath.Dir(dumpResponseFilePath), 0o755); err != nil {
		return fmt.Errorf("creating dump directory: %w", err)
	}

	// Construct a minimal http.Response that httputil.DumpResponse can understand.
	resp := &http.Response{
		StatusCode: rec.Status,
		Header:     rec.Header(),
		Body:       io.NopCloser(bytes.NewReader(rec.Body.Bytes())),
	}
	// DumpResponse includes the status line, headers and body.
	data, err := httputil.DumpResponse(resp, true)
	if err != nil {
		return fmt.Errorf("dumping response: %w", err)
	}

	if err := os.WriteFile(dumpResponseFilePath, data, 0o644); err != nil {
		return fmt.Errorf("writing response dump file: %w", err)
	}
	return nil
}
