package distributor

import (
	"context"
	"errors"
	"flag"
	"strconv"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/kafka"
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
	failures prometheus.Counter
	total    prometheus.Counter

	// High cardinality metrics which are only emitted when debug metrics
	// are enabled.
	produced *prometheus.CounterVec
}

// NewDataObjTee returns a new DataObjTee.
func NewDataObjTee(
	cfg *DataObjTeeConfig,
	resolver *SegmentationPartitionResolver,
	limitsClient *ingestLimits,
	limits Limits,
	kafkaClient *kgo.Client,
	logger log.Logger,
	reg prometheus.Registerer,
) (*DataObjTee, error) {
	return &DataObjTee{
		cfg:          cfg,
		resolver:     resolver,
		kafkaClient:  kafkaClient,
		limitsClient: limitsClient,
		limits:       limits,
		logger:       logger,
		failures: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_distributor_dataobj_tee_duplicate_stream_failures_total",
			Help: "Total number of streams that could not be duplicated.",
		}),
		total: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_distributor_dataobj_tee_duplicate_streams_total",
			Help: "Total number of streams duplicated.",
		}),
		produced: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_distributor_dataobj_tee_produced_bytes_total",
			Help: "Total number of bytes produced to each partition.",
		}, []string{"tenant", "partition", "segmentation_key"}),
	}, nil
}

// A SegmentedStream is a KeyedStream with a segmentation key.
type SegmentedStream struct {
	KeyedStream
	SegmentationKey     SegmentationKey
	SegmentationKeyHash uint64
}

// Duplicate implements the [Tee] interface.
func (t *DataObjTee) Duplicate(ctx context.Context, tenant string, streams []KeyedStream) {
	segmentationKeyStreams := make([]SegmentedStream, 0, len(streams))
	for _, stream := range streams {
		segmentationKey, err := GetSegmentationKey(stream)
		if err != nil {
			level.Error(t.logger).Log("msg", "failed to get segmentation key", "err", err)
			t.failures.Inc()
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
		go t.duplicate(ctx, tenant, s, fastRates[s.SegmentationKeyHash], tenantRateBytesLimit)
	}
}

func (t *DataObjTee) duplicate(ctx context.Context, tenant string, stream SegmentedStream, rateBytes, tenantRateBytes uint64) {
	t.total.Inc()
	partition, err := t.resolver.Resolve(ctx, tenant, stream.SegmentationKey, rateBytes, tenantRateBytes)
	if err != nil {
		level.Error(t.logger).Log("msg", "failed to resolve partition", "err", err)
		t.failures.Inc()
		return
	}
	records, err := kafka.EncodeWithTopic(t.cfg.Topic, partition, tenant, stream.Stream, t.cfg.MaxBufferedBytes)
	if err != nil {
		level.Error(t.logger).Log("msg", "failed to encode stream", "err", err)
		t.failures.Inc()
		return
	}
	results := t.kafkaClient.ProduceSync(ctx, records...)
	if err := results.FirstErr(); err != nil {
		level.Error(t.logger).Log("msg", "failed to produce records", "err", err)
		t.failures.Inc()
	}
	if t.cfg.DebugMetricsEnabled {
		t.produced.WithLabelValues(
			tenant,
			strconv.FormatInt(int64(partition), 10),
			string(stream.SegmentationKey),
		).Add(float64(stream.Stream.Size()))
	}
}
