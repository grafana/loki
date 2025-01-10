package frontend

import (
	"github.com/prometheus/client_golang/prometheus"
)

type IngestLimitsClientMetrics struct {
}

func NewIngestLimitsClientsMetrics(r prometheus.Registerer) *IngestLimitsClientMetrics {
	return &IngestLimitsClientMetrics{}
}
