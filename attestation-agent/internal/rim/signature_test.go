package rim

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/beevik/etree"
	"github.com/edgelesssys/continuum/attestation-agent/internal/gpu"
	dsig "github.com/russellhaering/goxmldsig"
	"github.com/stretchr/testify/require"
)

func TestFetchRIMSignature(t *testing.T) {
	keyStore := dsig.RandomKeyStoreForTest()
	signingKey, signingCert, err := keyStore.GetKeyPair()
	require.NoError(t, err)
	_, otherCert, err := dsig.RandomKeyStoreForTest().GetKeyPair()
	require.NoError(t, err)

	for _, tc := range []struct {
		name      string
		modify    func(*etree.Element)
		modifyXML func(string) string
		exclusive bool
		wantErr   string
	}{
		{name: "valid"},
		{
			name: "duplicate hash attribute",
			modifyXML: func(raw string) string {
				return strings.Replace(raw, `h:Hash0="`, `h:Hash0="`+strings.Repeat("ab", 48)+`" h:Hash0="`, 1)
			},
			wantErr: "duplicate XML attribute",
		},
		{
			name:      "unsigned namespace declaration is not a hash",
			exclusive: true,
			modify: func(root *etree.Element) {
				root.FindElement("Payload/Resource").CreateAttr("xmlns:HashInjected", "urn:attacker:hash")
			},
		},
		{
			name: "multiple signatures",
			modify: func(root *etree.Element) {
				root.AddChild(root.FindElement("Signature").Copy())
			},
			wantErr: "multiple signature elements",
		},
		{
			name: "nested signature with different prefix",
			modify: func(root *etree.Element) {
				signature := root.FindElement("Signature/KeyInfo").CreateElement("other:Signature")
				signature.CreateAttr("xmlns:other", dsig.Namespace)
			},
			wantErr: "multiple signature elements",
		},
		{
			name: "signer is not the presented leaf",
			modify: func(root *etree.Element) {
				data := root.FindElement("Signature/KeyInfo/X509Data")
				cert := data.ChildElements()[0]
				data.AddChild(cert.Copy())
				cert.SetText(base64.StdEncoding.EncodeToString(otherCert))
			},
			wantErr: "validating XML signature",
		},
		{
			name: "missing certificates",
			modify: func(root *etree.Element) {
				data := root.FindElement("Signature/KeyInfo/X509Data")
				for _, cert := range data.ChildElements() {
					data.RemoveChild(cert)
				}
			},
			wantErr: "no signing certificates",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := etree.NewElement("SoftwareIdentity")
			root.CreateAttr("name", "test RIM")
			resource := root.CreateElement("Payload").CreateElement("Resource")
			resource.CreateAttr("xmlns:h", "http://www.w3.org/2001/04/xmlenc#sha384")
			resource.CreateAttr("index", "7")
			resource.CreateAttr("active", "true")
			resource.CreateAttr("h:Hash0", strings.Repeat("12", 48))
			signingContext, err := dsig.NewSigningContext(signingKey, [][]byte{signingCert, otherCert})
			require.NoError(t, err)
			if tc.exclusive {
				signingContext.Canonicalizer = dsig.MakeC14N10ExclusiveCanonicalizerWithPrefixList("")
			}
			signed, err := signingContext.SignEnveloped(root)
			require.NoError(t, err)
			if tc.modify != nil {
				tc.modify(signed)
			}
			doc := etree.NewDocument()
			doc.SetRoot(signed)
			xmlData, err := doc.WriteToBytes()
			require.NoError(t, err)
			if tc.modifyXML != nil {
				modified := tc.modifyXML(string(xmlData))
				require.NotEqual(t, string(xmlData), modified)
				xmlData = []byte(modified)
			}
			hash := sha256.Sum256(xmlData)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if err := json.NewEncoder(w).Encode(rimResponse{RIM: xmlData, SHA256: hex.EncodeToString(hash[:])}); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			identity, err := New(server.Client(), server.URL+"/", slog.Default()).FetchRIM(t.Context(), "test")
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				require.Nil(t, identity)
			} else {
				require.NoError(t, err)
				require.Equal(t, "test RIM", identity.Name)
				require.Equal(t, []Resource{{Index: 7, Active: true, Hashes: []string{strings.Repeat("12", 48)}}}, identity.Payload.Resource)
				certs, err := identity.SigningCerts()
				require.NoError(t, err)
				require.Len(t, certs, 2)
				require.Equal(t, signingCert, certs[0].Raw)
				require.Equal(t, otherCert, certs[1].Raw)
			}
		})
	}
}

