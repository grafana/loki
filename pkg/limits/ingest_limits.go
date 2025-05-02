package limits

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/client"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/constants"
)

const (
	// Ring
	RingKey  = "ingest-limits"
	RingName = "ingest-limits"

	// Kafka
	consumerGroup = "ingest-limits"
)

var (
	tenantPartitionDesc = prometheus.NewDesc(
		constants.Loki+"_ingest_limits_partitions",
		"The current number of partitions per tenant.",
		[]string{"tenant"},
		nil,
	)

	tenantRecordedStreamsDesc = prometheus.NewDesc(
		constants.Loki+"_ingest_limits_recorded_streams",
		"The current number of recorded streams per tenant. This is not a global total, as tenants can be sharded over multiple pods.",
		[]string{"tenant"},
		nil,
	)

	tenantActiveStreamsDesc = prometheus.NewDesc(
		constants.Loki+"_ingest_limits_active_streams",
		"The current number of active streams (seen within the window) per tenant. This is not a global total, as tenants can be sharded over multiple pods.",
		[]string{"tenant"},
		nil,
	)
)

type metrics struct {
	tenantStreamEvictionsTotal *prometheus.CounterVec

	kafkaConsumptionLag prometheus.Histogram
	kafkaReadBytesTotal prometheus.Counter

	tenantIngestedBytesTotal *prometheus.CounterVec
}

func newMetrics(reg prometheus.Registerer) *metrics {
	return &metrics{
		tenantStreamEvictionsTotal: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "ingest_limits_stream_evictions_total",
			Help:      "The total number of streams evicted due to age per tenant. This is not a global total, as tenants can be sharded over multiple pods.",
		}, []string{"tenant"}),
		kafkaConsumptionLag: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_ingest_limits_kafka_consumption_lag_seconds",
			Help:                            "The estimated consumption lag in seconds, measured as the difference between the current time and the timestamp of the record.",
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMinResetDuration: 1 * time.Hour,
			NativeHistogramMaxBucketNumber:  100,
			Buckets:                         prometheus.ExponentialBuckets(0.125, 2, 18),
		}),
		kafkaReadBytesTotal: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "ingest_limits_kafka_read_bytes_total",
			Help:      "Total number of bytes read from Kafka.",
		}),
		tenantIngestedBytesTotal: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "ingest_limits_tenant_ingested_bytes_total",
			Help:      "Total number of bytes ingested per tenant within the active window. This is not a global total, as tenants can be sharded over multiple pods.",
		}, []string{"tenant"}),
	}
}

// IngestLimits is a service that manages stream metadata limits.
type IngestLimits struct {
	services.Service

	cfg    Config
	logger log.Logger
	client *kgo.Client

	lifecycler        *ring.Lifecycler
	lifecyclerWatcher *services.FailureWatcher

	// metrics
	metrics *metrics

	// Track stream metadata
	metadata StreamMetadata

	// Track partition assignments
	partitionManager *PartitionManager
}

// Flush implements ring.FlushTransferer. It transfers state to another ingest limits instance.
func (s *IngestLimits) Flush() {}

// TransferOut implements ring.FlushTransferer. It transfers state to another ingest limits instance.
func (s *IngestLimits) TransferOut(_ context.Context) error {
	return nil
}

// NewIngestLimits creates a new IngestLimits service. It initializes the metadata map and sets up a Kafka client
// The client is configured to consume stream metadata from a dedicated topic with the metadata suffix.
func NewIngestLimits(cfg Config, logger log.Logger, reg prometheus.Registerer) (*IngestLimits, error) {
	var err error
	s := &IngestLimits{
		cfg:              cfg,
		logger:           logger,
		metadata:         NewStreamMetadata(cfg.NumPartitions),
		metrics:          newMetrics(reg),
		partitionManager: NewPartitionManager(logger),
	}

	// Initialize internal metadata metrics
	if err := reg.Register(s); err != nil {
		return nil, fmt.Errorf("failed to register ingest limits internal metadata metrics: %w", err)
	}

	// Initialize lifecycler
	s.lifecycler, err = ring.NewLifecycler(cfg.LifecyclerConfig, s, RingName, RingKey, true, logger, reg)
	if err != nil {
		return nil, fmt.Errorf("failed to create %s lifecycler: %w", RingName, err)
	}

	// Watch the lifecycler
	s.lifecyclerWatcher = services.NewFailureWatcher()
	s.lifecyclerWatcher.WatchService(s.lifecycler)

	// Create a copy of the config to modify the topic
	kCfg := cfg.KafkaConfig
	kCfg.Topic = kafka.MetadataTopicFor(kCfg.Topic)
	kCfg.AutoCreateTopicEnabled = true
	kCfg.AutoCreateTopicDefaultPartitions = cfg.NumPartitions

	s.client, err = client.NewReaderClient("ingest-limits", kCfg, logger, reg,
		kgo.ConsumerGroup(consumerGroup),
		kgo.ConsumeTopics(kCfg.Topic),
		kgo.Balancers(kgo.CooperativeStickyBalancer()),
		kgo.ConsumeResetOffset(kgo.NewOffset().AfterMilli(time.Now().Add(-s.cfg.WindowSize).UnixMilli())),
		kgo.DisableAutoCommit(),
		kgo.OnPartitionsAssigned(s.onPartitionsAssigned),
		kgo.OnPartitionsRevoked(s.onPartitionsRevoked),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create kafka client: %w", err)
	}

	s.Service = services.NewBasicService(s.starting, s.running, s.stopping)
	return s, nil
}

