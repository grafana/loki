package distributor

import (
	"context"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/limits/proto"
)

// RateBatcherConfig contains the configuration for the RateBatcher.
type RateBatcherConfig struct {
	// BatchWindow is the duration to accumulate rate updates before flushing.
	BatchWindow time.Duration
}

// rateBatcherClient is the interface for sending rate updates.
// This allows the batcher to use the ingestLimits wrapper which tracks metrics.
type rateBatcherClient interface {
	UpdateRatesRaw(ctx context.Context, req *proto.UpdateRatesRequest) ([]*proto.UpdateRatesResult, error)
}

// rateBatcher accumulates UpdateRates requests and dispatches them in batches.
// This is a fire-and-forget mechanism - callers add streams to the batch and
// don't wait for results. The batch is periodically flushed to the frontend.
// Results from UpdateRates are stored and can be looked up for partition resolution.
type rateBatcher struct {
	services.Service

	cfg    RateBatcherConfig
	client rateBatcherClient
	logger log.Logger

	// pending accumulates streams to be sent in the next batch.
	// Map: tenant -> segmentationKeyHash -> *proto.StreamMetadata
	pendingMu sync.Mutex
	pending   map[string]map[uint64]*proto.StreamMetadata

	// rates stores the last known rate for each stream, updated on each flush.
	// Map: tenant -> segmentationKeyHash -> rate (bytes/sec)
	ratesMu sync.RWMutex
	rates   map[string]map[uint64]uint64

	// Metrics
	batchesSent     prometheus.Counter
	batchesFailed   prometheus.Counter
	streamsPerBatch prometheus.Histogram
	pendingStreams  prometheus.Gauge
	streamsFlushed  prometheus.Counter
}

// newRateBatcher creates a new rate batcher.
func newRateBatcher(cfg RateBatcherConfig, client rateBatcherClient, logger log.Logger, reg prometheus.Registerer) *rateBatcher {
	b := &rateBatcher{
		cfg:     cfg,
		client:  client,
		logger:  logger,
		pending: make(map[string]map[uint64]*proto.StreamMetadata),
		rates:   make(map[string]map[uint64]uint64),
		batchesSent: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_distributor_rate_batcher_batches_sent_total",
			Help: "Total number of batches sent to the limits frontend.",
		}),
		batchesFailed: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_distributor_rate_batcher_batches_failed_total",
			Help: "Total number of batches that failed to send.",
		}),
		streamsPerBatch: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name:    "loki_distributor_rate_batcher_streams_per_batch",
			Help:    "Number of unique streams per batch.",
			Buckets: prometheus.ExponentialBuckets(10, 2, 10), // 10, 20, 40, ... 5120
		}),
		pendingStreams: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Name: "loki_distributor_rate_batcher_pending_streams",
			Help: "Current number of streams pending in the batch.",
		}),
		streamsFlushed: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_distributor_rate_batcher_streams_flushed_total",
			Help: "Total number of streams flushed to the limits frontend.",
		}),
	}
	b.Service = services.NewBasicService(nil, b.running, nil)
	return b
}

// running is the main loop that periodically flushes batches.
func (b *rateBatcher) running(ctx context.Context) error {
	ticker := time.NewTicker(b.cfg.BatchWindow)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			b.flush(ctx)
		}
	}
}

// Add adds streams to the pending batch and returns the last known rates for them.
// This is a non-blocking operation. Streams with unknown rates will have rate=0.
func (b *rateBatcher) Add(tenant string, streams []SegmentedStream) map[uint64]uint64 {
	if len(streams) == 0 {
		return nil
	}

	// Get known rates first (read lock).
	rates := make(map[uint64]uint64, len(streams))
	b.ratesMu.RLock()
	tenantRates := b.rates[tenant]
	for _, stream := range streams {
		if tenantRates != nil {
			rates[stream.SegmentationKeyHash] = tenantRates[stream.SegmentationKeyHash]
		} else {
			rates[stream.SegmentationKeyHash] = 0
		}
	}
	b.ratesMu.RUnlock()

	// Add to pending batch (separate lock).
	b.pendingMu.Lock()
	tenantPending, ok := b.pending[tenant]
	if !ok {
		tenantPending = make(map[uint64]*proto.StreamMetadata)
		b.pending[tenant] = tenantPending
	}

	for _, stream := range streams {
		hash := stream.SegmentationKeyHash
		totalSize := uint64(stream.Stream.Size())

		// If we already have this stream in the pending batch, accumulate the size.
		if existing, ok := tenantPending[hash]; ok {
			existing.TotalSize += totalSize
		} else {
			tenantPending[hash] = &proto.StreamMetadata{
				StreamHash:      hash,
				TotalSize:       totalSize,
				IngestionPolicy: stream.Policy,
			}
		}
	}

	// Update pending streams gauge.
	var total int
	for _, t := range b.pending {
		total += len(t)
	}
	b.pendingStreams.Set(float64(total))
	b.pendingMu.Unlock()

	return rates
}

// flush sends all pending streams to the frontend.
func (b *rateBatcher) flush(ctx context.Context) {
	// Swap out the pending map so we don't hold the lock during the RPC.
	b.pendingMu.Lock()
	toFlush := b.pending
	b.pending = make(map[string]map[uint64]*proto.StreamMetadata)
	b.pendingMu.Unlock()

	b.pendingStreams.Set(0)

	if len(toFlush) == 0 {
		return
	}

	// Count total streams for metrics.
	var totalStreams int
	for _, streams := range toFlush {
		totalStreams += len(streams)
	}
	b.streamsPerBatch.Observe(float64(totalStreams))

	// Send each tenant's streams to the frontend.
	for tenant, streams := range toFlush {
		if len(streams) == 0 {
			continue
		}

		// Convert map to slice for the request.
		metadata := make([]*proto.StreamMetadata, 0, len(streams))
		for _, m := range streams {
			metadata = append(metadata, m)
		}

		req := &proto.UpdateRatesRequest{
			Tenant:  tenant,
			Streams: metadata,
		}

		// Inject tenant ID into context for the RPC.
		tenantCtx := user.InjectOrgID(ctx, tenant)
		results, err := b.client.UpdateRatesRaw(tenantCtx, req)
		if err != nil {
			level.Error(b.logger).Log(
				"msg", "failed to flush rate batch",
				"tenant", tenant,
				"streams", len(metadata),
				"err", err,
			)
			b.batchesFailed.Inc()
			continue
		}

		b.batchesSent.Inc()
		b.streamsFlushed.Add(float64(len(metadata)))

		// Store the rates for future lookups.
		b.storeRates(tenant, results)
	}
}

// storeRates replaces the rates for a tenant with the results from the latest flush.
// This ensures we don't accumulate stale rates for streams that stopped sending.
func (b *rateBatcher) storeRates(tenant string, results []*proto.UpdateRatesResult) {
	b.ratesMu.Lock()
	defer b.ratesMu.Unlock()

	if len(results) == 0 {
		delete(b.rates, tenant)
		return
	}

	tenantRates := make(map[uint64]uint64, len(results))
	for _, result := range results {
		tenantRates[result.StreamHash] = result.Rate
	}
	b.rates[tenant] = tenantRates
}

// GetRate returns the last known rate for a stream, or 0 if unknown.
func (b *rateBatcher) GetRate(tenant string, segmentationKeyHash uint64) uint64 {
	b.ratesMu.RLock()
	defer b.ratesMu.RUnlock()

	if tenantRates, ok := b.rates[tenant]; ok {
		return tenantRates[segmentationKeyHash]
	}
	return 0
}
