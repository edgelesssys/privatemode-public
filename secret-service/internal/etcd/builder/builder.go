// Package builder handles the set up of an etcd server on the Attestation Service node.
package builder

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"time"

	"github.com/edgelesssys/continuum/internal/gpl/constants"
	"go.etcd.io/etcd/api/v3/authpb"
	"go.etcd.io/etcd/api/v3/etcdserverpb"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/server/v3/embed"
)

const (
	// EtcdClientName is the user name for etcd clients.
	// Client certificates should use this name as the Common Name.
	EtcdClientName = "continuum-etcd-client"
)

// BootstrapCluster creates a new etcd cluster with the current node as the first member.
func BootstrapCluster(ctx context.Context, k8sNamespace, serverCrt, serverKey, caCrt string) (srv *embed.Etcd, err error) {
	hostname, err := getHostname()
	if err != nil {
		return nil, fmt.Errorf("getting hostname: %w", err)
	}
	cfg, err := newClusterConfig(
		k8sNamespace,
		hostname, // Not strictly necessary, but useful to correlate an etcd member to a specific node
		serverCrt, serverKey, caCrt)
	if err != nil {
		return nil, fmt.Errorf("creating etcd bootstrap config: %w", err)
	}

	server, err := embed.StartEtcd(cfg)
	if err != nil {
		return nil, fmt.Errorf("starting etcd: %w", err)
	}
	defer func() {
		if err != nil {
			server.Close()
		}
	}()

	if err := configureAuth(ctx, server); err != nil {
		return nil, err
	}

	return server, nil
}

// JoinExistingCluster starts etcd and joins an existing etcd cluster.
// It works both when the node joins the existing cluster for the first time, but
// also when it has previously ungracefully left the cluster and is now rejoining.
func JoinExistingCluster(ctx context.Context, k8sNamespace,
	serverCrt, serverKey, caCrt string, log *slog.Logger,
) (srv *embed.Etcd, err error) {
	cli, err := newClient(k8sNamespace, serverCrt, serverKey, caCrt)
	if err != nil {
		return nil, fmt.Errorf("creating etcd client: %w", err)
	}
	defer cli.Close()

	hostname, err := getHostname()
	if err != nil {
		return nil, fmt.Errorf("getting hostname: %w", err)
	}

	// Needs to happen *before* adding the member again,
	// as otherwise, the cluster tries to talk to the added member that's not
	// yet started.
	log.Info("Getting available peers")
	knownPeers, err := getPeers(ctx, cli, log)
	if err != nil {
		return nil, fmt.Errorf("getting etcd peers: %w", err)
	}

	log.Info("Trying to remove existing etcd member", "hostname", hostname)
	if err := tryRemoveMember(ctx, cli, knownPeers, hostname, log); err != nil {
		return nil, fmt.Errorf("removing member %q from etcd cluster: %w", hostname, err)
	}

	log.Info("Trying to add etcd member", "hostname", hostname)
	if err := memberAdd(ctx, cli, k8sNamespace, hostname); err != nil {
		return nil, fmt.Errorf("adding member %q to existing etcd cluster: %w", hostname, err)
	}

	cfg, err := joinClusterConfig(knownPeers, k8sNamespace, hostname, serverCrt, serverKey, caCrt)
	if err != nil {
		return nil, fmt.Errorf("creating etcd join config: %w", err)
	}

	log.Info("Starting embedded etcd server", "hostname", hostname)
	server, err := embed.StartEtcd(cfg)
	if err != nil {
		return nil, fmt.Errorf("starting etcd: %w", err)
	}

	return server, nil
}

// memberAdd adds a new member to the etcd cluster.
func memberAdd(ctx context.Context, cli *clientv3.Client, k8sNamespace, hostname string) error {
	headlessServiceName, err := serviceName(headlessService, k8sNamespace)
	if err != nil {
		return fmt.Errorf("getting etcd headless service endpoint: %w", err)
	}

	ctxAdd, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_, err = cli.MemberAdd(ctxAdd, []string{
		// Peer URL of the new member (us)
		fmt.Sprintf("https://%s.%s", hostname, net.JoinHostPort(headlessServiceName, constants.EtcdPeerPort())),
	})
	if err != nil {
		return fmt.Errorf("adding member to etcd cluster: %w", err)
	}
	return nil
}

// tryRemoveMember attempts to remove a member from the etcd cluster by its name.
// If the member is not found, it returns nil.
// This is used as an idempotent operation to ensure that a member which has previously
// ungracefully left is removed before joining again.
func tryRemoveMember(ctx context.Context, cli *clientv3.Client, members map[string]etcdPeer,
	memberName string, log *slog.Logger,
) error {
	member, found := members[memberName]
	if !found {
		log.Info("Member not found, nothing to remove", "memberName", memberName)
		return nil // Member not found, nothing to remove
	}

	log.Info("Removing previously-failed etcd member", "memberName", memberName, "memberID", member.id)
	ctxRemove, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if _, err := cli.MemberRemove(ctxRemove, member.id); err != nil {
		return fmt.Errorf("removing member %q from etcd cluster: %w", memberName, err)
	}

	return nil
}

type etcdPeer struct {
	url string
	id  uint64
}

