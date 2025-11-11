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
	RandomWithinShard     bool   `yaml:"random_within_shard"`
}

func (c *DataObjTeeConfig) RegisterFlags(f *flag.FlagSet) {
	f.BoolVar(&c.Enabled, "distributor.dataobj-tee.enabled", false, "Enable data object tee.")
	f.StringVar(&c.Topic, "distributor.dataobj-tee.topic", "", "Topic for data object tee.")
	f.IntVar(&c.MaxBufferedBytes, "distributor.dataobj-tee.max-buffered-bytes", 100<<20, "Maximum number of bytes to buffer.")
	f.IntVar(&c.PerPartitionRateBytes, "distributor.dataobj-tee.per-partition-rate-bytes", 1024*1024, "The per-tenant partition rate (bytes/sec).")
	f.BoolVar(&c.RandomWithinShard, "distributor.dataobj-tee.random-within-shard", false, "Randomize within shard.")
}

func (c *DataObjTeeConfig) Validate() error {
	if c.Enabled && c.Topic == "" {
		return errors.New("the topic is required")
	}
	return nil
}

// DataObjTee is a tee that duplicates streams to the data object topic.
// It is a temporary solution while we work on segmentation keys.
type DataObjTee struct {
	cfg      *DataObjTeeConfig
	client   *kgo.Client
	resolver *SegmentationPartitionResolver
	logger   log.Logger

	// Metrics.
	failures        prometheus.Counter
	total           prometheus.Counter
	producedEntries *prometheus.CounterVec
	producedStreams *prometheus.CounterVec
	producedBytes   *prometheus.CounterVec
	fallback        *prometheus.CounterVec
}

// NewDataObjTee returns a new DataObjTee.
func NewDataObjTee(
	cfg *DataObjTeeConfig,
	client *kgo.Client,
	resolver *SegmentationPartitionResolver,
	logger log.Logger,
	reg prometheus.Registerer,
) (*DataObjTee, error) {
	return &DataObjTee{
		cfg:      cfg,
		client:   client,
		resolver: resolver,
		logger:   logger,
		failures: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_distributor_dataobj_tee_duplicate_stream_failures_total",
			Help: "Total number of streams that could not be duplicated.",
		}),
		total: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_distributor_dataobj_tee_duplicate_streams_total",
			Help: "Total number of streams duplicated.",
		}),
		// producedEntries: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
		// 	Name: "loki_distributor_dataobj_tee_produced_entries_total",
		// 	Help: "Total number of streams produced.",
		// }, []string{"partition", "tenant", "segmentation_key"}),
		// producedStreams: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
		// 	Name: "loki_distributor_dataobj_tee_produced_streams_total",
		// 	Help: "Total number of streams produced.",
		// }, []string{"partition", "tenant", "segmentation_key"}),
		producedBytes: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_distributor_dataobj_tee_produced_bytes_total",
			Help: "Total number of streams produced.",
		}, []string{"partition", "tenant", "segmentation_key"}),
		fallback: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_distributor_dataobj_tee_fallback_total",
			Help: "Total number of streams that could not be duplicated.",
		}, []string{"tenant", "segmentation_key"}),
	}, nil
}

// Duplicate implements the [Tee] interface.
func (t *DataObjTee) Duplicate(ctx context.Context, tenant string, streams []KeyedStream) {
	for _, s := range streams {
		go t.duplicate(ctx, tenant, s)
	}
}

func (t *DataObjTee) duplicate(ctx context.Context, tenant string, stream KeyedStream) {
	t.total.Inc()
	segmentationKey, err := GetSegmentationKey(tenant, stream)
	if err != nil {
		level.Error(t.logger).Log("msg", "failed to get segmentation key", "err", err)
		t.failures.Inc()
		return
	}
	partition, fallback, err := t.resolver.Resolve(ctx, tenant, segmentationKey, stream)
	if err != nil {
		level.Error(t.logger).Log("msg", "failed to get partition", "err", err)
		t.failures.Inc()
		return
	}
	if fallback {
		t.fallback.WithLabelValues(tenant, string(segmentationKey)).Inc()
	}
	records, err := kafka.EncodeWithTopic(t.cfg.Topic, partition, tenant, stream.Stream, t.cfg.MaxBufferedBytes)
	if err != nil {
		level.Error(t.logger).Log("msg", "failed to encode stream", "err", err)
		t.failures.Inc()
		return
	}
	results := t.client.ProduceSync(context.TODO(), records...)
	if err := results.FirstErr(); err != nil {
		level.Error(t.logger).Log("msg", "failed to produce records", "err", err)
		t.failures.Inc()
	}
	// t.producedEntries.WithLabelValues(strconv.Itoa(int(partition)), tenant, string(segmentationKey)).Add(float64(len(stream.Stream.Entries)))
	// t.producedStreams.WithLabelValues(strconv.Itoa(int(partition)), tenant, string(segmentationKey)).Inc()
	t.producedBytes.WithLabelValues(strconv.Itoa(int(partition)), tenant, string(segmentationKey)).Add(float64(stream.Stream.Size()))
}
