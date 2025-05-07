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
	exceedsLimits(context.Context, *proto.ExceedsLimitsRequest) (*proto.ExceedsLimitsResponse, error)
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
func (c *ingestLimitsFrontendRingClient) exceedsLimits(ctx context.Context, req *proto.ExceedsLimitsRequest) (*proto.ExceedsLimitsResponse, error) {
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
	limitsFailures prometheus.Counter
}

func newIngestLimits(client ingestLimitsFrontendClient, r prometheus.Registerer) *ingestLimits {
	return &ingestLimits{
		client: client,
		limitsFailures: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_distributor_ingest_limits_failures_total",
			Help: "The total number of failures checking ingest limits.",
		}),
	}
}

// enforceLimits returns a slice of streams that are within the per-tenant
// limits, and in the case where one or more streams exceed per-tenant
// limits, the reasons those streams were not included in the result.
// An error is returned if per-tenant limits could not be enforced.
func (l *ingestLimits) enforceLimits(ctx context.Context, tenant string, streams []KeyedStream) ([]KeyedStream, map[uint64][]string, error) {
	exceedsLimits, reasons, err := l.exceedsLimits(ctx, tenant, streams)
	if !exceedsLimits || err != nil {
		return streams, nil, err
	}
	// We can do this without allocation if needed, but doing so will modify
	// the original backing array. See "Filtering without allocation" from
	// https://go.dev/wiki/SliceTricks.
	withinLimits := make([]KeyedStream, 0, len(streams))
	for _, s := range streams {
		if _, ok := reasons[s.HashKeyNoShard]; !ok {
			withinLimits = append(withinLimits, s)
		}
	}
	return withinLimits, reasons, nil
}

// ExceedsLimits returns true if one or more streams exceeds per-tenant limits,
// and false if no streams exceed per-tenant limits. In the case where one or
// more streams exceeds per-tenant limits, it returns the reasons for each stream.
// An error is returned if per-tenant limits could not be checked.
func (l *ingestLimits) exceedsLimits(ctx context.Context, tenant string, streams []KeyedStream) (bool, map[uint64][]string, error) {
	req, err := newExceedsLimitsRequest(tenant, streams)
	if err != nil {
		return false, nil, err
	}
	resp, err := l.client.exceedsLimits(ctx, req)
	if err != nil {
		return false, nil, err
	}
	if len(resp.Results) == 0 {
		return false, nil, nil
	}
	reasonsForHashes := make(map[uint64][]string)
	for _, result := range resp.Results {
		reasons := reasonsForHashes[result.StreamHash]
		humanized := limits.Reason(result.Reason).String()
		reasons = append(reasons, humanized)
		reasonsForHashes[result.StreamHash] = reasons
	}
	return true, reasonsForHashes, nil
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
			StreamHash:             stream.HashKeyNoShard,
			EntriesSize:            entriesSize,
			StructuredMetadataSize: structuredMetadataSize,
		})
	}
	return &proto.ExceedsLimitsRequest{
		Tenant:  tenant,
		Streams: streamMetadata,
	}, nil
}

func firstReasonForHashes(reasonsForHashes map[uint64][]string) string {
	for _, reasons := range reasonsForHashes {
		return reasons[0]
	}
	return "unknown reason"
}
