// package builder handles the set up of an etcd server on the Attestation Service node.
package builder

import (
	"context"
	"fmt"

	"github.com/edgelesssys/continuum/internal/gpl/constants"
	"go.etcd.io/etcd/api/v3/authpb"
	"go.etcd.io/etcd/api/v3/etcdserverpb"
	"go.etcd.io/etcd/server/v3/embed"
)

const (
	// EtcdClientName is the user name for etcd clients.
	// Client certificates should use this name as the Common Name.
	EtcdClientName = "continuum-etcd-client"
)

// StartNewCluster creates a new etcd cluster with the current node as the first member.
func StartNewCluster(ctx context.Context, host, serverCrt, serverKey, caCrt string) (srv *embed.Etcd, err error) {
	cfg, err := newClusterConfig("default", host, serverCrt, serverKey, caCrt) // TODO(daniel-weisse): get node name from metadata API
	if err != nil {
		return nil, fmt.Errorf("creating etcd config: %w", err)
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
func JoinExistingCluster() error {
	_, err := joinClusterConfig("", "", "", "", "", "")
	if err != nil {
		return fmt.Errorf("creating etcd config: %w", err)
	}
	panic("not implemented")
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
