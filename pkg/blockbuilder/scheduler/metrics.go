package scheduler

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type Metrics struct {
	lag             *prometheus.GaugeVec
	committedOffset *prometheus.GaugeVec
}

func NewMetrics(reg prometheus.Registerer) *Metrics {
	return &Metrics{
		lag: promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
			Name: "loki_block_scheduler_group_lag",
			Help: "How far behind the block scheduler consumer group is from the latest offset.",
		}, []string{"partition"}),
		committedOffset: promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
			Name: "loki_block_scheduler_group_committed_offset",
			Help: "The current offset the block scheduler consumer group is at.",
		}, []string{"partition"}),
	}
}