func (s *IngestLimits) Describe(descs chan<- *prometheus.Desc) {
	descs <- tenantPartitionDesc
	descs <- tenantRecordedStreamsDesc
	descs <- tenantActiveStreamsDesc
}

func (s *IngestLimits) Collect(m chan<- prometheus.Metric) {
	cutoff := time.Now().Add(-s.cfg.WindowSize).UnixNano()

	var (
		recorded            = make(map[string]int)
		active              = make(map[string]int)
		partitionsPerTenant = make(map[string]map[int32]struct{})
	)

	s.metadata.All(func(tenant string, partitionID int32, stream Stream) {
		if assigned := s.partitionManager.Has(partitionID); !assigned {
			return
		}

		if stream.LastSeenAt < cutoff {
			return
		}

		active[tenant]++

		if _, ok := partitionsPerTenant[tenant]; !ok {
			partitionsPerTenant[tenant] = make(map[int32]struct{})
		}

		if _, ok := partitionsPerTenant[tenant][partitionID]; !ok {
			partitionsPerTenant[tenant][partitionID] = struct{}{}
		}
	})

	for tenant, partitions := range partitionsPerTenant {
		m <- prometheus.MustNewConstMetric(tenantPartitionDesc, prometheus.GaugeValue, float64(len(partitions)), tenant)
	}

	for tenant, active := range active {
		m <- prometheus.MustNewConstMetric(tenantActiveStreamsDesc, prometheus.GaugeValue, float64(active), tenant)
	}

	for tenant, recorded := range recorded {
		m <- prometheus.MustNewConstMetric(tenantRecordedStreamsDesc, prometheus.GaugeValue, float64(recorded), tenant)
	}
}

func (s *IngestLimits) onPartitionsAssigned(ctx context.Context, client *kgo.Client, partitions map[string][]int32) {
	s.partitionManager.Assign(ctx, client, partitions)
}

func (s *IngestLimits) onPartitionsRevoked(ctx context.Context, client *kgo.Client, partitions map[string][]int32) {
	s.partitionManager.Remove(ctx, client, partitions)

	for _, ids := range partitions {
		s.metadata.EvictPartitions(ids)
	}
}

func (s *IngestLimits) CheckReady(ctx context.Context) error {
	if s.State() != services.Running {
		return fmt.Errorf("service is not running: %v", s.State())
	}
	err := s.lifecycler.CheckReady(ctx)
	if err != nil {
		return fmt.Errorf("lifecycler not ready: %w", err)
	}
	return nil
}

// starting implements the Service interface's starting method.
// It is called when the service starts and performs any necessary initialization.
func (s *IngestLimits) starting(ctx context.Context) (err error) {
	defer func() {
		if err != nil {
			// if starting() fails for any reason (e.g., context canceled),
			// the lifecycler must be stopped.
			_ = services.StopAndAwaitTerminated(context.Background(), s.lifecycler)
		}
	}()

	// pass new context to lifecycler, so that it doesn't stop automatically when IngestLimits's service context is done
	err = s.lifecycler.StartAsync(context.Background())
	if err != nil {
		return err
	}

	err = s.lifecycler.AwaitRunning(ctx)
	if err != nil {
		return err
	}

	return nil
}

// running implements the Service interface's running method.
// It runs the main service loop that consumes stream metadata from Kafka and manages
// the metadata map. The method also starts a goroutine to periodically evict old streams from the metadata map.
func (s *IngestLimits) running(ctx context.Context) error {
	// Start the eviction goroutine
	go s.evictOldStreamsPeriodic(ctx)

	for {
		select {
		case <-ctx.Done():
			return nil
		// stop
		case err := <-s.lifecyclerWatcher.Chan():
			return fmt.Errorf("lifecycler failed: %w", err)
		default:
			fetches := s.client.PollRecords(ctx, 100)
			if fetches.IsClientClosed() {
				return nil
			}

			if errs := fetches.Errors(); len(errs) > 0 {
				for _, err := range errs {
					level.Error(s.logger).Log("msg", "error fetching records", "err", err.Err.Error())
				}
				continue
			}

			// Process the fetched records
			var sizeBytes int

			iter := fetches.RecordIter()
			for !iter.Done() {
				record := iter.Next()
				sizeBytes += len(record.Value)

				// Update the estimated consumption lag.
				s.metrics.kafkaConsumptionLag.Observe(time.Since(record.Timestamp).Seconds())

				metadata, err := kafka.DecodeStreamMetadata(record)
				if err != nil {
					level.Error(s.logger).Log("msg", "error decoding metadata", "err", err)
					continue
				}

				s.updateMetadata(metadata, string(record.Key), record.Partition, record.Timestamp)
			}

			s.metrics.kafkaReadBytesTotal.Add(float64(sizeBytes))
		}
	}
}