// getPeers returns a mapping of etcd member names to their peer URLs.
func getPeers(ctx context.Context, cli *clientv3.Client, log *slog.Logger) (map[string]etcdPeer, error) {
	resp, err := cli.MemberList(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing etcd members: %w", err)
	}

	peers := make(map[string]etcdPeer)
	for _, member := range resp.Members {
		if len(member.PeerURLs) == 0 {
			log.Warn("Member has no peer URLs, skipping", "member", member.Name)
			continue // Skip members without peer URLs
		}
		if len(member.PeerURLs) > 1 {
			log.Warn("Member has multiple peer URLs, using the first one",
				"member", member.Name, "peerURLs", member.PeerURLs)
		}
		peers[member.Name] = etcdPeer{
			url: member.PeerURLs[0], // Use the first peer URL
			id:  member.ID,
		}
	}

	return peers, nil
}

func newClient(k8sNamespace, serverCrt, serverKey, caCrt string) (*clientv3.Client, error) {
	internalServiceName, err := serviceName(internalService, k8sNamespace)
	if err != nil {
		return nil, fmt.Errorf("getting etcd endpoint: %w", err)
	}

	keyPair, err := tls.LoadX509KeyPair(serverCrt, serverKey)
	if err != nil {
		return nil, fmt.Errorf("loading etcd server key pair: %w", err)
	}
	caCert, err := os.ReadFile(caCrt)
	if err != nil {
		return nil, fmt.Errorf("reading etcd CA certificate: %w", err)
	}
	rootCAs := x509.NewCertPool()
	if !rootCAs.AppendCertsFromPEM(caCert) {
		return nil, errors.New("failed adding CA certificate to pool")
	}

	cliCfg := clientv3.Config{
		Endpoints: []string{
			// Endpoint of the existing cluster. We use the non-headless service name here,
			// so that we just get to any node in the existing etcd cluster.
			fmt.Sprintf("https://%s", net.JoinHostPort(internalServiceName, constants.EtcdClientPort())),
		},
		TLS: &tls.Config{
			Certificates: []tls.Certificate{keyPair},
			RootCAs:      rootCAs,
		},
	}
	cli, err := clientv3.New(cliCfg)
	if err != nil {
		return nil, fmt.Errorf("creating etcd client: %w", err)
	}

	return cli, nil
}

// configureAuth sets up RBAC for the etcd server.
func configureAuth(ctx context.Context, server *embed.Etcd) error {
	// Return early if auth is already enabled
	// This indicates the node was restarted and the etcd cluster was restored from disk
	if status, err := server.Server.AuthStatus(ctx, &etcdserverpb.AuthStatusRequest{}); err != nil {
		return fmt.Errorf("checking auth status: %w", err)
	} else if status.Enabled {
		return nil
	}

	if _, err := server.Server.RoleAdd(ctx, &etcdserverpb.AuthRoleAddRequest{
		Name: EtcdClientName,
	}); err != nil {
		return fmt.Errorf("adding role %q: %w", EtcdClientName, err)
	}

	if _, err := server.Server.RoleGrantPermission(ctx, &etcdserverpb.AuthRoleGrantPermissionRequest{
		Name: EtcdClientName,
		Perm: &authpb.Permission{
			PermType: authpb.READ,
			// Grant read permissions to all keys in the inference secret prefix
			// Using the etcd range syntax,
			// giving read access to the range [/foo/, /foo0) is equal to giving access to keys with a prefix /foo/
			Key:      []byte(constants.EtcdInferenceSecretPrefix),
			RangeEnd: []byte(constants.EtcdInferenceSecretPrefix[:len(constants.EtcdInferenceSecretPrefix)-1] + "0"),
		},
	}); err != nil {
		return fmt.Errorf("granting permission to role %q: %w", EtcdClientName, err)
	}

	for _, user := range []string{"root", EtcdClientName} {
		if _, err := server.Server.UserAdd(ctx, &etcdserverpb.AuthUserAddRequest{
			Name: user,
			// Disable password authentication
			// This means users can only authenticate using client certificates
			Options: &authpb.UserAddOptions{
				NoPassword: true,
			},
		}); err != nil {
			return fmt.Errorf("adding user %q: %w", user, err)
		}

		if _, err := server.Server.UserGrantRole(ctx, &etcdserverpb.AuthUserGrantRoleRequest{
			User: user,
			Role: user, // roles are named the same as the users, e.g. the role for the user "root" is "root"
		}); err != nil {
			return fmt.Errorf("granting role %q to user %q: %w", user, user, err)
		}
	}

	if _, err := server.Server.AuthEnable(ctx, &etcdserverpb.AuthEnableRequest{}); err != nil {
		return fmt.Errorf("enabling authentication: %w", err)
	}

	return nil
}

// getHostname retrieves the hostname of the current machine by
// checking the HOSTNAME environment variable or resorting to
// gethostname(2) if the variable is not set. This precedence
// ensures that the hostname can be set explicitly in tests.
func getHostname() (string, error) {
	if hostname := os.Getenv("HOSTNAME"); hostname != "" {
		return hostname, nil
	}

	hostname, err := os.Hostname()
	if err != nil {
		return "", fmt.Errorf("getting hostname: %w", err)
	}

	return hostname, nil
}
