// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package deltatocumulativeprocessor // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/deltatocumulativeprocessor"

import (
	"context"
	"sync"
	"time"

	"github.com/puzpuzpuz/xsync/v4"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/processor"

	"github.com/open-telemetry/opentelemetry-collector-contrib/internal/exp/metrics/identity"
	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/deltatocumulativeprocessor/internal/data"
	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/deltatocumulativeprocessor/internal/delta"
	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/deltatocumulativeprocessor/internal/maps"
	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/deltatocumulativeprocessor/internal/metrics"
	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/deltatocumulativeprocessor/internal/telemetry"
)

var _ processor.Metrics = (*deltaToCumulativeProcessor)(nil)

type deltaToCumulativeProcessor struct {
	next consumer.Metrics
	cfg  Config

	last state
	aggr data.Aggregator

	ctx    context.Context
	cancel context.CancelFunc

	stale *xsync.Map[identity.Stream, time.Time]
	tel   telemetry.Metrics
}

func newProcessor(cfg *Config, tel telemetry.Metrics, next consumer.Metrics) *deltaToCumulativeProcessor {
	ctx, cancel := context.WithCancel(context.Background())

	limit := maps.Limit(int64(cfg.MaxStreams))
	proc := deltaToCumulativeProcessor{
		next: next,
		cfg:  *cfg,
		last: state{
			ctx:  limit,
			nums: maps.New[identity.Stream, *mutex[pmetric.NumberDataPoint]](limit),
			hist: maps.New[identity.Stream, *mutex[pmetric.HistogramDataPoint]](limit),
			expo: maps.New[identity.Stream, *mutex[pmetric.ExponentialHistogramDataPoint]](limit),
		},
		aggr:   delta.Aggregator{Aggregator: new(data.Adder)},
		ctx:    ctx,
		cancel: cancel,

		stale: xsync.NewMap[identity.Stream, time.Time](),
		tel:   tel,
	}

	tel.WithTracked(proc.last.Size)
	cfg.Metrics(tel)

	return &proc
}

type vals struct {
	nums *mutex[pmetric.NumberDataPoint]
	hist *mutex[pmetric.HistogramDataPoint]
	expo *mutex[pmetric.ExponentialHistogramDataPoint]
}

func (p *deltaToCumulativeProcessor) ConsumeMetrics(ctx context.Context, md pmetric.Metrics) error {
	now := time.Now()

	const (
		keep = true
		drop = false
	)

	zero := vals{
		nums: guard(pmetric.NewNumberDataPoint()),
		hist: guard(pmetric.NewHistogramDataPoint()),
		expo: guard(pmetric.NewExponentialHistogramDataPoint()),
	}

	metrics.Filter(md, func(m metrics.Metric) bool {
		if m.AggregationTemporality() != pmetric.AggregationTemporalityDelta {
			return keep
		}

		// aggregate the datapoints.
		// using filter here, as the pmetric.*DataPoint are reference types so
		// we can modify them using their "value".
		m.Filter(func(id identity.Stream, dp any) bool {
			// count the processed datatype.
			// uses whatever value of attrs has at return-time
			var attrs telemetry.Attributes
			defer func() { p.tel.Datapoints().Inc(ctx, attrs...) }()

			var err error
			switch dp := dp.(type) {
			case pmetric.NumberDataPoint:
				last, loaded := p.last.nums.LoadOrStore(id, zero.nums)
				if maps.Exceeded(last, loaded) {
					// state is full, reject stream
					attrs.Set(telemetry.Error("limit"))
					return drop
				}

				// stream is ok and active, update stale tracker
				p.stale.Store(id, now)

				if !loaded {
					// cached zero was stored, alloc new one
					zero.nums = guard(pmetric.NewNumberDataPoint())
				}

				last.use(func(last pmetric.NumberDataPoint) {
					err = p.aggr.Numbers(last, dp)
					last.CopyTo(dp)
				})
			case pmetric.HistogramDataPoint:
				last, loaded := p.last.hist.LoadOrStore(id, zero.hist)
				if maps.Exceeded(last, loaded) {
					// state is full, reject stream
					attrs.Set(telemetry.Error("limit"))
					return drop
				}

				// stream is ok and active, update stale tracker
				p.stale.Store(id, now)

				if !loaded {
					// cached zero was stored, alloc new one
					zero.hist = guard(pmetric.NewHistogramDataPoint())
				}

				last.use(func(last pmetric.HistogramDataPoint) {
					err = p.aggr.Histograms(last, dp)
					last.CopyTo(dp)
				})
			case pmetric.ExponentialHistogramDataPoint:
				last, loaded := p.last.expo.LoadOrStore(id, zero.expo)
				if maps.Exceeded(last, loaded) {
					// state is full, reject stream
					attrs.Set(telemetry.Error("limit"))
					return drop
				}

				// stream is ok and active, update stale tracker
				p.stale.Store(id, now)

				if !loaded {
					// cached zero was stored, alloc new one
					zero.expo = guard(pmetric.NewExponentialHistogramDataPoint())
				}

				last.use(func(last pmetric.ExponentialHistogramDataPoint) {
					err = p.aggr.Exponential(last, dp)
					last.CopyTo(dp)
				})
			}

			if err != nil {
				attrs.Set(telemetry.Cause(err))
				return drop
			}

			return keep
		})

		// all remaining datapoints of this metric are now cumulative
		m.Typed().SetAggregationTemporality(pmetric.AggregationTemporalityCumulative)
		// if no datapoints remain, drop empty metric
		return m.Typed().Len() > 0
	})

	// no need to continue pipeline if we dropped all metrics
	if md.MetricCount() == 0 {
		return nil
	}
	return p.next.ConsumeMetrics(ctx, md)
}

func (p *deltaToCumulativeProcessor) Start(_ context.Context, _ component.Host) error {
	if p.cfg.MaxStale != 0 {
		// delete stale streams once per minute
		go func() {
			tick := time.NewTicker(time.Minute)
			defer tick.Stop()
			for {
				select {
				case <-p.ctx.Done():
					return
				case <-tick.C:
					now := time.Now()
					p.stale.Range(func(id identity.Stream, last time.Time) bool {
						if now.Sub(last) > p.cfg.MaxStale {
							p.last.nums.LoadAndDelete(id)
							p.last.hist.LoadAndDelete(id)
							p.last.expo.LoadAndDelete(id)
							p.stale.Delete(id)
						}
						return true
					})
				}
			}
		}()
	}

	return nil
}

func (p *deltaToCumulativeProcessor) Shutdown(_ context.Context) error {
	p.cancel()
	return nil
}

func (*deltaToCumulativeProcessor) Capabilities() consumer.Capabilities {
	return consumer.Capabilities{MutatesData: true}
}

// state keeps a cumulative value, aggregated over time, per stream
type state struct {
	ctx  maps.Context
	nums *maps.Parallel[identity.Stream, *mutex[pmetric.NumberDataPoint]]
	hist *maps.Parallel[identity.Stream, *mutex[pmetric.HistogramDataPoint]]
	expo *maps.Parallel[identity.Stream, *mutex[pmetric.ExponentialHistogramDataPoint]]
}

func (s state) Size() int {
	return int(s.ctx.Size())
}

type mutex[T any] struct {
	mtx sync.Mutex
	v   T
}

func (mtx *mutex[T]) use(do func(T)) {
	mtx.mtx.Lock()
	do(mtx.v)
	mtx.mtx.Unlock()
}

func guard[T any](v T) *mutex[T] {
	return &mutex[T]{v: v}
}
