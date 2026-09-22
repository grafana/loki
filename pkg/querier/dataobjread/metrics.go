package dataobjread

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// metrics are the read path's metrics. One set is shared by every query a store serves.
type Metrics struct {
	taskWaitSeconds prometheus.Counter
	taskScanSeconds prometheus.Counter
}

func NewMetrics(reg prometheus.Registerer) *Metrics {
	return &Metrics{
		taskWaitSeconds: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_querier_dataobj_read_task_wait_seconds_total",
			Help: "Total time data-object section readers spent waiting for the read planner to produce the next task. Meaningful as a share of loki_querier_dataobj_read_task_scan_seconds_total, not on its own: a rising share means section resolution cannot keep up with scanning.",
		}),
		taskScanSeconds: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_querier_dataobj_read_task_scan_seconds_total",
			Help: "Total time data-object section readers spent scanning logs sections.",
		}),
	}
}
