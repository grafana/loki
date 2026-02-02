package distributor

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strconv"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/kafka"
)

type TeeErrorCodes int

const (
	// Since Tee is such a specific part of our write path its error codes start at 1000.
	TeeCouldntSolvePartitionError TeeErrorCodes = 1000
	TeeCouldntEncodeStreamError   TeeErrorCodes = 1001
	TeeCouldntProduceRecordsError TeeErrorCodes = 1002
)

type DataObjTeeConfig struct {
	Enabled               bool   `yaml:"enabled"`
	Topic                 string `yaml:"topic"`
	MaxBufferedBytes      int    `yaml:"max_buffered_bytes"`
	PerPartitionRateBytes int    `yaml:"per_partition_rate_bytes"`
	DebugMetricsEnabled   bool   `yaml:"debug_metrics_enabled"`
}

func (c *DataObjTeeConfig) RegisterFlags(f *flag.FlagSet) {
	f.BoolVar(&c.Enabled, "distributor.dataobj-tee.enabled", false, "Enable data object tee.")
	f.StringVar(&c.Topic, "distributor.dataobj-tee.topic", "", "Topic for data object tee.")
	f.IntVar(&c.MaxBufferedBytes, "distributor.dataobj-tee.max-buffered-bytes", 100<<20, "Maximum number of bytes to buffer.")
	f.IntVar(&c.PerPartitionRateBytes, "distributor.dataobj-tee.per-partition-rate-bytes", 1024*1024, "The per-tenant partition rate (bytes/sec).")
	f.BoolVar(&c.DebugMetricsEnabled, "distributor.dataobj-tee.debug-metrics-enabled", false, "Enables optional debug metrics.")
}

func (c *DataObjTeeConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	if c.Topic == "" {
		return errors.New("the topic is required")
	}
	if c.MaxBufferedBytes < 0 {
		return errors.New("max buffered bytes cannot be negative")
	}
	if c.PerPartitionRateBytes < 0 {
		return errors.New("per partition rate bytes cannot be negative")
	}
	return nil
}

// DataObjTee is a tee that duplicates streams to the data object topic.
// It is a temporary solution while we work on segmentation keys.
type DataObjTee struct {
	cfg          *DataObjTeeConfig
	limitsClient *ingestLimits
	limits       Limits
	kafkaClient  *kgo.Client
	resolver     *SegmentationPartitionResolver
	logger       log.Logger

	// Metrics.
	streams         prometheus.Counter
	streamFailures  prometheus.Counter
	producedBytes   *prometheus.CounterVec
	producedRecords *prometheus.CounterVec
}

// NewDataObjTee returns a new DataObjTee.
func NewDataObjTee(
	cfg *DataObjTeeConfig,
	resolver *SegmentationPartitionResolver,
	limitsClient *ingestLimits,
	limits Limits,
	kafkaClient *kgo.Client,
	logger log.Logger,
	r prometheus.Registerer,
) (*DataObjTee, error) {
	return &DataObjTee{
		cfg:          cfg,
		resolver:     resolver,
		kafkaClient:  kafkaClient,
		limitsClient: limitsClient,
		limits:       limits,
		logger:       logger,
		streams: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_distributor_dataobj_tee_duplicate_streams_total",
			Help: "Total number of streams duplicated.",
		}),
		streamFailures: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_distributor_dataobj_tee_duplicate_stream_failures_total",
			Help: "Total number of streams that could not be duplicated.",
		}),
		// The tenant and segmentation key labels are not emitted unless debug metrics
		// are enabled.
		producedBytes: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_distributor_dataobj_tee_produced_bytes_total",
			Help: "Total number of bytes produced to each partition.",
		}, []string{"partition", "tenant", "segmentation_key"}),
		producedRecords: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_distributor_dataobj_tee_produced_records_total",
			Help: "Total number of records produced to each partition.",
		}, []string{"partition", "tenant", "segmentation_key"}),
	}, nil
}

// A SegmentedStream is a KeyedStream with a segmentation key.
type SegmentedStream struct {
	KeyedStream
	SegmentationKey     SegmentationKey
	SegmentationKeyHash uint64
}

