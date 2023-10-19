/*
Bloom Gateway package

The bloom gateway is a component that can be run as a standalone microserivce
target and provides capabilities for filtering ChunkRefs based on a given list
of line filter expressions.

			     Querier   Query Frontend
			        |           |
			................................... service boundary
			        |           |
			        +----+------+
			             |
			     indexgateway.Gateway
			             |
			   bloomgateway.BloomQuerier
			             |
			   bloomgateway.GatewayClient
			             |
			  logproto.BloomGatewayClient
			             |
			................................... service boundary
			             |
			      bloomgateway.Gateway
			             |
			       bloomshipper.Store
			             |
			      bloomshipper.Shipper
			             |
	     bloomshipper.BloomFileClient
			             |
			        ObjectClient
			             |
			................................... service boundary
			             |
		         object storage
*/
package bloomgateway

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/tenant"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/queue"
	"github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/shipper/bloomshipper"
)

var errGatewayUnhealthy = errors.New("bloom-gateway is unhealthy in the ring")
var errInvalidTenant = errors.New("invalid tenant in chunk refs")

type metrics struct {
	queueDuration    prometheus.Histogram
	inflightRequests prometheus.Summary
}

func newMetrics(subsystem string, registerer prometheus.Registerer) *metrics {
	return &metrics{
		queueDuration: promauto.With(registerer).NewHistogram(prometheus.HistogramOpts{
			Namespace: "loki",
			Subsystem: subsystem,
			Name:      "queue_duration_seconds",
			Help:      "Time spent by tasks in queue before getting picked up by a worker.",
			Buckets:   prometheus.DefBuckets,
		}),
		inflightRequests: promauto.With(registerer).NewSummary(prometheus.SummaryOpts{
			Namespace:  "loki",
			Subsystem:  subsystem,
			Name:       "inflight_tasks",
			Help:       "Number of inflight tasks (either queued or processing) sampled at a regular interval. Quantile buckets keep track of inflight tasks over the last 60s.",
			Objectives: map[float64]float64{0.5: 0.05, 0.75: 0.02, 0.8: 0.02, 0.9: 0.01, 0.95: 0.01, 0.99: 0.001},
			MaxAge:     time.Minute,
			AgeBuckets: 6,
		}),
	}
}

type Gateway struct {
	services.Service

	cfg     Config
	logger  log.Logger
	metrics *metrics

	queue      *queue.RequestQueue
	bloomStore bloomshipper.Store

	sharding ShardingStrategy

	pendingRequestsMu sync.Mutex
	pendingRequests   map[string]queue.Request

	serviceMngr    *services.Manager
	serviceWatcher *services.FailureWatcher
}

// New returns a new instance of the Bloom Gateway.
func New(cfg Config, schemaCfg config.SchemaConfig, storageCfg storage.Config, shardingStrategy ShardingStrategy, cm storage.ClientMetrics, logger log.Logger, reg prometheus.Registerer) (*Gateway, error) {
	g := &Gateway{
		cfg:      cfg,
		logger:   logger,
		metrics:  newMetrics("bloom_gateway", reg),
		sharding: shardingStrategy,
		queue:    queue.NewRequestQueue(1024, time.Minute, queue.NewMetrics("bloom_gateway", reg)),
	}

	client, err := bloomshipper.NewBloomClient(schemaCfg.Configs, storageCfg, cm)
	if err != nil {
		return nil, err
	}

	bloomShipper, err := bloomshipper.NewShipper(client, storageCfg.BloomShipperConfig, logger)
	if err != nil {
		return nil, err
	}

	bloomStore, err := bloomshipper.NewBloomStore(bloomShipper)
	if err != nil {
		return nil, err
	}

	g.bloomStore = bloomStore

	svcs := []services.Service{g.queue}
	g.serviceMngr, err = services.NewManager(svcs...)
	if err != nil {
		return nil, err
	}
	g.serviceWatcher = services.NewFailureWatcher()
	g.serviceWatcher.WatchManager(g.serviceMngr)

	g.Service = services.NewBasicService(g.starting, g.running, g.stopping).WithName("bloom-gateway")

	return g, nil
}

func (g *Gateway) starting(ctx context.Context) error {
	var err error
	defer func() {
		if err == nil || g.serviceMngr == nil {
			return
		}
		if err := services.StopManagerAndAwaitStopped(context.Background(), g.serviceMngr); err != nil {
			level.Error(g.logger).Log("msg", "failed to gracefully stop bloom gateway dependencies", "err", err)
		}
	}()

	if err := services.StartManagerAndAwaitHealthy(ctx, g.serviceMngr); err != nil {
		return errors.Wrap(err, "unable to start bloom gateway subservices")
	}

	return nil
}

func (g *Gateway) running(ctx context.Context) error {
	// We observe inflight tasks frequently and at regular intervals, to have a good
	// approximation of max inflight tasks over percentiles of time. We also do it with
	// a ticker so that we keep tracking it even if we have no new requests but stuck inflight
	// tasks (eg. worker are all exhausted).
	inflightTasksTicker := time.NewTicker(250 * time.Millisecond)
	defer inflightTasksTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-g.serviceWatcher.Chan():
			return errors.Wrap(err, "bloom gateway subservice failed")
		case <-inflightTasksTicker.C:
			g.pendingRequestsMu.Lock()
			inflight := len(g.pendingRequests)
			g.pendingRequestsMu.Unlock()
			g.metrics.inflightRequests.Observe(float64(inflight))
		}
	}
}

func (g *Gateway) stopping(_ error) error {
	g.bloomStore.Stop()
	return services.StopManagerAndAwaitStopped(context.Background(), g.serviceMngr)
}

// FilterChunkRefs implements BloomGatewayServer
func (g *Gateway) FilterChunkRefs(ctx context.Context, req *logproto.FilterChunkRefRequest) (*logproto.FilterChunkRefResponse, error) {
	tenantID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}

	for _, ref := range req.Refs {
		if ref.Tenant != tenantID {
			return nil, errors.Wrapf(errInvalidTenant, "expected chunk refs from tenant %s, got tenant %s", tenantID, ref.Tenant)
		}
	}

	// Sort ChunkRefs by fingerprint in ascending order
	sort.Slice(req.Refs, func(i, j int) bool {
		return req.Refs[i].Fingerprint < req.Refs[j].Fingerprint
	})

	chunkRefs := req.Refs

	// Only query bloom filters if filters are present
	if len(req.Filters) > 0 {
		chunkRefs, err = g.bloomStore.FilterChunkRefs(ctx, tenantID, req.From.Time(), req.Through.Time(), req.Refs, req.Filters...)
		if err != nil {
			return nil, err
		}
	}

	return &logproto.FilterChunkRefResponse{ChunkRefs: chunkRefs}, nil
}
