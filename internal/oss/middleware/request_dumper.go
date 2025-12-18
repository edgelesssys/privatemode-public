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
	"time"
)

// dumpRequestToFile writes the HTTP request (including body) to a
// timestamped file under {workspace}/requests/YYYY-MM-DD/.  The function relies on
// httputil.DumpRequest which consumes req.Body and replaces it with a new
// io.ReadCloser that yields the same bytes, so the request can still be forwarded.
func dumpRequestToFile(req *http.Request, dumpRequestsDir string) error {
	ts := time.Now().UTC()
	dir := filepath.Join(dumpRequestsDir, ts.Format("2006-01-02"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating dump directory: %w", err)
	}

	// Dump the request (includeBody = true)
	data, err := httputil.DumpRequest(req, true)
	if err != nil {
		return fmt.Errorf("dumping request: %w", err)
	}

	file := filepath.Join(dir, fmt.Sprintf("%s_req.txt", ts.Format("20060102_150405.000000000")))
	if err := os.WriteFile(file, data, 0o644); err != nil {
		return fmt.Errorf("writing request dump file: %w", err)
	}
	return nil
}

// dumpResponseRecorderToFile writes the HTTP response captured by a
// ResponseRecorder to a timestamped file.  It mirrors the behaviour of
// DumpResponseToFile but works directly with the recorder, avoiding the need
// to build an http.Response manually.
func dumpResponseRecorderToFile(rec *ResponseRecorder, dumpRequestsDir string) error {
	ts := time.Now().UTC()
	dir := filepath.Join(dumpRequestsDir, ts.Format("2006-01-02"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
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

	file := filepath.Join(dir, fmt.Sprintf("%s_resp.txt", ts.Format("20060102_150405.000000000")))
	if err := os.WriteFile(file, data, 0o644); err != nil {
		return fmt.Errorf("writing response dump file: %w", err)
	}
	return nil
}
