// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package gate

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	promgate "github.com/prometheus/prometheus/pkg/gate"
)

// Gate is an interface that mimics prometheus/pkg/gate behavior.
type Gate interface {
	Start(ctx context.Context) error
	Done()
}

// Gate wraps the Prometheus gate with extra metrics.
type gate struct {
	g *promgate.Gate
	m *metrics
}

type metrics struct {
	inflightQueries prometheus.Gauge
	gateTiming      prometheus.Histogram
}

// Keeper is used to create multiple gates sharing the same metrics.
type Keeper struct {
	m *metrics
}

// NewKeeper creates a new Keeper.
func NewKeeper(reg prometheus.Registerer) *Keeper {
	return &Keeper{
		m: &metrics{
			inflightQueries: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
				Name: "gate_queries_in_flight",
				Help: "Number of queries that are currently in flight.",
			}),
			gateTiming: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
				Name:    "gate_duration_seconds",
				Help:    "How many seconds it took for queries to wait at the gate.",
				Buckets: []float64{0.01, 0.1, 0.3, 0.6, 1, 3, 6, 9, 20, 30, 60, 90, 120, 240, 360, 720},
			}),
		},
	}
}

// NewGate returns a new Gate that collects metrics.
func (k *Keeper) NewGate(maxConcurrent int) Gate {
	return &gate{g: promgate.New(maxConcurrent), m: k.m}
}

// Start initiates a new request and waits until it's our turn to fulfill a request.
func (g *gate) Start(ctx context.Context) error {
	start := time.Now()
	defer func() {
		g.m.gateTiming.Observe(time.Since(start).Seconds())
	}()

	if err := g.g.Start(ctx); err != nil {
		return err
	}

	g.m.inflightQueries.Inc()
	return nil
}

// Done finishes a query.
func (g *gate) Done() {
	g.m.inflightQueries.Dec()
	g.g.Done()
}
