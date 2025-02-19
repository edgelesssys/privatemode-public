package nras

import (
	"context"
	"fmt"
	"io"
	"net/http"
)

// JWKS returns the JSON Web Key Set (JWKS) used to verify the EATs issued by the NRAS.
func (c *Client) JWKS(ctx context.Context) ([]byte, error) {
	c.log.Info("Retrieving NRAS JWKS")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://nras.attestation.nvidia.com/.well-known/jwks.json", nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	res, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("performing request: %w", err)
	}
	defer res.Body.Close()

	resBody, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", res.StatusCode)
	}

	return resBody, nil
}
