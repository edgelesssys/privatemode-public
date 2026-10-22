// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

// Package mutators provides shared request injectors/mutators for Privatemode clients.
//
// TODO(msanft): Revisit if this package should exist with internal client RFC.
package mutators

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/edgelesssys/continuum/internal/oss/forwarder"
	"github.com/edgelesssys/continuum/internal/oss/model"
	"github.com/edgelesssys/continuum/internal/oss/persist"
	"github.com/tidwall/gjson"
)

// ErrRemoteResourceURL indicates that a media URL uses a scheme other than data.
var ErrRemoteResourceURL = errors.New("remote resource URLs are not allowed, use data URLs instead")

// imageDataQueries locate image payloads (base64 data or URLs) in chat messages, across the
// OpenAI and Anthropic formats. "#" + "@flatten" collect every image, so each is counted once.
var imageDataQueries = []string{
	// OpenAI: content[].image_url.url (object form) or content[].image_url (string form).
	`#.content.#.image_url.url|@flatten`,
	`#.content.#.image_url|@flatten`,
	// Anthropic: content[].source.data (base64) or content[].source.url.
	`#.content.#.source.data|@flatten`,
	`#.content.#.source.url|@flatten`,
	// Anthropic tool results nest media one level deeper: content[].content[].source.{data,url}.
	`#.content.#.content.#.source.data|@flatten|@flatten`,
	`#.content.#.content.#.source.url|@flatten|@flatten`,
}

// ShardKeyInjector returns a [forwarder.RequestMutator] that injects the estimated
// usage and a shard key header into the request. When defaultCacheSalt is empty, a
// random cache salt is assumed and no shard key is set unless the
// request already contains one.
func ShardKeyInjector(defaultCacheSalt string) forwarder.RequestMutator {
	// Reads the cache salt and generates a shard key using sha256.
	// Returns an error if there is no cache salt in the request body.
	return forwarder.RequestMutatorChain(ContentLengthTokensInjector, func(r *http.Request) error {
		bodyBytes, err := persist.ReadBodyUnlimited(r)
		if err != nil {
			return fmt.Errorf("reading request body: %w", err)
		}

		if len(bodyBytes) == 0 {
			return nil
		}

		cacheSalt := gjson.GetBytes(bodyBytes, "cache_salt").String()

		// If there is no explicit cache salt, we use the default cache salt.
		if cacheSalt == "" {
			cacheSalt = defaultCacheSalt
		}

		// If there is no cache salt, we use default sharding without a shard key.
		if cacheSalt != "" {
			shardKey, err := generateShardKey(cacheSalt, shardContent(bodyBytes))
			if err != nil {
				return forwarder.NewError("generating shard key", err, http.StatusRequestEntityTooLarge)
			}

			r.Header.Set(constants.PrivatemodeShardKeyHeader, shardKey)
		}

		return nil
	})
}

// ContentLengthTokensInjector sets the estimated inference usage header on a plaintext request.
// The [constants.PrivatemodeTargetModel] header must be set before calling this mutator.
func ContentLengthTokensInjector(r *http.Request) error {
	body, err := persist.ReadBodyUnlimited(r)
	if err != nil {
		return fmt.Errorf("reading request body: %w", err)
	}
	estimate, err := EstimateContentTokens(body, r.URL.Path, r.Header.Get(constants.PrivatemodeTargetModel))
	if err != nil {
		return fmt.Errorf("estimating content tokens: %w", err)
	}
	r.Header.Set(constants.PrivatemodeContentLengthTokensHeader, strconv.FormatInt(estimate, 10))
	return nil
}

// EstimateContentTokens estimates the tokens in a plaintext inference request.
// Images are counted by their embedding size, matching the content used for sharding.
// Audio duration is estimated in whole seconds and converted to model-specific prompt tokens.
func EstimateContentTokens(httpBody []byte, endpoint, modelName string) (int64, error) {
	switch endpoint {
	case "/v1/audio/transcriptions":
		return EstimateAudioTokens(int64(len(httpBody)), modelName), nil
	case "/v1/embeddings":
		return estimateEmbeddingTokens(httpBody), nil
	case "/v1/chat/completions", "/v1/completions", "/v1/messages":
		return int64(len(shardContent(httpBody))) / constants.ShardKeyCharsPerToken, nil
	case "/unstructured", "/unstructured/general/v0/general", "/general/v0/general":
		// The API gateway strips /unstructured before forwarding to the inference proxy.
		return 0, nil
	default:
		return -1, fmt.Errorf("unknown endpoint: %s", endpoint)
	}
}

// estimateEmbeddingTokens estimates tokens from only the input of an embeddings request.
func estimateEmbeddingTokens(httpBody []byte) int64 {
	input := gjson.GetBytes(httpBody, "input").String()
	return int64(len(input)) / constants.ShardKeyCharsPerToken
}

// EstimateAudioTokens estimates prompt tokens from the total request body size.
// Duration is estimated at 64 Kibit/s of compressed mono audio, rounding up to full seconds.
func EstimateAudioTokens(bodySize int64, modelName string) int64 {
	const audioBytesPerSecond = 64 * 1024 / 8
	seconds := bodySize / audioBytesPerSecond
	if bodySize%audioBytesPerSecond != 0 {
		seconds++
	}
	if model.Resolve(modelName) == model.OpenAIWhisperLargeV3 {
		return seconds * model.TokensPerAudioSecondWhisper
	}
	return seconds*model.TokensPerAudioSecondVoxtral + model.TokenOverheadVoxtral
}

