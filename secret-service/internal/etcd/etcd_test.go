package etcd

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	pb "go.etcd.io/etcd/api/v3/etcdserverpb"
	"go.etcd.io/etcd/api/v3/mvccpb"
)

func TestSetSecrets(t *testing.T) {
	defaultSecrets := map[string][]byte{
		"key1": bytes.Repeat([]byte{0x01}, 16),
		"key2": bytes.Repeat([]byte{0x01}, 24),
		"key3": bytes.Repeat([]byte{0x01}, 32),
	}

	testCases := map[string]struct {
		server  *stubEtcdServer
		secrets map[string][]byte
		wantErr bool
	}{
		"success": {
			server: &stubEtcdServer{
				txnResponse: &pb.TxnResponse{Succeeded: true},
			},
			secrets: defaultSecrets,
		},
		"commit error": {
			server: &stubEtcdServer{
				err: assert.AnError,
			},
			secrets: defaultSecrets,
			wantErr: true,
		},
		"commit failure": {
			server: &stubEtcdServer{
				// Setting just succeeded to false, should cause a fallback error
				txnResponse: &pb.TxnResponse{Succeeded: false},
			},
			secrets: defaultSecrets,
			wantErr: true,
		},
		"secret already exists": {
			server: &stubEtcdServer{
				txnResponse: &pb.TxnResponse{
					Succeeded: false,
					Responses: []*pb.ResponseOp{
						{
							Response: &pb.ResponseOp_ResponseRange{
								ResponseRange: &pb.RangeResponse{
									Kvs: []*mvccpb.KeyValue{
										{Key: []byte("key1")},
									},
								},
							},
						},
						{
							Response: &pb.ResponseOp_ResponseRange{
								ResponseRange: &pb.RangeResponse{},
							},
						},
					},
				},
			},
			secrets: defaultSecrets,
			wantErr: true,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)

			e := &Etcd{server: tc.server}

			err := e.SetSecrets(t.Context(), tc.secrets, 0)
			if tc.wantErr {
				assert.Error(err)
				return
			}
			assert.NoError(err)

			// Check that the transaction was set up correctly
			assert.Len(tc.server.txnRequest.Compare, len(tc.secrets))
			assert.Len(tc.server.txnRequest.Success, len(tc.secrets))
			assert.Len(tc.server.txnRequest.Failure, len(tc.secrets))
		})
	}
}

func TestDeleteSecrets(t *testing.T) {
	testCases := map[string]struct {
		server  *stubEtcdServer
		secrets []string
		wantErr bool
	}{
		"success": {
			server: &stubEtcdServer{
				txnResponse: &pb.TxnResponse{Succeeded: true},
			},
			secrets: []string{"key1", "key2"},
		},
		"commit error": {
			server:  &stubEtcdServer{err: assert.AnError},
			secrets: []string{"key1", "key2"},
			wantErr: true,
		},
		"transaction does not succeed": {
			server: &stubEtcdServer{
				txnResponse: &pb.TxnResponse{Succeeded: false},
			},
			secrets: []string{"key1", "key2"},
			wantErr: true,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)

			e := &Etcd{server: tc.server}

			err := e.DeleteSecrets(t.Context(), tc.secrets)
			if tc.wantErr {
				assert.Error(err)
				return
			}
			assert.NoError(err)

			// Check that the transaction was set up correctly
			assert.Len(tc.server.txnRequest.Compare, len(tc.secrets))
			assert.Len(tc.server.txnRequest.Success, len(tc.secrets))
			assert.Empty(tc.server.txnRequest.Failure) // No else statements in DeleteSecrets
		})
	}
}

type stubEtcdServer struct {
	txnRequest  *pb.TxnRequest
	txnResponse *pb.TxnResponse
	err         error
}

func (s *stubEtcdServer) Txn(_ context.Context, req *pb.TxnRequest) (*pb.TxnResponse, error) {
	s.txnRequest = req
	return s.txnResponse, s.err
}

func (s *stubEtcdServer) LeaseGrant(_ context.Context, _ *pb.LeaseGrantRequest) (*pb.LeaseGrantResponse, error) {
	return nil, nil
}

func (s *stubEtcdServer) LeaseRevoke(_ context.Context, _ *pb.LeaseRevokeRequest) (*pb.LeaseRevokeResponse, error) {
	return nil, nil
}

func (s *stubEtcdServer) Close() {
}
