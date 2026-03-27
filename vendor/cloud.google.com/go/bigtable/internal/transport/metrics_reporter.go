// Copyright 2025 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package internal

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	btopt "cloud.google.com/go/bigtable/internal/option"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	// outstandingRPCsMetricName is the name for the outstanding RPCs histogram.
	outstandingRPCsMetricName = "connection_pool/outstanding_rpcs"
	// perConnErrorCountMetricName is the name for the per-connection error count histogram.
	perConnErrorCountMetricName = "per_connection_error_count"
	// clientMeterName is the prefix for metrics name
	clientMeterName = "bigtable.googleapis.com/internal/client/"
)

// MetricsReporter periodically collects and reports metrics for the connection pool.
type MetricsReporter struct {
	config                btopt.MetricsReporterConfig
	connPoolStatsSupplier connPoolStatsSupplier
	logger                *log.Logger
	ticker                *time.Ticker
	done                  chan struct{}
	stopOnce              sync.Once

	// OpenTelemetry metric instruments
	meterProvider                    metric.MeterProvider
	outstandingRPCsHistogram         metric.Float64Histogram
	perConnectionErrorCountHistogram metric.Float64Histogram
}

// NewMetricsReporter starts a monitor to export periodic metrics
func NewMetricsReporter(config btopt.MetricsReporterConfig, connPoolStatsSupplier connPoolStatsSupplier, logger *log.Logger, mp metric.MeterProvider) (*MetricsReporter, error) {
	mr := &MetricsReporter{
		config:                config,
		connPoolStatsSupplier: connPoolStatsSupplier,
		logger:                logger,
		done:                  make(chan struct{}),
		meterProvider:         mp,
	}

	if !config.Enabled {
		// Stats Disabled, return error during ctor
		return nil, fmt.Errorf("bigtable_connpool: MetricsReporterConfig.Enabled is false")
	}

	if mp == nil {
		// State: Stats Enabled but MeterProvider is nil, return error during ctor
		return nil, fmt.Errorf("bigtable_connpool: MetricsReporterConfig.Enabled is true, but MeterProvider is nil")
	}

	// create meter
	meter := mp.Meter(clientMeterName)
	var err error

	// fail if meter cannot be created.
	mr.outstandingRPCsHistogram, err = meter.Float64Histogram(outstandingRPCsMetricName, metric.WithDescription("Distribution of outstanding RPCs per connection."), metric.WithUnit("1"))
	if err != nil {
		return nil, fmt.Errorf("bigtable_connpool: failed to create %s histogram: %w", outstandingRPCsMetricName, err)
	}

	mr.perConnectionErrorCountHistogram, err = meter.Float64Histogram(perConnErrorCountMetricName, metric.WithDescription("Distribution of errors per connection per minute."), metric.WithUnit("1"))
	if err != nil {
		return nil, fmt.Errorf("bigtable_connpool: failed to create %s histogram: %w", perConnErrorCountMetricName, err)
	}

	return mr, nil
}

// Start starts the reporter.
func (mr *MetricsReporter) Start(ctx context.Context) {
	// config.Enabled should ensure that all relevant thing (meterProvider and meter should exists)
	if !mr.config.Enabled {
		return
	}

	mr.ticker = time.NewTicker(mr.config.ReportingInterval)
	go func() {
		defer mr.ticker.Stop()
		for {
			select {
			case <-mr.ticker.C:
				mr.snapshotAndRecordMetrics(ctx)
			case <-mr.done:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
}

// Stop stops the reporter gracefully
func (mr *MetricsReporter) Stop() {
	mr.stopOnce.Do(func() {
		if mr.config.Enabled {
			close(mr.done)
		}
	})
}

// snapshotAndRecordMetrics collects and records metrics for the current state of the connection pool.
func (mr *MetricsReporter) snapshotAndRecordMetrics(ctx context.Context) {
	stats := mr.connPoolStatsSupplier()
	if len(stats) == 0 {
		return
	}

	for _, stat := range stats {
		// Record per-connection error count for the interval
		if mr.perConnectionErrorCountHistogram != nil {
			mr.perConnectionErrorCountHistogram.Record(ctx, float64(stat.ErrorCount))
		}
		transportType := "cloudpath"
		if stat.IsALTSUsed {
			transportType = "directpath"
		}

		// Common attributes for this connection
		baseAttrs := []attribute.KeyValue{
			attribute.String("transport_type", transportType),
			attribute.String("lb_policy", stat.LBPolicy),
		}

		if mr.outstandingRPCsHistogram != nil {
			// Record distribution sample for unary load
			unaryAttrs := attribute.NewSet(append(baseAttrs, attribute.Bool("streaming", false))...)
			mr.outstandingRPCsHistogram.Record(ctx, float64(stat.OutstandingUnaryLoad), metric.WithAttributeSet(unaryAttrs))

			// Record distribution sample for streaming load
			streamingAttrs := attribute.NewSet(append(baseAttrs, attribute.Bool("streaming", true))...)
			mr.outstandingRPCsHistogram.Record(ctx, float64(stat.OutstandingStreamingLoad), metric.WithAttributeSet(streamingAttrs))
		}

	}
}