// EstimateAudioDuration estimates audio duration in whole seconds from model-specific prompt tokens.
func EstimateAudioDuration(audioTokens int64, modelName string) int64 {
	if model.Resolve(modelName) == model.OpenAIWhisperLargeV3 {
		return audioTokens / model.TokensPerAudioSecondWhisper
	}
	return max(0, (audioTokens-model.TokenOverheadVoxtral)/model.TokensPerAudioSecondVoxtral)
}

// shardContent assembles the request content used for shard key generation and token
// estimation. Image payloads are replaced by a hash sized to the image's embedding so the
// resulting length reflects the number of tokens the model actually sees (see [replaceImages]).
func shardContent(httpBody []byte) string {
	// /chat/completions
	tools := gjson.GetBytes(httpBody, "tools").String()
	messages := gjson.GetBytes(httpBody, "messages").String()

	// /completions
	prompt := gjson.GetBytes(httpBody, "prompt").String()
	suffix := gjson.GetBytes(httpBody, "suffix").String()

	// /v1/messages sends the system prompt as its own field
	systemPrompt := gjson.GetBytes(httpBody, "system").String()

	messages = replaceImages(messages)

	// NOTE: The order is important and must match the chat template of the model.
	// For many models, tools are defined first, whithin or after the system message.
	// This is the case for Llama and DeepSeek. Gemma does not have tools right now.
	//
	// Mistral puts tools right before the last user message. Once we use a model
	// that does not store tools in the beginning, we may want to create a
	// model-specific shard key to avoid cache misses due to changing tools.
	// Potentially, we may also adjust the chat template for such models but this
	// could have a performance impact.
	return systemPrompt + tools + messages + prompt + suffix
}

// replaceImages swaps every image payload in messages for a fixed-size placeholder (see
// [imagePlaceholder]); payloads are located via [imageDataQueries].
//
// We match on the raw JSON token ([gjson.Result.Raw], quoted and still-escaped), not the
// decoded value, so the replace is exact whatever the escaping (quotes/backslashes in a URL)
// and even malformed data stays bounded to one embedding. The decoded value only feeds the hash.
func replaceImages(messages string) string {
	seen := make(map[string]struct{})
	for _, query := range imageDataQueries {
		gjson.Get(messages, query).ForEach(func(_, res gjson.Result) bool {
			// Skip empty values: an empty image string has res.Raw == `""`, and replacing
			// that would rewrite every empty string in messages (e.g. an empty text part).
			if res.Type != gjson.String || res.Str == "" {
				return true
			}
			if _, ok := seen[res.Raw]; ok {
				return true
			}
			seen[res.Raw] = struct{}{}
			// res.Raw is quoted; keep quotes around the placeholder so messages stays well-formed.
			messages = strings.ReplaceAll(messages, res.Raw, `"`+imagePlaceholder(res.String())+`"`)
			return true
		})
	}
	return messages
}

// imagePlaceholder returns the image's sha256 hash repeated to fill one embedding's worth of
// content ([constants.ImageEmbeddingTokens] tokens). Content-derived, so the shard key stays
// sensitive to the image while its length is bounded.
func imagePlaceholder(data string) string {
	sum := sha256.Sum256([]byte(data))
	hash := hex.EncodeToString(sum[:])

	// Shard content is char-based, so scale tokens to chars.
	size := constants.ImageEmbeddingTokens * constants.ShardKeyCharsPerToken
	return strings.Repeat(hash, size/len(hash)+1)[:size]
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

// maxShardKeyTokens is the largest estimated token count a shard key is generated for. It is
// twice the largest context window, as the estimate of JSON bytes per
// [constants.ShardKeyCharsPerToken] overshoots the real token count.
const maxShardKeyTokens = constants.ShardKeyThirdBoundaryBlocks * constants.CacheBlockSizeTokens

// generateShardKey generates a shard key from a cache salt and content
// string.
func generateShardKey(cacheSalt string, content string) (string, error) {
	cacheSaltHash := sha256.Sum256([]byte(cacheSalt))
	shardKeyStr := hex.EncodeToString(cacheSaltHash[:])[:constants.CacheSaltHashLength]

	// Estimate number of tokens from bytes
	n := len(content) / constants.ShardKeyCharsPerToken

	// The limit bounds the shard key size, ~2.7k chars at maxShardKeyTokens. We budget 4k for the
	// header line: beyond ~3.4M tokens the key would exceed it, which may matter for client-side
	// proxies (nginx's default large header buffer is 8k); the API Gateway's Envoy accepts more.
	//
	// For extending this further we should have a clear plan on how to support larger keys
	// and/or compress a bit more for large context (e.g., > 100k tokens).
	if n > maxShardKeyTokens {
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
		// - step = 16 from 16...1k -> 64 chars
		// - step = 128 from 1k...100k -> 774 chars
		// - step = 1024 from 100k...2M -> 1855 chars
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
