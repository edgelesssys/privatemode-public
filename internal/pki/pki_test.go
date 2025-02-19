package pki

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"log/slog"
	"math"
	"math/big"
	"net"
	"path/filepath"
	"testing"
	"time"

	ccrypto "github.com/edgelesssys/continuum/internal/crypto"
	"github.com/edgelesssys/continuum/internal/gpl/constants"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	testCert, testKey := setUpTestCA(t)

	testCases := map[string]struct {
		caCert    *x509.Certificate
		caKey     *ecdsa.PrivateKey
		fs        afero.Afero
		wantNewCA bool
		wantErr   bool
	}{
		"success new CA": {
			caCert:    nil,
			caKey:     nil,
			fs:        afero.Afero{Fs: afero.NewMemMapFs()},
			wantNewCA: true,
		},
		"success existing CA": {
			caCert:    testCert,
			caKey:     testKey,
			fs:        afero.Afero{Fs: afero.NewMemMapFs()},
			wantNewCA: false,
		},
		"passing just CA cert is not allowed": {
			caCert:  testCert,
			caKey:   nil,
			fs:      afero.Afero{Fs: afero.NewMemMapFs()},
			wantErr: true,
		},
		"passing just CA key is not allowed": {
			caCert:  nil,
			caKey:   testKey,
			fs:      afero.Afero{Fs: afero.NewMemMapFs()},
			wantErr: true,
		},
		"fs not writable": {
			caCert:  nil,
			caKey:   nil,
			fs:      afero.Afero{Fs: afero.NewReadOnlyFs(afero.NewMemMapFs())},
			wantErr: true,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			ca, err := New(tc.caCert, tc.caKey, tc.fs, slog.Default())
			if tc.wantErr {
				assert.Error(err)
				return
			}
			assert.NoError(err)

			certPEM, err := tc.fs.ReadFile(filepath.Join(constants.EtcdPKIPath(), "ca.crt"))
			require.NoError(err)
			savedCert, err := ccrypto.ParseCertificateFromPEM(certPEM)
			require.NoError(err)
			assert.True(ca.caCert.Equal(savedCert))

			keyPEM, err := tc.fs.ReadFile(filepath.Join(constants.EtcdPKIPath(), "ca.key"))
			require.NoError(err)
			savedKey, err := ccrypto.ParseECPrivateKeyFromPEM(keyPEM)
			require.NoError(err)
			assert.True(ca.caKey.Equal(savedKey))

			if !tc.wantNewCA {
				assert.True(ca.caCert.Equal(tc.caCert))
				assert.True(ca.caKey.Equal(tc.caKey))
			}
		})
	}
}

func TestCreateCertificate(t *testing.T) {
	testCert, testKey := setUpTestCA(t)

	testCases := map[string]struct {
		commonName string
		sans       []string
		ips        []net.IP
		asserts    func(*assert.Assertions, *x509.Certificate)
		wantErr    bool
	}{
		"success": {
			commonName: "test",
			sans:       []string{"test1", "test2"},
			ips:        []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
			asserts: func(assert *assert.Assertions, cert *x509.Certificate) {
				assert.Equal([]string{"test1", "test2"}, cert.DNSNames)
				assert.Equal([]net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback}, cert.IPAddresses)
			},
		},
		"empty SANs and IPs get trimmed": {
			commonName: "test",
			sans:       []string{"", "test1", ""},
			ips:        []net.IP{net.IPv4(127, 0, 0, 1), nil, net.IPv6loopback},
			asserts: func(assert *assert.Assertions, cert *x509.Certificate) {
				assert.Equal([]string{"test1"}, cert.DNSNames)
				assert.Equal([]net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback}, cert.IPAddresses)
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			ca, err := New(testCert, testKey, afero.Afero{Fs: afero.NewMemMapFs()}, slog.Default())
			require.NoError(err)

			certPEM, keyPEM, err := ca.CreateCertificate(tc.commonName, tc.sans, tc.ips, math.MaxInt64)
			if tc.wantErr {
				assert.Error(err)
				return
			}
			assert.NoError(err)

			certBlock, _ := pem.Decode(certPEM)
			require.NotNil(certBlock)
			cert, err := x509.ParseCertificate(certBlock.Bytes)
			require.NoError(err)
			assert.NoError(cert.CheckSignatureFrom(ca.caCert))

			keyBlock, _ := pem.Decode(keyPEM)
			require.NotNil(keyBlock)
			key, err := x509.ParseECPrivateKey(keyBlock.Bytes)
			require.NoError(err)
			assert.NotNil(key)
			pubKey := crypto.PublicKey(cert.PublicKey)
			assert.NotNil(pubKey)
		})
	}
}

func setUpTestCA(t *testing.T) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	require := require.New(t)

	testKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(err)
	certTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "test-ca",
		},
		NotBefore:             time.Time{},
		NotAfter:              time.Time{}.Add(math.MaxInt64),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	testCertDER, err := x509.CreateCertificate(rand.Reader, certTemplate, certTemplate, &testKey.PublicKey, testKey)
	require.NoError(err)
	testCert, err := x509.ParseCertificate(testCertDER)
	require.NoError(err)

	return testCert, testKey
}
