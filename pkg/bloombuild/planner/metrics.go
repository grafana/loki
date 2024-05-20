package planner

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	metricsNamespace = "loki"
	metricsSubsystem = "bloomplanner"
)

type Metrics struct {
	running prometheus.Gauge
}

func NewMetrics(r prometheus.Registerer) *Metrics {
	return &Metrics{
		running: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Subsystem: metricsSubsystem,
			Name:      "running",
			Help:      "Value will be 1 if bloom planner is currently running on this instance",
		}),
	}
}
