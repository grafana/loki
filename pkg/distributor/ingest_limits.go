package distributor

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"

	"github.com/grafana/dskit/ring"
	ring_client "github.com/grafana/dskit/ring/client"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/limits"
	limits_frontend_client "github.com/grafana/loki/v3/pkg/limits/frontend/client"
	"github.com/grafana/loki/v3/pkg/limits/proto"
)

// The ingestLimitsFrontendClient interface is used to mock calls in tests.
type ingestLimitsFrontendClient interface {
	ExceedsLimits(context.Context, *proto.ExceedsLimitsRequest) (*proto.ExceedsLimitsResponse, error)
	UpdateRates(context.Context, *proto.UpdateRatesRequest) (*proto.UpdateRatesResponse, error)
}

// ingestLimitsFrontendRingClient uses the ring to discover ingest-limits-frontend
// instances and proxy requests to them.
type ingestLimitsFrontendRingClient struct {
	ring                ring.ReadRing
	pool                *ring_client.Pool
	shuffleShardEnabled bool
	shuffleShardSize    int
}

func newIngestLimitsFrontendRingClient(ring ring.ReadRing, pool *ring_client.Pool, shuffleShardEnabled bool, shuffleShardSize int) *ingestLimitsFrontendRingClient {
	return &ingestLimitsFrontendRingClient{
		ring:                ring,
		pool:                pool,
		shuffleShardEnabled: shuffleShardEnabled,
		shuffleShardSize:    shuffleShardSize,
	}
}

// Implements the [ingestLimitsFrontendClient] interface.
func (c *ingestLimitsFrontendRingClient) ExceedsLimits(ctx context.Context, req *proto.ExceedsLimitsRequest) (*proto.ExceedsLimitsResponse, error) {
	var (
		err  error
		resp *proto.ExceedsLimitsResponse
		// doExceedsLimitsFn is used as a closure to call [ExceedsLimits] and then
		// update the [resp] variable which we can then return to the caller.
		doExceedsLimitsFn = func(ctx context.Context, client proto.IngestLimitsFrontendClient) error {
			var clientErr error
			resp, clientErr = client.ExceedsLimits(ctx, req)
			return clientErr
		}
	)
	if c.shuffleShardEnabled {
		err = c.withTenantShuffleShard(ctx, req.Tenant, doExceedsLimitsFn)
	} else {
		err = c.withRandomShuffle(ctx, doExceedsLimitsFn)
	}
	return resp, err
}

// Implements the [ingestLimitsFrontendClient] interface.
func (c *ingestLimitsFrontendRingClient) UpdateRates(ctx context.Context, req *proto.UpdateRatesRequest) (*proto.UpdateRatesResponse, error) {
	var resp *proto.UpdateRatesResponse
	err := c.withRandomShuffle(ctx, func(ctx context.Context, client proto.IngestLimitsFrontendClient) error {
		var clientErr error
		resp, clientErr = client.UpdateRates(ctx, req)
		return clientErr
	})
	return resp, err
}

// withTenantShuffleShard shuffle shards the tenant over [shuffleShardSize] frontends.
func (c *ingestLimitsFrontendRingClient) withTenantShuffleShard(ctx context.Context, tenant string, f func(ctx context.Context, client proto.IngestLimitsFrontendClient) error) error {
	subring := c.ring.ShuffleShard(tenant, c.shuffleShardSize)
	rs, err := subring.GetAllHealthy(limits_frontend_client.LimitsRead)
	if err != nil {
		return fmt.Errorf("failed to get limits-frontend instances from ring: %w", err)
	}
	if len(rs.Instances) == 0 {
		return errors.New("no healthy instances found")
	}
	// Randomly shuffle instances to evenly distribute requests amongst the shards.
	rand.Shuffle(len(rs.Instances), func(i, j int) {
		rs.Instances[i], rs.Instances[j] = rs.Instances[j], rs.Instances[i]
	})
	return c.walkInstances(ctx, rs.Instances, f)
}

