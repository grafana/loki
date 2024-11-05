package partition

import (
	"math"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/twmb/franz-go/plugin/kprom"

	"github.com/grafana/loki/v3/pkg/kafka/client"
)

type ReaderMetrics struct {
	Partition         *prometheus.GaugeVec
	Phase             *prometheus.GaugeVec
	ReceiveDelay      *prometheus.HistogramVec
	RecordsPerFetch   prometheus.Histogram
	FetchesErrors     prometheus.Counter
	FetchesTotal      prometheus.Counter
	FetchWaitDuration prometheus.Histogram
	ConsumeLatency    prometheus.Histogram
	Kprom             *kprom.Metrics
}

// NewReaderMetrics initializes and returns a new set of metrics for the PartitionReader.
func NewReaderMetrics(r prometheus.Registerer) *ReaderMetrics {
	return &ReaderMetrics{
		Partition: promauto.With(r).NewGaugeVec(prometheus.GaugeOpts{
			Name: "loki_ingest_storage_reader_partition",
			Help: "The partition ID assigned to this reader.",
		}, []string{"id"}),
		Phase: promauto.With(r).NewGaugeVec(prometheus.GaugeOpts{
			Name: "loki_ingest_storage_reader_phase",
			Help: "The current phase of the consumer.",
		}, []string{"phase"}),
		ReceiveDelay: promauto.With(r).NewHistogramVec(prometheus.HistogramOpts{
			Name:                            "loki_ingest_storage_reader_receive_delay_seconds",
			Help:                            "Delay between producing a record and receiving it in the consumer.",
			NativeHistogramZeroThreshold:    math.Pow(2, -10), // Values below this will be considered to be 0. Equals to 0.0009765625, or about 1ms.
			NativeHistogramBucketFactor:     1.2,              // We use higher factor (scheme=2) to have wider spread of buckets.
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 1 * time.Hour,
			Buckets:                         prometheus.ExponentialBuckets(0.125, 2, 18), // Buckets between 125ms and 9h.
		}, []string{"phase"}),
		Kprom: client.NewReaderClientMetrics("partition-reader", r),
		FetchWaitDuration: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Name:                        "loki_ingest_storage_reader_records_batch_wait_duration_seconds",
			Help:                        "How long a consumer spent waiting for a batch of records from the Kafka client. If fetching is faster than processing, then this will be close to 0.",
			NativeHistogramBucketFactor: 1.1,
		}),
		RecordsPerFetch: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Name:    "loki_ingest_storage_reader_records_per_fetch",
			Help:    "The number of records received by the consumer in a single fetch operation.",
			Buckets: prometheus.ExponentialBuckets(1, 2, 15),
		}),
		FetchesErrors: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_ingest_storage_reader_fetch_errors_total",
			Help: "The number of fetch errors encountered by the consumer.",
		}),
		FetchesTotal: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_ingest_storage_reader_fetches_total",
			Help: "Total number of Kafka fetches received by the consumer.",
		}),
	}
}

func (m *ReaderMetrics) reportStarting(partitionID int32) {
	m.Partition.WithLabelValues(strconv.Itoa(int(partitionID))).Set(1)
	m.Phase.WithLabelValues(PhaseStarting).Set(1)
	m.Phase.WithLabelValues(PhaseRunning).Set(0)
}

func (m *ReaderMetrics) reportRunning(partitionID int32) {
	m.Partition.WithLabelValues(strconv.Itoa(int(partitionID))).Set(1)
	m.Phase.WithLabelValues(PhaseStarting).Set(0)
	m.Phase.WithLabelValues(PhaseRunning).Set(1)
}
