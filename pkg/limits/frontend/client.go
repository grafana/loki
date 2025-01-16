package frontend

import (
	"context"

	"github.com/grafana/dskit/ring"

	"github.com/grafana/loki/v3/pkg/logproto"
)

type IngestLimitsClient interface {
	// ExceedsLimits checksif the request would exceed the current tenants
	// limits.
	ExceedsLimits(ctx context.Context, r *logproto.ExceedsLimitsRequest) (*logproto.ExceedsLimitsResponse, error)
}

// RingIngestLimitsClient is an IngestLimitsClients that uses the ring to read the responses
// from all limits backends.
type RingIngestLimitsClient struct {
	ring ring.ReadRing
}

// NewRingIngestLimitsClients returns a new RingIngestLimitsCLient.
func NewRingIngestLimitsClient(ring ring.ReadRing) *RingIngestLimitsClient {
	return &RingIngestLimitsClient{
		ring: ring,
	}
}

func (c *RingIngestLimitsClient) forAllBackends(_ context.Context, _ func(context.Context, logproto.IngestLimitsClient) (interface{}, error)) ([]Response, error) {
	return nil, nil
}

func (c *RingIngestLimitsClient) ExceedsLimits(_ context.Context, _ *logproto.ExceedsLimitsRequest) (*logproto.ExceedsLimitsResponse, error) {
	return nil, nil
	// req := &logproto.GetLimitsRequest{
	// 	Tenant: tenantID,
	// }
	// resps, err := c.forAllBackends(ctx, func(_ context.Context, client logproto.IngestLimitsClient) (interface{}, error) {
	// 	return client.NumStreams(ctx, req)
	// })
	// if err != nil {
	// 	return nil, err
	// }
	// var sum uint64
	// for _, resp := range resps {
	// 	sum += resp.num
	// }
	// return &logproto.ExceedsLimitsResponse{

	// }, nil
}

type Response struct {
	Addr string
	Data interface{}
}
