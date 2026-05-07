// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package privatemode

import (
	"bytes"
	"context"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strconv"

	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/edgelesssys/continuum/internal/oss/crypto"
	"github.com/edgelesssys/continuum/internal/oss/forwarder"
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
}

// TranscribeAudio sends an encrypted request to the OpenAI-compatible
// audio transcriptions endpoint and returns the decrypted response.
func (c *Client) TranscribeAudio(ctx context.Context, file AudioFile, opts AudioTranscriptionOptions) ([]byte, error) {
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
	if len(c.currentSecret.Data) == 0 {
		return nil, fmt.Errorf("no secret available: call Initialize() and UpdateSecret() first")
	}

	cipher, err := crypto.NewRequestCipher(c.currentSecret.Data, c.currentSecret.ID)
	if err != nil {
		return nil, fmt.Errorf("creating request cipher: %w", err)
	}

	body, contentType, err := buildAudioTranscriptionBody(file, opts, cipher)
	if err != nil {
		return nil, fmt.Errorf("building request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiBaseURL+openai.TranscriptionsEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set(constants.PrivatemodeTargetModel, opts.Model)

	respBody, err := c.doAPIRequestAndReadBody(req)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", tryDecryptResponseError(err, cipher, openai.PlainTranscriptionResponseFields))
	}

	decrypted, err := forwarder.MutateJSONFields(respBody, cipher.DecryptResponse, openai.PlainTranscriptionResponseFields)
	if err != nil {
		return nil, fmt.Errorf("decrypting response: %w", err)
	}

	return decrypted, nil
}

func buildAudioTranscriptionBody(
	file AudioFile, opts AudioTranscriptionOptions, cipher *crypto.RequestCipher,
) ([]byte, string, error) {
	buf := &bytes.Buffer{}
	writer := multipart.NewWriter(buf)

	if err := writer.WriteField("model", opts.Model); err != nil {
		return nil, "", fmt.Errorf("writing model field: %w", err)
	}
	if opts.Language != "" {
		if err := writeEncryptedField(writer, "language", opts.Language, cipher); err != nil {
			return nil, "", err
		}
	}
	if opts.Prompt != "" {
		if err := writeEncryptedField(writer, "prompt", opts.Prompt, cipher); err != nil {
			return nil, "", err
		}
	}
	if opts.ResponseFormat != "" {
		if err := writeEncryptedField(writer, "response_format", opts.ResponseFormat, cipher); err != nil {
			return nil, "", err
		}
	}
	if opts.Temperature != nil {
		if err := writeEncryptedField(writer, "temperature", strconv.FormatFloat(float64(*opts.Temperature), 'f', -1, 32), cipher); err != nil {
			return nil, "", err
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
	encryptedFile, err := cipher.Encrypt(string(file.Content))
	if err != nil {
		return nil, "", fmt.Errorf("encrypting file: %w", err)
	}
	if _, err := part.Write([]byte(encryptedFile)); err != nil {
		return nil, "", fmt.Errorf("writing file content: %w", err)
	}

	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("closing multipart writer: %w", err)
	}

	return buf.Bytes(), writer.FormDataContentType(), nil
}

func writeEncryptedField(writer *multipart.Writer, name, value string, cipher *crypto.RequestCipher) error {
	encrypted, err := cipher.Encrypt(value)
	if err != nil {
		return fmt.Errorf("encrypting field %q: %w", name, err)
	}
	if err := writer.WriteField(name, encrypted); err != nil {
		return fmt.Errorf("writing field %q: %w", name, err)
	}
	return nil
}
