package distributor

import (
	"context"
	"errors"
	"flag"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/ring"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/kafka"
)

type DataObjTeeConfig struct {
	Enabled          bool   `yaml:"enabled"`
	Topic            string `yaml:"topic"`
	MaxBufferedBytes int    `yaml:"max_buffered_bytes"`
}

func (c *DataObjTeeConfig) RegisterFlags(f *flag.FlagSet) {
	f.BoolVar(&c.Enabled, "distributor.dataobj-tee.enabled", false, "Enable data object tee.")
	f.StringVar(&c.Topic, "distributor.dataobj-tee.topic", "", "Topic for data object tee.")
	f.IntVar(&c.MaxBufferedBytes, "distributor.dataobj-tee.max-buffered-bytes", 100<<20, "Maximum number of bytes to buffer.")
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
	cfg        *DataObjTeeConfig
	client     *kgo.Client
	ringReader ring.PartitionRingReader
	logger     log.Logger

	// Metrics.
	failures prometheus.Counter
	total    prometheus.Counter
}

// NewDataObjTee returns a new DataObjTee.
func NewDataObjTee(
	cfg *DataObjTeeConfig,
	client *kgo.Client,
	ringReader ring.PartitionRingReader,
	logger log.Logger,
	reg prometheus.Registerer,
) (*DataObjTee, error) {
	return &DataObjTee{
		cfg:        cfg,
		client:     client,
		ringReader: ringReader,
		logger:     logger,
		failures: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_distributor_dataobj_tee_duplicate_stream_failures_total",
			Help: "Total number of streams that could not be duplicated.",
		}),
		total: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_distributor_dataobj_tee_duplicate_streams_total",
			Help: "Total number of streams duplicated.",
		}),
	}, nil
}

// Duplicate implements the [Tee] interface.
func (t *DataObjTee) Duplicate(tenant string, streams []KeyedStream) {
	for _, s := range streams {
		go t.duplicate(tenant, s)
	}
}

func (t *DataObjTee) duplicate(tenant string, stream KeyedStream) {
	t.total.Inc()
	partition, err := t.ringReader.PartitionRing().ActivePartitionForKey(stream.HashKey)
	if err != nil {
		level.Error(t.logger).Log("msg", "failed to get partition", "err", err)
		t.failures.Inc()
		return
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
}
