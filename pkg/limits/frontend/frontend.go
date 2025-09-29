// Package frontend contains provides a frontend service for ingest limits.
// It is responsible for receiving and answering gRPC requests from distributors,
// such as exceeds limits requests, forwarding them to individual limits backends,
// gathering and aggregating their responses (where required), and returning
// the final result.
package frontend

import (
	"context"
	"fmt"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/limits"
	limits_client "github.com/grafana/loki/v3/pkg/limits/client"
	"github.com/grafana/loki/v3/pkg/limits/proto"
)

// Frontend is a frontend for the limits service. It is responsible for
// receiving RPCs from clients, forwarding them to the correct limits
// instances, and returning their responses.
type Frontend struct {
	services.Service
	cfg                     Config
	logger                  log.Logger
	limitsClient            limitsClient
	assignedPartitionsCache cache[string, *proto.GetAssignedPartitionsResponse]
	subservices             *services.Manager
	subservicesWatcher      *services.FailureWatcher
	lifecycler              *ring.Lifecycler
	lifecyclerWatcher       *services.FailureWatcher

	// Metrics.
	streams         prometheus.Counter
	streamsFailed   prometheus.Counter
	streamsRejected prometheus.Counter
}

// New returns a new Frontend.
func New(cfg Config, ringName string, limitsRing ring.ReadRing, logger log.Logger, reg prometheus.Registerer) (*Frontend, error) {
	f := &Frontend{
		cfg:    cfg,
		logger: logger,
		streams: promauto.With(reg).NewCounter(
			prometheus.CounterOpts{
				Name: "loki_ingest_limits_frontend_streams_total",
				Help: "The total number of received streams.",
			},
		),
		streamsFailed: promauto.With(reg).NewCounter(
			prometheus.CounterOpts{
				Name: "loki_ingest_limits_frontend_streams_failed_total",
				Help: "The total number of received streams that could not be checked.",
			},
		),
		streamsRejected: promauto.With(reg).NewCounter(
			prometheus.CounterOpts{
				Name: "loki_ingest_limits_frontend_streams_rejected_total",
				Help: "The total number of rejected streams.",
			},
		),
	}
	// Set up a client pool for the limits service. The frontend will use this
	// to make RPCs that get the current stream usage to checks per-tenant limits.
	clientPoolFactory := limits_client.NewPoolFactory(cfg.ClientConfig)
	clientPool := limits_client.NewPool(
		ringName,
		cfg.ClientConfig.PoolConfig,
		limitsRing,
		clientPoolFactory,
		logger,
	)
	// Set up the assigned partitions cache.
	var assignedPartitionsCache cache[string, *proto.GetAssignedPartitionsResponse]
	if cfg.AssignedPartitionsCacheTTL == 0 {
		// When the TTL is 0, the cache is disabled.
		assignedPartitionsCache = newNopCache[string, *proto.GetAssignedPartitionsResponse]()
	} else {
		assignedPartitionsCache = newTTLCache[string, *proto.GetAssignedPartitionsResponse](cfg.AssignedPartitionsCacheTTL)
	}
	f.assignedPartitionsCache = assignedPartitionsCache
	f.limitsClient = newRingLimitsClient(limitsRing, clientPool, cfg.NumPartitions, assignedPartitionsCache, logger, reg)
	lifecycler, err := ring.NewLifecycler(cfg.LifecyclerConfig, f, RingName, RingKey, true, logger, reg)
	if err != nil {
		return nil, fmt.Errorf("failed to create %s lifecycler: %w", RingName, err)
	}
	f.lifecycler = lifecycler
	// Watch the lifecycler.
	f.lifecyclerWatcher = services.NewFailureWatcher()
	f.lifecyclerWatcher.WatchService(f.lifecycler)
	servs := []services.Service{lifecycler, clientPool}
	mgr, err := services.NewManager(servs...)
	if err != nil {
		return nil, err
	}
	f.subservices = mgr
	f.subservicesWatcher = services.NewFailureWatcher()
	f.subservicesWatcher.WatchManager(f.subservices)
	f.Service = services.NewBasicService(f.starting, f.running, f.stopping)
	return f, nil
}

// ExceedsLimits implements proto.IngestLimitsFrontendClient.
func (f *Frontend) ExceedsLimits(ctx context.Context, req *proto.ExceedsLimitsRequest) (*proto.ExceedsLimitsResponse, error) {
	f.streams.Add(float64(len(req.Streams)))
	results := make([]*proto.ExceedsLimitsResult, 0, len(req.Streams))
	resps, err := f.limitsClient.ExceedsLimits(ctx, req)
	if err != nil {
		// If the entire call failed, then all streams failed.
		for _, stream := range req.Streams {
			results = append(results, &proto.ExceedsLimitsResult{
				StreamHash: stream.StreamHash,
				Reason:     uint32(limits.ReasonFailed),
			})
		}
		f.streamsFailed.Add(float64(len(req.Streams)))
		level.Error(f.logger).Log("msg", "failed to check request against limits", "err", err)
	} else {
		for _, resp := range resps {
			for _, res := range resp.Results {
				// Even if the call succeeded, some (or all) streams might
				// still have failed.
				if res.Reason == uint32(limits.ReasonFailed) {
					f.streamsFailed.Inc()
				} else {
					f.streamsRejected.Inc()
				}
			}
			results = append(results, resp.Results...)
		}
	}
	return &proto.ExceedsLimitsResponse{Results: results}, nil
}

func (f *Frontend) CheckReady(ctx context.Context) error {
	if f.State() != services.Running {
		return fmt.Errorf("service is not running: %v", f.State())
	}
	err := f.lifecycler.CheckReady(ctx)
	if err != nil {
		return fmt.Errorf("lifecycler not ready: %w", err)
	}
	return nil
}

// starting implements services.Service.
func (f *Frontend) starting(ctx context.Context) (err error) {
	defer func() {
		if err == nil {
			return
		}
		stopErr := services.StopManagerAndAwaitStopped(context.Background(), f.subservices)
		if stopErr != nil {
			level.Error(f.logger).Log("msg", "failed to stop subservices", "err", stopErr)
		}
	}()
	level.Info(f.logger).Log("msg", "starting subservices")
	if err := services.StartManagerAndAwaitHealthy(ctx, f.subservices); err != nil {
		return fmt.Errorf("failed to start subservices: %w", err)
	}
	return nil
}

// running implements services.Service.
func (f *Frontend) running(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return nil
	case err := <-f.subservicesWatcher.Chan():
		return fmt.Errorf("ingest limits frontend subservice failed: %w", err)
	}
}

// stopping implements services.Service.
func (f *Frontend) stopping(_ error) error {
	return services.StopManagerAndAwaitStopped(context.Background(), f.subservices)
}

// Flush implements ring.FlushTransferer. It transfers state to another ingest
// limits frontend instance.
func (f *Frontend) Flush() {}

// TransferOut implements ring.FlushTransferer. It transfers state to another
// ingest limits frontend instance.
func (f *Frontend) TransferOut(_ context.Context) error {
	return nil
}
