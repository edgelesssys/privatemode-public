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
	dsig "github.com/russellhaering/goxmldsig"
)

// GPUArch is the architecture to fetch RIM data for.
type GPUArch int

const (
	// GPUArchHopper is the Hopper architecture.
	GPUArchHopper GPUArch = iota
	// GPUArchBlackwell is the Blackwell architecture.
	GPUArchBlackwell
)

// Client is a client for the Reference Integrity Measurement (RIM) service of NVIDIA.
type Client struct {
	httpClient *http.Client
	baseURL    string
	log        *slog.Logger
}

// New creates a new RIMClient.
func New(baseURL string, log *slog.Logger) *Client {
	return &Client{
		httpClient: &http.Client{},
		baseURL:    baseURL,
		log:        log,
	}
}

// FetchDriverRIM fetches reference values for the given GPU architecture and version.
func (c *Client) FetchDriverRIM(ctx context.Context, gpuArch GPUArch, version string) (*SoftwareIdentity, error) {
	var driverID string
	switch gpuArch {
	case GPUArchHopper:
		driverID = "NV_GPU_DRIVER_GH100_" + version
	case GPUArchBlackwell:
		driverID = "NV_GPU_CC_DRIVER_GB100_" + version
	default:
		return nil, fmt.Errorf("unsupported GPU architecture: %d", gpuArch)
	}

	return c.FetchRIM(ctx, driverID)
}

// FetchVBIOSRIM fetches reference values for the VBIOS of the given GPU architecture and version.
func (c *Client) FetchVBIOSRIM(ctx context.Context, project, projectSku, chipSku, vbiosVersion string) (*SoftwareIdentity, error) {
	vbiosRIMString := strings.ToUpper(strings.ReplaceAll(vbiosVersion, ".", ""))
	return c.FetchRIM(ctx, "NV_GPU_VBIOS_"+project+"_"+projectSku+"_"+chipSku+"_"+vbiosRIMString)
}

// FetchRIM fetches the reference values for the given RIM ID.
func (c *Client) FetchRIM(ctx context.Context, id string) (*SoftwareIdentity, error) {
	c.log.Info("Fetching reference values from RIM service", "id", id)

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

	c.log.Info("Validating RIM SoftwareIdentity signature", "id", id)
	signingCerts, err := softwareIdentity.SigningCerts()
	if err != nil {
		return nil, fmt.Errorf("extract signing certificates: %w", err)
	}
	if err := validateXMLSignature(rimRes.RIM, signingCerts); err != nil {
		return nil, err
	}

	return &softwareIdentity, nil
}

func validateXMLSignature(xmlData []byte, signingCerts []*x509.Certificate) error {
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(xmlData); err != nil {
		return fmt.Errorf("reading XML document: %w", err)
	}
	validateCtx := dsig.NewDefaultValidationContext(&dsig.MemoryX509CertificateStore{
		Roots: signingCerts,
	})
	if _, err := validateCtx.Validate(doc.Root()); err != nil {
		return fmt.Errorf("validating XML signature: %w", err)
	}
	return nil
}

type rimResponse struct {
	ID          string `json:"id"`
	RIM         []byte `json:"rim"`
	SHA256      string `json:"sha256"`
	LastUpdated string `json:"last_updated"`
	RIMFormat   string `json:"rim_format"`
	RequestID   string `json:"request_id"`
}