func TestFetchRIMBinding(t *testing.T) {
	keyStore := dsig.RandomKeyStoreForTest()
	for _, tc := range []struct {
		name                   string
		arch                   gpu.Architecture
		chip, product, version string
		vbios                  bool
		wantErr                bool
	}{
		{name: "Hopper", arch: gpu.ArchHopper, product: "GH100", version: "580.105.08"},
		{name: "Blackwell", arch: gpu.ArchBlackwell, chip: "GB100", product: "GB100", version: "580.105.08"},
		{name: "GB110", arch: gpu.ArchBlackwell, chip: "GB110", product: "GB110", version: "580.105.08"},
		{name: "other driver version", arch: gpu.ArchHopper, product: "GH100", version: "580.95.05", wantErr: true},
		{name: "other GPU product", arch: gpu.ArchHopper, product: "GB100", version: "580.105.08", wantErr: true},
		{name: "other Blackwell chip", arch: gpu.ArchBlackwell, chip: "GB100", product: "GB110", version: "580.105.08", wantErr: true},
		{name: "missing product", arch: gpu.ArchHopper, version: "580.105.08", wantErr: true},
		{name: "missing version", arch: gpu.ArchHopper, product: "GH100", wantErr: true},
		{name: "VBIOS", vbios: true, product: "GH100", version: "96.00.5E.00.01"},
		{name: "other VBIOS product", vbios: true, product: "GB100", version: "96.00.5E.00.01", wantErr: true},
		{name: "other VBIOS version", vbios: true, product: "GH100", version: "96.00.5E.00.02", wantErr: true},
		{name: "driver instead of VBIOS", vbios: true, product: "GH100", version: "580.105.08", wantErr: true},
		{name: "VBIOS instead of driver", arch: gpu.ArchHopper, product: "GH100", version: "96.00.5E.00.01", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := etree.NewElement("SoftwareIdentity")
			meta := root.CreateElement("Meta")
			meta.CreateAttr("product", tc.product)
			meta.CreateAttr("colloquialVersion", tc.version)
			signed, err := dsig.NewDefaultSigningContext(keyStore).SignEnveloped(root)
			require.NoError(t, err)
			doc := etree.NewDocument()
			doc.SetRoot(signed)
			xmlData, err := doc.WriteToBytes()
			require.NoError(t, err)
			hash := sha256.Sum256(xmlData)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// A malicious proxy can forge the envelope ID and hash, but cannot
				// change the signed product or version of a substituted manifest.
				if err := json.NewEncoder(w).Encode(rimResponse{ID: strings.TrimPrefix(r.URL.Path, "/v1/rim/"), RIM: xmlData, SHA256: hex.EncodeToString(hash[:])}); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			client := New(server.Client(), server.URL+"/", slog.Default())
			var identity *SoftwareIdentity
			if tc.vbios {
				identity, err = client.FetchVBIOSRIM(t.Context(), "GH100", "1010", "0200", "882", "96.00.5e.00.01")
			} else {
				identity, err = client.FetchDriverRIM(t.Context(), tc.arch, tc.chip, "580.105.08")
			}
			if tc.wantErr {
				require.ErrorContains(t, err, "RIM identity mismatch")
				require.Nil(t, identity)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.product, identity.Meta.Product)
				require.Equal(t, tc.version, identity.Meta.ColloquialVersion)
			}
		})
	}
}
