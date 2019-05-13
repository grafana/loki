package metric

import (
	"time"

	"github.com/grafana/loki/pkg/promtail/api"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
)

// LogCount counts logs line for each stream.
func LogCount(reg prometheus.Registerer, next api.EntryHandler) api.EntryHandler {
	c := NewCounters("log_entries_total", "the total count of log entries")
	reg.MustRegister(c)
	return api.EntryHandlerFunc(func(labels model.LabelSet, time time.Time, entry string) error {
		if err := next.Handle(labels, time, entry); err != nil {
			return err
		}
		c.With(labels).Inc()
		return nil
	})
}

// LogSize observes log size for each stream.
func LogSize(reg prometheus.Registerer, next api.EntryHandler) api.EntryHandler {
	c := NewHistograms("log_entries_bytes", "the total count of bytes", prometheus.ExponentialBuckets(16, 2, 8))
	reg.MustRegister(c)
	return api.EntryHandlerFunc(func(labels model.LabelSet, time time.Time, entry string) error {
		if err := next.Handle(labels, time, entry); err != nil {
			return err
		}
		c.With(labels).Observe(float64(len(entry)))
		return nil
	})
}
