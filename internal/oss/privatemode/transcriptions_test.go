// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package privatemode

import (
	"bytes"
	"context"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"github.com/edgelesssys/continuum/internal/oss/auth"
	"github.com/edgelesssys/continuum/internal/oss/sse"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTranscribeAudio(t *testing.T) {
	testCases := map[string]struct {
		file AudioFile
		opts AudioTranscriptionOptions
	}{
		"empty content": {
			file: AudioFile{Name: "sample.wav"},
			opts: AudioTranscriptionOptions{Model: "voxtral-mini-3b"},
		},
		"empty name": {
			file: AudioFile{Content: []byte("audio")},
			opts: AudioTranscriptionOptions{Model: "voxtral-mini-3b"},
		},
		"empty model": {
			file: AudioFile{Name: "sample.wav", Content: []byte("audio")},
		},
		"unsupported response format": {
			file: AudioFile{Name: "sample.wav", Content: []byte("audio")},
			opts: AudioTranscriptionOptions{Model: "voxtral-mini-3b", ResponseFormat: "text"},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			client := New(auth.APIKey("test-key"))
			_, err := client.TranscribeAudio(context.Background(), tc.file, tc.opts)
			require.Error(t, err)
		})
	}
}

func TestBuildAudioTranscriptionBodyAdditionalFields(t *testing.T) {
	body, contentType, err := buildAudioTranscriptionBody(
		AudioFile{Name: "sample.mp3", Content: []byte("audio"), ContentType: "audio/mpeg"},
		AudioTranscriptionOptions{
			Model: "voxtral-mini-3b",
			AdditionalFields: map[string][]string{
				"timestamp_granularities[]": {"word", "segment"},
			},
		},
		false,
	)
	require.NoError(t, err)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "/", bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", contentType)
	require.NoError(t, req.ParseMultipartForm(int64(len(body))))
	t.Cleanup(func() {
		if req.MultipartForm != nil {
			_ = req.MultipartForm.RemoveAll()
		}
	})

	require.Equal(t, []string{"word", "segment"}, req.MultipartForm.Value["timestamp_granularities[]"])
	file, _, err := req.FormFile("file")
	require.NoError(t, err)
	defer file.Close()
	gotAudio, err := io.ReadAll(file)
	require.NoError(t, err)
	require.Equal(t, []byte("audio"), gotAudio)
}

func TestBuildStreamingAudioTranscriptionBody(t *testing.T) {
	file := AudioFile{Name: "sample.wav", Content: []byte("audio"), ContentType: "audio/wav"}
	opts := AudioTranscriptionOptions{
		Model:          "whisper-large-v3",
		Language:       "en",
		Prompt:         "Privatemode",
		ResponseFormat: "verbose_json",
	}

	body, contentType, err := buildAudioTranscriptionBody(file, opts, true)
	require.NoError(t, err)

	_, params, err := mime.ParseMediaType(contentType)
	require.NoError(t, err)
	reader := multipart.NewReader(strings.NewReader(string(body)), params["boundary"])
	form, err := reader.ReadForm(1 << 20)
	require.NoError(t, err)
	defer func() { require.NoError(t, form.RemoveAll()) }()

	assert.Equal(t, []string{"whisper-large-v3"}, form.Value["model"])
	assert.Equal(t, []string{"en"}, form.Value["language"])
	assert.Equal(t, []string{"Privatemode"}, form.Value["prompt"])
	assert.Equal(t, []string{"verbose_json"}, form.Value["response_format"])
	assert.Equal(t, []string{"true"}, form.Value["stream"])
	require.Len(t, form.File["file"], 1)
	assert.Equal(t, "sample.wav", form.File["file"][0].Filename)
	assert.Equal(t, "audio/wav", form.File["file"][0].Header.Get("Content-Type"))
}

func TestAudioTranscriptionStream(t *testing.T) {
	body := io.NopCloser(strings.NewReader("data: {\"delta\":\"hello\"}\n\ndata: [DONE]\n\n"))
	stream := &AudioTranscriptionStream{reader: sse.NewEventReader(body, 1024), body: body}

	event, err := stream.Next()
	require.NoError(t, err)
	assert.JSONEq(t, `{"delta":"hello"}`, string(event))

	_, err = stream.Next()
	assert.ErrorIs(t, err, io.EOF)
	_, err = stream.Next()
	assert.ErrorIs(t, err, io.EOF)
}

func TestAudioTranscriptionStreamUnexpectedEOF(t *testing.T) {
	body := io.NopCloser(strings.NewReader("data: {}\n\n"))
	stream := &AudioTranscriptionStream{reader: sse.NewEventReader(body, 1024), body: body}

	_, err := stream.Next()
	require.NoError(t, err)
	_, err = stream.Next()
	assert.ErrorIs(t, err, io.ErrUnexpectedEOF)
}
