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
	ring ring.ReadRing
	pool *ring_client.Pool
}

func newIngestLimitsFrontendRingClient(ring ring.ReadRing, pool *ring_client.Pool) *ingestLimitsFrontendRingClient {
	return &ingestLimitsFrontendRingClient{
		ring: ring,
		pool: pool,
	}
}

// Implements the [ingestLimitsFrontendClient] interface.
func (c *ingestLimitsFrontendRingClient) ExceedsLimits(ctx context.Context, req *proto.ExceedsLimitsRequest) (*proto.ExceedsLimitsResponse, error) {
	var resp *proto.ExceedsLimitsResponse
	err := c.withRandomShuffle(ctx, func(ctx context.Context, client proto.IngestLimitsFrontendClient) error {
		var clientErr error
		resp, clientErr = client.ExceedsLimits(ctx, req)
		return clientErr
	})
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
	var lastErr error
	// Pass the instance to f. If it fails, failover to the next instance.
	// Repeat until there are no more instances.
	for _, instance := range rs.Instances {
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
func (l *ingestLimits) UpdateRates(ctx context.Context, tenant string, streams []SegmentedStream) ([]*proto.UpdateRatesResult, error) {
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

func newUpdateRatesRequest(tenant string, streams []SegmentedStream) (*proto.UpdateRatesRequest, error) {
	// The distributor sends the hashes of all streams in the request to the
	// limits-frontend. The limits-frontend is responsible for deciding if
	// the request would exceed the tenants limits, and if so, which streams
	// from the request caused it to exceed its limits.
	streamMetadata := make([]*proto.StreamMetadata, 0, len(streams))
	for _, stream := range streams {
		entriesSize, structuredMetadataSize := calculateStreamSizes(stream.Stream)
		streamMetadata = append(streamMetadata, &proto.StreamMetadata{
			StreamHash:      stream.SegmentationKeyHash,
			TotalSize:       entriesSize + structuredMetadataSize,
			IngestionPolicy: stream.Policy,
		})
	}
	return &proto.UpdateRatesRequest{
		Tenant:  tenant,
		Streams: streamMetadata,
	}, nil
}
