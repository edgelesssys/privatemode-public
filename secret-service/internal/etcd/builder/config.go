package builder

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/edgelesssys/continuum/internal/oss/constants"
	"go.etcd.io/etcd/client/pkg/v3/transport"
	"go.etcd.io/etcd/server/v3/embed"
)

type serviceKind int

const (
	// headlessService is the headless service used for per-pod addressing of the etcd cluster.
	headlessService serviceKind = iota
	// internalService is the internal service used to access the etcd cluster as a whole.
	internalService
)

// newClusterConfig set up an etcd config to create a new cluster.
func newClusterConfig(k8sNamespace, memberName, serverCrt, serverKey, caCrt string) (*embed.Config, error) {
	cfg, err := baseEtcdConfig(map[string]etcdPeer{}, k8sNamespace, memberName, serverCrt, serverKey, caCrt)
	if err != nil {
		return nil, err
	}
	cfg.ClusterState = embed.ClusterStateFlagNew
	return cfg, nil
}

// joinClusterConfig sets up an etcd config to join an existing cluster.
func joinClusterConfig(knownPeers map[string]etcdPeer, k8sNamespace, memberName, serverCrt, serverKey, caCrt string) (*embed.Config, error) {
	cfg, err := baseEtcdConfig(knownPeers, k8sNamespace, memberName, serverCrt, serverKey, caCrt)
	if err != nil {
		return nil, err
	}
	cfg.ClusterState = embed.ClusterStateFlagExisting
	return cfg, nil
}

// baseEtcdConfig sets up the base config for an etcd server.
func baseEtcdConfig(knownPeers map[string]etcdPeer, k8sNamespace, hostname, serverCrt, serverKey, caCrt string) (*embed.Config, error) {
	cfg := embed.NewConfig()

	serviceName, err := serviceName(headlessService, k8sNamespace)
	if err != nil {
		return nil, fmt.Errorf("getting etcd endpoint: %w", err)
	}

	cfg.Name = hostname
	cfg.Dir = constants.EtcdBasePath()
	cfg.SnapshotCount = 10 // Continuum does not perform a lot of transactions, so we should create snapshots more regularly
	cfg.MaxTxnOps = 256

	initialCluster, err := initialCluster(knownPeers, k8sNamespace, hostname)
	if err != nil {
		return nil, fmt.Errorf("getting initial cluster configuration: %w", err)
	}
	cfg.InitialCluster = initialCluster

	listenClientURL, err := url.Parse(fmt.Sprintf("https://%s", net.JoinHostPort("0.0.0.0", constants.EtcdClientPort())))
	if err != nil {
		return nil, err
	}
	advertiseClientURL, err := url.Parse(fmt.Sprintf("https://%s.%s", hostname, net.JoinHostPort(serviceName, constants.EtcdClientPort())))
	if err != nil {
		return nil, err
	}
	listenPeerURL, err := url.Parse(fmt.Sprintf("https://%s", net.JoinHostPort("0.0.0.0", constants.EtcdPeerPort())))
	if err != nil {
		return nil, err
	}
	advertisePeerURL, err := url.Parse(fmt.Sprintf("https://%s.%s", hostname, net.JoinHostPort(serviceName, constants.EtcdPeerPort())))
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
	cfg.PeerTLSInfo.SkipClientSANVerify = true

	return cfg, nil
}

// serviceName returns the name of the service etcd can be reached at.
// If headless is true, it returns the headless service name of the statefulset.
func serviceName(kind serviceKind, k8sNamespace string) (string, error) {
	// secretServiceHostname is the hostname used to discover the etcd cluster.
	var secretServiceHostname string
	switch kind {
	case headlessService:
		secretServiceHostname = "secret-service-headless"
	case internalService:
		secretServiceHostname = "secret-service-internal"
	default:
		return "", fmt.Errorf("unknown service kind: %d", kind)
	}

	return fmt.Sprintf("%s.%s.svc.cluster.local", secretServiceHostname, k8sNamespace), nil
}

// initialCluster returns the initial cluster configuration for etcd depending on the node's role.
func initialCluster(knownPeers map[string]etcdPeer, k8sNamespace, podName string) (string, error) {
	headlessServiceName, err := serviceName(headlessService, k8sNamespace)
	if err != nil {
		return "", fmt.Errorf("getting etcd endpoint: %w", err)
	}

	instanceNumber, err := strconv.Atoi(strings.TrimPrefix(podName, "secret-service-"))
	if err != nil {
		return "", fmt.Errorf("parsing instance number from node name %q: %w", podName, err)
	}

	// Make sure that we get at least all pods up to our current one
	for i := 0; i <= instanceNumber; i++ {
		knownPeers[fmt.Sprintf("secret-service-%d", i)] = etcdPeer{
			url: fmt.Sprintf(
				"https://secret-service-%d.%s",
				i, net.JoinHostPort(headlessServiceName, constants.EtcdPeerPort())),
			id: 0, // Unknown ID
		}
	}

	nodes := make([]string, len(knownPeers))
	for node, peerURL := range knownPeers {
		nodes = append(nodes, fmt.Sprintf("%s=%s", node, peerURL.url))
	}

	return strings.Join(nodes, ","), nil
}
