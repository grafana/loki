package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

//go:generate go run ../bindgen/bindgen.go github.com/grafana/loki/v3/pkg/plugins/metrics PluginMetrics
type PluginMetrics interface {
	NewCounter(name, help string, labelNames ...string) int32
	CounterAdd(index int32, value float64, labelValues ...string)
}

type HostPluginMetrics struct {
	reg      prometheus.Registerer
	counters []*prometheus.CounterVec
}

func NewHostPluginMetrics(reg prometheus.Registerer) *HostPluginMetrics {
	return &HostPluginMetrics{
		reg:      reg,
		counters: make([]*prometheus.CounterVec, 0),
	}
}

func (m *HostPluginMetrics) NewCounter(name, help string, labelNames ...string) int32 {
	index := len(m.counters)
	m.counters = append(m.counters, promauto.With(m.reg).NewCounterVec(prometheus.CounterOpts{
		Name: name,
		Help: help,
	}, labelNames))
	return int32(index)
}

func (m *HostPluginMetrics) CounterAdd(index int32, value float64, labelValues ...string) {
	m.counters[index].WithLabelValues(labelValues...).Add(value)
}
