package chunkbuilder

import (
	"errors"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	dslog "github.com/grafana/dskit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/kafka"
)

// The rate of decode failure logs. A topic in another format fails every
// record, and the metric counts them all.
const (
	decodeFailureLogsPerSecond = 1
	decodeFailureLogsBurst     = 5
)

var (
	// errNoTenant means a record has no key, which holds the tenant.
	errNoTenant = errors.New("the record has no tenant")
	// errNoLabels means the stream of a record has no labels. An empty record
	// decodes to such a stream.
	errNoLabels = errors.New("the stream of the record has no labels")
)

// A processor decodes the records of one partition. It is not safe for
// concurrent use.
//
// TODO(chunk-builders): Group the decoded streams into chunks.
type processor struct {
	decoder *kafka.Decoder
	metrics *metrics
	// failureLogger logs decode failures at a limited rate.
	failureLogger log.Logger
}

func newProcessor(metrics *metrics, logger log.Logger) *processor {
	decoder, err := kafka.NewDecoder()
	if err != nil {
		// NewDecoder fails only if the size of its label cache is not positive.
		panic(err)
	}
	return &processor{
		decoder:       decoder,
		metrics:       metrics,
		failureLogger: dslog.NewRateLimitedLogger(logger, decodeFailureLogsPerSecond, decodeFailureLogsBurst, nil),
	}
}

// process decodes rec, which the service consumed at now. It skips a record
// that fails to decode, since reading the record again cannot fix it.
func (p *processor) process(rec *kgo.Record, now time.Time) {
	p.metrics.records.Inc()
	p.metrics.receivedBytes.Add(float64(len(rec.Value)))
	p.metrics.lastOffset.Set(float64(rec.Offset))
	p.metrics.consumptionLag.Set(now.Sub(rec.Timestamp).Seconds())

	if err := p.decode(rec); err != nil {
		p.metrics.decodeFailures.Inc()
		level.Warn(p.failureLogger).Log("msg", "skipping a record that failed to decode", "offset", rec.Offset, "err", err)
	}
}

// decode decodes the stream in rec, in either record encoding, and parses its
// labels.
func (p *processor) decode(rec *kgo.Record) error {
	if len(rec.Key) == 0 {
		return errNoTenant
	}
	_, ls, err := p.decoder.Decode(rec.Value)
	if err != nil {
		return err
	}
	if ls.IsEmpty() {
		return errNoLabels
	}
	return nil
}

type metrics struct {
	records        prometheus.Counter
	receivedBytes  prometheus.Counter
	decodeFailures prometheus.Counter
	lastOffset     prometheus.Gauge
	consumptionLag prometheus.Gauge
}

func newMetrics(reg prometheus.Registerer) *metrics {
	return &metrics{
		records: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_chunk_builder_records_total",
			Help: "Total number of Kafka records that the chunk-builder consumed.",
		}),
		receivedBytes: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_chunk_builder_received_bytes_total",
			Help: "Total size in bytes of the values of the Kafka records that the chunk-builder consumed.",
		}),
		decodeFailures: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_chunk_builder_decode_failures_total",
			Help: "Total number of consumed Kafka records that failed to decode. The chunk-builder skips them.",
		}),
		lastOffset: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Name: "loki_chunk_builder_last_offset",
			Help: "The offset of the last Kafka record that the chunk-builder consumed.",
		}),
		consumptionLag: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Name: "loki_chunk_builder_consumption_lag_seconds",
			Help: "The time in seconds between the Kafka timestamp of the last consumed record and when the chunk-builder consumed it. It does not change while no records arrive, so it does not grow when consumption stalls; alert on loki_chunk_builder_single_partition_consumer_fetch_errors_total for that.",
		}),
	}
}
