package metric

import (
	"time"

	"github.com/grafana/loki/pkg/promtail/api"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
)

func LogCount(reg prometheus.Registerer) api.EntryHandler {
	c := NewCounters("log_entries_total", "the total count of log entries")
	reg.MustRegister(c)
	return api.EntryHandlerFunc(func(labels model.LabelSet, time time.Time, entry string) error {
		c.With(labels).Inc()
		return nil
	})
}

func LogSize(reg prometheus.Registerer) api.EntryHandler {
	c := NewCounters("log_entries_bytes", "the total count of bytes")
	reg.MustRegister(c)
	return api.EntryHandlerFunc(func(labels model.LabelSet, time time.Time, entry string) error {
		c.With(labels).Add(float64(len(entry)))
		return nil
	})
}
