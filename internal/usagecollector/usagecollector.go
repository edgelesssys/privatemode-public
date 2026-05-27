// Package usagecollector specifies the interface of the Usage Collector RPC service.
package usagecollector

import (
	"time"

	"github.com/edgelesssys/continuum/internal/httprpc"
	"github.com/edgelesssys/continuum/internal/oss/usage"
)

// SubmitUsageReportRPC submits a usage report describing token consumption for a request.
// The request is [SubmitUsageReportRequest], the response is [SubmitUsageReportResponse].
// It may return specific errors: [ErrInvalidUsageID].
const SubmitUsageReportRPC = "SubmitUsageReport"

// PrivatemodeUsageIDHeader carries the opaque usage ID from the API Gateway to the Inference
// Proxy.
const PrivatemodeUsageIDHeader = "Privatemode-Usage-ID"

// ErrInvalidUsageID is returned if the [SubmitUsageReportRequest.UsageID] is invalid.
var ErrInvalidUsageID = httprpc.NewSentinelError("invalid usage ID", "InvalidUsageID")

// SubmitUsageReportRequest specifies the request structure for [SubmitUsageReportRPC].
// UsageID is an opaque string issued by the API Gateway; the inference proxy passes it
// back unchanged.
type SubmitUsageReportRequest struct {
	RequestID string      `json:"request_id"`
	Timestamp time.Time   `json:"timestamp"`
	UsageID   string      `json:"usage_id"`
	Stats     usage.Stats `json:"stats"`
	Completed bool        `json:"completed"`
	Streaming bool        `json:"streaming"`
}

// SubmitUsageReportResponse specifies the response structure for [SubmitUsageReportRPC].
type SubmitUsageReportResponse struct{}
