package attestation

import (
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"log/slog"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/edgelesssys/continuum/attestation-agent/internal/gpu/attestation/nras"
	"github.com/edgelesssys/continuum/attestation-agent/internal/gpu/attestation/policy"
	"github.com/edgelesssys/continuum/attestation-agent/internal/gpu/attestation/testdata"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVerify(t *testing.T) {
	keySupplier := &stubNRASClient{
		jwksRes: testdata.JWKS,
	}

	createToken := func(
		t *testing.T,
		transformer func(token *jwt.Token) *jwt.Token,
		signingKey *ecdsa.PrivateKey,
		signingMethod jwt.SigningMethod,
	) string {
		token := jwt.New(signingMethod)
		// Set some reasonable defaults
		token.Header["kid"] = "edgelesstest" // Default to the kid of the testing key
		iat := time.Now()
		token.Claims = jwt.MapClaims{
			"iat":                         iat.Unix(),
			"exp":                         iat.Add(time.Hour).Unix(),
			"nbf":                         iat.Unix(),
			"iss":                         nras.URL,
			"sub":                         nras.Subject,
			"eat_nonce":                   "0000000000000000000000000000000000000000000000000000000000000000",
			"x-nvidia-eat-ver":            "1",
			"x-nvidia-gpu-driver-version": "2",
			"x-nvidia-gpu-vbios-version":  "3",
			"dbgstat":                     "disabled",
			"secboot":                     true,
			"measres":                     "comparison-successful",
			"x-nvidia-attestation-detailed-result": map[string]interface{}{
				"x-nvidia-gpu-driver-rim-schema-validated":              true,
				"x-nvidia-gpu-vbios-rim-cert-validated":                 true,
				"x-nvidia-gpu-attestation-report-cert-chain-validated":  true,
				"x-nvidia-gpu-driver-rim-schema-fetched":                true,
				"x-nvidia-gpu-attestation-report-parsed":                true,
				"x-nvidia-gpu-nonce-match":                              true,
				"x-nvidia-gpu-vbios-rim-signature-verified":             true,
				"x-nvidia-gpu-driver-rim-signature-verified":            true,
				"x-nvidia-gpu-arch-check":                               true,
				"x-nvidia-gpu-measurements-match":                       true,
				"x-nvidia-gpu-attestation-report-signature-verified":    true,
				"x-nvidia-gpu-vbios-rim-schema-validated":               true,
				"x-nvidia-gpu-driver-rim-cert-validated":                true,
				"x-nvidia-gpu-vbios-rim-schema-fetched":                 true,
				"x-nvidia-gpu-vbios-rim-measurements-available":         true,
				"x-nvidia-gpu-driver-rim-driver-measurements-available": true,
			},
		}
		token = transformer(token)
		signedToken, err := token.SignedString(signingKey)
		require.NoError(t, err)
		return signedToken
	}

	signingKey := func(t *testing.T) *ecdsa.PrivateKey {
		signingKeyBlock, rest := pem.Decode(testdata.SigningKeyPEM)
		require.Empty(t, rest)
		require.NotNil(t, signingKeyBlock)
		signingKey, err := x509.ParsePKCS8PrivateKey(signingKeyBlock.Bytes)
		require.NoError(t, err)
		ecdsaKey, ok := signingKey.(*ecdsa.PrivateKey)
		require.True(t, ok)
		return ecdsaKey
	}

	testCases := map[string]struct {
		tokenTransformer func(token *jwt.Token) *jwt.Token
		pubKeySupplier   *stubNRASClient
		signingKey       func(t *testing.T) *ecdsa.PrivateKey
		signingMethod    jwt.SigningMethod
		wantErr          bool
	}{
		"valid": {
			tokenTransformer: func(token *jwt.Token) *jwt.Token {
				return token
			},
			pubKeySupplier: keySupplier,
			signingKey:     signingKey,
			signingMethod:  jwt.SigningMethodES384,
		},
		"expired": {
			tokenTransformer: func(token *jwt.Token) *jwt.Token {
				token.Claims.(jwt.MapClaims)["exp"] = time.Now().Add(-time.Hour).Unix()
				return token
			},
			pubKeySupplier: keySupplier,
			signingKey:     signingKey,
			signingMethod:  jwt.SigningMethodES384,
			wantErr:        true,
		},
		"not yet valid": {
			tokenTransformer: func(token *jwt.Token) *jwt.Token {
				token.Claims.(jwt.MapClaims)["nbf"] = time.Now().Add(time.Hour).Unix()
				return token
			},
			pubKeySupplier: keySupplier,
			signingKey:     signingKey,
			signingMethod:  jwt.SigningMethodES384,
			wantErr:        true,
		},
		"invalid issuer": {
			tokenTransformer: func(token *jwt.Token) *jwt.Token {
				token.Claims.(jwt.MapClaims)["iss"] = "invalid"
				return token
			},
			pubKeySupplier: keySupplier,
			signingKey:     signingKey,
			signingMethod:  jwt.SigningMethodES384,
			wantErr:        true,
		},
		"invalid subject": {
			tokenTransformer: func(token *jwt.Token) *jwt.Token {
				token.Claims.(jwt.MapClaims)["sub"] = "invalid"
				return token
			},
			pubKeySupplier: keySupplier,
			signingKey:     signingKey,
			signingMethod:  jwt.SigningMethodES384,
			wantErr:        true,
		},
		"invalid public key": {
			tokenTransformer: func(token *jwt.Token) *jwt.Token {
				return token
			},
			pubKeySupplier: &stubNRASClient{
				jwksRes: []byte("invalid"),
			},
			signingKey:    signingKey,
			signingMethod: jwt.SigningMethodES384,
			wantErr:       true,
		},
		"different signing key": {
			tokenTransformer: func(token *jwt.Token) *jwt.Token {
				return token
			},
			pubKeySupplier: keySupplier,
			signingKey: func(t *testing.T) *ecdsa.PrivateKey {
				s := signingKey(t)
				s.D = big.NewInt(0)
				return s
			},
			signingMethod: jwt.SigningMethodES384,
			wantErr:       true,
		},
		"jwks error": {
			tokenTransformer: func(token *jwt.Token) *jwt.Token {
				return token
			},
			pubKeySupplier: &stubNRASClient{
				jwksErr: assert.AnError,
			},
			signingKey:    signingKey,
			signingMethod: jwt.SigningMethodES384,
			wantErr:       true,
		},
		"fetching nras attestation error": {
			tokenTransformer: func(token *jwt.Token) *jwt.Token {
				return token
			},
			pubKeySupplier: &stubNRASClient{
				attestErr: assert.AnError,
			},
			signingKey:    signingKey,
			signingMethod: jwt.SigningMethodES384,
			wantErr:       true,
		},
		"debug mode enabled": {
			tokenTransformer: func(token *jwt.Token) *jwt.Token {
				token.Claims.(jwt.MapClaims)["dbgstat"] = "enabled"
				return token
			},
			pubKeySupplier: keySupplier,
			signingKey:     signingKey,
			signingMethod:  jwt.SigningMethodES384,
			wantErr:        true,
		},
		"eat version mismatch": {
			tokenTransformer: func(token *jwt.Token) *jwt.Token {
				token.Claims.(jwt.MapClaims)["x-nvidia-eat-ver"] = "eat-invalid"
				return token
			},
			pubKeySupplier: keySupplier,
			signingKey:     signingKey,
			signingMethod:  jwt.SigningMethodES384,
			wantErr:        true,
		},
		"driver version mismatch": {
			tokenTransformer: func(token *jwt.Token) *jwt.Token {
				token.Claims.(jwt.MapClaims)["x-nvidia-gpu-driver-version"] = "driver-invalid"
				return token
			},
			pubKeySupplier: keySupplier,
			signingKey:     signingKey,
			signingMethod:  jwt.SigningMethodES384,
			wantErr:        true,
		},
		"vbios version mismatch": {
			tokenTransformer: func(token *jwt.Token) *jwt.Token {
				token.Claims.(jwt.MapClaims)["x-nvidia-gpu-vbios-version"] = "vbios-invalid"
				return token
			},
			pubKeySupplier: keySupplier,
			signingKey:     signingKey,
			signingMethod:  jwt.SigningMethodES384,
			wantErr:        true,
		},
		"secure boot mismatch": {
			tokenTransformer: func(token *jwt.Token) *jwt.Token {
				token.Claims.(jwt.MapClaims)["secboot"] = false
				return token
			},
			pubKeySupplier: keySupplier,
			signingKey:     signingKey,
			signingMethod:  jwt.SigningMethodES384,
			wantErr:        true,
		},
		"mismatching measurements": {
			tokenTransformer: func(token *jwt.Token) *jwt.Token {
				token.Claims.(jwt.MapClaims)["x-nvidia-attestation-detailed-result"] = map[string]interface{}{
					"x-nvidia-mismatch-indexes": []int{0, 1, 2},
				}
				return token
			},
			pubKeySupplier: keySupplier,
			signingKey:     signingKey,
			signingMethod:  jwt.SigningMethodES384,
			wantErr:        true,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)

			logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
			v := &Verifier{
				policy: &policy.NvidiaHopper{
					Debug:                   false,
					EATVersion:              "1",
					DriverVersions:          []string{"2"},
					VBIOSVersions:           []string{"3"},
					SecureBoot:              true,
					MismatchingMeasurements: []int{},
				},
				log:        logger,
				nrasClient: tc.pubKeySupplier,
			}

			token := createToken(t, tc.tokenTransformer, tc.signingKey(t), tc.signingMethod)

			err := v.Verify(context.Background(), token, [32]byte{})
			if tc.wantErr {
				assert.Error(err)
			} else {
				assert.NoError(err)
			}
		})
	}
}

type stubNRASClient struct {
	jwksRes   []byte
	jwksErr   error
	attestErr error
}

func (c *stubNRASClient) JWKS(_ context.Context) ([]byte, error) {
	return c.jwksRes, c.jwksErr
}
