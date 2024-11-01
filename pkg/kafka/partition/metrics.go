package partition

import (
	"math"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/twmb/franz-go/plugin/kprom"

	"github.com/grafana/loki/v3/pkg/kafka/client"
)

type readerMetrics struct {
	partition         prometheus.Gauge
	phase             *prometheus.GaugeVec
	receiveDelay      *prometheus.HistogramVec
	recordsPerFetch   prometheus.Histogram
	fetchesErrors     prometheus.Counter
	fetchesTotal      prometheus.Counter
	fetchWaitDuration prometheus.Histogram
	consumeLatency    prometheus.Histogram
	kprom             *kprom.Metrics
}

// newReaderMetrics initializes and returns a new set of metrics for the PartitionReader.
func newReaderMetrics(r prometheus.Registerer) readerMetrics {
	return readerMetrics{
		partition: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "loki_ingest_storage_reader_partition_id",
			Help: "The partition ID assigned to this reader.",
		}),
		phase: promauto.With(r).NewGaugeVec(prometheus.GaugeOpts{
			Name: "loki_ingest_storage_reader_phase",
			Help: "The current phase of the consumer.",
		}, []string{"phase"}),
		receiveDelay: promauto.With(r).NewHistogramVec(prometheus.HistogramOpts{
			Name:                            "loki_ingest_storage_reader_receive_delay_seconds",
			Help:                            "Delay between producing a record and receiving it in the consumer.",
			NativeHistogramZeroThreshold:    math.Pow(2, -10), // Values below this will be considered to be 0. Equals to 0.0009765625, or about 1ms.
			NativeHistogramBucketFactor:     1.2,              // We use higher factor (scheme=2) to have wider spread of buckets.
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 1 * time.Hour,
			Buckets:                         prometheus.ExponentialBuckets(0.125, 2, 18), // Buckets between 125ms and 9h.
		}, []string{"phase"}),
		kprom: client.NewReaderClientMetrics("partition-reader", r),
		fetchWaitDuration: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Name:                        "loki_ingest_storage_reader_records_batch_wait_duration_seconds",
			Help:                        "How long a consumer spent waiting for a batch of records from the Kafka client. If fetching is faster than processing, then this will be close to 0.",
			NativeHistogramBucketFactor: 1.1,
		}),
		recordsPerFetch: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Name:    "loki_ingest_storage_reader_records_per_fetch",
			Help:    "The number of records received by the consumer in a single fetch operation.",
			Buckets: prometheus.ExponentialBuckets(1, 2, 15),
		}),
		fetchesErrors: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_ingest_storage_reader_fetch_errors_total",
			Help: "The number of fetch errors encountered by the consumer.",
		}),
		fetchesTotal: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_ingest_storage_reader_fetches_total",
			Help: "Total number of Kafka fetches received by the consumer.",
		}),
	}
}

func (m *readerMetrics) reportStarting(partitionID int32) {
	m.partition.Set(float64(partitionID))
	m.phase.WithLabelValues(phaseStarting).Set(1)
	m.phase.WithLabelValues(phaseRunning).Set(0)
}

func (m *readerMetrics) reportRunning(partitionID int32) {
	m.partition.Set(float64(partitionID))
	m.phase.WithLabelValues(phaseStarting).Set(0)
	m.phase.WithLabelValues(phaseRunning).Set(1)
}
