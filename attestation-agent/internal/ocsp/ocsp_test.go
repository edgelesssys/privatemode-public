package ocsp

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	internalOCSP "github.com/edgelesssys/continuum/internal/oss/ocsp"
	"github.com/stretchr/testify/require"
	xocsp "golang.org/x/crypto/ocsp"
)

func TestVerifyCertChain(t *testing.T) {
	keys := make(map[*x509.Certificate]*ecdsa.PrivateKey)
	newCert := func(serial int64, parent *x509.Certificate, isCA bool) *x509.Certificate {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		require.NoError(t, err)
		template := &x509.Certificate{
			SerialNumber:          big.NewInt(serial),
			Subject:               pkix.Name{CommonName: big.NewInt(serial).String()},
			NotBefore:             time.Now().Add(-time.Hour),
			NotAfter:              time.Now().Add(time.Hour),
			BasicConstraintsValid: true,
			IsCA:                  isCA,
			KeyUsage:              x509.KeyUsageDigitalSignature,
		}
		if isCA {
			template.KeyUsage |= x509.KeyUsageCertSign
		}
		signer := keys[parent]
		if parent == nil {
			parent = template
			signer = key
		}
		der, err := x509.CreateCertificate(rand.Reader, template, parent, &key.PublicKey, signer)
		require.NoError(t, err)
		cert, err := x509.ParseCertificate(der)
		require.NoError(t, err)
		keys[cert] = key
		return cert
	}
	root := newCert(1, nil, true)
	leaf := newCert(2, root, false)
	extraLeaf := newCert(3, root, false)
	intermediate2 := newCert(4, root, true)
	intermediate1 := newCert(5, intermediate2, true)
	deepLeaf := newCert(6, intermediate1, false)
	// Same names and serials, but independent keys throughout the other hierarchy.
	otherRoot := newCert(1, nil, true)
	otherIntermediate2 := newCert(4, otherRoot, true)
	otherIntermediate1 := newCert(5, otherIntermediate2, true)
	otherLeaf := newCert(6, otherIntermediate1, false)
	originalRoot := gpuDeviceIdentityCACert
	gpuDeviceIdentityCACert = root
	t.Cleanup(func() { gpuDeviceIdentityCACert = originalRoot })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	client := New(slog.Default())
	client.url = server.URL

	for _, tc := range []struct {
		name    string
		chain   []*x509.Certificate
		mode    VerificationMode
		wantErr string
	}{
		{"valid", []*x509.Certificate{leaf, root}, VerificationModeGPUAttestation, ""},
		{"unknown mode", []*x509.Certificate{leaf, root}, "invalid", "unknown verification mode"},
		{"extra leaf", []*x509.Certificate{leaf, extraLeaf, root}, VerificationModeGPUAttestation, "does not match presented certificate chain"},
		{"duplicate leaf", []*x509.Certificate{leaf, leaf, root}, VerificationModeGPUAttestation, "does not match presented certificate chain"},
		{"valid intermediates", []*x509.Certificate{deepLeaf, intermediate1, intermediate2, root}, VerificationModeGPUAttestation, ""},
		{"fake root with same subject", []*x509.Certificate{deepLeaf, intermediate1, intermediate2, otherRoot}, VerificationModeGPUAttestation, "does not match pinned trust anchor"},
		{"other hierarchy", []*x509.Certificate{otherLeaf, otherIntermediate1, otherIntermediate2, otherRoot}, VerificationModeGPUAttestation, "does not match pinned trust anchor"},
		{"other hierarchy with pinned root", []*x509.Certificate{otherLeaf, otherIntermediate1, otherIntermediate2, root}, VerificationModeGPUAttestation, "verifying certificate chain"},
		{"intermediates out of order", []*x509.Certificate{deepLeaf, intermediate2, intermediate1, root}, VerificationModeGPUAttestation, "does not match presented certificate chain"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			originalChain := slices.Clone(tc.chain)
			status, err := client.VerifyCertChain(t.Context(), tc.chain, tc.mode)
			for i := range originalChain {
				require.Same(t, originalChain[i], tc.chain[i], "caller chain must remain unchanged")
			}
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				require.Equal(t, internalOCSP.StatusUnknown, status)
			} else {
				require.NoError(t, err)
				if len(tc.chain) > 2 {
					// The chain is valid, but the OCSP server returns HTTP 500.
					require.Equal(t, internalOCSP.StatusUnknown, status)
				} else {
					require.Equal(t, internalOCSP.StatusGood, status)
				}
			}
		})
	}
}

