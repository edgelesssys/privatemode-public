// Copyright (c) Edgeless Systems GmbH
// SPDX-License-Identifier: MIT

package tlsconfig

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRotation(t *testing.T) {
	certPath, keyPath := filepath.Join(t.TempDir(), "tls.crt"), filepath.Join(t.TempDir(), "tls.key")
	cert1, key1 := keyPair(t, 1)
	cert2, key2 := keyPair(t, 2)
	require.NoError(t, os.WriteFile(certPath, cert1, 0o600))
	require.NoError(t, os.WriteFile(keyPath, key1, 0o600))
	cfg, err := New(certPath, keyPath)
	require.NoError(t, err)

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	server.TLS = cfg
	server.StartTLS()
	defer server.Close()
	pool := x509.NewCertPool()
	require.True(t, pool.AppendCertsFromPEM(cert1))
	require.True(t, pool.AppendCertsFromPEM(cert2))
	check := func(serial int64) {
		t.Helper()
		dialer := tls.Dialer{Config: &tls.Config{RootCAs: pool, ServerName: "localhost"}}
		conn, err := dialer.DialContext(t.Context(), "tcp", server.Listener.Addr().String())
		require.NoError(t, err)
		defer conn.Close()
		tlsConn, ok := conn.(*tls.Conn)
		require.True(t, ok)
		require.Equal(t, serial, tlsConn.ConnectionState().PeerCertificates[0].SerialNumber.Int64())
	}
	check(1)
	require.NoError(t, os.WriteFile(certPath, cert2, 0o600))
	require.NoError(t, os.WriteFile(keyPath, key2, 0o600))
	check(2)
}

func keyPair(t *testing.T, serial int64) ([]byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	cert := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		Subject:      pkix.Name{CommonName: "localhost"},
		DNSNames:     []string{"localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	require.NoError(t, err)
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
}
