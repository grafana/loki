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

	limits_frontend_client "github.com/grafana/loki/v3/pkg/limits/frontend/client"
	"github.com/grafana/loki/v3/pkg/logproto"
)

// ingestLimitsFrontendClient is used for tests.
type ingestLimitsFrontendClient interface {
	ExceedsLimits(context.Context, *logproto.ExceedsLimitsRequest) (*logproto.ExceedsLimitsResponse, error)
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
func (c *ingestLimitsFrontendRingClient) ExceedsLimits(ctx context.Context, req *logproto.ExceedsLimitsRequest) (*logproto.ExceedsLimitsResponse, error) {
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
		client := c.(logproto.IngestLimitsFrontendClient)
		resp, err := client.ExceedsLimits(ctx, req)
		if err != nil {
			lastErr = err
			continue
		}
		return resp, nil
	}
	return nil, lastErr
}

// exceedsIngestLimitsResult contains the reasons a stream exceeds per-tenant
// ingest limits.
type exceedsIngestLimitsResult struct {
	hash    uint64
	reasons []string
}

type ingestLimits struct {
	client         ingestLimitsFrontendClient
	limitsFailures prometheus.Counter
}

func newIngestLimits(client ingestLimitsFrontendClient, r prometheus.Registerer) *ingestLimits {
	return &ingestLimits{
		client: client,
		limitsFailures: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_distributor_ingest_limits_failures_total",
			Help: "The total number of failures checking per-tenant ingest limits.",
		}),
	}
}

// ExceedsLimits returns true if at least one stream exceeds the per-tenant
// limits, otherwise false. It also returns a slice containing the streams
// that exceeded the per-tenant limits, and for each stream the reasons it
// exceeded the limits. This slice can be nil. An error is returned if the
// limits could not be checked.
func (l *ingestLimits) ExceedsLimits(ctx context.Context, tenant string, streams []KeyedStream) (bool, []exceedsIngestLimitsResult, error) {
	req, err := newExceedsLimitsRequest(tenant, streams)
	if err != nil {
		return false, nil, err
	}
	resp, err := l.client.ExceedsLimits(ctx, req)
	if err != nil {
		return false, nil, err
	}
	if len(resp.Results) == 0 {
		return false, nil, nil
	}
	// A stream can exceed limits for multiple reasons. For example, exceeding
	// both per-tenant stream limit and rate limits. We organize the reasons
	// for each stream into a slice, and then add that to the results.
	reasonsForHashes := make(map[uint64][]string)
	for _, result := range resp.Results {
		reasons := reasonsForHashes[result.StreamHash]
		reasons = append(reasons, result.Reason)
		reasonsForHashes[result.StreamHash] = reasons
	}
	result := make([]exceedsIngestLimitsResult, 0, len(reasonsForHashes))
	for hash, reasons := range reasonsForHashes {
		result = append(result, exceedsIngestLimitsResult{
			hash:    hash,
			reasons: reasons,
		})
	}
	return true, result, nil
}

func newExceedsLimitsRequest(tenant string, streams []KeyedStream) (*logproto.ExceedsLimitsRequest, error) {
	// The distributor sends the hashes of all streams in the request to the
	// limits-frontend. The limits-frontend is responsible for deciding if
	// the request would exceed the tenants limits, and if so, which streams
	// from the request caused it to exceed its limits.
	streamMetadata := make([]*logproto.StreamMetadata, 0, len(streams))
	for _, stream := range streams {
		entriesSize, structuredMetadataSize := calculateStreamSizes(stream.Stream)
		streamMetadata = append(streamMetadata, &logproto.StreamMetadata{
			StreamHash:             stream.HashKeyNoShard,
			EntriesSize:            entriesSize,
			StructuredMetadataSize: structuredMetadataSize,
		})
	}
	return &logproto.ExceedsLimitsRequest{
		Tenant:  tenant,
		Streams: streamMetadata,
	}, nil
}
