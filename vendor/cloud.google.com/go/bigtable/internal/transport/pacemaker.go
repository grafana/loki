// Copyright 2026 Google LLC
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
	"log"
	"time"

	btopt "cloud.google.com/go/bigtable/internal/option"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Pacemaker monitors the runtime scheduling delay
// It measures the time difference between when a ticker was scheduled to fire
// and when the ticker actually fires.
type Pacemaker struct {
	meterProvider metric.MeterProvider
	logger        *log.Logger
	histogram     metric.Float64Histogram
	attrs         metric.MeasurementOption
}

// NewPacemaker creates a new Pacemaker and initializes its metrics.
func NewPacemaker(mp metric.MeterProvider, logger *log.Logger) *Pacemaker {
	pm := &Pacemaker{
		meterProvider: mp,
		logger:        logger,
		attrs:         metric.WithAttributes(attribute.String("executor", "goroutine")),
	}

	if mp == nil {
		return pm
	}

	// create meter
	meter := mp.Meter(clientMeterName)
	var err error
	// Buckets in microseconds (us).
	// Ranges cover: 0us, 100us, 500us, 1ms(1k), 2ms(2k), 5ms(5k), 10ms(10k),
	// 50ms(50k), 100ms(100k), 500ms(500k), 1s(1M).
	bounds := []float64{0, 100, 500, 1000, 2000, 5000, 10000, 50000, 100000, 500000, 1000000}

	pm.histogram, err = meter.Float64Histogram(
		"pacemaker_delays",
		metric.WithDescription("Distribution of delays between the scheduled time and actual execution time of the pacemaker goroutine."),
		metric.WithUnit("us"),
		metric.WithExplicitBucketBoundaries(bounds...),
	)
	if err != nil {
		btopt.Debugf(logger, "bigtable_connpool: failed to create pacemaker metric: %v", err)
	}

	return pm
}

// Start begins the pacemaker ticker.
func (p *Pacemaker) Start(ctx context.Context) {
	if p.histogram == nil {
		btopt.Debugf(p.logger, "bigtable_connpool: Pacemaker skipped (no histogram initialized)")
		return
	}

	go func() {
		interval := 100 * time.Millisecond
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		lastTick := time.Now()

		for {
			select {
			case t := <-ticker.C:
				actualInterval := t.Sub(lastTick)

				delay := actualInterval - interval
				if delay < 0 {
					delay = 0
				}

				delayUs := float64(delay.Nanoseconds()) / 1e3
				p.histogram.Record(ctx, delayUs, p.attrs)

				lastTick = t

			case <-ctx.Done():
				return
			}
		}
	}()
}

// Stop acts as a cleanup method. no-op
func (p *Pacemaker) Stop() {
}
