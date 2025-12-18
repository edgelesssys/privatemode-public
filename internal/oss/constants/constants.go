// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

// Package constants defines constants such as file names and paths used by Continuum.
package constants

import (
	"os"
	"path/filepath"
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
	// WorkloadTaskTranslate indicates models that support the /v1/audio/translations API.
	WorkloadTaskTranslate = "translate"

	// CacheDirEnv is the environment variable that specifies the cache directory of Continuum.
	// If unset, [os.UserCacheDir()] is used.
	// This defaults to $XDG_CACHE_HOME/continuum or $HOME/.cache/continuum on Unix systems, and $HOME/Library/Caches/continuum on Darwin.
	CacheDirEnv = "CONTINUUM_CACHE_DIR"
	// ProxyServerPort is the port on which the proxy server runs. It can be static, since it is run in a container.
	ProxyServerPort = "8085"
	// MetricsServerPort is the port on which our standalone metrics servers are exposed by default.
	MetricsServerPort = "8185"
	// AttestationServiceUserPort is the port on which the Attestation Service listens on for connections with users (Continuum CLI).
	AttestationServiceUserPort = "3000"
	// AttestationServiceHealthPort is the port on which the Attestation Service Health Server listens for health probes.
	AttestationServiceHealthPort = "3001"
	// AttestationServiceBackendPort is the port on which the Attestation Service listens on for connections with worker nodes.
	AttestationServiceBackendPort = "9000"
	// SecretServiceUserPort is the port on which the Secret Service listens on for connections with users (Continuum CLI).
	SecretServiceUserPort = "3000"
	// SecretServiceBackendPort is the port on which the Secret Service listens on for connections with worker nodes.
	SecretServiceBackendPort = "9000"
	// WorkloadDefaultExposedPort is the default port on which a workload container, and therefore the inference-proxy, listens for connections.
	WorkloadDefaultExposedPort = "8008"
	// LoadBalancerDefaultPort is the default port on which the local API Gateway load balancer listens for connections.
	LoadBalancerDefaultPort = "8000"

	// EtcdInferenceSecretPrefix is the prefix for inference secrets stored in etcd.
	EtcdInferenceSecretPrefix = "inference-secrets/"
	// etcdClientPort is the port on which the etcd server listens for client connections.
	etcdClientPort = "2379"
	// etcdPeerPort is the port on which the etcd server listens for peer connections.
	etcdPeerPort = "2380"
	// ManifestDir is the directory where the manifest log is stored.
	ManifestDir = "manifests"

	// PrivatemodeShardKeyHeader is the key used to decide how to route requests, e.g., to reuse a cache.
	// Currently used for routing chat completions to reuse the prefix cache.
	PrivatemodeShardKeyHeader = "Privatemode-Shard-Key"
	// PrivatemodeVersionHeader is an HTTP header sent by the Privatemode components on every request.
	// It is used to check for version compatibility between client and server.
	PrivatemodeVersionHeader = "Privatemode-Version"
	// PrivatemodeOSHeader is the OS the Privatemode proxy is running on.
	PrivatemodeOSHeader = "Privatemode-OS"
	// PrivatemodeArchitectureHeader is the Platform the Privatemode proxy is running on.
	PrivatemodeArchitectureHeader = "Privatemode-Architecture"
	// PrivatemodeClientHeader is the App the Privatemode proxy is running, either "Proxy" or "App".
	PrivatemodeClientHeader = "Privatemode-Client"
	// PrivatemodeClientApp is the PrivatemodeClientHeader value for the Privatemode client app.
	PrivatemodeClientApp = "App"
	// PrivatemodeClientProxy is the PrivatemodeClientHeader value for the Privatemode client proxy.
	PrivatemodeClientProxy = "Proxy"
	// PrivatemodeClientAPIGateway is the PrivatemodeClientHeader value for the Api Gateway.
	PrivatemodeClientAPIGateway = "ApiGateway"
	// RequestIDHeader is the header used to identify requests. It will be set by envoy if not set by the client.
	// X-Request-ID is mostly standard and also supported by envoy.
	// cf. https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_conn_man/headers#config-http-conn-man-headers-x-request-id
	RequestIDHeader = "X-Request-ID"
	// PrivatemodeNvidiaOCSPPolicyHeader is the header used to allow specific NVIDIA OCSP status codes.
	PrivatemodeNvidiaOCSPPolicyHeader = "Privatemode-NVIDIA-OCSP-Policy"
	// PrivatemodeNvidiaOCSPPolicyMACHeader is the header used to verify the integrity of the Privatemode-NVIDIA-OCSP-Policy header.
	PrivatemodeNvidiaOCSPPolicyMACHeader = "Privatemode-NVIDIA-OCSP-Policy-MAC"
	// PrivatemodeSecretIDHeader is the header used to pass the inference secret ID to the inference proxy from the client.
	// Even though this information is already available in the request body if used, this serves as an additional hint for the proxy
	// to facilitate OCSP checks, which rely on the inference secret ID.
	PrivatemodeSecretIDHeader = "Privatemode-Secret-ID"

	// SecretServiceEndpoint is the endpoint of the secret service.
	SecretServiceEndpoint = "secret.privatemode.ai:443"
	// APIEndpoint is the endpoint of the Privatemode API.
	APIEndpoint = "api.privatemode.ai:443"
	// CoordinatorEndpoint is the endpoint of the Contrast coordinator.
	CoordinatorEndpoint = "coordinator.privatemode.ai:443"

	// ErrorNoSecretForID is the error message returned when no secret is found for a given ID.
	// NOTE: This is used for error checking in the PM proxy and should not be changed lightly for backwards compatibility.
	ErrorNoSecretForID = "no secret for ID"

	// CacheSaltHashLength is the length of the cache salt hash, i.e., the first bytes of the shard key.
	CacheSaltHashLength = 16
	// CacheBlockSizeTokens is the number of tokens in a cache block.
	CacheBlockSizeTokens = 16
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
	// 32 blocks * 16 tokens = 512 tokens.
	ShardKeyThirdBoundaryBlocksPerChar = 32
	// ShardKeyThirdBoundaryBlocks is the number of cache blocks after the second boundary.
	ShardKeyThirdBoundaryBlocks = 1_000_000 / CacheBlockSizeTokens

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
