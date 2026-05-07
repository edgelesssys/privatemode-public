// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package privatemode

import (
	"context"
	"testing"

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
			client := New("test-key")
			_, err := client.TranscribeAudio(context.Background(), tc.file, tc.opts)
			require.Error(t, err)
		})
	}
}
