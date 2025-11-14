// Package etcd implements a client to interact with etcd.
package etcd

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/edgelesssys/continuum/inference-proxy/internal/secrets"
	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/spf13/afero"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// Etcd is a client to interact with etcd.
type Etcd struct {
	client etcdClient

	closeChan chan struct{}
	log       *slog.Logger
}

// New creates a new etcd client.
// This function attempts to load client certificates and CA from the filesystem.
func New(hosts []string, etcdMemberCert, etcdMemberKey, etcdCA string, fs afero.Afero, log *slog.Logger) (*Etcd, func(), error) {
	keyPair, err := tls.LoadX509KeyPair(etcdMemberCert, etcdMemberKey)
	if err != nil {
		return nil, nil, err
	}
	caCert, err := fs.ReadFile(etcdCA)
	if err != nil {
		return nil, nil, err
	}
	rootCAs := x509.NewCertPool()
	if !rootCAs.AppendCertsFromPEM(caCert) {
		return nil, nil, errors.New("failed adding CA certificate to pool")
	}

	client, err := clientv3.New(clientv3.Config{
		Endpoints:            getEndpointsFromHosts(hosts),
		AutoSyncInterval:     10 * time.Minute, // check for new endpoints once every 10 minutes
		DialTimeout:          5 * time.Second,  // fail if we can't connect within 5 seconds
		DialKeepAliveTime:    2 * time.Minute,  // send a keepalive every 2 minutes
		DialKeepAliveTimeout: 5 * time.Second,  // fail if we can't send a keepalive within 5 seconds
		TLS: &tls.Config{
			Certificates: []tls.Certificate{keyPair},
			RootCAs:      rootCAs,
		},
	})
	if err != nil {
		return nil, nil, err
	}

	e := &Etcd{
		client:    client,
		closeChan: make(chan struct{}),
		log:       log,
	}

	return e, e.close, nil
}

// WatchSecrets starts a watch on the etcd's inference secrets and updates the local secret store on changes.
func (e *Etcd) WatchSecrets(ctx context.Context) (*secrets.Secrets, error) {
	secrets, startingRevision, err := e.fetchSecrets(ctx)
	if err != nil {
		return nil, err
	}

	go e.watchSecrets(ctx, secrets, startingRevision)
	return secrets, nil
}

// GetSecret retrieves a secret from etcd by its key.
func (e *Etcd) GetSecret(ctx context.Context, key string) ([]byte, error) {
	response, err := e.client.Get(ctx, constants.EtcdInferenceSecretPrefix+key)
	if err != nil {
		return nil, err
	}
	if len(response.Kvs) != 1 {
		return nil, errors.New("secret not found")
	}
	return response.Kvs[0].Value, nil
}

func (e *Etcd) watchSecrets(ctx context.Context, secrets *secrets.Secrets, watchRevision int64) {
	startWatch := func(ctx context.Context, revision int64) (clientv3.WatchChan, func()) {
		e.log.Info("Starting watch", "revision", revision)
		watchCtx, cancel := context.WithCancel(ctx)
		return e.client.Watch(
			clientv3.WithRequireLeader(watchCtx),
			constants.EtcdInferenceSecretPrefix,
			clientv3.WithPrefix(),
			clientv3.WithRev(revision),
			clientv3.WithProgressNotify(),
		), cancel
	}
	watchChan, cancel := startWatch(ctx, watchRevision)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			return
		case <-e.closeChan:
			return

		case event := <-watchChan:
			if event.Err() != nil {
				e.log.Error("Error watching etcd", "error", event.Err())
				e.log.Info("Restarting watch")

				cancel()
				watchChan, cancel = startWatch(ctx, watchRevision)
				continue
			}
			if event.Canceled {
				e.log.Error("Watch canceled")
				e.log.Info("Restarting watch")

				cancel()
				watchChan, cancel = startWatch(ctx, watchRevision)
				continue
			}
			if event.IsProgressNotify() {
				e.log.Info("Watch still alive")
				continue
			}

			e.log.Info("Received inference secret update event. Updating local secret cache...")
			for _, ev := range event.Events {
				if ev.IsCreate() || ev.IsModify() {
					// Save new secret or update existing secret
					secrets.Set(strings.TrimPrefix(string(ev.Kv.Key), constants.EtcdInferenceSecretPrefix), ev.Kv.Value)
					e.log.Info("Updated secret", "key", string(ev.Kv.Key))
				} else {
					// Remove existing key
					secrets.Delete(strings.TrimPrefix(string(ev.Kv.Key), constants.EtcdInferenceSecretPrefix))
					e.log.Info("Deleted secret", "key", string(ev.Kv.Key))
				}
			}

			// Update target revision to the next revision after this one.
			watchRevision = event.Header.Revision
			e.log.Info("Updating revision", "revision", watchRevision, "keys", secrets.Keys())
		}
	}
}

// fetchSecrets gets the initial secrets from etcd and the revision of their last update.
func (e *Etcd) fetchSecrets(ctx context.Context) (*secrets.Secrets, int64, error) {
	e.log.Info("Fetching initial set of inference secret")

	resp, err := e.client.Get(ctx, constants.EtcdInferenceSecretPrefix, clientv3.WithPrefix())
	if err != nil {
		return nil, -1, fmt.Errorf("fetching secrets from etcd: %w", err)
	}

	secretMap := make(map[string][]byte)
	for _, kv := range resp.Kvs {
		if kv == nil {
			return nil, -1, errors.New("nil key-value pair in etcd response")
		}
		secretMap[strings.TrimPrefix(string(kv.Key), constants.EtcdInferenceSecretPrefix)] = kv.Value
	}

	return secrets.New(e, secretMap), resp.Header.Revision + 1, nil
}

func (e *Etcd) close() {
	e.closeChan <- struct{}{}
	_ = e.client.Close()
}

// getEndpointsFromHosts adds etcd client ports to the given hosts.
func getEndpointsFromHosts(hosts []string) []string {
	var endpoints []string
	for _, host := range hosts {
		endpoints = append(endpoints, net.JoinHostPort(host, constants.EtcdClientPort()))
	}
	return endpoints
}

type etcdClient interface {
	Get(ctx context.Context, key string, opts ...clientv3.OpOption) (*clientv3.GetResponse, error)
	Watch(ctx context.Context, key string, opts ...clientv3.OpOption) clientv3.WatchChan
	Close() error
}
