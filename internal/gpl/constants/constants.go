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
	// ServedModel is the deployed model for the SaaS.
	ServedModel = "ibnzterrell/Meta-Llama-3.3-70B-Instruct-AWQ-INT4"
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

	// EtcdInferenceSecretPrefix is the prefix for inference secrets stored in etcd.
	EtcdInferenceSecretPrefix = "inference-secrets/"
	// EtcdClientPort is the port on which the etcd server listens for client connections.
	EtcdClientPort = "2379"
	// EtcdPeerPort is the port on which the etcd server listens for peer connections.
	EtcdPeerPort = "2380"
	// ManifestDir is the directory where the manifest log is stored.
	ManifestDir = "manifests"

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

// EtcdPKIPath is the path where the etcd PKI files are stored.
func EtcdPKIPath() string { return filepath.Join(EtcdBasePath(), "pki") }
