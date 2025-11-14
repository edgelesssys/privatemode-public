package etcd

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/edgelesssys/continuum/inference-proxy/internal/secrets"
	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/stretchr/testify/assert"
	"go.etcd.io/etcd/api/v3/etcdserverpb"
	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
)

func TestFetchSecrets(t *testing.T) {
	testCases := map[string]struct {
		etcdClient *stubEtcdClient
		wantErr    bool
	}{
		"success": {
			etcdClient: &stubEtcdClient{
				getResponse: &clientv3.GetResponse{
					Header: &etcdserverpb.ResponseHeader{},
					Kvs: []*mvccpb.KeyValue{
						{
							Key:   []byte(constants.EtcdInferenceSecretPrefix + "key1"),
							Value: []byte("value1"),
						},
					},
				},
			},
		},
		"get error": {
			etcdClient: &stubEtcdClient{
				getErr: assert.AnError,
			},
			wantErr: true,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			etcd := &Etcd{
				client: tc.etcdClient,
				log:    slog.Default(),
			}

			secrets, _, err := etcd.fetchSecrets(t.Context())
			if tc.wantErr {
				assert.Error(err)
				return
			}
			assert.NoError(err)
			for _, kvs := range tc.etcdClient.getResponse.Kvs {
				_, ok := secrets.Get(t.Context(), strings.TrimPrefix(string(kvs.Key), constants.EtcdInferenceSecretPrefix))
				assert.True(ok)
			}
		})
	}
}

func TestGetEndpointFromInstances(t *testing.T) {
	assert := assert.New(t)
	instances := []string{"host1", "192.0.2.1", "host2", "192.0.2.2"}

	endpoints := getEndpointsFromHosts(instances)
	assert.ElementsMatch([]string{"host1:2379", "192.0.2.1:2379", "host2:2379", "192.0.2.2:2379"}, endpoints)

	instances = []string{"host1", "192.0.2.1", "host2"}

	endpoints = getEndpointsFromHosts(instances)
	assert.ElementsMatch([]string{"host1:2379", "192.0.2.1:2379", "host2:2379"}, endpoints)
}

func TestWatchSecrets(t *testing.T) {
	testCases := map[string]struct {
		initialSecrets map[string][]byte
		events         []clientv3.WatchResponse
		assertions     func(*testing.T, *secrets.Secrets)
	}{
		"add secret": {
			initialSecrets: nil,
			events: []clientv3.WatchResponse{
				{
					Events: []*clientv3.Event{
						{
							Type: mvccpb.PUT,
							Kv: &mvccpb.KeyValue{
								Key:   []byte(constants.EtcdInferenceSecretPrefix + "key1"),
								Value: []byte("value1"),
							},
						},
					},
				},
				{
					Events: []*clientv3.Event{
						{
							Type: mvccpb.PUT,
							Kv: &mvccpb.KeyValue{
								Key:   []byte(constants.EtcdInferenceSecretPrefix + "key2"),
								Value: []byte("value2"),
							},
						},
					},
				},
			},
			assertions: func(t *testing.T, sec *secrets.Secrets) {
				secret, ok := sec.Get(t.Context(), "key1")
				assert.True(t, ok)
				assert.Equal(t, "value1", string(secret))
				secret, ok = sec.Get(t.Context(), "key2")
				assert.True(t, ok)
				assert.Equal(t, "value2", string(secret))
			},
		},
		"remove secret": {
			initialSecrets: map[string][]byte{"key1": []byte("value1")},
			events: []clientv3.WatchResponse{
				{
					Events: []*clientv3.Event{
						{
							Type: mvccpb.DELETE,
							Kv: &mvccpb.KeyValue{
								Key: []byte(constants.EtcdInferenceSecretPrefix + "key1"),
							},
						},
					},
				},
			},
			assertions: func(t *testing.T, sec *secrets.Secrets) {
				_, ok := sec.Get(t.Context(), "key1")
				assert.False(t, ok)
			},
		},
		"update secret": {
			initialSecrets: map[string][]byte{"key1": []byte("value1")},
			events: []clientv3.WatchResponse{
				{
					Events: []*clientv3.Event{
						{
							Type: mvccpb.PUT,
							Kv: &mvccpb.KeyValue{
								Key:            []byte(constants.EtcdInferenceSecretPrefix + "key1"),
								Value:          []byte("value2"),
								CreateRevision: 0,
								ModRevision:    1,
							},
						},
					},
				},
			},
			assertions: func(t *testing.T, sec *secrets.Secrets) {
				secret, ok := sec.Get(t.Context(), "key1")
				assert.True(t, ok)
				assert.Equal(t, "value2", string(secret))
			},
		},
		"canceled then add secret": {
			initialSecrets: nil,
			events: []clientv3.WatchResponse{
				{
					Events:   []*clientv3.Event{},
					Canceled: true,
				},
				{
					Events: []*clientv3.Event{
						{
							Type: mvccpb.PUT,
							Kv: &mvccpb.KeyValue{
								Key:   []byte(constants.EtcdInferenceSecretPrefix + "key1"),
								Value: []byte("value1"),
							},
						},
					},
				},
			},
			assertions: func(t *testing.T, sec *secrets.Secrets) {
				secret, ok := sec.Get(t.Context(), "key1")
				assert.True(t, ok)
				assert.Equal(t, "value1", string(secret))
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			stubClient := &stubEtcdClient{
				getResponse:   nil,
				getErr:        assert.AnError,
				watchResponse: make(chan clientv3.WatchResponse),
			}
			etcd := &Etcd{
				client: stubClient,
				log:    slog.Default(),
			}
			secrets := secrets.New(etcd, tc.initialSecrets)

			ctx, cancel := context.WithCancel(t.Context())
			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				defer wg.Done()
				etcd.watchSecrets(ctx, secrets, 0)
			}()

			for _, event := range tc.events {
				stubClient.watchResponse <- event
			}
			cancel()
			wg.Wait()

			tc.assertions(t, secrets)
		})
	}
}

type stubEtcdClient struct {
	getResponse   *clientv3.GetResponse
	getErr        error
	watchResponse chan clientv3.WatchResponse
}

func (s *stubEtcdClient) Close() error {
	return nil
}

func (s *stubEtcdClient) Get(_ context.Context, _ string, _ ...clientv3.OpOption) (*clientv3.GetResponse, error) {
	return s.getResponse, s.getErr
}

func (s *stubEtcdClient) Watch(_ context.Context, _ string, _ ...clientv3.OpOption) clientv3.WatchChan {
	return s.watchResponse
}
