// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package mutators

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestGenerateShardKey(t *testing.T) {
	cacheSalt := "test-salt"

	// Test different token count ranges to verify k calculation
	testCases := map[string]struct {
		contentLength     int
		contentHashLength int
		contentKey        string
		expectError       bool
	}{
		"empty":                 {contentLength: 0, contentHashLength: 0},
		"1->0, block size 16*4": {contentLength: 1, contentHashLength: 0},
		// 1 block of 16 tokens
		"63->1, block size 16*4": {contentLength: 16*4 - 1, contentHashLength: 0},
		"64->1, block size 16*4": {contentLength: 16 * 4, contentHashLength: 1, contentKey: "Q"},
		// 2 blocks of 16 tokens
		"127->1, block size 16*4": {contentLength: 32*4 - 1, contentHashLength: 1, contentKey: "Q"},
		"128->2, block size 16*4": {contentLength: 32 * 4, contentHashLength: 2, contentKey: "QK"},
		"129->2, block size 16*4": {contentLength: 32*4 + 1, contentHashLength: 2, contentKey: "QK"},
		// 3 blocks of 16 tokens
		"191->2, block size 16*4":    {contentLength: 48*4 - 1, contentHashLength: 2, contentKey: "QK"},
		"192->3, block size 16*4":    {contentLength: 48 * 4, contentHashLength: 3, contentKey: "QKx"},
		"193->3, block size 16*4":    {contentLength: 48*4 + 1, contentHashLength: 3, contentKey: "QKx"},
		"4095->63, block size 16*4":  {contentLength: 1024*4 - 1, contentHashLength: 63, contentKey: "QKxToFQ1MRRq7Cv3lYFdwK6SCGf2xm2Lb85NGe7Z+Zy8goI7wAWd/zYccoVSVlj"},
		"4096->64, block size 128*4": {contentLength: 1024 * 4, contentHashLength: 64, contentKey: "QKxToFQ1MRRq7Cv3lYFdwK6SCGf2xm2Lb85NGe7Z+Zy8goI7wAWd/zYccoVSVljT"},
		"4097->64, block size 128*4": {contentLength: 1024*4 + 1, contentHashLength: 64, contentKey: "QKxToFQ1MRRq7Cv3lYFdwK6SCGf2xm2Lb85NGe7Z+Zy8goI7wAWd/zYccoVSVljT"},
		"4607->64, block size 128*4": {contentLength: (1024+128)*4 - 1, contentHashLength: 64, contentKey: "QKxToFQ1MRRq7Cv3lYFdwK6SCGf2xm2Lb85NGe7Z+Zy8goI7wAWd/zYccoVSVljT"},
		"4224->64, block size 128*4": {contentLength: (1024 + 128) * 4, contentHashLength: 65, contentKey: "QKxToFQ1MRRq7Cv3lYFdwK6SCGf2xm2Lb85NGe7Z+Zy8goI7wAWd/zYccoVSVljTI"},
		"100k-1, block size 128*4":   {contentLength: 100_096*4 - 1, contentHashLength: 64 + 773},
		"100k, block size 512*4":     {contentLength: 100_096 * 4, contentHashLength: 64 + 774},
		"100k+1, block size 512*4":   {contentLength: 100_096*4 + 1, contentHashLength: 64 + 774},
		"1M-1, block size 512*4":     {contentLength: 1_000_000*4 - 1, contentHashLength: 64 + 774 + 1757},
		"1M, block size 512*4":       {contentLength: 1_000_000 * 4, contentHashLength: 64 + 774 + 1757},
		"1M+0.75, block size 512*4":  {contentLength: 1_000_000*4 + 3, contentHashLength: 64 + 774 + 1757},
		"1M+1, error":                {contentLength: 1_000_000*4 + 4, contentHashLength: -1, expectError: true},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			assert := assert.New(t)
			content := string(bytes.Repeat([]byte("a"), tc.contentLength))

			shardKey, err := generateShardKey(cacheSalt, content)

			if tc.expectError {
				require.Error(err)
				return
			}

			require.NoError(err)

			// "saltHash-contentHash"
			shardKeyLength := constants.CacheSaltHashLength + tc.contentHashLength
			if tc.contentHashLength > 0 {
				shardKeyLength++ // '-'
			}
			assert.Len(shardKey, shardKeyLength)

			if len(tc.contentKey) > 0 {
				actualContentHash := shardKey[constants.CacheSaltHashLength:]
				assert.Equal("-"+tc.contentKey, actualContentHash)
			}
		})
	}
}

// imageDataURL builds a base64 data URL with rawBytes of random image data.
func imageDataURL(t *testing.T, rawBytes int) string {
	t.Helper()
	raw := make([]byte, rawBytes)
	_, err := rand.Read(raw)
	require.NoError(t, err)
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(raw)
}