func (t *DataObjTee) Register(_ context.Context, _ string, streams []KeyedStream, pushTracker *PushTracker) {
	// Add our streams to the pending count so the distributor waits for them.
	pushTracker.streamsPending.Add(int32(len(streams)))
}

// Duplicate implements the [Tee] interface.
func (t *DataObjTee) Duplicate(ctx context.Context, tenant string, streams []KeyedStream, pushTracker *PushTracker) {
	segmentationKeyStreams := make([]SegmentedStream, 0, len(streams))
	for _, stream := range streams {
		segmentationKey, err := GetSegmentationKey(stream)
		if err != nil {
			level.Error(t.logger).Log("msg", "failed to get segmentation key", "err", err)
			t.streamFailures.Inc()
			return
		}
		segmentationKeyStreams = append(segmentationKeyStreams, SegmentedStream{
			KeyedStream:         stream,
			SegmentationKey:     segmentationKey,
			SegmentationKeyHash: segmentationKey.Sum64(),
		})
	}
	rates, err := t.limitsClient.UpdateRates(ctx, tenant, segmentationKeyStreams)
	if err != nil {
		level.Error(t.logger).Log("msg", "failed to update rates", "err", err)
	}
	// fastRates is a temporary lookup table that lets us find the rate
	// for a segmentation key in constant time.
	fastRates := make(map[uint64]uint64, len(rates))
	for _, rate := range rates {
		fastRates[rate.StreamHash] = rate.Rate
	}

	// We use max to prevent negative values becoming large positive values
	// when converting from float64 to uint64.
	tenantRateBytesLimit := uint64(max(t.limits.IngestionRateBytes(tenant), 0))

	for _, s := range segmentationKeyStreams {
		go func(stream SegmentedStream) {
			t.duplicate(ctx, tenant, stream, fastRates[stream.SegmentationKeyHash], tenantRateBytesLimit, pushTracker)
		}(s)
	}
}

func (t *DataObjTee) duplicate(ctx context.Context, tenant string, stream SegmentedStream, rateBytes, tenantRateBytes uint64, pushTracker *PushTracker) {
	t.streams.Inc()

	partition, err := t.resolver.Resolve(ctx, tenant, stream.SegmentationKey, rateBytes, tenantRateBytes)
	if err != nil {
		level.Error(t.logger).Log("msg", "failed to resolve partition", "err", err)
		t.streamFailures.Inc()
		pushTracker.doneWithResult(fmt.Errorf("couldn't process request internally due to tee error: %d", TeeCouldntSolvePartitionError))
		return
	}

	records, err := kafka.EncodeWithTopic(t.cfg.Topic, partition, tenant, stream.Stream, t.cfg.MaxBufferedBytes)
	if err != nil {
		level.Error(t.logger).Log("msg", "failed to encode stream", "err", err)
		t.streamFailures.Inc()
		pushTracker.doneWithResult(fmt.Errorf("couldn't process request internally due to tee error: %d", TeeCouldntEncodeStreamError))
		return
	}

	results := t.kafkaClient.ProduceSync(ctx, records...)
	if err := results.FirstErr(); err != nil {
		level.Error(t.logger).Log("msg", "failed to produce records", "err", err)
		t.streamFailures.Inc()
		pushTracker.doneWithResult(fmt.Errorf("couldn't process request internally due to tee error: %d", TeeCouldntProduceRecordsError))
		return
	}

	var size int64
	for _, rec := range records {
		size += int64(len(rec.Value))
	}
	t.observeDuplicate(partition, tenant, string(stream.SegmentationKey), size)

	pushTracker.doneWithResult(nil)
}

func (t *DataObjTee) observeDuplicate(partition int32, tenant, segmentationKey string, size int64) {
	partitionLabelValue := strconv.FormatInt(int64(partition), 10)
	var tenantLabelValue, segmentationKeyLabelValue string
	if t.cfg.DebugMetricsEnabled {
		tenantLabelValue = tenant
		segmentationKeyLabelValue = segmentationKey
	}
	t.producedBytes.WithLabelValues(
		partitionLabelValue,
		tenantLabelValue,
		segmentationKeyLabelValue,
	).Add(float64(size))
	t.producedRecords.WithLabelValues(
		partitionLabelValue,
		tenantLabelValue,
		segmentationKeyLabelValue,
	).Inc()
}
