package congestion

import (
	"github.com/grafana/loki/v3/pkg/util/constants"

	"github.com/prometheus/client_golang/prometheus"
)

type Metrics struct {
	reg                prometheus.Registerer
	currentLimit       prometheus.Gauge
	backoffSec         prometheus.Counter
	requests           prometheus.Counter
	retries            prometheus.Counter
	nonRetryableErrors prometheus.Counter
	retriesExceeded    prometheus.Counter
}

func (m Metrics) Unregister() {
	if m.reg == nil {
		return
	}
	m.reg.Unregister(m.currentLimit)
	m.reg.Unregister(m.backoffSec)
	m.reg.Unregister(m.requests)
	m.reg.Unregister(m.retries)
	m.reg.Unregister(m.nonRetryableErrors)
	m.reg.Unregister(m.retriesExceeded)
}

// NewMetrics creates metrics to be used for monitoring congestion control.
// name must be unique per store/period because many object clients can exist
// in one process. A second registration of the same name is ignored so tests
// can construct clients more than once against a shared registerer.
// If reg is nil, collectors are created but not registered.
func NewMetrics(name string, cfg Config, reg prometheus.Registerer) *Metrics {
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

	m.reg = reg
	registerCollector(reg, m.currentLimit)
	registerCollector(reg, m.backoffSec)
	registerCollector(reg, m.requests)
	registerCollector(reg, m.retries)
	registerCollector(reg, m.nonRetryableErrors)
	registerCollector(reg, m.retriesExceeded)
	return &m
}

func registerCollector(reg prometheus.Registerer, c prometheus.Collector) {
	if reg == nil {
		return
	}
	if err := reg.Register(c); err != nil {
		if _, ok := err.(prometheus.AlreadyRegisteredError); !ok {
			panic(err)
		}
	}
}
