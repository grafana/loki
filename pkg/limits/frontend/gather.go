package frontend

import (
	"context"

	"github.com/grafana/loki/v3/pkg/logproto"
)

type StreamUsageGatherer interface {
	// GetStreamUsage returns the current usage data for the stream hashes
	// in the request. It returns multiple responses if the usage data for
	// the requested stream hashes is partitioned over a number of limits
	// instances.
	GetStreamUsage(context.Context, GetStreamUsageRequest) ([]GetStreamUsageResponse, error)
}

type GetStreamUsageRequest struct {
	Tenant       string
	StreamHashes []uint64
}

type GetStreamUsageResponse struct {
	Addr     string
	Response *logproto.GetStreamUsageResponse
}
