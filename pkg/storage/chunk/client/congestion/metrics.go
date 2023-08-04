package congestion

import "github.com/prometheus/client_golang/prometheus"

type Metrics struct {
	currentLimit    prometheus.Gauge
	backoffTimeSec  prometheus.Counter
	retries         prometheus.Counter
	retriesExceeded prometheus.Counter
}

func (m Metrics) Unregister() {
	prometheus.Unregister(m.currentLimit)
}

// NewMetrics creates metrics to be used for monitoring congestion control.
// It needs to accept a "name" because congestion control is used in object clients, and there can be many object clients
// creates for the same store (multiple period configs, etc). It is the responsibility of the caller to ensure uniqueness,
// otherwise a duplicate registration panic will occur.
func NewMetrics(name string, cfg Config) *Metrics {
	labels := map[string]string{
		"strategy": cfg.Controller.Strategy,
		"name":     name,
	}

	m := Metrics{
		currentLimit: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace:   "loki",
			Subsystem:   "store",
			Name:        "congestion_control_limit",
			Help:        "Current per-second request limit to control congestion",
			ConstLabels: labels,
		}),
		backoffTimeSec: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace:   "loki",
			Subsystem:   "store",
			Name:        "congestion_control_backoff_time_seconds_total",
			Help:        "How much time is spent backing off once throughput limit is encountered",
			ConstLabels: labels,
		}),
		retries: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace:   "loki",
			Subsystem:   "store",
			Name:        "congestion_control_retries_total",
			Help:        "How many retries occurred",
			ConstLabels: labels,
		}),
		retriesExceeded: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace:   "loki",
			Subsystem:   "store",
			Name:        "congestion_control_retries_exceeded_total",
			Help:        "How many times the number of retries exceeded the configured limit.",
			ConstLabels: labels,
		}),
	}

	prometheus.MustRegister(m.currentLimit)
	prometheus.MustRegister(m.backoffTimeSec)
	prometheus.MustRegister(m.retries)
	prometheus.MustRegister(m.retriesExceeded)
	return &m
}
