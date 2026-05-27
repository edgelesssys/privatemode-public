// Package usagereporter implements a client for the Usage Collector RPC service.
package usagereporter

import (
	"context"
	"net/http"

	"github.com/edgelesssys/continuum/internal/httprpc"
	"github.com/edgelesssys/continuum/internal/mtls"
	contract "github.com/edgelesssys/continuum/internal/usagecollector"
)

// peerServerName is the apigateway which serves the usage-collector service at the moment.
const peerServerName = "apigateway"

// Client submits usage reports via the Usage Collector RPC.
type Client struct {
	rpc *httprpc.Client
}

// New constructs a [Client] that dials endpoint (host:port) over mTLS using identity.
func New(endpoint string, identity mtls.Identity) (*Client, error) {
	tlsCfg := identity.ClientConfig()
	tlsCfg.ServerName = peerServerName
	httpClient := &http.Client{Transport: httprpc.NewHTTP2Transport(tlsCfg)}
	rpc, err := httprpc.NewClient("https://"+endpoint, httpClient)
	if err != nil {
		return nil, err
	}
	return &Client{rpc: rpc}, nil
}

// Submit sends a single report. This method does not retry.
func (c *Client) Submit(ctx context.Context, req contract.SubmitUsageReportRequest) error {
	_, err := httprpc.ClientCall[contract.SubmitUsageReportRequest, contract.SubmitUsageReportResponse](
		ctx, c.rpc, contract.SubmitUsageReportRPC, req,
	)
	return err
}
