// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

// Package unstructured contains types for the unstructured API.
// It defines the subset of the public API used or tested in the codebase,
// leaving out unused parts to avoid unnecessary dependencies and broken
// code.
package unstructured

// JSONResponse represents a JSON array in response to unstructured API
// calls if JSON output is requested (also supports CSV).
type JSONResponse []ResponseElement

// ResponseElement represents a single element JSONResponse. This element
// is partially defined, see the documentation for the full definition:
// https://docs.unstructured.io/api-reference/partition/document-elements
type ResponseElement struct {
	Text string `json:"text"`
}
