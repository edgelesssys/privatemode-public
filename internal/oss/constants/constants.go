// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

// Package constants defines constants such as file names and paths used by Continuum.
package constants

import (
	"os"
	"path/filepath"
	"time"
)

var version = "0.0.0-dev"

// Version is the version string embedded into binaries.
func Version() string { return version }

const (
	// WorkloadTaskGenerate is the vLLM task for text generation.
	WorkloadTaskGenerate = "generate"
	// WorkloadTaskToolCalling indicates models that support tool calling for the /v1/chat/completions API.
	WorkloadTaskToolCalling = "tool_calling"
	// WorkloadTaskVision indicates models that support image recognition for the /v1/chat/completions API.
	WorkloadTaskVision = "vision"
	// WorkloadTaskEmbed is the vLLM task for creating embeddings.
	WorkloadTaskEmbed = "embed"
	// WorkloadTaskTranscribe indicates models that support the /v1/audio/transcriptions API.
	WorkloadTaskTranscribe = "transcribe"
	// WorkloadTaskUnstructured indicates that a workload serves the Unstructured API for document
	// processing.
	WorkloadTaskUnstructured = "unstructured"

	// CacheDirEnv is the environment variable that specifies the cache directory of Continuum.
	// If unset, [os.UserCacheDir()] is used.
	// This defaults to $XDG_CACHE_HOME/continuum or $HOME/.cache/continuum on Unix systems, and $HOME/Library/Caches/continuum on Darwin.
	CacheDirEnv = "CONTINUUM_CACHE_DIR"
	// InferenceProxyServerPort is the port on which the inference-proxy listens for client connections.
	InferenceProxyServerPort = "8085"
	// MetricsServerPort is the port on which our standalone metrics servers are exposed by default.
	MetricsServerPort = "8185"
	// StatsFeedServerPort is the port on which the inference-proxy receives the vLLM stat-logger
	// feed by default.
	StatsFeedServerPort = "8285"
	// SecretServiceHealthPort is the port on which the Secret Service Health Server listens for health probes.
	SecretServiceHealthPort = "8080"
	// SecretServiceUserPort is the port on which the Secret Service listens on for client connection.
	SecretServiceUserPort = "3000"
	// WorkloadDefaultPort is the default port on which a workload container, e.g. vLLM or unstructured-api, listens for connections.
	WorkloadDefaultPort = "8008"

	// EtcdInferenceSecretPrefix is the prefix for inference secrets stored in etcd.
	EtcdInferenceSecretPrefix = "inference-secrets/"
	// etcdClientPort is the port on which the etcd server listens for client connections.
	etcdClientPort = "2379"
	// etcdPeerPort is the port on which the etcd server listens for peer connections.
	etcdPeerPort = "2380"
	// ManifestDir is the directory where the manifest log is stored.
	ManifestDir = "manifests"

	// PrivatemodeTargetModel is the header used to pass the name of the model which should process the request.
	PrivatemodeTargetModel = "Privatemode-Target-Model"
	// PrivatemodeShardKeyHeader is the key used to decide how to route requests, e.g., to reuse a cache.
	// Currently used for routing chat completions to reuse the prefix cache.
	PrivatemodeShardKeyHeader = "Privatemode-Shard-Key"
	// PrivatemodeContentLengthTokensHeader carries the estimated request length in tokens.
	PrivatemodeContentLengthTokensHeader = "Privatemode-Content-Length-Tokens"
	// PrivatemodeVersionHeader is a header sent by Privatemode clients with their version, indicating the expected API version.
	// It is used to check for version compatibility between client and server.
	PrivatemodeVersionHeader = "Privatemode-Version"
	// PrivatemodeOSHeader is the OS the Privatemode proxy is running on.
	PrivatemodeOSHeader = "Privatemode-OS"
	// PrivatemodeArchitectureHeader is the Platform the Privatemode proxy is running on.
	PrivatemodeArchitectureHeader = "Privatemode-Architecture"
	// PrivatemodeClientHeader is the App the Privatemode proxy is running.
	PrivatemodeClientHeader = "Privatemode-Client"
	// PrivatemodeClientApp is the [PrivatemodeClientHeader] value for the Privatemode client app.
	PrivatemodeClientApp = "App"
	// PrivatemodeClientProxy is the [PrivatemodeClientHeader] value for the Privatemode client proxy.
	PrivatemodeClientProxy = "Proxy"
	// PrivatemodeClientAPIGateway is the [PrivatemodeClientHeader] value for the Api Gateway.
	PrivatemodeClientAPIGateway = "ApiGateway"
	// PrivatemodeClientSDK is the [PrivatemodeClientHeader] value for the Privatemode SDK client.
	PrivatemodeClientSDK = "SDK"
	// PrivatemodeNvidiaOCSPPolicyHeader is the header used to allow specific NVIDIA OCSP status codes.
	PrivatemodeNvidiaOCSPPolicyHeader = "Privatemode-NVIDIA-OCSP-Policy"
	// PrivatemodeNvidiaOCSPPolicyMACHeader is the header used to verify the integrity of the Privatemode-NVIDIA-OCSP-Policy header.
	PrivatemodeNvidiaOCSPPolicyMACHeader = "Privatemode-NVIDIA-OCSP-Policy-MAC"
	// PrivatemodeSecretIDHeader is the header used to pass the inference secret ID to the inference proxy from the client.
	// Even though this information is already available in the request body if used, this serves as an additional hint for the proxy
	// to facilitate OCSP checks, which rely on the inference secret ID.
	PrivatemodeSecretIDHeader = "Privatemode-Secret-ID"
	// PrivatemodeJWTHeader carries a Clerk session JWT for API-key-less authentication.
	PrivatemodeJWTHeader = "Privatemode-JWT"
	// PrivatemodeAnonymousAuthHeader carries client fingerprinting data for anonymous auth.
	PrivatemodeAnonymousAuthHeader = "Privatemode-Anonymous-Auth"

	// APIEndpoint is the endpoint of the Privatemode API.
	APIEndpoint = "api.privatemode.ai:443"

	// ErrorNoSecretForID is the error message returned when no secret is found for a given ID.
	// NOTE: This is used for error checking in the PM proxy and should not be changed lightly for backwards compatibility.
	ErrorNoSecretForID = "no secret for ID"

	// CacheSaltHashLength is the length of the cache salt hash, i.e., the first bytes of the shard key.
	CacheSaltHashLength = 16
	// CacheBlockSizeTokens is the number of tokens in a cache block.
	CacheBlockSizeTokens = 16
	// ShardKeyCharsPerToken is the number of content characters used to estimate one token
	// when generating shard keys and estimating the request length in tokens.
	ShardKeyCharsPerToken = 4
	// ImageEmbeddingTokens is the number of tokens a single image is counted as for sharding
	// and load balancing. It is the maximum embedding size of the Kimi K2.6 vision model,
	// derived from its image processor config: in_patch_limit (16384) divided by the merged
	// patch area (merge_kernel_size^2 = 2^2 = 4) yields 4096 tokens. This is an approximation
	// that is good enough for load balancing and different values may be used for other models
	// in the future.
	ImageEmbeddingTokens = 4096
	// ShardKeyFirstBoundaryBlocksPerChar is the number of blocks per character before the first boundary.
	ShardKeyFirstBoundaryBlocksPerChar = 1
	// ShardKeyFirstBoundaryBlocks is the number of cache blocks before the first boundary.
	ShardKeyFirstBoundaryBlocks = 1024 / CacheBlockSizeTokens
	// ShardKeySecondBoundaryBlocksPerChar is the number of blocks per character between the first and second boundary.
	// 8 blocks * 16 tokens = 128 tokens.
	ShardKeySecondBoundaryBlocksPerChar = 8
	// ShardKeySecondBoundaryBlocks is the number of cache blocks between the first and second boundary.
	ShardKeySecondBoundaryBlocks = 100_096 / CacheBlockSizeTokens
	// ShardKeyThirdBoundaryBlocksPerChar is the number of blocks per character after the second boundary.
	// 64 blocks * 16 tokens = 1024 tokens. Keys from proxies < v1.57.0 encode 512 tokens per character
	// here; the API Gateway then overestimates their cached prefix, which only degrades load balancing.
	ShardKeyThirdBoundaryBlocksPerChar = 64
	// ShardKeyThirdBoundaryBlocks is the number of cache blocks at the end of the third range,
	// i.e., the largest context a shard key is generated for.
	ShardKeyThirdBoundaryBlocks = 2_000_000 / CacheBlockSizeTokens

	// InferenceSecretTTL is the time-to-live for inference secrets.
	InferenceSecretTTL = time.Hour

	// MaxFileSizeBytes is the maximum request body size that users may upload. The user-facing
	// limit is 50 MB of file content. This higher value allows for overhead due to encryption,
	// multipart form-data boundaries, and HTTP framing.
	MaxFileSizeBytes = 64 * 1024 * 1024 // 64MiB
	// MaxBodySizeExceededMsg should be presented to the user when [MaxFileSizeBytes] is exceeded.
	MaxBodySizeExceededMsg = "maximum request size exceeded (max 50MB)"

	// MaxUnstructuredBodySize is the maximum request body size that users may submit to the
	// unstructured API. The user-facing limit is 25 MB of file content. This higher value
	// intentionally includes overhead, see also [MaxFileSizeBytes].
	MaxUnstructuredBodySize = 25 * 1024 * 1024 // 25MiB
	// MaxUnstructuredBodySizeExceededMsg should be presented to the user when [MaxUnstructuredBodySize] is exceeded.
	MaxUnstructuredBodySizeExceededMsg = "file too large (max 25MB)"

	// MaxUnaryResponseBodyBytes is the maximum size of an upstream unary response body that the
	// forwarder will buffer into memory. Streaming responses are not subject to this limit.
	MaxUnaryResponseBodyBytes = 128 * 1024 * 1024 // 128MiB

	// MaxSSEEventBytes is the maximum allowed length of a single SSE (Server-Sent Events) event
	// when reading streaming inference responses.
	MaxSSEEventBytes = 256 * 1024 // 256 KiB

	// MaxEncryptedFrameSize is the maximum size of a single encrypted body frame that a decrypter
	// will read. It accommodates the largest body in either direction
	// ([MaxUnaryResponseBodyBytes] exceeds [MaxFileSizeBytes]) plus encryption and framing overhead.
	MaxEncryptedFrameSize = MaxUnaryResponseBodyBytes + 64

	// MetricsEndpoint is the endpoint where Prometheus metrics are exposed by default.
	MetricsEndpoint = "/metrics"
)

