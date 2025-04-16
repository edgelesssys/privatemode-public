package builder

import (
	"fmt"
	"net"
	"net/url"

	"github.com/edgelesssys/continuum/internal/gpl/constants"
	"go.etcd.io/etcd/client/pkg/v3/transport"
	"go.etcd.io/etcd/server/v3/embed"
)

// newClusterConfig set up an etcd config to create a new cluster.
func newClusterConfig(nodeName, host, serverCrt, serverKey, caCrt string) (*embed.Config, error) {
	cfg, err := baseEtcdConfig(nodeName, host, serverCrt, serverKey, caCrt)
	if err != nil {
		return nil, err
	}

	cfg.InitialCluster = nodeName + fmt.Sprintf("=https://%s", net.JoinHostPort(host, constants.EtcdPeerPort))
	cfg.ClusterState = embed.ClusterStateFlagNew
	return cfg, nil
}

// joinClusterConfig sets up an etcd config to join an existing cluster.
func joinClusterConfig(nodeName, host, clusterURL, serverCrt, serverKey, caCrt string) (*embed.Config, error) {
	cfg, err := baseEtcdConfig(nodeName, host, serverCrt, serverKey, caCrt)
	if err != nil {
		return nil, err
	}

	cfg.InitialCluster = clusterURL
	cfg.ClusterState = embed.ClusterStateFlagExisting
	return cfg, nil
}

// baseEtcdConfig sets up the base config for an etcd server.
func baseEtcdConfig(nodeName, host, serverCrt, serverKey, caCrt string) (*embed.Config, error) {
	cfg := embed.NewConfig()

	cfg.Name = nodeName
	cfg.Dir = constants.EtcdBasePath()
	cfg.SnapshotCount = 10 // Continuum does not perform a lot of transactions, so we should create snapshots more regularly
	cfg.MaxTxnOps = 256

	listenClientURL, err := url.Parse(fmt.Sprintf("https://%s", net.JoinHostPort("0.0.0.0", constants.EtcdClientPort)))
	if err != nil {
		return nil, err
	}
	advertiseClientURL, err := url.Parse(fmt.Sprintf("https://%s", net.JoinHostPort(host, constants.EtcdClientPort)))
	if err != nil {
		return nil, err
	}
	listenPeerURL, err := url.Parse(fmt.Sprintf("https://%s", net.JoinHostPort("0.0.0.0", constants.EtcdPeerPort)))
	if err != nil {
		return nil, err
	}
	advertisePeerURL, err := url.Parse(fmt.Sprintf("https://%s", net.JoinHostPort(host, constants.EtcdPeerPort)))
	if err != nil {
		return nil, err
	}

	cfg.ListenPeerUrls = []url.URL{*listenPeerURL}
	cfg.ListenClientUrls = []url.URL{*listenClientURL}
	cfg.AdvertisePeerUrls = []url.URL{*advertisePeerURL}
	cfg.AdvertiseClientUrls = []url.URL{*advertiseClientURL}

	tlsConfig := transport.TLSInfo{
		CertFile:       serverCrt,
		KeyFile:        serverKey,
		TrustedCAFile:  caCrt,
		ClientCertAuth: true,
	}
	cfg.ClientTLSInfo = tlsConfig
	cfg.PeerTLSInfo = tlsConfig

	return cfg, nil
}
