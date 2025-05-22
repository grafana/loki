package engine

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/util/constants"
)

var (
	status               = "status"
	statusSuccess        = "success"
	statusFailure        = "failure"
	statusNotImplemented = "notimplemented"
)

// NOTE: Metrics are subject to rapid change!
type metrics struct {
	subqueries       *prometheus.CounterVec
	logicalPlanning  prometheus.Histogram
	physicalPlanning prometheus.Histogram
	execution        prometheus.Histogram
}

func newMetrics(r prometheus.Registerer) *metrics {
	subsystem := "engine_v2"
	return &metrics{
		subqueries: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Subsystem: subsystem,
			Name:      "subqueries",
		}, []string{status}),
		logicalPlanning: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Subsystem: subsystem,
			Name:      "logical_planning",
		}),
		physicalPlanning: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Subsystem: subsystem,
			Name:      "physical_planning",
		}),
		execution: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Subsystem: subsystem,
			Name:      "execution",
		}),
	}
}