// withRandomShuffle gets all healthy frontends in the ring, randomly shuffles
// them, and then calls f.
func (c *ingestLimitsFrontendRingClient) withRandomShuffle(ctx context.Context, f func(ctx context.Context, client proto.IngestLimitsFrontendClient) error) error {
	rs, err := c.ring.GetAllHealthy(limits_frontend_client.LimitsRead)
	if err != nil {
		return fmt.Errorf("failed to get limits-frontend instances from ring: %w", err)
	}
	if len(rs.Instances) == 0 {
		return errors.New("no healthy instances found")
	}
	// Randomly shuffle instances to evenly distribute requests.
	rand.Shuffle(len(rs.Instances), func(i, j int) {
		rs.Instances[i], rs.Instances[j] = rs.Instances[j], rs.Instances[i]
	})
	return c.walkInstances(ctx, rs.Instances, f)
}

func (c *ingestLimitsFrontendRingClient) walkInstances(ctx context.Context, instances []ring.InstanceDesc, f func(ctx context.Context, client proto.IngestLimitsFrontendClient) error) error {
	var lastErr error
	// Pass the instance to f. If it fails, failover to the next instance.
	// Repeat until there are no more instances.
	for _, instance := range instances {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		c, err := c.pool.GetClientFor(instance.Addr)
		if err != nil {
			lastErr = err
			continue
		}
		client := c.(proto.IngestLimitsFrontendClient)
		if err = f(ctx, client); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	return lastErr
}

type ingestLimits struct {
	client         ingestLimitsFrontendClient
	requests       *prometheus.CounterVec
	requestsFailed *prometheus.CounterVec
}

func newIngestLimits(client ingestLimitsFrontendClient, r prometheus.Registerer) *ingestLimits {
	return &ingestLimits{
		client: client,
		requests: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_distributor_ingest_limits_requests_total",
			Help: "The total number of requests.",
		}, []string{"operation"}),
		requestsFailed: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_distributor_ingest_limits_requests_failed_total",
			Help: "The total number of requests that failed.",
		}, []string{"operation"}),
	}
}

// EnforceLimits checks all streams against the per-tenant limits and returns
// two slices: one containing the streams that are accepted (within the per-tenant
// limits) and one containing the streams that are rejected. Any streams that
// could not have their limits checked are also accepted.
func (l *ingestLimits) EnforceLimits(ctx context.Context, tenant string, streams []KeyedStream) ([]KeyedStream, []KeyedStream, error) {
	results, err := l.ExceedsLimits(ctx, tenant, streams)
	if err != nil {
		return streams, []KeyedStream{}, err
	}
	// Fast path. No results means all streams were accepted and there were
	// no failures, so we can return the input streams.
	if len(results) == 0 {
		return streams, []KeyedStream{}, nil
	}
	// We can do this without allocation if needed, but doing so will modify
	// the original backing array. See "Filtering without allocation" from
	// https://go.dev/wiki/SliceTricks.
	accepted := make([]KeyedStream, 0, len(streams))
	rejected := make([]KeyedStream, 0, len(streams))
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
		} else {
			rejected = append(rejected, s)
		}
	}
	return accepted, rejected, nil
}

// ExceedsLimits checks all streams against the per-tenant limits. It returns
// an error if the client failed to send the request or receive a response
// from the server. Any streams that could not have their limits checked
// and returned in the results with the reason "ReasonFailed".
func (l *ingestLimits) ExceedsLimits(ctx context.Context, tenant string, streams []KeyedStream) ([]*proto.ExceedsLimitsResult, error) {
	l.requests.WithLabelValues("ExceedsLimits").Inc()
	req, err := newExceedsLimitsRequest(tenant, streams)
	if err != nil {
		l.requestsFailed.WithLabelValues("ExceedsLimits").Inc()
		return nil, err
	}
	resp, err := l.client.ExceedsLimits(ctx, req)
	if err != nil {
		l.requestsFailed.WithLabelValues("ExceedsLimits").Inc()
		return nil, err
	}
	return resp.Results, nil
}