// evictOldStreamsPeriodic runs a periodic job that evicts old streams.
// It runs two evictions per window size.
func (s *IngestLimits) evictOldStreamsPeriodic(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.WindowSize / 2)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cutoff := time.Now().Add(-s.cfg.WindowSize).UnixNano()
			s.metadata.Evict(cutoff)
		}
	}
}

// updateMetadata updates the metadata map with the provided StreamMetadata.
// It uses the provided lastSeenAt timestamp as the last seen time.
func (s *IngestLimits) updateMetadata(rec *logproto.StreamMetadata, tenant string, partition int32, lastSeenAt time.Time) {
	var (
		// Use the provided lastSeenAt timestamp as the last seen time
		recordTime = lastSeenAt.UnixNano()
		// Get the bucket for this timestamp using the configured interval duration
		bucketStart = lastSeenAt.Truncate(s.cfg.BucketDuration).UnixNano()
		// Calculate the rate window cutoff for cleaning up old buckets
		rateWindowCutoff = lastSeenAt.Add(-s.cfg.RateWindow).UnixNano()
		// Calculate the total size of the stream
		totalSize = rec.EntriesSize + rec.StructuredMetadataSize
	)

	if assigned := s.partitionManager.Has(partition); !assigned {
		return
	}

	s.metadata.Store(tenant, partition, rec.StreamHash, totalSize, recordTime, bucketStart, rateWindowCutoff)

	s.metrics.tenantIngestedBytesTotal.WithLabelValues(tenant).Add(float64(totalSize))
}

// stopping implements the Service interface's stopping method.
// It performs cleanup when the service is stopping, including closing the Kafka client.
// It returns nil for expected termination cases (context cancellation or client closure)
// and returns the original error for other failure cases.
func (s *IngestLimits) stopping(failureCase error) error {
	if s.client != nil {
		s.client.Close()
	}
	if errors.Is(failureCase, context.Canceled) || errors.Is(failureCase, kgo.ErrClientClosed) {
		return nil
	}

	var allErrs util.MultiError
	allErrs.Add(services.StopAndAwaitTerminated(context.Background(), s.lifecycler))
	allErrs.Add(failureCase)

	return allErrs.Err()
}

// ExceedsLimits implements the logproto.IngestLimitsServer interface.
func (s *IngestLimits) ExceedsLimits(_ context.Context, _ *logproto.ExceedsLimitsRequest) (*logproto.ExceedsLimitsResponse, error) {
	return &logproto.ExceedsLimitsResponse{}, nil
}

// GetAssignedPartitions implements the logproto.IngestLimitsServer interface.
// It returns the partitions that the tenant is assigned to and the instance still owns.
func (s *IngestLimits) GetAssignedPartitions(_ context.Context, _ *logproto.GetAssignedPartitionsRequest) (*logproto.GetAssignedPartitionsResponse, error) {
	resp := logproto.GetAssignedPartitionsResponse{
		AssignedPartitions: s.partitionManager.List(),
	}
	return &resp, nil
}

// GetStreamUsage implements the logproto.IngestLimitsServer interface.
// It returns the number of active streams for a tenant and the status of requested streams.
func (s *IngestLimits) GetStreamUsage(_ context.Context, req *logproto.GetStreamUsageRequest) (*logproto.GetStreamUsageResponse, error) {
	// Get the cutoff time for active streams
	cutoff := time.Now().Add(-s.cfg.WindowSize).UnixNano()

	// Calculate the rate window cutoff in nanoseconds
	rateWindowCutoff := time.Now().Add(-s.cfg.RateWindow).UnixNano()

	// Count total active streams for the tenant
	// across all assigned partitions and record
	// the streams that have been seen within the
	// window
	var (
		activeStreams uint64
		totalSize     uint64
	)

	// If the stream is written into a partition we are
	// assigned to and has been seen within the window,
	// it is an active stream.
	unknownStreams := req.StreamHashes

	s.metadata.Usage(req.Tenant, func(partitionID int32, stream Stream) {
		if assigned := s.partitionManager.Has(partitionID); !assigned {
			return
		}

		if stream.LastSeenAt < cutoff {
			return
		}

		activeStreams++

		// Calculate size only within the rate window
		for _, bucket := range stream.RateBuckets {
			if bucket.Timestamp >= rateWindowCutoff {
				totalSize += bucket.Size
			}
		}

		for i, streamHash := range unknownStreams {
			if stream.Hash == streamHash {
				unknownStreams = append(unknownStreams[:i], unknownStreams[i+1:]...)
				break
			}
		}
	})

	// Calculate rate using only data from within the rate window
	rate := float64(totalSize) / s.cfg.RateWindow.Seconds()

	return &logproto.GetStreamUsageResponse{
		Tenant:         req.Tenant,
		ActiveStreams:  activeStreams,
		UnknownStreams: unknownStreams,
		Rate:           uint64(rate),
	}, nil
}
