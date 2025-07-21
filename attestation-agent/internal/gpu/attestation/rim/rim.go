// Package rim provides a client for the NVIDIA Reference Integrity Measurement (RIM) service.
package rim

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
)

// Client is a client for the Reference Integrity Measurement (RIM) service of NVIDIA.
type Client struct {
	httpClient *http.Client
}

// New creates a new RIMClient.
func New() *Client {
	return &Client{
		httpClient: &http.Client{},
	}
}

// FetchRIM fetches the reference values for the given RIM ID.
func (c *Client) FetchRIM(ctx context.Context, ID string) (*SoftwareIdentity, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://rim.attestation.nvidia.com/v1/rim/"+ID, nil)
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

	rimBytes, err := base64.StdEncoding.DecodeString(rimRes.RIM)
	if err != nil {
		return nil, fmt.Errorf("decode response base64: %w", err)
	}

	var softwareIdentity SoftwareIdentity
	if err := xml.Unmarshal(rimBytes, &softwareIdentity); err != nil {
		return nil, fmt.Errorf("unmarshal XML response: %w", err)
	}

	return &softwareIdentity, nil
}

type rimResponse struct {
	ID          string `json:"id"`
	RIM         string `json:"rim"`
	SHA256      string `json:"sha256"`
	LastUpdated string `json:"last_updated"`
	RIMFormat   string `json:"rim_format"`
	RequestID   string `json:"request_id"`
}