// part is one content part. Its fields cover every layout the injector must handle across the
// OpenAI and Anthropic formats; building the body with json.Marshal (see makeBody) guarantees
// valid escaping for any input. omitempty keeps each part minimal, and the pointer Text
// distinguishes an absent text field from a present-but-empty one (the empty-value test).
type part struct {
	Type     string         `json:"type"`
	Text     *string        `json:"text,omitempty"`      // text part
	ImageURL any            `json:"image_url,omitempty"` // OpenAI: {"url":...} object or a bare string
	Source   map[string]any `json:"source,omitempty"`    // Anthropic: {"type":...,"data"|"url":...}
	Content  []part         `json:"content,omitempty"`   // Anthropic tool_result nesting
}

func ptr[T any](v T) *T { return &v }

// makeBody marshals a chat-completions body with one user message per parts slice and an
// optional cache salt. json.Marshal handles all escaping, so callers pass raw values.
func makeBody(t *testing.T, cacheSalt string, messages ...[]part) string {
	t.Helper()
	type message struct {
		Role    string `json:"role"`
		Content []part `json:"content"`
	}
	msgs := make([]message, 0, len(messages))
	for _, parts := range messages {
		msgs = append(msgs, message{Role: "user", Content: parts})
	}
	b, err := json.Marshal(struct {
		CacheSalt string    `json:"cache_salt,omitempty"`
		Messages  []message `json:"messages"`
	}{CacheSalt: cacheSalt, Messages: msgs})
	require.NoError(t, err)
	return string(b)
}

// chatBodyWithImage builds a /chat/completions body with a text part and an image part.
func chatBodyWithImage(t *testing.T, cacheSalt, text, imageURL string) string {
	return makeBody(t, cacheSalt, []part{
		{Type: "text", Text: ptr(text)},
		{Type: "image_url", ImageURL: map[string]any{"url": imageURL}},
	})
}

// runInjector runs the ShardKeyInjector mutator (with an empty default cache salt) on a
// request with the given body.
func runInjector(t *testing.T, body string) *http.Request {
	t.Helper()
	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	require.NoError(t, ShardKeyInjector("")(r))
	return r
}

// wantTokens returns the token count for a body whose only shard content is its messages
// array, with each image swapped for a one-embedding placeholder. The placeholder keeps the
// surrounding quotes, so messages length changes by placeholderLen + 2 - len(rawToken) per
// image, where rawToken is the JSON-encoded value (matching what replaceImages substitutes,
// including any escaping). Result is divided into tokens.
func wantTokens(t *testing.T, body string, images ...string) int {
	t.Helper()
	const placeholderLen = constants.ImageEmbeddingTokens * constants.ShardKeyCharsPerToken
	contentLen := len(gjson.Get(body, "messages").String())
	for _, img := range images {
		raw, err := json.Marshal(img)
		require.NoError(t, err)
		contentLen += placeholderLen + 2 - len(raw)
	}
	return contentLen / constants.ShardKeyCharsPerToken
}

