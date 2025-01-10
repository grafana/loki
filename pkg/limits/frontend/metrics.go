package frontend

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type ServerMetrics struct {
	Requests        prometheus.Counter
	RequestFailures prometheus.Counter
}

func NewServerMetrics(r prometheus.Registerer) *ServerMetrics {
	return &ServerMetrics{
		Requests: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "limits_frontend_requests_total",
			Help: "The total number of requests to the limits-frontend",
		}),
		RequestFailures: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "limits_frontend_request_failures_total",
			Help: "The total number of failed requests to the limits-frontend",
		}),
	}
}

type IngestLimitsClientMetrics struct {
	Requests        prometheus.Counter
	RequestFailures prometheus.Counter
}

func NewIngestLimitsClientsMetrics(r prometheus.Registerer) *IngestLimitsClientMetrics {
	return &IngestLimitsClientMetrics{
		Requests: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "limits_frontend_proxied_requests_total",
			Help: "The total number of proxied requests to limits service backends",
		}),
		RequestFailures: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "limits_frontend_proxied_request_failures_total",
			Help: "The total number of failed proxied requests to limits service backends",
		}),
	}
}