func TestVerifyCertChainOCSPResponse(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	issuerKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	responderKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	issuerTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "issuer"},
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, issuerTemplate, issuerTemplate, &issuerKey.PublicKey, issuerKey)
	require.NoError(t, err)
	issuer, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	leafTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "leaf"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
	}
	der, err = x509.CreateCertificate(rand.Reader, leafTemplate, issuer, &responderKey.PublicKey, issuerKey)
	require.NoError(t, err)
	leaf, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	originalRoot := rimSigningRootCACert
	rimSigningRootCACert = issuer
	t.Cleanup(func() { rimSigningRootCACert = originalRoot })

	for _, tc := range []struct {
		name            string
		modifyResponse  func(*xocsp.Response)
		modifyResponder func(*x509.Certificate)
		embedIssuer     bool
		want            internalOCSP.Status
	}{
		{name: "issuer signed", want: internalOCSP.StatusGood},
		{name: "embedded issuer", embedIssuer: true, want: internalOCSP.StatusGood},
		{name: "authorized delegate", modifyResponder: func(*x509.Certificate) {}, want: internalOCSP.StatusGood},
		{name: "wrong serial", modifyResponse: func(r *xocsp.Response) { r.SerialNumber = big.NewInt(3) }, want: internalOCSP.StatusUnknown},
		{name: "expired response", modifyResponse: func(r *xocsp.Response) { r.ThisUpdate = now.Add(-2 * time.Hour); r.NextUpdate = now.Add(-time.Hour) }, want: internalOCSP.StatusUnknown},
		{name: "future response", modifyResponse: func(r *xocsp.Response) { r.ThisUpdate = now.Add(time.Hour); r.NextUpdate = now.Add(2 * time.Hour) }, want: internalOCSP.StatusUnknown},
		{name: "within clock skew", modifyResponse: func(r *xocsp.Response) { r.ThisUpdate = time.Now().Add(4*time.Minute + 59*time.Second) }, want: internalOCSP.StatusGood},
		{name: "beyond clock skew", modifyResponse: func(r *xocsp.Response) { r.ThisUpdate = time.Now().Add(5*time.Minute + 30*time.Second) }, want: internalOCSP.StatusUnknown},
		{name: "recent expiry is not extended", modifyResponse: func(r *xocsp.Response) { r.NextUpdate = now.Add(-time.Second) }, want: internalOCSP.StatusUnknown},
		{name: "inverted future interval", modifyResponse: func(r *xocsp.Response) {
			r.ThisUpdate = now.Add(4 * time.Minute)
			r.NextUpdate = now.Add(2 * time.Minute)
		}, want: internalOCSP.StatusUnknown},
		{name: "empty future interval", modifyResponse: func(r *xocsp.Response) { r.ThisUpdate = now.Add(2 * time.Minute); r.NextUpdate = r.ThisUpdate }, want: internalOCSP.StatusUnknown},
		{name: "missing expiry", modifyResponse: func(r *xocsp.Response) { r.NextUpdate = time.Time{} }, want: internalOCSP.StatusUnknown},
		{name: "missing this update", modifyResponse: func(r *xocsp.Response) { r.ThisUpdate = time.Time{} }, want: internalOCSP.StatusUnknown},
		{name: "server auth delegate", modifyResponder: func(c *x509.Certificate) { c.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth} }, want: internalOCSP.StatusUnknown},
		{name: "unrestricted delegate", modifyResponder: func(c *x509.Certificate) { c.ExtKeyUsage = nil }, want: internalOCSP.StatusUnknown},
		{name: "any usage delegate", modifyResponder: func(c *x509.Certificate) { c.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageAny} }, want: internalOCSP.StatusUnknown},
		{name: "expired delegate", modifyResponder: func(c *x509.Certificate) { c.NotBefore = now.Add(-2 * time.Hour); c.NotAfter = now.Add(-time.Hour) }, want: internalOCSP.StatusUnknown},
		{name: "future delegate", modifyResponder: func(c *x509.Certificate) { c.NotBefore = now.Add(time.Minute) }, want: internalOCSP.StatusUnknown},
		{name: "non signing delegate", modifyResponder: func(c *x509.Certificate) { c.KeyUsage = x509.KeyUsageKeyEncipherment }, want: internalOCSP.StatusUnknown},
		{name: "revoked", modifyResponse: func(r *xocsp.Response) { r.Status = xocsp.Revoked }, want: internalOCSP.StatusRevoked(now.Add(-time.Minute))},
		{name: "unknown", modifyResponse: func(r *xocsp.Response) { r.Status = xocsp.Unknown }, want: internalOCSP.StatusUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := xocsp.Response{
				SerialNumber: leaf.SerialNumber, Status: xocsp.Good,
				ThisUpdate: now.Add(-time.Minute), NextUpdate: now.Add(time.Hour),
				RevokedAt: now.Add(-time.Minute),
			}
			if tc.modifyResponse != nil {
				tc.modifyResponse(&response)
			}
			signer, key := issuer, issuerKey
			if tc.embedIssuer {
				response.Certificate = issuer
			}
			if tc.modifyResponder != nil {
				template := &x509.Certificate{
					SerialNumber: big.NewInt(4), Subject: pkix.Name{CommonName: "responder"},
					NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
					ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageOCSPSigning},
					KeyUsage:    x509.KeyUsageDigitalSignature,
				}
				tc.modifyResponder(template)
				der, err := x509.CreateCertificate(rand.Reader, template, issuer, &responderKey.PublicKey, issuerKey)
				require.NoError(t, err)
				signer, err = x509.ParseCertificate(der)
				require.NoError(t, err)
				key = responderKey
				response.Certificate = signer
			}
			body, err := xocsp.CreateResponse(issuer, signer, response, key)
			require.NoError(t, err)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(body) }))
			defer server.Close()
			var logs bytes.Buffer
			client := New(slog.New(slog.NewTextHandler(&logs, nil)))
			client.url = server.URL
			status, err := client.VerifyCertChain(t.Context(), []*x509.Certificate{leaf, issuer}, VerificationModeDriverRIM)
			require.NoError(t, err)
			require.Equal(t, tc.want, status)
			if tc.name == "future response" {
				require.Contains(t, logs.String(), "OCSP response is outside its validity interval: now=")
				require.Contains(t, logs.String(), "thisUpdate="+response.ThisUpdate.Format(time.RFC3339Nano))
				require.Contains(t, logs.String(), "nextUpdate="+response.NextUpdate.Format(time.RFC3339Nano))
				require.Contains(t, logs.String(), "allowedClockSkew=5m0s")
			}
		})
	}
}

