package distributor

import (
	"context"
	"encoding/binary"
	"fmt"
	"hash/fnv"

	"github.com/grafana/dskit/ring"
	ring_client "github.com/grafana/dskit/ring/client"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/limits"
	limits_frontend_client "github.com/grafana/loki/v3/pkg/limits/frontend/client"
	"github.com/grafana/loki/v3/pkg/limits/proto"
)

// ingestLimitsFrontendClient is used for tests.
type ingestLimitsFrontendClient interface {
	ExceedsLimits(context.Context, *proto.ExceedsLimitsRequest) (*proto.ExceedsLimitsResponse, error)
}

// ingestLimitsFrontendRingClient uses the ring to query ingest-limits frontends.
type ingestLimitsFrontendRingClient struct {
	ring ring.ReadRing
	pool *ring_client.Pool
}

func newIngestLimitsFrontendRingClient(ring ring.ReadRing, pool *ring_client.Pool) *ingestLimitsFrontendRingClient {
	return &ingestLimitsFrontendRingClient{
		ring: ring,
		pool: pool,
	}
}

// Implements the ingestLimitsFrontendClient interface.
func (c *ingestLimitsFrontendRingClient) ExceedsLimits(ctx context.Context, req *proto.ExceedsLimitsRequest) (*proto.ExceedsLimitsResponse, error) {
	// We use an FNV-1 of all stream hashes in the request to load balance requests
	// to limits-frontends instances.
	h := fnv.New32()
	for _, stream := range req.Streams {
		// Add the stream hash to FNV-1.
		buf := make([]byte, binary.MaxVarintLen64)
		binary.PutUvarint(buf, stream.StreamHash)
		_, _ = h.Write(buf)
	}
	// Get the limits-frontend instances from the ring.
	var descs [5]ring.InstanceDesc
	rs, err := c.ring.Get(h.Sum32(), limits_frontend_client.LimitsRead, descs[0:], nil, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get limits-frontend instances from ring: %w", err)
	}
	var lastErr error
	// Send the request to the limits-frontend to see if it exceeds the tenant
	// limits. If the RPC fails, failover to the next instance in the ring.
	for _, instance := range rs.Instances {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		c, err := c.pool.GetClientFor(instance.Addr)
		if err != nil {
			lastErr = err
			continue
		}
		client := c.(proto.IngestLimitsFrontendClient)
		resp, err := client.ExceedsLimits(ctx, req)
		if err != nil {
			lastErr = err
			continue
		}
		return resp, nil
	}
	return nil, lastErr
}

type ingestLimits struct {
	client         ingestLimitsFrontendClient
	requests       prometheus.Counter
	requestsFailed prometheus.Counter
}

func newIngestLimits(client ingestLimitsFrontendClient, r prometheus.Registerer) *ingestLimits {
	return &ingestLimits{
		client: client,
		requests: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_distributor_ingest_limits_requests_total",
			Help: "The total number of requests.",
		}),
		requestsFailed: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_distributor_ingest_limits_requests_failed_total",
			Help: "The total number of requests that failed.",
		}),
	}
}

// EnforceLimits checks all streams against the per-tenant limits and returns
// a slice containing the streams that are accepted (within the per-tenant
// limits). Any streams that could not have their limits checked are also
// accepted.
func (l *ingestLimits) EnforceLimits(ctx context.Context, tenant string, streams []KeyedStream) ([]KeyedStream, error) {
	results, err := l.ExceedsLimits(ctx, tenant, streams)
	if err != nil {
		return streams, err
	}
	// We can do this without allocation if needed, but doing so will modify
	// the original backing array. See "Filtering without allocation" from
	// https://go.dev/wiki/SliceTricks.
	accepted := make([]KeyedStream, 0, len(streams))
	for _, s := range streams {
		// Check each stream to see if it failed.
		// TODO(grobinson): We have an O(N*M) loop here. Need to benchmark if
		// its faster to do this or if we should create a map instead.
		var (
			found  bool
			reason uint32
		)
		for _, res := range results {
			if res.StreamHash == s.HashKeyNoShard {
				found = true
				reason = res.Reason
				break
			}
		}
		if !found || reason == uint32(limits.ReasonFailed) {
			accepted = append(accepted, s)
		}
	}
	return accepted, nil
}

// ExceedsLimits checks all streams against the per-tenant limits. It returns
// an error if the client failed to send the request or receive a response
// from the server. Any streams that could not have their limits checked
// and returned in the results with the reason "ReasonFailed".
func (l *ingestLimits) ExceedsLimits(
	ctx context.Context,
	tenant string,
	streams []KeyedStream,
) ([]*proto.ExceedsLimitsResult, error) {
	l.requests.Inc()
	req, err := newExceedsLimitsRequest(tenant, streams)
	if err != nil {
		l.requestsFailed.Inc()
		return nil, err
	}
	resp, err := l.client.ExceedsLimits(ctx, req)
	if err != nil {
		l.requestsFailed.Inc()
		return nil, err
	}
	return resp.Results, nil
}

func newExceedsLimitsRequest(tenant string, streams []KeyedStream) (*proto.ExceedsLimitsRequest, error) {
	// The distributor sends the hashes of all streams in the request to the
	// limits-frontend. The limits-frontend is responsible for deciding if
	// the request would exceed the tenants limits, and if so, which streams
	// from the request caused it to exceed its limits.
	streamMetadata := make([]*proto.StreamMetadata, 0, len(streams))
	for _, stream := range streams {
		entriesSize, structuredMetadataSize := calculateStreamSizes(stream.Stream)
		streamMetadata = append(streamMetadata, &proto.StreamMetadata{
			StreamHash: stream.HashKeyNoShard,
			TotalSize:  entriesSize + structuredMetadataSize,
		})
	}
	return &proto.ExceedsLimitsRequest{
		Tenant:  tenant,
		Streams: streamMetadata,
	}, nil
}