// ContinuumBaseDir is the base directory for files created or used by Continuum.
func ContinuumBaseDir() string {
	if baseDir := os.Getenv("CONTINUUM_BASE_DIR"); baseDir != "" {
		return baseDir
	}
	return "/var/run/continuum"
}

// EtcdBasePath is the base path for etcd related files.
func EtcdBasePath() string { return filepath.Join(ContinuumBaseDir(), "etcd") }

// OCSPStatusFile is the file where the OCSP status of the GPU, VBIOS, and driver is stored.
func OCSPStatusFile() string { return filepath.Join(ContinuumBaseDir(), "ocsp-status.json") }

// EtcdClientPort is the port on which the etcd server listens for client connections.
// Returns the value of the CONTINUUM_ETCD_CLIENT_PORT env variable or [etcdClientPort] if not set.
func EtcdClientPort() string {
	if port := os.Getenv("CONTINUUM_ETCD_CLIENT_PORT"); port != "" {
		return port
	}
	return etcdClientPort
}

// EtcdPeerPort is the port on which the etcd server listens for peer connections.
// Returns the value of the CONTINUUM_ETCD_PEER_PORT env variable or [etcdPeerPort] if not set.
func EtcdPeerPort() string {
	if port := os.Getenv("CONTINUUM_ETCD_PEER_PORT"); port != "" {
		return port
	}
	return etcdPeerPort
}
