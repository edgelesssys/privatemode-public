package etcd

import (
	"context"
	"log/slog"
	"sync"
	"testing"

	"github.com/edgelesssys/continuum/inference-proxy/internal/secrets"
	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/stretchr/testify/assert"
	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
)

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
		"remove nonexistent secret": {
			initialSecrets: nil,
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
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			initialGet := &clientv3.GetResponse{Kvs: []*mvccpb.KeyValue{}}
			for k, v := range tc.initialSecrets {
				initialGet.Kvs = append(initialGet.Kvs, &mvccpb.KeyValue{
					Key:   []byte(constants.EtcdInferenceSecretPrefix + k),
					Value: v,
				})
			}
			stubClient := &stubEtcdClient{
				getResponses:  []*clientv3.GetResponse{initialGet},
				getErr:        nil,
				watchResponse: make(chan clientv3.WatchResponse),
			}
			etcd := &Etcd{
				client: stubClient,
				log:    slog.Default(),
			}
			secrets := secrets.New(etcd)
			etcd.secrets = secrets

			ctx, cancel := context.WithCancel(t.Context())
			var wg sync.WaitGroup
			wg.Go(func() {
				etcd.watchSecrets(ctx)
			})

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
	getResponses  []*clientv3.GetResponse
	getErr        error
	watchResponse chan clientv3.WatchResponse
}

func (s *stubEtcdClient) Close() error {
	return nil
}

func (s *stubEtcdClient) Get(_ context.Context, _ string, _ ...clientv3.OpOption) (*clientv3.GetResponse, error) {
	var getResponse *clientv3.GetResponse
	if len(s.getResponses) > 0 {
		getResponse = s.getResponses[0]
		s.getResponses = s.getResponses[1:]
	} else {
		getResponse = &clientv3.GetResponse{Kvs: []*mvccpb.KeyValue{}}
	}
	return getResponse, s.getErr
}

func (s *stubEtcdClient) Watch(_ context.Context, _ string, _ ...clientv3.OpOption) clientv3.WatchChan {
	return s.watchResponse
}
