// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package privatemode

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"slices"
	"strconv"

	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/edgelesssys/continuum/internal/oss/openai"
)

// AudioFile represents an audio file to transcribe.
type AudioFile struct {
	// Name is the filename (e.g. "recording.mp3").
	Name string
	// Content is the raw audio file content.
	Content []byte
	// ContentType is an optional MIME type hint for the file
	// (e.g. "audio/mpeg"). If empty, the server will auto-detect
	// the type.
	ContentType string
}

// AudioTranscriptionOptions contains optional parameters for the
// OpenAI-compatible audio transcriptions endpoint.
type AudioTranscriptionOptions struct {
	// Model is the speech-to-text model to use.
	Model string `json:"model"`
	// Language is an optional ISO-639-1 language code.
	Language string `json:"language,omitempty"`
	// Prompt is optional text to guide transcription style or continue
	// a previous audio segment.
	Prompt string `json:"prompt,omitempty"`
	// ResponseFormat controls the response format. The default is JSON.
	ResponseFormat string `json:"response_format,omitempty"`
	// Temperature controls sampling temperature.
	Temperature *float32 `json:"temperature,omitempty"`
	// AdditionalFields contains OpenAI-compatible multipart fields that are not
	// modeled explicitly above. Field names use the flattened multipart
	// notation used by the OpenAI SDK, such as "include[]".
	AdditionalFields map[string][]string `json:"additional_fields,omitempty"`
}

// TranscribeAudio sends an encrypted request to the OpenAI-compatible
// audio transcriptions endpoint and returns the decrypted response.
func (c *Client) TranscribeAudio(ctx context.Context, file AudioFile, opts AudioTranscriptionOptions) ([]byte, error) {
	resp, err := c.TranscribeAudioWithOptions(ctx, file, opts, RequestOptions{})
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

// TranscribeAudioWithOptions sends an encrypted request to the OpenAI-compatible
// audio transcriptions endpoint and preserves the response headers.
func (c *Client) TranscribeAudioWithOptions(
	ctx context.Context,
	file AudioFile,
	opts AudioTranscriptionOptions,
	requestOpts RequestOptions,
) (*Response, error) {
	if len(file.Content) == 0 {
		return nil, fmt.Errorf("audio file content must not be empty")
	}
	if file.Name == "" {
		return nil, fmt.Errorf("audio file name must not be empty")
	}
	if opts.Model == "" {
		return nil, fmt.Errorf("model must not be empty")
	}
	if opts.ResponseFormat != "" && opts.ResponseFormat != "json" && opts.ResponseFormat != "verbose_json" {
		return nil, fmt.Errorf("unsupported response format %q: only JSON transcription responses are supported", opts.ResponseFormat)
	}

	body, contentType, err := buildAudioTranscriptionBody(file, opts)
	if err != nil {
		return nil, fmt.Errorf("building request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiBaseURL+openai.TranscriptionsEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header = cloneRequestHeader(requestOpts.Header)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set(constants.PrivatemodeTargetModel, opts.Model)

	resp, err := c.doEncryptedAPIRequest(req)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	decrypted, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading decrypted response: %w", err)
	}

	return &Response{Header: resp.Header.Clone(), Body: decrypted}, nil
}

func buildAudioTranscriptionBody(file AudioFile, opts AudioTranscriptionOptions) ([]byte, string, error) {
	buf := &bytes.Buffer{}
	writer := multipart.NewWriter(buf)

	if err := writer.WriteField("model", opts.Model); err != nil {
		return nil, "", fmt.Errorf("writing model field: %w", err)
	}
	if opts.Language != "" {
		if err := writer.WriteField("language", opts.Language); err != nil {
			return nil, "", fmt.Errorf("writing language field: %w", err)
		}
	}
	if opts.Prompt != "" {
		if err := writer.WriteField("prompt", opts.Prompt); err != nil {
			return nil, "", fmt.Errorf("writing prompt field: %w", err)
		}
	}
	if opts.ResponseFormat != "" {
		if err := writer.WriteField("response_format", opts.ResponseFormat); err != nil {
			return nil, "", fmt.Errorf("writing response_format field: %w", err)
		}
	}
	if opts.Temperature != nil {
		if err := writer.WriteField("temperature", strconv.FormatFloat(float64(*opts.Temperature), 'f', -1, 32)); err != nil {
			return nil, "", fmt.Errorf("writing temperature field: %w", err)
		}
	}
	fieldNames := make([]string, 0, len(opts.AdditionalFields))
	for name := range opts.AdditionalFields {
		fieldNames = append(fieldNames, name)
	}
	slices.Sort(fieldNames)
	for _, name := range fieldNames {
		for _, value := range opts.AdditionalFields[name] {
			if err := writer.WriteField(name, value); err != nil {
				return nil, "", fmt.Errorf("writing %s field: %w", name, err)
			}
		}
	}

	partHeader := make(textproto.MIMEHeader)
	partHeader.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename=%q`, file.Name))
	if file.ContentType != "" {
		partHeader.Set("Content-Type", file.ContentType)
	}
	part, err := writer.CreatePart(partHeader)
	if err != nil {
		return nil, "", fmt.Errorf("creating file part: %w", err)
	}
	if _, err := part.Write(file.Content); err != nil {
		return nil, "", fmt.Errorf("writing file content: %w", err)
	}

	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("closing multipart writer: %w", err)
	}

	return buf.Bytes(), writer.FormDataContentType(), nil
}
