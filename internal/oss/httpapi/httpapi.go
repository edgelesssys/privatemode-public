// Package httpapi provides helpers and types for the /privatemode/v1 API.
package httpapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/edgelesssys/continuum/internal/oss/auth"
	"github.com/edgelesssys/continuum/internal/oss/constants"
)

// ErrUnauthorized is returned if the API returned 401, indicating that the credential is invalid.
var ErrUnauthorized = errors.New(http.StatusText(http.StatusUnauthorized))

// Do performs an HTTP request to the Privatemode API.
func Do(ctx context.Context, client *http.Client, method, url string, reqBody []byte, cred auth.Credential, extraHTTPHeaders http.Header) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	for key, values := range extraHTTPHeaders {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	cred.Apply(req.Header)
	req.Header.Set(constants.PrivatemodeVersionHeader, constants.Version())
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("doing request: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("%w (status code: %d)", ErrUnauthorized, resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		// Callers log these errors; never include potentially sensitive response bodies.
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
	return respBody, nil
}

// AttestReq is the request type for the /attest endpoint.
type AttestReq struct {
	Nonce []byte
}

// AttestResp is the response type for the /attest endpoint.
type AttestResp struct {
	AttestationDoc []byte
}

// SecretReq is the request type for the /secret endpoint.
type SecretReq struct {
	PublicKey []byte
}

// SecretResp is the response type for the /secret endpoint.
type SecretResp struct {
	EncapsulatedKey []byte
	Signature       []byte
	MeshCert        []byte
}
