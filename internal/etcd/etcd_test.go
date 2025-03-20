package etcd

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/edgelesssys/continuum/internal/gpl/constants"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.etcd.io/etcd/api/v3/etcdserverpb"
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
					Responses: []*etcdserverpb.ResponseOp{
						{
							Response: &etcdserverpb.ResponseOp_ResponseRange{
								ResponseRange: &etcdserverpb.RangeResponse{
									Kvs: []*mvccpb.KeyValue{
										{Key: []byte("key1")},
									},
								},
							},
						},
						{
							Response: &etcdserverpb.ResponseOp_ResponseRange{
								ResponseRange: &etcdserverpb.RangeResponse{},
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

func TestCreateEtcdMemberKeyPair(t *testing.T) {
	require := require.New(t)

	testCases := map[string]struct {
		fs            afero.Afero
		ca            stubCA
		wantOverwrite bool
		wantErr       bool
	}{
		"create new key pair": {
			fs:            afero.Afero{Fs: afero.NewMemMapFs()},
			ca:            stubCA{},
			wantOverwrite: true,
		},
		"key pair exists on disk": {
			fs: func() afero.Afero {
				fs := afero.Afero{Fs: afero.NewMemMapFs()}
				require.NoError(fs.WriteFile(filepath.Join(constants.EtcdPKIPath(), "etcd.key"), []byte("oldKey"), 0o600))
				require.NoError(fs.WriteFile(filepath.Join(constants.EtcdPKIPath(), "etcd.crt"), []byte("oldCert"), 0o600))
				return fs
			}(),
			ca:            stubCA{},
			wantOverwrite: false,
		},
		"error creating key pair": {
			fs:      afero.Afero{Fs: afero.NewMemMapFs()},
			ca:      stubCA{err: assert.AnError},
			wantErr: true,
		},
		"only key exists on disk": {
			fs: func() afero.Afero {
				fs := afero.Afero{Fs: afero.NewMemMapFs()}
				require.NoError(fs.WriteFile(filepath.Join(constants.EtcdPKIPath(), "etcd.key"), []byte("oldKey"), 0o600))
				return fs
			}(),
			ca:            stubCA{},
			wantOverwrite: true,
		},
		"only cert exists on disk": {
			fs: func() afero.Afero {
				fs := afero.Afero{Fs: afero.NewMemMapFs()}
				require.NoError(fs.WriteFile(filepath.Join(constants.EtcdPKIPath(), "etcd.crt"), []byte("oldCert"), 0o600))
				return fs
			}(),
			ca:            stubCA{},
			wantOverwrite: true,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)

			createdCrt, err := createEtcdMemberKeyPair("192.0.2.1", tc.ca, tc.fs, slog.Default())
			if tc.wantErr {
				assert.Error(err)
				return
			}
			assert.NoError(err)
			key, err := tc.fs.ReadFile(filepath.Join(constants.EtcdPKIPath(), "etcd.key"))
			require.NoError(err)
			cert, err := tc.fs.ReadFile(filepath.Join(constants.EtcdPKIPath(), "etcd.crt"))
			require.NoError(err)
			assert.Equal(createdCrt, cert)

			if tc.wantOverwrite {
				assert.Equal("key", string(key))
				assert.Equal("cert", string(cert))
			} else {
				assert.Equal("oldKey", string(key))
				assert.Equal("oldCert", string(cert))
			}
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

type stubCA struct {
	err error
}

func (s stubCA) CreateCertificate(string, []string, []net.IP, time.Duration) ([]byte, []byte, error) {
	return []byte("cert"), []byte("key"), s.err
}
