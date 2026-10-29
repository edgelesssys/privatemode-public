// Package rim provides a client for the NVIDIA Reference Integrity Measurement (RIM) service.
package rim

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/beevik/etree"
	"github.com/edgelesssys/continuum/attestation-agent/internal/gpu"
	dsig "github.com/russellhaering/goxmldsig"
)

// Client is a client for the Reference Integrity Measurement (RIM) service of NVIDIA.
type Client struct {
	httpClient *http.Client
	baseURL    string
	log        *slog.Logger
}

// New creates a new RIMClient.
func New(httpClient *http.Client, baseURL string, log *slog.Logger) *Client {
	return &Client{
		httpClient: httpClient,
		baseURL:    baseURL,
		log:        log,
	}
}

// FetchDriverRIM fetches reference values for the given GPU architecture, chip, and version.
func (c *Client) FetchDriverRIM(ctx context.Context, gpuArch gpu.Architecture, chip, version string) (*SoftwareIdentity, error) {
	driverID, err := driverRIMID(gpuArch, chip, version)
	if err != nil {
		return nil, err
	}

	identity, err := c.FetchRIM(ctx, driverID)
	if err != nil {
		return nil, err
	}
	if gpuArch == gpu.ArchHopper {
		chip = "GH100"
	}
	if identity.Meta.Product != chip || identity.Meta.ColloquialVersion != version {
		return nil, fmt.Errorf("RIM identity mismatch: expected %s %s, got %s %s", chip, version, identity.Meta.Product, identity.Meta.ColloquialVersion)
	}
	return identity, nil
}

func driverRIMID(gpuArch gpu.Architecture, chip, version string) (string, error) {
	switch gpuArch {
	case gpu.ArchHopper:
		return "NV_GPU_DRIVER_GH100_" + version, nil
	case gpu.ArchBlackwell:
		switch chip {
		case "GB100", "GB110":
			return "NV_GPU_CC_DRIVER_" + chip + "_" + version, nil
		default:
			return "", fmt.Errorf("unsupported Blackwell GPU chip: %q", chip)
		}
	default:
		return "", fmt.Errorf("unsupported GPU architecture: %d", gpuArch)
	}
}

// FetchVBIOSRIM fetches reference values for the given GPU product and VBIOS version.
// Product must come from the verified driver RIM. The signed VBIOS metadata binds
// product and version, but does not authenticate the project or SKU lookup fields.
func (c *Client) FetchVBIOSRIM(ctx context.Context, product, project, projectSku, chipSku, vbiosVersion string) (*SoftwareIdentity, error) {
	vbiosRIMString := strings.ToUpper(strings.ReplaceAll(vbiosVersion, ".", ""))
	identity, err := c.FetchRIM(ctx, "NV_GPU_VBIOS_"+project+"_"+projectSku+"_"+chipSku+"_"+vbiosRIMString)
	if err != nil {
		return nil, err
	}
	if identity.Meta.Product != product || !strings.EqualFold(identity.Meta.ColloquialVersion, vbiosVersion) {
		return nil, fmt.Errorf("RIM identity mismatch: expected %s %s, got %s %s", product, vbiosVersion, identity.Meta.Product, identity.Meta.ColloquialVersion)
	}
	return identity, nil
}

// FetchRIM fetches the reference values for the given RIM ID.
// Callers must bind the signed metadata to their expected product and version;
// the response envelope's ID and SHA256 are controlled by the RIM service.
func (c *Client) FetchRIM(ctx context.Context, id string) (*SoftwareIdentity, error) {
	c.log.InfoContext(ctx, "Fetching reference values from RIM service", "id", id)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%sv1/rim/%s", c.baseURL, id), nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	res, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch RIM service: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code %d", res.StatusCode)
	}

	var rimRes rimResponse
	if err := json.NewDecoder(res.Body).Decode(&rimRes); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	computedSHA256 := sha256.Sum256(rimRes.RIM)
	if rimRes.SHA256 != hex.EncodeToString(computedSHA256[:]) {
		return nil, fmt.Errorf("SHA256 mismatch: expected %s, got %s", rimRes.SHA256, hex.EncodeToString(computedSHA256[:]))
	}

	var softwareIdentity SoftwareIdentity
	if err := xml.Unmarshal(rimRes.RIM, &softwareIdentity); err != nil {
		return nil, fmt.Errorf("unmarshal XML response: %w", err)
	}

	c.log.InfoContext(ctx, "Validating RIM SoftwareIdentity signature", "id", id)
	signingCerts, err := softwareIdentity.SigningCerts()
	if err != nil {
		return nil, fmt.Errorf("extract signing certificates: %w", err)
	}
	if len(signingCerts) == 0 {
		return nil, fmt.Errorf("no signing certificates")
	}
	verifiedXML, err := validateXMLSignature(rimRes.RIM, signingCerts[0])
	if err != nil {
		return nil, err
	}

	// Consume only the XML covered by the signature, not the original input.
	var verifiedIdentity SoftwareIdentity
	if err := xml.Unmarshal(verifiedXML, &verifiedIdentity); err != nil {
		return nil, fmt.Errorf("unmarshal verified XML: %w", err)
	}
	// The enveloped-signature transform removes the signature element. Preserve
	// its certificates for the caller's separate chain and revocation checks.
	verifiedIdentity.Signature = softwareIdentity.Signature
	return &verifiedIdentity, nil
}

func validateXMLSignature(xmlData []byte, signingCert *x509.Certificate) ([]byte, error) {
	doc := etree.NewDocument()
	// Preserve duplicate attributes so we can reject them rather than silently
	// verifying a different document from the one parsed by encoding/xml.
	doc.ReadSettings.PreserveDuplicateAttrs = true
	if err := doc.ReadFromBytes(xmlData); err != nil {
		return nil, fmt.Errorf("reading XML document: %w", err)
	}
	for _, element := range doc.FindElements("//*") {
		seen := make(map[string]bool, len(element.Attr))
		for _, attr := range element.Attr {
			name := attr.FullKey()
			if seen[name] {
				return nil, fmt.Errorf("duplicate XML attribute %q", name)
			}
			seen[name] = true
		}
	}
	if len(doc.FindElements("//Signature")) > 1 {
		return nil, fmt.Errorf("multiple signature elements")
	}
	validateCtx := dsig.NewDefaultValidationContext(&dsig.MemoryX509CertificateStore{
		Roots: []*x509.Certificate{signingCert},
	})
	verified, err := validateCtx.Validate(doc.Root())
	if err != nil {
		return nil, fmt.Errorf("validating XML signature: %w", err)
	}
	doc.SetRoot(verified)
	return doc.WriteToBytes()
}

type rimResponse struct {
	ID          string `json:"id"`
	RIM         []byte `json:"rim"`
	SHA256      string `json:"sha256"`
	LastUpdated string `json:"last_updated"`
	RIMFormat   string `json:"rim_format"`
	RequestID   string `json:"request_id"`
}
