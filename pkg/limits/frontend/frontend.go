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

	// checkLimitsAndShardStreams, checkLimitsAndShardShards,
	// checkLimitsAndShardFailed, and checkLimitsAndShardRejected are all
	// dedicated to CheckLimitsAndShard rather than reusing
	// streams/streamsFailed/streamsRejected above: in shadow mode, a push's
	// streams are also sent through ExceedsLimits, so folding both
	// endpoints into the same counters would double-count.
	checkLimitsAndShardStreams  *prometheus.CounterVec
	checkLimitsAndShardShards   *prometheus.CounterVec
	checkLimitsAndShardFailed   *prometheus.CounterVec
	checkLimitsAndShardRejected *prometheus.CounterVec
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
		checkLimitsAndShardStreams: promauto.With(reg).NewCounterVec(
			prometheus.CounterOpts{
				Name: "loki_ingest_limits_frontend_check_limits_and_shard_streams_total",
				Help: "The total number of logical (pre-shard) streams received via CheckLimitsAndShard.",
			}, []string{"tenant"},
		),
		checkLimitsAndShardShards: promauto.With(reg).NewCounterVec(
			prometheus.CounterOpts{
				Name: "loki_ingest_limits_frontend_check_limits_and_shard_shards_total",
				Help: "The total number of shards granted via CheckLimitsAndShard, i.e. the total physical stream count (unsharded and sharded) implied by these decisions.",
			}, []string{"tenant"},
		),
		checkLimitsAndShardFailed: promauto.With(reg).NewCounterVec(
			prometheus.CounterOpts{
				Name: "loki_ingest_limits_frontend_check_limits_and_shard_failed_total",
				Help: "The total number of streams received via CheckLimitsAndShard that could not be checked.",
			}, []string{"tenant"},
		),
		checkLimitsAndShardRejected: promauto.With(reg).NewCounterVec(
			prometheus.CounterOpts{
				Name: "loki_ingest_limits_frontend_check_limits_and_shard_rejected_total",
				Help: "The total number of streams rejected via CheckLimitsAndShard.",
			}, []string{"tenant"},
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
	if cfg.AssignedPartitionsCacheEnabled {
		f.assignedPartitionsCache = newTTLCache[string, *proto.GetAssignedPartitionsResponse](cfg.AssignedPartitionsCacheTTL)
	} else {
		f.assignedPartitionsCache = newNopCache[string, *proto.GetAssignedPartitionsResponse]()
	}
	// Set up the limits client.
	f.limitsClient = newRingLimitsClient(limitsRing, clientPool, cfg.NumPartitions, f.assignedPartitionsCache, logger, reg)
	if cfg.AcceptedStreamsCacheEnabled {
		f.limitsClient = newCacheLimitsClient(
			newAcceptedStreamsCache(
				cfg.AcceptedStreamsCacheTTL,
				cfg.AcceptedStreamsCacheTTLJitter,
				reg,
			),
			f.limitsClient,
		)
	}
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
	resp, err := f.limitsClient.ExceedsLimits(ctx, req)
	if err != nil {
		// If the entire call failed, then all streams failed.
		resp = &proto.ExceedsLimitsResponse{
			Results: make([]*proto.ExceedsLimitsResult, 0, len(req.Streams)),
		}
		for _, stream := range req.Streams {
			resp.Results = append(resp.Results, &proto.ExceedsLimitsResult{
				StreamHash: stream.StreamHash,
				Reason:     uint32(limits.ReasonFailed),
			})
		}
		f.streamsFailed.Add(float64(len(req.Streams)))
		level.Error(f.logger).Log("msg", "failed to check request against limits", "err", err)
	} else {
		for _, res := range resp.Results {
			// Even if the call succeeded, some (or all) streams might still
			// have failed.
			if res.Reason == uint32(limits.ReasonFailed) {
				f.streamsFailed.Inc()
			} else {
				f.streamsRejected.Inc()
			}
		}
	}
	return resp, nil
}

// CheckLimitsAndShard implements proto.IngestLimitsFrontendClient.
func (f *Frontend) CheckLimitsAndShard(ctx context.Context, req *proto.CheckLimitsAndShardRequest) (*proto.CheckLimitsAndShardResponse, error) {
	f.checkLimitsAndShardStreams.WithLabelValues(req.Tenant).Add(float64(len(req.Streams)))
	resp, err := f.limitsClient.CheckLimitsAndShard(ctx, req)
	if err != nil {
		// If the entire call failed, degrade to "don't shard this push" for
		// every stream rather than rejecting it -- a materially safer
		// failure mode for a throughput optimization than dropping data.
		resp = &proto.CheckLimitsAndShardResponse{
			Results: make([]*proto.StreamShardResult, 0, len(req.Streams)),
		}
		for _, stream := range req.Streams {
			resp.Results = append(resp.Results, &proto.StreamShardResult{
				StreamHash:           stream.StreamHash,
				Shards:               1,
				ShardDecisionContext: uint32(limits.ReasonFailed),
			})
		}
		f.checkLimitsAndShardShards.WithLabelValues(req.Tenant).Add(float64(len(req.Streams)))
		f.checkLimitsAndShardFailed.WithLabelValues(req.Tenant).Add(float64(len(req.Streams)))
		level.Error(f.logger).Log("msg", "failed to check limits and shard", "err", err)
	} else {
		for _, res := range resp.Results {
			f.checkLimitsAndShardShards.WithLabelValues(req.Tenant).Add(float64(res.Shards))
			switch {
			case res.ShardDecisionContext == uint32(limits.ReasonFailed):
				f.checkLimitsAndShardFailed.WithLabelValues(req.Tenant).Inc()
			case res.RejectReason != "":
				f.checkLimitsAndShardRejected.WithLabelValues(req.Tenant).Inc()
			}
		}
	}
	return resp, nil
}

func (f *Frontend) UpdateRates(ctx context.Context, req *proto.UpdateRatesRequest) (*proto.UpdateRatesResponse, error) {
	resp, err := f.limitsClient.UpdateRates(ctx, req)
	if err != nil {
		// If the entire call failed, then all streams failed.
		resp = &proto.UpdateRatesResponse{
			Results: make([]*proto.UpdateRatesResult, 0, len(req.Streams)),
		}
		for _, stream := range req.Streams {
			resp.Results = append(resp.Results, &proto.UpdateRatesResult{
				StreamHash: stream.StreamHash,
				Rate:       0,
			})
		}
	}
	return resp, nil
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
