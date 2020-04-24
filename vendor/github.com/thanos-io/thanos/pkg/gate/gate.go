// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package gate

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/prometheus/pkg/gate"
)

type Gater interface {
	IsMyTurn(ctx context.Context) error
	Done()
}

// Gate wraps the Prometheus gate with extra metrics.
type Gate struct {
	g               *gate.Gate
	inflightQueries prometheus.Gauge
	gateTiming      prometheus.Histogram
}

// NewGate returns a new query gate.
func NewGate(maxConcurrent int, reg prometheus.Registerer) *Gate {
	g := &Gate{
		g: gate.New(maxConcurrent),
		inflightQueries: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Name: "gate_queries_in_flight",
			Help: "Number of queries that are currently in flight.",
		}),
		gateTiming: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name:    "gate_duration_seconds",
			Help:    "How many seconds it took for queries to wait at the gate.",
			Buckets: []float64{0.01, 0.1, 0.3, 0.6, 1, 3, 6, 9, 20, 30, 60, 90, 120, 240, 360, 720},
		}),
	}

	return g
}

// IsMyTurn iniates a new query and waits until it's our turn to fulfill a query request.
func (g *Gate) IsMyTurn(ctx context.Context) error {
	start := time.Now()
	defer func() {
		g.gateTiming.Observe(time.Since(start).Seconds())
	}()

	if err := g.g.Start(ctx); err != nil {
		return err
	}

	g.inflightQueries.Inc()
	return nil
}

// Done finishes a query.
func (g *Gate) Done() {
	g.inflightQueries.Dec()
	g.g.Done()
}
