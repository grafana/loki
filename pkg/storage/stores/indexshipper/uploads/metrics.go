package uploads

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	statusFailure = "failure"
	statusSuccess = "success"
)

type metrics struct {
	tablesUploadOperationTotal *prometheus.CounterVec
}

func newMetrics(r prometheus.Registerer) *metrics {
	return &metrics{
		tablesUploadOperationTotal: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: "loki_boltdb_shipper",
			Name:      "tables_upload_operation_total",
			Help:      "Total number of upload operations done by status",
		}, []string{"status"}),
	}
}
