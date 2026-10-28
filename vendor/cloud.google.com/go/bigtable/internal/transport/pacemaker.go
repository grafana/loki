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

// pacemakerInterval is how often the pacemaker goroutine wakes up. It only
// sets the sampling rate: the delay reported per tick is independent of it.
const pacemakerInterval = 100 * time.Millisecond

// Pacemaker monitors the runtime scheduling delay. It measures the time
// between when a tick was scheduled to fire and when the pacemaker goroutine
// was actually scheduled to handle it, which is a proxy for how contended the
// process is -- for CPU (an undersized GOMAXPROCS, or CFS throttling against a
// cgroup quota), or for the scheduler generally (a stop-the-world pause).
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
		ticker := time.NewTicker(pacemakerInterval)
		defer ticker.Stop()

		for {
			select {
			case scheduled := <-ticker.C:
				p.recordTick(ctx, scheduled, time.Now())

			case <-ctx.Done():
				return
			}
		}
	}()
}

// recordTick records how late this goroutine was to handle a tick that was
// scheduled for the given time.
//
// The value a ticker sends on its channel is not the time of delivery: the
// runtime reconstructs the time the tick was *due* (time.sendTime sends
// Now().Add(-delta)). So the gap between that and the moment we get around to
// reading it is exactly the delay we want to report -- the time the goroutine
// spent waiting for a P, plus any time the tick sat in the channel buffer.
//
// Comparing consecutive tick timestamps instead would measure nothing: due
// times are one interval apart by construction, so the difference is exactly
// the interval unless the runtime drops a tick entirely. That only registers
// starvation longer than a whole interval, and only in interval-sized steps.
func (p *Pacemaker) recordTick(ctx context.Context, scheduled, now time.Time) {
	delay := now.Sub(scheduled)
	if delay < 0 {
		delay = 0
	}
	p.histogram.Record(ctx, float64(delay)/float64(time.Microsecond), p.attrs)
}

// Stop acts as a cleanup method. no-op
func (p *Pacemaker) Stop() {
}
