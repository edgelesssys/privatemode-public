package secretclient

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hpke"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/edgelesssys/continuum/internal/oss/auth"
	"github.com/edgelesssys/continuum/internal/oss/constants"
	"github.com/edgelesssys/continuum/internal/oss/httpapi"
	"github.com/edgelesssys/continuum/internal/oss/secretexchange"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExchangeSecret(t *testing.T) {
	meshCA, meshCert, meshKey := newTestMeshCertificate(t)
	const clientID = "test-id"

	for _, tc := range []struct {
		name          string
		invalidateSig bool
		wantErr       string
	}{
		{name: "success"},
		{name: "invalid signature", invalidateSig: true, wantErr: "invalid signature"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var wantID string
			var wantSecret []byte
			secretService := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, SecretPath, r.URL.Path)
				assert.Equal(t, constants.Version(), r.Header.Get(constants.PrivatemodeVersionHeader))
				assert.Equal(t, clientID, r.Header.Get(constants.PrivatemodeClientHeader))

				var req httpapi.SecretReq
				if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&req)) {
					return
				}
				wantID = secretexchange.ID(req.PublicKey)
				pub, err := hpke.MLKEM768X25519().NewPublicKey(req.PublicKey)
				if !assert.NoError(t, err) {
					return
				}
				encapsulatedKey, sender, err := hpke.NewSender(pub, hpke.HKDFSHA256(), hpke.ExportOnly(), nil)
				if !assert.NoError(t, err) {
					return
				}
				wantSecret, err = sender.Export("", 32)
				if !assert.NoError(t, err) {
					return
				}
				signature, err := ecdsa.SignASN1(rand.Reader, meshKey, secretexchange.Hash(req.PublicKey, encapsulatedKey))
				if !assert.NoError(t, err) {
					return
				}
				if tc.invalidateSig {
					signature[0] ^= 0xFF
				}
				assert.NoError(t, json.NewEncoder(w).Encode(httpapi.SecretResp{
					EncapsulatedKey: encapsulatedKey,
					Signature:       signature,
					MeshCert:        meshCert,
				}))
			}))
			defer secretService.Close()

			client := New(
				secretService.Client(),
				secretService.Listener.Addr().String(),
				http.Header{constants.PrivatemodeClientHeader: {clientID}},
			)
			id, secret, err := client.ExchangeSecret(t.Context(), meshCA, auth.Credential{})

			if tc.wantErr != "" {
				assert.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, wantID, id)
			assert.Equal(t, wantSecret, secret)
		})
	}
}

func newTestMeshCertificate(t *testing.T) (*x509.Certificate, []byte, *ecdsa.PrivateKey) {
	t.Helper()

	meshKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	now := time.Now()
	meshTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "secret service"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	meshCertRaw, err := x509.CreateCertificate(rand.Reader, meshTemplate, meshTemplate, &meshKey.PublicKey, meshKey)
	require.NoError(t, err)
	meshCert, err := x509.ParseCertificate(meshCertRaw)
	require.NoError(t, err)

	return meshCert, meshCertRaw, meshKey
}
