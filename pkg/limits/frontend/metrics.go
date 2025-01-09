package frontend

import (
	"github.com/prometheus/client_golang/prometheus"
)

type Metrics struct{}

func NewMetrics(_ prometheus.Registerer) *Metrics {
	return &Metrics{}
}
