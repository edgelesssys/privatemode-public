package httprpc

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

// Client is an HTTP-RPC client.
type Client struct {
	httpClient *http.Client
	baseURL    *url.URL
}

// NewClient returns a [Client] with baseURL.
func NewClient(baseURL string, httpClient *http.Client) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parsing base URL: %w", err)
	}
	return &Client{
		httpClient: httpClient,
		baseURL:    u,
	}, nil
}

// ClientCall calls the RPC func with name on c, sending req as JSON and decoding the response into
// Resp. Unsuccessful responses are returned as [*ResponseError]. Transport and encoding failures
// are plain errors.
func ClientCall[Req any, Resp any](ctx context.Context, c *Client, name string, req Req) (Resp, error) {
	var resp Resp

	body, err := json.Marshal(req)
	if err != nil {
		return resp, fmt.Errorf("encoding request: %w", err)
	}

	url := c.baseURL.JoinPath(name).String()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return resp, fmt.Errorf("creating request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return resp, fmt.Errorf("performing request: %w", err)
	}
	defer httpResp.Body.Close()

	// http.MaxBytesReader: passing nil for the ResponseWriter is explicitly supported though not documented
	respBody, err := io.ReadAll(http.MaxBytesReader(nil, httpResp.Body, maxBodyBytes))
	if err != nil {
		return resp, fmt.Errorf("reading response body: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		if isJSONContentType(httpResp.Header.Get("Content-Type")) {
			var errResp ErrorResponse
			if err := json.Unmarshal(respBody, &errResp); err == nil {
				return resp, &ResponseError{Message: errResp.Message, Code: errResp.Code, ServerFault: errResp.ServerFault}
			}
			// Failed JSON parsing falls back to full response as message
		}
		serverFault := httpResp.StatusCode >= 500
		return resp, &ResponseError{Message: string(respBody), ServerFault: serverFault}
	}

	if !isJSONContentType(httpResp.Header.Get("Content-Type")) {
		return resp, fmt.Errorf("unexpected content type %q", httpResp.Header.Get("Content-Type"))
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return resp, fmt.Errorf("decoding response: %w", err)
	}
	return resp, nil
}

// ResponseError is returned by [ClientCall] on error response.
type ResponseError struct {
	Message     string
	Code        string
	ServerFault bool
}

func (r *ResponseError) Error() string {
	if r.Code != "" {
		return fmt.Sprintf("rpc response: [%s] %s", r.Code, r.Message)
	}
	return "rpc response: " + r.Message
}

// Is matches by Code. Use errors.As with [*ResponseError] to inspect ServerFault.
func (r *ResponseError) Is(target error) bool {
	if r.Code == "" {
		return false
	}

	if matcher, ok := target.(ErrorCodeMatcher); ok {
		return matcher.MatchesErrorCode(r.Code)
	}

	return false
}

// NewHTTP2Transport returns an HTTP/2-only [*http.Transport] for service-to-service RPC. Intended
// for use against a Kubernetes ClusterIP service. tlsConfig is cloned.
func NewHTTP2Transport(tlsConfig *tls.Config) *http.Transport {
	// HTTP/2-only: kube-proxy DNATs at TCP setup, pinning all streams on a connection to one
	// backend pod. Dead pods must be detected via HTTP/2 PINGs, not connection replacement.
	var protocols http.Protocols
	protocols.SetHTTP2(true)

	return &http.Transport{
		TLSClientConfig: tlsConfig.Clone(),
		Protocols:       &protocols,
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 15 * time.Second,
		}).DialContext,
		MaxConnsPerHost:       4,
		MaxIdleConns:          4,
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		HTTP2: &http.HTTP2Config{
			SendPingTimeout:  15 * time.Second,
			PingTimeout:      5 * time.Second,
			WriteByteTimeout: 10 * time.Second,
		},
	}
}
