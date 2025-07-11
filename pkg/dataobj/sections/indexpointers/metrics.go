package indexpointers

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"
)

/* var (
	sectionLabels = prometheus.Labels{"section": sectionType.String()}
) */

type Metrics struct {
	encodeSeconds prometheus.Histogram
	recordsTotal  prometheus.Counter
	minTimestamp  prometheus.Gauge
	maxTimestamp  prometheus.Gauge
}

func NewMetrics() *Metrics {
	return &Metrics{
		encodeSeconds: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "loki",
			Subsystem: "dataobj",
			Name:      "index_pointers_encode_seconds",
			Help:      "The number of seconds it takes to encode the index pointers section.",
		}),
		recordsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "loki_dataobj",
			Subsystem: "index_pointers",
			Name:      "records_total",

			Help: "Total number of records in the index pointers section.",
		}),

		minTimestamp: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "loki_dataobj",
			Subsystem: "index_pointers",
			Name:      "min_timestamp",

			Help: "The minimum timestamp (in unix seconds) across all index pointers; this resets after an encode.",
		}),

		maxTimestamp: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "loki_dataobj",
			Subsystem: "index_pointers",
			Name:      "max_timestamp",

			Help: "The maximum timestamp (in unix seconds) across all index pointers; this resets after an encode.",
		}),
	}
}

func (m *Metrics) Register(reg prometheus.Registerer) error {
	var errs []error
	errs = append(errs, reg.Register(m.encodeSeconds))
	errs = append(errs, reg.Register(m.recordsTotal))
	errs = append(errs, reg.Register(m.minTimestamp))
	errs = append(errs, reg.Register(m.maxTimestamp))
	return errors.Join(errs...)
}

func (m *Metrics) Unregister(reg prometheus.Registerer) {
	reg.Unregister(m.encodeSeconds)
	reg.Unregister(m.recordsTotal)
	reg.Unregister(m.minTimestamp)
	reg.Unregister(m.maxTimestamp)
}
