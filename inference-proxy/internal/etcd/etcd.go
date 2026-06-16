// Package etcd implements a client to interact with etcd.
package etcd

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/edgelesssys/continuum/inference-proxy/internal/secrets"
	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/spf13/afero"
	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
)

const (
	// restartBackoff is the time to wait before restarting a watch after an error.
	restartBackoff = time.Second * 5
)

// Etcd is a client to interact with etcd.
type Etcd struct {
	client etcdClient

	secrets   *secrets.Secrets
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
	e.secrets = secrets.New(e)

	return e, e.close, nil
}

// WatchSecrets starts a watch on the etcd's inference secrets and updates the local secret store on changes.
func (e *Etcd) WatchSecrets(ctx context.Context) *secrets.Secrets {
	go e.watchSecrets(ctx)
	return e.secrets
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

func (e *Etcd) watchSecrets(ctx context.Context) {
	watchChan, cancel, err := e.startWatch(ctx, 0)
	if err != nil {
		return
	}
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			return
		case <-e.closeChan:
			return

		case event, ok := <-watchChan:
			if !ok {
				e.log.Error("Watch channel closed")
				e.log.Info("Restarting watch")

				cancel()
				watchChan, cancel, err = e.startWatch(ctx, restartBackoff)
				if err != nil {
					return
				}
				continue
			}
			if event.Err() != nil {
				e.log.Error("Error watching etcd", "error", event.Err())
				e.log.Info("Restarting watch")

				cancel()
				watchChan, cancel, err = e.startWatch(ctx, restartBackoff)
				if err != nil {
					return
				}
				continue
			}
			if event.Canceled {
				e.log.Error("Watch canceled")
				e.log.Info("Restarting watch")

				cancel()
				watchChan, cancel, err = e.startWatch(ctx, restartBackoff)
				if err != nil {
					return
				}
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
					e.secrets.Set(strings.TrimPrefix(string(ev.Kv.Key), constants.EtcdInferenceSecretPrefix), ev.Kv.Value)
					e.log.Info("Updated secret", "key", string(ev.Kv.Key))
				} else {
					// Remove existing key
					e.secrets.Delete(strings.TrimPrefix(string(ev.Kv.Key), constants.EtcdInferenceSecretPrefix))
					e.log.Info("Deleted secret", "key", string(ev.Kv.Key))
				}
			}
		}
	}
}

func (e *Etcd) close() {
	e.closeChan <- struct{}{}
	_ = e.client.Close()
}

// startWatch starts a watcher on etcd using the given [context.Context].
// The function retries until a watch can be successfully established, or the context is canceled,
// at which point it returns an error.
// Cancelling the [context.Context], or calling the returned cancel function, stops the watcher.
func (e *Etcd) startWatch(ctx context.Context, delay time.Duration) (ch clientv3.WatchChan, cancelFunc func(), err error) {
	backoff := delay

	for {
		select {
		case <-ctx.Done():
			return nil, func() {}, ctx.Err()
		case <-time.After(backoff):
			e.log.Info("Starting watch")
			watchCtx, cancel := context.WithCancel(ctx)
			watch := e.client.Watch(
				clientv3.WithRequireLeader(watchCtx),
				constants.EtcdInferenceSecretPrefix,
				clientv3.WithPrefix(),
				clientv3.WithProgressNotify(),
			)

			// We start the watch without a revision, meaning we need to retrieve the current
			// secrets list manually to populate our cache before we can properly start
			getCtx, getCancel := context.WithTimeout(ctx, 10*time.Second) // This should usually be a quick operation
			resp, err := e.client.Get(getCtx, constants.EtcdInferenceSecretPrefix, clientv3.WithPrefix())
			getCancel()
			if err != nil {
				e.log.Error("Failed retrieving inference secrets from etcd, restarting watch...", "error", err)
				backoff = restartBackoff
				cancel()
				continue
			}

			secretMap, err := kvsToMap(resp.Kvs)
			if err != nil {
				e.log.Error("Failed parsing inference secrets from etcd", "error", err)
				backoff = restartBackoff
				cancel()
				continue
			}

			// Watch (re)-started successfully, update the secret cache with the current values from etcd
			e.secrets.Reset(secretMap)

			return watch, cancel, nil
		}
	}
}

func kvsToMap(kvs []*mvccpb.KeyValue) (map[string][]byte, error) {
	result := make(map[string][]byte)
	for _, kv := range kvs {
		if kv == nil {
			return nil, errors.New("nil key-value pair in etcd response")
		}
		result[strings.TrimPrefix(string(kv.Key), constants.EtcdInferenceSecretPrefix)] = kv.Value
	}
	return result, nil
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
