//go:build integration

package etcd

import (
	"bytes"
	"log/slog"
	"testing"
	"time"

	ca "github.com/edgelesssys/continuum/internal/pki"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEtcd(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	// Our code is able to use a memfs through afero, however, the embedded etcd server is not
	// Therefore, we use a tempdir and override Continuum's base directory to use it
	tmpDir := t.TempDir()
	fs := afero.Afero{Fs: afero.NewOsFs()}
	t.Setenv("CONTINUUM_BASE_DIR", tmpDir)

	ctx := t.Context()
	log := slog.Default()
	pki, err := ca.New(nil, nil, fs, log)
	require.NoError(err)

	etcdServer, done, err := New(t.Context(), "0.0.0.0", pki, fs, log)
	require.NoError(err)
	defer done()

	// Test that setting secrets works as expected
	secrets := map[string][]byte{
		"16_byte_key": bytes.Repeat([]byte("A"), 16),
		"24_byte_key": bytes.Repeat([]byte("B"), 24),
		"32_byte_key": bytes.Repeat([]byte("C"), 32),
	}
	assert.NoError(etcdServer.SetSecrets(ctx, secrets, 0))

	err = etcdServer.SetSecrets(ctx, map[string][]byte{"16_byte_key": bytes.Repeat([]byte("A"), 16)}, 0)
	assert.Error(err, "Setting an already existing key should fail")

	// Test that deletion works as expected
	err = etcdServer.DeleteSecrets(ctx, []string{"does_not_exist"})
	assert.Error(err, "Deletion of non existent key should fail")

	assert.NoError(etcdServer.DeleteSecrets(ctx, []string{"24_byte_key", "32_byte_key"}))

	// Create a a secret with a TTL
	ttl := 5
	assert.NoError(etcdServer.SetSecrets(ctx, map[string][]byte{"ttl_key": []byte("ttl_value")}, int64(ttl)))
	time.Sleep(time.Duration(ttl)*time.Second + time.Second)

	// Secret is now expired, and we should be able to set it again
	assert.NoError(etcdServer.SetSecrets(ctx, map[string][]byte{"ttl_key": []byte("ttl_value")}, 0))
}
