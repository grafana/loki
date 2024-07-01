package congestion

import (
	"github.com/grafana/loki/v3/pkg/util/constants"

	"github.com/prometheus/client_golang/prometheus"
)

type Metrics struct {
	currentLimit       prometheus.Gauge
	backoffSec         prometheus.Counter
	requests           prometheus.Counter
	retries            prometheus.Counter
	nonRetryableErrors prometheus.Counter
	retriesExceeded    prometheus.Counter
}

func (m Metrics) Unregister() {
	prometheus.Unregister(m.currentLimit)
	prometheus.Unregister(m.backoffSec)
	prometheus.Unregister(m.requests)
	prometheus.Unregister(m.retries)
	prometheus.Unregister(m.nonRetryableErrors)
	prometheus.Unregister(m.retriesExceeded)
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

	const namespace = constants.Loki
	const subsystem = "store_congestion_control"
	m := Metrics{
		currentLimit: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace:   namespace,
			Subsystem:   subsystem,
			Name:        "limit",
			Help:        "Current per-second request limit to control congestion",
			ConstLabels: labels,
		}),
		backoffSec: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace:   namespace,
			Subsystem:   subsystem,
			Name:        "backoff_seconds_total",
			Help:        "How much time is spent backing off once throughput limit is encountered",
			ConstLabels: labels,
		}),
		requests: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace:   namespace,
			Subsystem:   subsystem,
			Name:        "requests_total",
			Help:        "How many requests were issued to the store",
			ConstLabels: labels,
		}),
		retries: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace:   namespace,
			Subsystem:   subsystem,
			Name:        "retries_total",
			Help:        "How many retries occurred",
			ConstLabels: labels,
		}),
		nonRetryableErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace:   namespace,
			Subsystem:   subsystem,
			Name:        "non_retryable_errors_total",
			Help:        "How many request errors occurred which could not be retried",
			ConstLabels: labels,
		}),
		retriesExceeded: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace:   namespace,
			Subsystem:   subsystem,
			Name:        "retries_exceeded_total",
			Help:        "How many times the number of retries exceeded the configured limit.",
			ConstLabels: labels,
		}),
	}

	prometheus.MustRegister(m.currentLimit)
	prometheus.MustRegister(m.backoffSec)
	prometheus.MustRegister(m.requests)
	prometheus.MustRegister(m.retries)
	prometheus.MustRegister(m.nonRetryableErrors)
	prometheus.MustRegister(m.retriesExceeded)
	return &m
}
