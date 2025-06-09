// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package deltatocumulativeprocessor // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/deltatocumulativeprocessor"

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/processor"

	"github.com/open-telemetry/opentelemetry-collector-contrib/internal/exp/metrics/identity"
	"github.com/open-telemetry/opentelemetry-collector-contrib/internal/exp/metrics/staleness"
	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/deltatocumulativeprocessor/internal/delta"
	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/deltatocumulativeprocessor/internal/metrics"
	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/deltatocumulativeprocessor/internal/telemetry"
)

var _ processor.Metrics = (*Processor)(nil)

type Processor struct {
	next consumer.Metrics
	cfg  Config

	last state
	mtx  sync.Mutex

	ctx    context.Context
	cancel context.CancelFunc

	stale staleness.Tracker
	tel   telemetry.Metrics
}

func newProcessor(cfg *Config, tel telemetry.Metrics, next consumer.Metrics) *Processor {
	ctx, cancel := context.WithCancel(context.Background())

	proc := Processor{
		next: next,
		cfg:  *cfg,
		last: state{
			nums: make(map[identity.Stream]pmetric.NumberDataPoint),
			hist: make(map[identity.Stream]pmetric.HistogramDataPoint),
			expo: make(map[identity.Stream]pmetric.ExponentialHistogramDataPoint),
		},
		ctx:    ctx,
		cancel: cancel,

		stale: staleness.NewTracker(),
		tel:   tel,
	}

	tel.WithTracked(proc.last.Len)
	cfg.Metrics(tel)

	return &proc
}

func (p *Processor) ConsumeMetrics(ctx context.Context, md pmetric.Metrics) error {
	p.mtx.Lock()
	defer p.mtx.Unlock()

	now := time.Now()

	const (
		keep = true
		drop = false
	)

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

			// if stream new and state capacity reached, reject
			exist := p.last.Has(id)
			if !exist && p.last.Len() >= p.cfg.MaxStreams {
				attrs.Set(telemetry.Error("limit"))
				return drop
			}

			// stream is ok and active, update stale tracker
			p.stale.Refresh(now, id)

			// this is the first sample of the stream. there is nothing to
			// aggregate with, so clone this value into the state and done
			if !exist {
				p.last.BeginWith(id, dp)
				return keep
			}

			// aggregate with state from previous requests.
			// delta.AccumulateInto(state, dp) stores result in `state`.
			// this is then copied into `dp` (the value passed onto the pipeline)
			var err error
			switch dp := dp.(type) {
			case pmetric.NumberDataPoint:
				state := p.last.nums[id]
				err = delta.AccumulateInto(state, dp)
				state.CopyTo(dp)
			case pmetric.HistogramDataPoint:
				state := p.last.hist[id]
				err = delta.AccumulateInto(state, dp)
				state.CopyTo(dp)
			case pmetric.ExponentialHistogramDataPoint:
				state := p.last.expo[id]
				err = delta.AccumulateInto(state, dp)
				state.CopyTo(dp)
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

func (p *Processor) Start(_ context.Context, _ component.Host) error {
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
					p.mtx.Lock()
					stale := p.stale.Collect(p.cfg.MaxStale)
					for _, id := range stale {
						p.last.Delete(id)
					}
					p.mtx.Unlock()
				}
			}
		}()
	}

	return nil
}

func (p *Processor) Shutdown(_ context.Context) error {
	p.cancel()
	return nil
}

func (p *Processor) Capabilities() consumer.Capabilities {
	return consumer.Capabilities{MutatesData: true}
}

// state keeps a cumulative value, aggregated over time, per stream
type state struct {
	nums map[identity.Stream]pmetric.NumberDataPoint
	hist map[identity.Stream]pmetric.HistogramDataPoint
	expo map[identity.Stream]pmetric.ExponentialHistogramDataPoint
}

func (m state) Len() int {
	return len(m.nums) + len(m.hist) + len(m.expo)
}

func (m state) Has(id identity.Stream) bool {
	_, nok := m.nums[id]
	_, hok := m.hist[id]
	_, eok := m.expo[id]
	return nok || hok || eok
}

func (m state) Delete(id identity.Stream) {
	delete(m.nums, id)
	delete(m.hist, id)
	delete(m.expo, id)
}

func (m state) BeginWith(id identity.Stream, dp any) {
	switch dp := dp.(type) {
	case pmetric.NumberDataPoint:
		m.nums[id] = pmetric.NewNumberDataPoint()
		dp.CopyTo(m.nums[id])
	case pmetric.HistogramDataPoint:
		m.hist[id] = pmetric.NewHistogramDataPoint()
		dp.CopyTo(m.hist[id])
	case pmetric.ExponentialHistogramDataPoint:
		m.expo[id] = pmetric.NewExponentialHistogramDataPoint()
		dp.CopyTo(m.expo[id])
	}
}