// newExceedsLimitsRequest builds the admission request. Its TotalSize is the tenant-facing
// UNEXPANDED size - calculateStreamSizes counts the shared structured metadata pool once per stream
// - and stays that way: the limits-frontend feeds it to
// loki_ingest_limits_tenant_ingested_bytes_total (see limitsChecker.ExceedsLimits) and nothing else,
// and the admission decision it is attached to counts streams, not bytes (usageStore.UpdateCond).
// Contrast newUpdateRatesRequest below, whose size is expanded-equivalent on purpose.
func newExceedsLimitsRequest(tenant string, streams []KeyedStream) (*proto.ExceedsLimitsRequest, error) {
	// The distributor sends the hashes of all streams in the request to the
	// limits-frontend. The limits-frontend is responsible for deciding if
	// the request would exceed the tenants limits, and if so, which streams
	// from the request caused it to exceed its limits.
	streamMetadata := make([]*proto.StreamMetadata, 0, len(streams))
	for _, stream := range streams {
		entriesSize, structuredMetadataSize := calculateStreamSizes(stream.Stream)
		streamMetadata = append(streamMetadata, &proto.StreamMetadata{
			StreamHash:      stream.HashKeyNoShard,
			TotalSize:       entriesSize + structuredMetadataSize,
			IngestionPolicy: stream.Policy,
		})
	}
	return &proto.ExceedsLimitsRequest{
		Tenant:  tenant,
		Streams: streamMetadata,
	}, nil
}

// UpdateRates updates the rates for the streams and returns a slice of the
// updated rates for all streams. Any streams that could not have rates updated
// have a rate of zero.
func (l *ingestLimits) UpdateRates(ctx context.Context, tenant string, streams []segmentedStream) ([]*proto.UpdateRatesResult, error) {
	req, err := newUpdateRatesRequest(tenant, streams)
	if err != nil {
		// We update `UpdateRates` here because we have clients directly calling `UpdateRatesRaw`.
		l.requests.WithLabelValues("UpdateRates").Inc()
		l.requestsFailed.WithLabelValues("UpdateRates").Inc()
		return nil, err
	}
	return l.UpdateRatesRaw(ctx, req)
}

// UpdateRatesRaw sends a pre-built UpdateRatesRequest to the frontend.
// This is used by the rate batcher which accumulates stream data over time.
func (l *ingestLimits) UpdateRatesRaw(ctx context.Context, req *proto.UpdateRatesRequest) ([]*proto.UpdateRatesResult, error) {
	l.requests.WithLabelValues("UpdateRates").Inc()
	resp, err := l.client.UpdateRates(ctx, req)
	if err != nil {
		l.requestsFailed.WithLabelValues("UpdateRates").Inc()
		return nil, err
	}
	return resp.Results, nil
}

// newUpdateRatesRequest builds the UpdateRates request DataObjTee sends when rate batching is
// disabled. It is the unbatched twin of rateBatcher.Add and reports sizes in the same unit.
//
// TotalSize here is EXPANDED-EQUIVALENT, unlike the TotalSize of newExceedsLimitsRequest above.
// The two RPCs are different questions asked of the same service and their sizes are consumed by
// different code: this one only ever lands in the per stream rate buckets
// (usageStore.updateWithBuckets), whose average comes back as the rateBytes DataObjTee gives the
// partition resolver to size a segmentation key's shuffle shard - internal load distribution, where
// the consumer-side work is still the expanded one. The ExceedsLimits size stays unexpanded because
// it is tenant-facing: it is what loki_ingest_limits_tenant_ingested_bytes_total measures, and
// admission itself is stream-count based (see usageStore.UpdateCond) so it is unaffected either way.
//
// TODO(otlp-deferred-expansion): drop the delta once the chunkenc attribute-aware append follow-up
// lands, see sharedStructuredMetadataExpansionDelta.
func newUpdateRatesRequest(tenant string, streams []segmentedStream) (*proto.UpdateRatesRequest, error) {
	// The distributor sends the hashes of all streams in the request to the
	// limits-frontend. The limits-frontend is responsible for deciding if
	// the request would exceed the tenants limits, and if so, which streams
	// from the request caused it to exceed its limits.
	streamMetadata := make([]*proto.StreamMetadata, 0, len(streams))
	for _, stream := range streams {
		entriesSize, structuredMetadataSize := calculateStreamSizes(stream.Stream)
		streamMetadata = append(streamMetadata, &proto.StreamMetadata{
			StreamHash:      stream.SegmentationKeyHash,
			TotalSize:       entriesSize + structuredMetadataSize + sharedStructuredMetadataExpansionDelta(stream.Stream),
			IngestionPolicy: stream.Policy,
		})
	}
	return &proto.UpdateRatesRequest{
		Tenant:  tenant,
		Streams: streamMetadata,
	}, nil
}
