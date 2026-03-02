// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package mutators

import (
	"bytes"
	"log/slog"
	"testing"
	"time"

	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

			shardKey, err := generateShardKey(cacheSalt, content, slog.Default())

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

func BenchmarkGenerateShardKey_1M(b *testing.B) {
	cacheSalt := "test-salt"
	// 1M tokens -> contentLength: 1_000_000 * 4 (see unit test)
	content := string(bytes.Repeat([]byte("a"), 1_000_000*4))

	start := time.Now()
	for b.Loop() {
		if _, err := generateShardKey(cacheSalt, content, slog.Default()); err != nil {
			b.Fatalf("unexpected error: %v", err)
		}
	}
	avg := time.Since(start) / time.Duration(b.N)

	// 1 Mio tokens take about 2.5ms on a Mac M2 and ~20ms in CI; enforce <50ms per op.
	assert.Less(b, avg, 50*time.Millisecond, "shard key generation too slow")
}