func TestShardKeyInjectorImages(t *testing.T) {
	// 4 MiB of raw data -> >5 MiB of base64. Counted as raw bytes this would be ~1.4M tokens
	// and exceed the 1M shard key limit; counted by embedding size it is one embedding.
	largeImg := imageDataURL(t, 4*1024*1024)
	require.Greater(t, len(largeImg), 4*1024*1024)

	// Three random test images.
	img1, img2, img3 := imageDataURL(t, 2048), imageDataURL(t, 2048), imageDataURL(t, 2048)

	// Adversarial URL with chars that JSON escapes (quote, backslash, newline): matching on the
	// decoded value would miss it; matching on the raw token bounds it. json.Marshal makes the
	// escaping exact.
	escapedImageURL := "data:image/png;base64," + strings.Repeat("A", 5*1024*1024) + "\"\\\nABC"

	testCases := map[string]struct {
		body         string
		images       []string // images embedded in body, used to derive the expected token count
		wantShardKey bool
	}{
		"large image not rejected": {
			body:   chatBodyWithImage(t, "salt", "describe this", largeImg),
			images: []string{largeImg}, wantShardKey: true,
		},
		"multiple images per message and across messages each count as an embedding": {
			body: makeBody(
				t, "salt",
				[]part{
					{Type: "image_url", ImageURL: map[string]any{"url": img1}},
					{Type: "image_url", ImageURL: map[string]any{"url": img2}},
				},
				[]part{{Type: "image_url", ImageURL: map[string]any{"url": img3}}},
			),
			images: []string{img1, img2, img3}, wantShardKey: true,
		},
		"the same image twice counts as two embeddings": {
			// The seen-map only avoids a redundant replace pass; ReplaceAll already swaps both
			// occurrences, so a repeated image must still count as two embeddings.
			body: makeBody(t, "salt", []part{
				{Type: "image_url", ImageURL: map[string]any{"url": img1}},
				{Type: "image_url", ImageURL: map[string]any{"url": img1}},
			}),
			images: []string{img1, img1}, wantShardKey: true,
		},
		"content length is set without a shard key": {
			// No cache_salt and an empty default salt -> no shard key, but the token count is set.
			body:   chatBodyWithImage(t, "", "hi", img1),
			images: []string{img1},
		},
		"openai string-form image_url counted by embedding size": {
			body: makeBody(t, "salt", []part{
				{Type: "image_url", ImageURL: img1},
			}),
			images: []string{img1}, wantShardKey: true,
		},
		"anthropic base64 image counted by embedding size": {
			body: makeBody(t, "salt", []part{
				{Type: "image", Source: map[string]any{"type": "base64", "data": img1}},
			}),
			images: []string{img1}, wantShardKey: true,
		},
		"anthropic url image counted by embedding size": {
			body: makeBody(t, "salt", []part{
				{Type: "image", Source: map[string]any{"type": "url", "url": img1}},
			}),
			images: []string{img1}, wantShardKey: true,
		},
		"anthropic tool-result nested image counted by embedding size": {
			// Tool results nest the image one level deeper (content[].content[].source.data).
			body: makeBody(t, "salt", []part{
				{Type: "tool_result", Content: []part{
					{Type: "image", Source: map[string]any{"type": "base64", "data": img1}},
				}},
			}),
			images: []string{img1}, wantShardKey: true,
		},
		"empty image value is skipped, not over-matched": {
			// res.Raw is `""`; replacing it would rewrite every empty string (here the text part).
			body: makeBody(t, "salt", []part{
				{Type: "text", Text: ptr("")},
				{Type: "image_url", ImageURL: map[string]any{"url": ""}},
			}),
			wantShardKey: true,
		},
		"image with JSON-escaped characters is still bounded": {
			body: makeBody(t, "salt", []part{
				{Type: "image_url", ImageURL: map[string]any{"url": escapedImageURL}},
			}),
			images: []string{escapedImageURL}, wantShardKey: true,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			assert := assert.New(t)

			r := runInjector(t, tc.body)

			shardKey := r.Header.Get(constants.PrivatemodeShardKeyHeader)
			if tc.wantShardKey {
				// A non-empty shard key also confirms generation did not overflow the 1M limit.
				assert.NotEmpty(shardKey)
			} else {
				assert.Empty(shardKey)
			}

			tokens, err := strconv.Atoi(r.Header.Get(constants.PrivatemodeContentLengthTokensHeader))
			require.NoError(err)
			assert.Equal(wantTokens(t, tc.body, tc.images...), tokens)
		})
	}
}

func TestShardKeyInjectorImageContent(t *testing.T) {
	assert := assert.New(t)

	bodyA := chatBodyWithImage(t, "salt", "hello", imageDataURL(t, 1024))
	bodyB := chatBodyWithImage(t, "salt", "hello", imageDataURL(t, 1024))

	keyA := runInjector(t, bodyA).Header.Get(constants.PrivatemodeShardKeyHeader)
	keyB := runInjector(t, bodyB).Header.Get(constants.PrivatemodeShardKeyHeader)
	keyARepeat := runInjector(t, bodyA).Header.Get(constants.PrivatemodeShardKeyHeader)

	assert.NotEmpty(keyA)
	assert.Equal(keyA, keyARepeat, "same content must yield the same shard key")
	assert.NotEqual(keyA, keyB, "different images must yield different shard keys")
}

func TestImagePlaceholder(t *testing.T) {
	assert := assert.New(t)

	p := imagePlaceholder("some-image-data")
	// The placeholder makes the image count as exactly one embedding worth of tokens.
	assert.Len(p, constants.ImageEmbeddingTokens*constants.ShardKeyCharsPerToken)
	assert.Equal(constants.ImageEmbeddingTokens, len(p)/constants.ShardKeyCharsPerToken)
	// It is the sha256 hex hash repeated, so it starts with that hash.
	assert.True(strings.HasPrefix(p, fmt.Sprintf("%x", sha256.Sum256([]byte("some-image-data")))))
	// Different data yields a different placeholder.
	assert.NotEqual(p, imagePlaceholder("other-image-data"))
}

func BenchmarkGenerateShardKey_1M(b *testing.B) {
	cacheSalt := "test-salt"
	// 1M tokens -> contentLength: 1_000_000 * 4 (see unit test)
	content := string(bytes.Repeat([]byte("a"), 1_000_000*4))

	start := time.Now()
	for b.Loop() {
		if _, err := generateShardKey(cacheSalt, content); err != nil {
			b.Fatalf("unexpected error: %v", err)
		}
	}
	avg := time.Since(start) / time.Duration(b.N)

	// 1 Mio tokens take about 2.5ms on a Mac M2 and ~20ms in CI; enforce <50ms per op.
	assert.Less(b, avg, 50*time.Millisecond, "shard key generation too slow")
}