func TestVerifyCertChainOCSPCertID(t *testing.T) {
	now := time.Now()
	newKey := func() *ecdsa.PrivateKey {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		require.NoError(t, err)
		return key
	}
	issuerKey, otherIssuerKey, responderKey := newKey(), newKey(), newKey()
	issue := func(name string, serial int64, key *ecdsa.PrivateKey, parent *x509.Certificate, signer *ecdsa.PrivateKey, ca bool) *x509.Certificate {
		template := &x509.Certificate{
			SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: name},
			NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
			IsCA: ca, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature,
		}
		if ca {
			template.KeyUsage |= x509.KeyUsageCertSign
		} else {
			template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageOCSPSigning, x509.ExtKeyUsageServerAuth}
		}
		if parent == nil {
			parent = template
		}
		der, err := x509.CreateCertificate(rand.Reader, template, parent, &key.PublicKey, signer)
		require.NoError(t, err)
		cert, err := x509.ParseCertificate(der)
		require.NoError(t, err)
		return cert
	}
	issuer := issue("issuer", 1, issuerKey, nil, issuerKey, true)
	leaf := issue("leaf", 42, newKey(), issuer, issuerKey, false)
	responder := issue("shared responder", 2, responderKey, issuer, issuerKey, false)
	originalRoot := rimSigningRootCACert
	rimSigningRootCACert = issuer
	t.Cleanup(func() { rimSigningRootCACert = originalRoot })

	for _, tc := range []struct {
		name       string
		issuerName string
		issuerKey  *ecdsa.PrivateKey
		hash       crypto.Hash
		additional string
		want       internalOCSP.Status
	}{
		{"valid SHA1", "issuer", issuerKey, crypto.SHA1, "", internalOCSP.StatusGood},
		{"valid SHA256", "issuer", issuerKey, crypto.SHA256, "", internalOCSP.StatusGood},
		{"valid SHA384", "issuer", issuerKey, crypto.SHA384, "", internalOCSP.StatusGood},
		{"valid SHA512", "issuer", issuerKey, crypto.SHA512, "", internalOCSP.StatusGood},
		{"wrong issuer name only", "other issuer", issuerKey, crypto.SHA384, "", internalOCSP.StatusUnknown},
		{"wrong issuer key only", "issuer", otherIssuerKey, crypto.SHA384, "", internalOCSP.StatusUnknown},
		{"different issuer same serial", "other issuer", otherIssuerKey, crypto.SHA1, "", internalOCSP.StatusUnknown},
		{"unrelated serial first", "issuer", issuerKey, crypto.SHA384, "before", internalOCSP.StatusGood},
		{"matching issuer later must not rescue first status", "other issuer", otherIssuerKey, crypto.SHA384, "after", internalOCSP.StatusUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			otherIssuer := issue(tc.issuerName, 3, tc.issuerKey, nil, tc.issuerKey, true)
			otherResponder := issue("shared responder", 4, responderKey, otherIssuer, tc.issuerKey, false)
			body, err := xocsp.CreateResponse(otherIssuer, otherResponder, xocsp.Response{
				SerialNumber: leaf.SerialNumber, Status: xocsp.Good, IssuerHash: tc.hash,
				ThisUpdate: now.Add(-time.Minute), NextUpdate: now.Add(time.Hour), Certificate: otherResponder,
				SignatureAlgorithm: x509.ECDSAWithSHA256,
			}, responderKey)
			require.NoError(t, err)
			original, err := xocsp.ParseResponseForCert(body, leaf, otherIssuer)
			require.NoError(t, err)

			// Certificate attachments are unsigned. A responder key authorized by
			// both issuers lets an attacker swap certificates without forging a signature.
			var outer struct {
				Status   asn1.Enumerated
				Response struct {
					Type asn1.ObjectIdentifier
					Body []byte
				} `asn1:"explicit,tag:0"`
			}
			_, err = asn1.Unmarshal(body, &outer)
			require.NoError(t, err)
			var basic struct {
				TBS       asn1.RawValue
				Algorithm pkix.AlgorithmIdentifier
				Signature asn1.BitString
				Certs     []asn1.RawValue `asn1:"explicit,tag:0,optional"`
			}
			_, err = asn1.Unmarshal(outer.Response.Body, &basic)
			require.NoError(t, err)
			if tc.additional != "" {
				// Build a signed multi-status fixture; the attacker still changes
				// only the certificate attachment below, not these signed bytes.
				type responseData struct {
					Version     int `asn1:"optional,default:0,explicit,tag:0"`
					ResponderID asn1.RawValue
					ProducedAt  time.Time `asn1:"generalized"`
					Responses   []asn1.RawValue
				}
				var data, extra responseData
				_, err = asn1.Unmarshal(basic.TBS.FullBytes, &data)
				require.NoError(t, err)
				serial := leaf.SerialNumber
				extraHash := tc.hash
				if tc.additional == "before" {
					serial = big.NewInt(99)
					extraHash = crypto.SHA1
				}
				extraBody, err := xocsp.CreateResponse(issuer, responder, xocsp.Response{
					SerialNumber: serial, Status: xocsp.Good, IssuerHash: extraHash,
					ThisUpdate: now.Add(-time.Minute), NextUpdate: now.Add(time.Hour), Certificate: responder,
				}, responderKey)
				require.NoError(t, err)
				extraParsed, err := xocsp.ParseResponse(extraBody, issuer)
				require.NoError(t, err)
				_, err = asn1.Unmarshal(extraParsed.TBSResponseData, &extra)
				require.NoError(t, err)
				if tc.additional == "before" {
					data.Responses = append(extra.Responses, data.Responses...)
				} else {
					data.Responses = append(data.Responses, extra.Responses...)
				}
				basic.TBS.FullBytes, err = asn1.Marshal(data)
				require.NoError(t, err)
				digest := sha256.Sum256(basic.TBS.FullBytes)
				basic.Signature.Bytes, err = ecdsa.SignASN1(rand.Reader, responderKey, digest[:])
				require.NoError(t, err)
				basic.Signature.BitLength = len(basic.Signature.Bytes) * 8
				original.TBSResponseData = basic.TBS.FullBytes
				original.Signature = basic.Signature.Bytes
			}
			basic.Certs = []asn1.RawValue{{FullBytes: responder.Raw}}
			outer.Response.Body, err = asn1.Marshal(basic)
			require.NoError(t, err)
			body, err = asn1.Marshal(outer)
			require.NoError(t, err)
			parsed, err := xocsp.ParseResponseForCert(body, leaf, issuer)
			require.NoError(t, err)
			require.Equal(t, original.TBSResponseData, parsed.TBSResponseData)
			require.Equal(t, original.Signature, parsed.Signature)

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(body) }))
			defer server.Close()
			client := New(slog.Default())
			client.url = server.URL
			status, err := client.VerifyCertChain(t.Context(), []*x509.Certificate{leaf, issuer}, VerificationModeDriverRIM)
			require.NoError(t, err)
			require.Equal(t, tc.want, status)
		})
	}
}
