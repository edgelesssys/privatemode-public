// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: GPL-3.0-only

// package constants defines constants such as file names and paths used by Continuum.
package constants

import (
	"os"
	"path/filepath"
)

var version = "0.0.0-dev"

// Version is the version string embedded into binaries.
func Version() string { return version }

const (
	// DefaultTextgenModel is the deployed model for the SaaS.
	DefaultTextgenModel = "ibnzterrell/Meta-Llama-3.3-70B-Instruct-AWQ-INT4"
	// WorkloadTaskGenerate is the vLLM task for text generation.
	WorkloadTaskGenerate = "generate"
	// WorkloadTaskToolCalling indicates models that support tool calling for the /v1/chat/completions API.
	WorkloadTaskToolCalling = "tool_calling"
	// WorkloadTaskVision indicates models that support image recognition for the /v1/chat/completions API.
	WorkloadTaskVision = "vision"
	// WorkloadTaskEmbed is the vLLM task for creating embeddings.
	WorkloadTaskEmbed = "embed"
	// WorkloadTaskTranscribe indicates models that support the /v1/transcriptions API.
	WorkloadTaskTranscribe = "transcribe"

	// CacheDirEnv is the environment variable that specifies the cache directory of Continuum.
	// If unset, [os.UserCacheDir()] is used.
	// This defaults to $XDG_CACHE_HOME/continuum or $HOME/.cache/continuum on Unix systems, and $HOME/Library/Caches/continuum on Darwin.
	CacheDirEnv = "CONTINUUM_CACHE_DIR"
	// ProxyServerPort is the port on which the proxy server runs. It can be static, since it is run in a container.
	ProxyServerPort = "8085"
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
	// EtcdClientPort is the port on which the etcd server listens for client connections.
	EtcdClientPort = "2379"
	// EtcdPeerPort is the port on which the etcd server listens for peer connections.
	EtcdPeerPort = "2380"
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

	// SecretServiceEndpoint is the endpoint of the secret service.
	SecretServiceEndpoint = "secret.privatemode.ai:443"
	// APIEndpoint is the endpoint of the Privatemode API.
	APIEndpoint = "api.privatemode.ai:443"
	// CoordinatorEndpoint is the endpoint of the Contrast coordinator.
	CoordinatorEndpoint = "coordinator.privatemode.ai:443"
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
