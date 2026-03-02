// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package privatemode

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strconv"

	"github.com/edgelesssys/continuum/internal/oss/crypto"
	"github.com/edgelesssys/continuum/internal/oss/forwarder"
)

// UnstructuredFile represents a file to be sent to the unstructured
// endpoint.
//
// See https://docs.unstructured.io/api-reference/legacy-api/partition/api-parameters
type UnstructuredFile struct {
	// Name is the filename (e.g. "document.pdf").
	Name string
	// Content is the raw file content.
	Content []byte
	// ContentType is an optional MIME type hint for the file
	// (e.g. "application/pdf"). If empty, the server will
	// auto-detect the type.
	ContentType string
}

// UnstructuredOptions contains optional parameters for the unstructured
// partition endpoint. All fields are optional; zero values are omitted
// from the request.
//
// See https://docs.unstructured.io/api-reference/legacy-api/partition/api-parameters
type UnstructuredOptions struct {
	// Strategy is the partitioning strategy to use.
	// Supported values: "hi_res", "fast", "ocr_only", "auto", "vlm".
	Strategy string
	// ChunkingStrategy selects the chunking strategy applied after
	// partitioning. Supported values: "basic", "by_title", "by_page",
	// "by_similarity".
	ChunkingStrategy string
	// Coordinates enables bounding box coordinates in the response.
	Coordinates bool
	// Encoding specifies the text encoding (default "utf-8").
	Encoding string
	// ExtractImageBlockTypes lists element types to extract as
	// Base64-encoded images (e.g. ["Image", "Table"]).
	ExtractImageBlockTypes []string
	// HiResModelName selects the model used with the "hi_res"
	// strategy.
	HiResModelName string
	// IncludePageBreaks includes PageBreak elements in the output.
	IncludePageBreaks bool
	// Languages lists languages present in the document for OCR.
	Languages []string
	// OutputFormat sets the response format (e.g. "application/json",
	// "text/csv").
	OutputFormat string
	// SkipInferTableTypes lists document types to skip table
	// extraction for.
	SkipInferTableTypes []string
	// StartingPageNumber sets the page number assigned to the first
	// page. nil means the parameter is omitted.
	StartingPageNumber *int
	// UniqueElementIDs uses random UUIDs instead of deterministic
	// hashes for element IDs.
	UniqueElementIDs bool
	// XMLKeepTags retains XML tags in the output when processing XML
	// documents.
	XMLKeepTags bool
}

// Unstructured sends an encrypted request to the unstructured
// partition endpoint and returns the decrypted response.
//
// files must contain at least one file. opts may be nil to use
// defaults.
func (c *Client) Unstructured(ctx context.Context, files []UnstructuredFile, opts *UnstructuredOptions) ([]byte, error) {
	if len(files) == 0 {
		return nil, fmt.Errorf("at least one file is required")
	}
	if len(c.currentSecret.Data) == 0 {
		return nil, fmt.Errorf("no secret available: call Initialize() and UpdateSecret() first")
	}

	body, contentType, err := buildUnstructuredBody(files, opts)
	if err != nil {
		return nil, fmt.Errorf("building request body: %w", err)
	}

	cipher, err := crypto.NewRequestCipher(c.currentSecret.Data, c.currentSecret.ID)
	if err != nil {
		return nil, fmt.Errorf("creating request cipher: %w", err)
	}

	encrypted, err := cipher.Encrypt(body)
	if err != nil {
		return nil, fmt.Errorf("encrypting request body: %w", err)
	}

	reqURL := c.apiBaseURL + "/unstructured/general/v0/general"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader([]byte(encrypted)))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)

	respBody, err := c.doAPIRequestAndReadBody(req)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}

	decrypted, err := forwarder.MutateAllJSONFields(respBody, cipher.DecryptResponse, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypting response: %w", err)
	}

	return decrypted, nil
}

// buildUnstructuredBody builds a multipart/form-data body from the
// given files and options. It returns the encoded body string and the
// Content-Type header value (including boundary).
func buildUnstructuredBody(files []UnstructuredFile, opts *UnstructuredOptions) (string, string, error) {
	buf := &bytes.Buffer{}
	writer := multipart.NewWriter(buf)

	for _, f := range files {
		partHeader := make(textproto.MIMEHeader)
		partHeader.Set("Content-Disposition", fmt.Sprintf(`form-data; name="files"; filename=%q`, f.Name))
		if f.ContentType != "" {
			partHeader.Set("Content-Type", f.ContentType)
		}
		part, err := writer.CreatePart(partHeader)
		if err != nil {
			return "", "", fmt.Errorf("creating file part: %w", err)
		}
		if _, err := part.Write(f.Content); err != nil {
			return "", "", fmt.Errorf("writing file content: %w", err)
		}
	}

	if opts != nil {
		formFields, err := unstructuredFormFields(opts)
		if err != nil {
			return "", "", fmt.Errorf("building form fields: %w", err)
		}
		for field, value := range formFields {
			if err := writer.WriteField(field, value); err != nil {
				return "", "", fmt.Errorf("writing field %q: %w", field, err)
			}
		}
	}

	if err := writer.Close(); err != nil {
		return "", "", fmt.Errorf("closing multipart writer: %w", err)
	}

	return buf.String(), writer.FormDataContentType(), nil
}

// unstructuredFormFields returns a map of non-empty form fields for the
// given options. Slice fields are JSON-encoded.
//
// See https://docs.unstructured.io/api-reference/legacy-api/partition/api-parameters
func unstructuredFormFields(opts *UnstructuredOptions) (map[string]string, error) {
	fields := map[string]string{}

	if opts.Strategy != "" {
		fields["strategy"] = opts.Strategy
	}
	if opts.ChunkingStrategy != "" {
		fields["chunking_strategy"] = opts.ChunkingStrategy
	}
	if opts.Coordinates {
		fields["coordinates"] = "true"
	}
	if opts.Encoding != "" {
		fields["encoding"] = opts.Encoding
	}
	if len(opts.ExtractImageBlockTypes) > 0 {
		b, err := json.Marshal(opts.ExtractImageBlockTypes)
		if err != nil {
			return nil, fmt.Errorf("marshalling extract_image_block_types: %w", err)
		}
		fields["extract_image_block_types"] = string(b)
	}
	if opts.HiResModelName != "" {
		fields["hi_res_model_name"] = opts.HiResModelName
	}
	if opts.IncludePageBreaks {
		fields["include_page_breaks"] = "true"
	}
	if len(opts.Languages) > 0 {
		b, err := json.Marshal(opts.Languages)
		if err != nil {
			return nil, fmt.Errorf("marshalling languages: %w", err)
		}
		fields["languages"] = string(b)
	}
	if opts.OutputFormat != "" {
		fields["output_format"] = opts.OutputFormat
	}
	if len(opts.SkipInferTableTypes) > 0 {
		b, err := json.Marshal(opts.SkipInferTableTypes)
		if err != nil {
			return nil, fmt.Errorf("marshalling skip_infer_table_types: %w", err)
		}
		fields["skip_infer_table_types"] = string(b)
	}
	if opts.StartingPageNumber != nil {
		fields["starting_page_number"] = strconv.Itoa(*opts.StartingPageNumber)
	}
	if opts.UniqueElementIDs {
		fields["unique_element_ids"] = "true"
	}
	if opts.XMLKeepTags {
		fields["xml_keep_tags"] = "true"
	}

	return fields, nil
}
