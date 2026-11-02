package limits

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/twmb/franz-go/pkg/kgo"
	"golang.org/x/time/rate"

	"github.com/grafana/loki/v3/pkg/limits/proto"
)

// removalRecordZone is the zone set on removal records. It is not the zone
// of any instance of this service, so consumers never mistake a removal
// record for one of their own.
const removalRecordZone = "ingester"

// StreamRemovalProducerConfig configures the [StreamRemovalProducer].
type StreamRemovalProducerConfig struct {
	// Enabled enables writing removal records.
	Enabled bool `yaml:"enabled"`
	// BufferSize is the maximum number of removal records waiting to be
	// written. Records are dropped when the buffer is full.
	BufferSize int `yaml:"buffer_size"`
	// MaxRecordsPerSecond limits the rate at which removal records are
	// written, so a flush of a large ingester does not cause a spike in the
	// consumer lag of the ingest limits service.
	MaxRecordsPerSecond float64 `yaml:"max_records_per_second"`
}

// RegisterFlagsWithPrefix registers the flags with the given prefix.
func (cfg *StreamRemovalProducerConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.BoolVar(&cfg.Enabled, prefix+"enabled", false, "EXPERIMENTAL: Write a record to the ingest limits metadata topic when a stream is removed from memory, so the ingest limits service stops counting it without waiting for its active window to expire. Requires -ingest-limits.topic to be set.")
	f.IntVar(&cfg.BufferSize, prefix+"buffer-size", 100_000, "The maximum number of stream removal records waiting to be written. Records are dropped when the buffer is full, and the stream then expires from the ingest limits service after its active window.")
	f.Float64Var(&cfg.MaxRecordsPerSecond, prefix+"max-records-per-second", 10_000, "The maximum number of stream removal records written per second.")
}

// Validate validates the config.
func (cfg *StreamRemovalProducerConfig) Validate() error {
	if !cfg.Enabled {
		return nil
	}
	if cfg.BufferSize <= 0 {
		return errors.New("stream removal records buffer size must be greater than 0")
	}
	if cfg.MaxRecordsPerSecond <= 0 {
		return errors.New("stream removal records max records per second must be greater than 0")
	}
	return nil
}

// kafkaProduceCloser allows mocking of certain [kgo.Client] methods in tests.
type kafkaProduceCloser interface {
	kafkaProducer
	Close()
}

// StreamRemovalProducer writes removal records to the metadata topic. It is
// used by ingesters to tell the ingest limits service that they no longer
// hold a stream. Writes are asynchronous and never block the caller: records
// are buffered, written at a limited rate, and dropped when the buffer is
// full. A dropped record only means the stream expires from the ingest
// limits service after its active window, as it does without removal
// records.
type StreamRemovalProducer struct {
	services.Service

	client        kafkaProduceCloser
	topic         string
	numPartitions int
	records       chan *proto.StreamMetadataRecord
	limiter       *rate.Limiter
	logger        log.Logger

	enqueued       prometheus.Counter
	dropped        prometheus.Counter
	produced       prometheus.Counter
	producedFailed prometheus.Counter
}

// NewStreamRemovalProducer returns a new StreamRemovalProducer. It takes
// ownership of the client and closes it when stopped.
func NewStreamRemovalProducer(cfg StreamRemovalProducerConfig, client kafkaProduceCloser, topic string, numPartitions int, logger log.Logger, reg prometheus.Registerer) *StreamRemovalProducer {
	p := &StreamRemovalProducer{
		client:        client,
		topic:         topic,
		numPartitions: numPartitions,
		records:       make(chan *proto.StreamMetadataRecord, cfg.BufferSize),
		limiter:       rate.NewLimiter(rate.Limit(cfg.MaxRecordsPerSecond), max(1, int(cfg.MaxRecordsPerSecond))),
		logger:        logger,
		enqueued: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_ingest_limits_removal_records_enqueued_total",
			Help: "The total number of stream removal records enqueued to be written.",
		}),
		dropped: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_ingest_limits_removal_records_dropped_total",
			Help: "The total number of stream removal records dropped because the buffer was full.",
		}),
		produced: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_ingest_limits_removal_records_produced_total",
			Help: "The total number of produced stream removal records, including failures.",
		}),
		producedFailed: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_ingest_limits_removal_records_produced_failed_total",
			Help: "The total number of failed produced stream removal records.",
		}),
	}
	p.Service = services.NewBasicService(nil, p.running, p.stopping)
	return p
}

// StreamRemoved enqueues a removal record for the stream. streamHash is the
// hash of the stream's labels without the stream shard label, the same hash
// the distributor sends to the ingest limits service, and lastPushAt is the
// time of the last push the caller received for the stream. It never blocks.
func (p *StreamRemovalProducer) StreamRemoved(tenant string, streamHash uint64, lastPushAt time.Time) {
	rec := &proto.StreamMetadataRecord{
		Zone:              removalRecordZone,
		Tenant:            tenant,
		Metadata:          &proto.StreamMetadata{StreamHash: streamHash},
		RemovedLastSeenAt: lastPushAt.UnixNano(),
	}
	select {
	case p.records <- rec:
		p.enqueued.Inc()
	default:
		p.dropped.Inc()
	}
}

func (p *StreamRemovalProducer) running(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case rec := <-p.records:
			// Wait fails only when the producer is stopping. The record is
			// written anyway, as stopping writes the rest of the buffer
			// without the rate limit too.
			waitErr := p.limiter.Wait(ctx)
			if err := p.produce(ctx, rec); err != nil {
				level.Error(p.logger).Log("msg", "failed to produce stream removal record", "err", err.Error())
			}
			if waitErr != nil {
				return nil
			}
		}
	}
}

// stopping writes the records still buffered, without the rate limit, so
// the removals of a flush on shutdown are not lost, and then closes the
// client, which waits for buffered writes to complete.
func (p *StreamRemovalProducer) stopping(_ error) error {
	defer p.client.Close()
	for {
		select {
		case rec := <-p.records:
			if err := p.produce(context.Background(), rec); err != nil {
				level.Error(p.logger).Log("msg", "failed to produce stream removal record", "err", err.Error())
			}
		default:
			return nil
		}
	}
}

func (p *StreamRemovalProducer) produce(ctx context.Context, rec *proto.StreamMetadataRecord) error {
	b, err := rec.Marshal()
	if err != nil {
		return fmt.Errorf("failed to marshal proto: %w", err)
	}
	// Removal records must go to the same partition as the stream's other
	// records, so that they are ordered after them on replay.
	partition := int32(rec.Metadata.StreamHash % uint64(p.numPartitions))
	p.produced.Inc()
	p.client.Produce(context.WithoutCancel(ctx), &kgo.Record{
		Key:       []byte(rec.Tenant),
		Value:     b,
		Topic:     p.topic,
		Partition: partition,
	}, p.handleProduceErr)
	return nil
}

func (p *StreamRemovalProducer) handleProduceErr(_ *kgo.Record, err error) {
	if err != nil {
		level.Error(p.logger).Log("msg", "failed to produce stream removal record", "err", err.Error())
		p.producedFailed.Inc()
	}
}
