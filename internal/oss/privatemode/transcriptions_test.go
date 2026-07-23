// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package privatemode

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/edgelesssys/continuum/internal/oss/auth"
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
