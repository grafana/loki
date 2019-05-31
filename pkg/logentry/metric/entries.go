package metric

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/promtail/api"
)

// LogSize observes log line size for each stream.
func LogSize(reg prometheus.Registerer, next api.EntryHandler) api.EntryHandler {
	cfg := HistogramConfig{
		Buckets: prometheus.ExponentialBuckets(16, 2, 8),
	}
	c, err := NewHistograms("log_entries_bytes", "the total count of bytes", cfg)
	if err != nil {
		panic(err)
	}
	reg.MustRegister(c)
	return api.EntryHandlerFunc(func(labels model.LabelSet, time time.Time, entry string) error {
		if err := next.Handle(labels, time, entry); err != nil {
			return err
		}
		c.With(labels).Observe(float64(len(entry)))
		return nil
	})
}
