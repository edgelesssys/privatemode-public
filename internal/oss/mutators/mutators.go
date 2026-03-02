// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

// Package mutators provides shared request injectors/mutators for Privatemode clients.
//
// TODO(msanft): Revisit if this package should exist with internal client RFC.
package mutators

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/edgelesssys/continuum/internal/oss/forwarder"
	"github.com/edgelesssys/continuum/internal/oss/persist"
	"github.com/tidwall/gjson"
)

// ShardKeyInjector returns a [forwarder.RequestMutator] that injects a
// shard key header into the request. When defaultCacheSalt is empty, a
// random cache salt is assumed and no shard key is set unless the
// request already contains one.
func ShardKeyInjector(defaultCacheSalt string, log *slog.Logger) forwarder.RequestMutator {
	// Reads the cache salt and generates a shard key using sha256.
	// Returns an error if there is no cache salt in the request body.
	return func(r *http.Request) error {
		bodyBytes, err := persist.ReadBodyUnlimited(r)
		if err != nil {
			return fmt.Errorf("reading request body: %w", err)
		}

		httpBody := string(bodyBytes)
		if len(httpBody) == 0 {
			return nil
		}
		cacheSalt := gjson.Get(httpBody, "cache_salt").String()

		// If there is no explicit cache salt, we use the default cache salt.
		if cacheSalt == "" {
			cacheSalt = defaultCacheSalt
		}

		// If there is no cache salt, we use default sharding without a shard key.
		if cacheSalt != "" {
			// /chat/completions
			tools := gjson.Get(httpBody, "tools").String()
			messages := gjson.Get(httpBody, "messages").String()

			// /completions
			prompt := gjson.Get(httpBody, "prompt").String()
			suffix := gjson.Get(httpBody, "suffix").String()

			// /v1/messages sends the system prompt as its own field
			systemPrompt := gjson.Get(httpBody, "system").String()

			// NOTE: The order is important and must match the chat template of the model.
			// For many models, tools are defined first, whithin or after the system message.
			// This is the case for Llama and DeepSeek. Gemma does not have tools right now.
			//
			// Mistral puts tools right before the last user message. Once we use a model
			// that does not store tools in the beginning, we may want to create a
			// model-specific shard key to avoid cache misses due to changing tools.
			// Potentially, we may also adjust the chat template for such models but this
			// could have a performance impact.
			content := systemPrompt + tools + messages + prompt + suffix
			shardKey, err := generateShardKey(cacheSalt, content, log)
			if err != nil {
				return fmt.Errorf("generating shard key: %w", err)
			}

			r.Header.Set(constants.PrivatemodeShardKeyHeader, shardKey)
		}

		return nil
	}
}

// ModelHeaderInjector returns a [forwarder.RequestMutator] that
// extracts the model name from the request and sets it as a header.
func ModelHeaderInjector(extractor func(*http.Request) (string, error)) forwarder.RequestMutator {
	return func(r *http.Request) error {
		model, err := extractor(r)
		if err != nil {
			return fmt.Errorf("extracting model: %w", err)
		}
		r.Header.Set(constants.PrivatemodeTargetModel, model)
		return nil
	}
}

// generateShardKey generates a shard key from a cache salt and content
// string.
func generateShardKey(cacheSalt string, content string, log *slog.Logger) (string, error) {
	cacheSaltHash := sha256.Sum256([]byte(cacheSalt))
	shardKeyStr := hex.EncodeToString(cacheSaltHash[:])[:constants.CacheSaltHashLength]

	// Estimate number of tokens n as content length // 4
	n := len(content) / 4

	// Currently, only 1Mio tokens to limit the shard key size. Limiting factors are proxies,
	// where nginx supports only 4kb. But currently, this only goes to the API Gateway such
	// that we could also work with headers larger than 4kb. Envoy also supports more. But
	// could still be a problem for client side proxies.
	//
	// For extending this beyond 1Mio token context size we should have a clear plan on how to
	// support larger keys and/or compress a bit more for large context (e.g., > 100k tokens).
	if n > 1_000_000 {
		log.Error("Context too large for shard key generation", slog.Int("tokens", n))
		return "", fmt.Errorf("context too large: ~%d tokens", n)
	}

	blockSize := constants.ShardKeyFirstBoundaryBlocksPerChar * constants.CacheBlockSizeTokens

	// No caching if n < blockSize
	// -> return the base shard key immediately
	if n < blockSize {
		return shardKeyStr, nil
	}

	// Iterate over content, starting with step size 16, doubling with each step
	// using 4 chars to represent 1 token.
	contentBytes := []byte(content)

	// Use the cache salt as initial hash.
	var chunkHash [32]byte
	copy(chunkHash[:], cacheSaltHash[:])
	shardKeyStr += "-"
	for i := 0; i+blockSize <= len(contentBytes)/4; {
		end := i + blockSize
		chunk := contentBytes[i*4 : end*4]

		// We prefix the chunk with the cache salt to avoid exposing any information
		// and to make the sequence unique even if there are minor changes not captured by the
		// 6 bit value extracted below. This also avoids side channel attacks, as the cache
		// salt is never exposed.
		chunkHash = sha256.Sum256(append(chunkHash[:], chunk...))
		last6Bits := chunkHash[len(chunkHash)-1] & 0x3F
		shardKeyStr += string("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"[last6Bits])

		// increase step size
		// - step = 16 from 16...100k -> 62 chars
		// - step = 128 from 1k...100k -> 774 chars
		// - step = 512 from 100k...1M -> 1758 chars
		i += blockSize
		switch i {
		case constants.ShardKeyFirstBoundaryBlocks * constants.CacheBlockSizeTokens:
			blockSize = constants.ShardKeySecondBoundaryBlocksPerChar * constants.CacheBlockSizeTokens
		case constants.ShardKeySecondBoundaryBlocks * constants.CacheBlockSizeTokens:
			blockSize = constants.ShardKeyThirdBoundaryBlocksPerChar * constants.CacheBlockSizeTokens
		}
	}

	return shardKeyStr, nil
}
