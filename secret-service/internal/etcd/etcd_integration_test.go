//go:build integration

package etcd

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"log/slog"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/edgelesssys/continuum/internal/crypto"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEtcd(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	// Our code is able to use a memfs through afero, however, the embedded etcd server is not
	// Therefore, we use a tempdir to save the certificates required by etcd
	tmpDir := t.TempDir()
	// Overwrite Continuum's base dir to ensure any etcd data is stored in the tempdir
	t.Setenv("CONTINUUM_BASE_DIR", tmpDir)
	t.Setenv("HOSTNAME", "secret-service-0")

	fs := afero.Afero{Fs: afero.NewOsFs()}

	ctx := t.Context()
	log := slog.Default()

	serverCrt := filepath.Join(tmpDir, "pki", "etcd.crt")
	serverKey := filepath.Join(tmpDir, "pki", "etcd.key")
	caCrt := filepath.Join(tmpDir, "pki", "ca.crt")
	require.NoError(fs.MkdirAll(filepath.Join(tmpDir, "pki"), 0o700))
	createEtcdCertificates(require, serverCrt, serverKey, caCrt, fs)

	freeClientPortListener, err := net.Listen("tcp", "0.0.0.0:")
	require.NoError(err)
	freePeerPortListener, err := net.Listen("tcp", "0.0.0.0:")
	require.NoError(err)

	_, freeClientPort, err := net.SplitHostPort(freeClientPortListener.Addr().String())
	require.NoError(err)
	freeClientPortListener.Close()
	t.Setenv("CONTINUUM_ETCD_CLIENT_PORT", freeClientPort)
	_, freePeerPort, err := net.SplitHostPort(freePeerPortListener.Addr().String())
	require.NoError(err)
	freePeerPortListener.Close()
	t.Setenv("CONTINUUM_ETCD_PEER_PORT", freePeerPort)

	etcdServer, done, err := New(t.Context(), Bootstrap,
		"test-namespace", serverCrt, serverKey, caCrt, fs, log)
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

func createEtcdCertificates(require *require.Assertions, serverCrtPath, serverKeyPath, caCrtPath string, fs afero.Afero) {
	caCrtDER, caKey, err := createCertificate("etcd-ca", nil, nil)
	require.NoError(err)
	caCrt, err := x509.ParseCertificate(caCrtDER)
	require.NoError(err)

	serverCrtDER, serverKey, err := createCertificate("root", caCrt, caKey)
	require.NoError(err)

	caCrtPEM := &pem.Block{Type: "CERTIFICATE", Bytes: caCrtDER}
	require.NoError(fs.WriteFile(caCrtPath, pem.EncodeToMemory(caCrtPEM), 0o644))

	serverCrtPEM := &pem.Block{Type: "CERTIFICATE", Bytes: serverCrtDER}
	require.NoError(fs.WriteFile(serverCrtPath, pem.EncodeToMemory(serverCrtPEM), 0o644))

	serverKeyDER, err := x509.MarshalECPrivateKey(serverKey)
	require.NoError(err)
	serverKeyPEM := &pem.Block{Type: "PRIVATE KEY", Bytes: serverKeyDER}
	require.NoError(fs.WriteFile(serverKeyPath, pem.EncodeToMemory(serverKeyPEM), 0o644))
}

func createCertificate(commonName string, parentCrt *x509.Certificate, parentKey *ecdsa.PrivateKey) ([]byte, *ecdsa.PrivateKey, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	serialNumber, err := crypto.GenerateCertificateSerialNumber()
	if err != nil {
		return nil, nil, err
	}

	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName: commonName,
		},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		NotBefore:             now.Add(-2 * time.Hour),
		NotAfter:              now.Add(2 * time.Hour),
	}

	if parentCrt == nil && parentKey == nil {
		template.IsCA = true
		parentCrt = template
		parentKey = priv
	}

	cert, err := x509.CreateCertificate(rand.Reader, template, parentCrt, &priv.PublicKey, parentKey)
	if err != nil {
		return nil, nil, err
	}

	return cert, priv, nil
}
