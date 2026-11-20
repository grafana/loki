package logql

import (
	"fmt"
	"strings"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"

	"github.com/grafana/loki/v3/pkg/iter"
	logqlerr "github.com/grafana/loki/v3/pkg/logql/errors"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

// hashKeyedRangeIterator describes a range iterator that is keyed on the hash of the label string
// of each series, for a parent that reads the values of the series and keeps them apart but reads
// no label, or only some. enabled is false for any other parent. keep is the labels that the
// parent reads.
type hashKeyedRangeIterator struct {
	enabled bool
	keep    LabelReads
}

// hashKeyedBatchRangeVectorIterator is the batch range-vector iterator for a parent that reads the
// values of every series, such as avg or stddev over the series of max_over_time, and either no
// label or only some of them. It aggregates each series as the batch iterator does, but keeps the
// series by the hash of their label string, and only the labels that the parent reads in place of
// the parsed labels. Series that carry __error__ keep all their parsed labels, since the
// evaluator fails the query on them unless it was asked to keep errored lines.
type hashKeyedBatchRangeVectorIterator struct {
	iter                                 iter.PeekingSampleIterator
	selRange, step, end, current, offset int64
	hasher                               seriesHasher
	keep                                 LabelReads
	builder                              labels.ScratchBuilder
	window                               map[seriesKey]*promql.Series
	// metrics holds every series seen so far, as the batch iterator does, so that the series limit
	// counts the same series.
	metrics map[seriesKey]labels.Labels
	at      []promql.Sample
	agg     BatchRangeVectorAggregator
	limits  iteratorLimits
	err     error
}

// newTimestampFirstHashKeyedRangeVectorIterator returns the range-vector iterator of expr for a
// parent that reads the values of its series and only the labels in keep, which must be bounded:
// none or an only set. The input iterator must return samples in global timestamp order. Without
// overlapping windows it returns the streaming iterator, which keeps no points either.
func newTimestampFirstHashKeyedRangeVectorIterator(
	it iter.PeekingSampleIterator,
	expr *syntax.RangeAggregationExpr,
	keep LabelReads,
	selRange, step, start, end, offset int64,
	limits iteratorLimits,
) (RangeVectorIterator, error) {
	if !windowsOverlap(selRange, step, start, end) {
		return newTimestampFirstRangeVectorIterator(it, expr, selRange, step, start, end, offset, limits)
	}
	vectorAggregator, err := aggregator(expr)
	if err != nil {
		return nil, err
	}
	// forces at least one step.
	if step == 0 {
		step = 1
	}
	if offset != 0 {
		start = start - offset
		end = end - offset
	}
	return &hashKeyedBatchRangeVectorIterator{
		iter:     it,
		step:     step,
		end:      end,
		selRange: selRange,
		hasher:   newSeriesHasher(),
		keep:     keep,
		builder:  labels.NewScratchBuilder(0),
		window:   map[seriesKey]*promql.Series{},
		metrics:  map[seriesKey]labels.Labels{},
		agg:      vectorAggregator,
		current:  start - step, // first loop iteration will set it to start
		offset:   offset,
		limits:   limits,
	}, nil
}

func (r *hashKeyedBatchRangeVectorIterator) Next() bool {
	if r.err != nil {
		return false
	}
	// slides the range window to the next position
	r.current = r.current + r.step
	if r.current > r.end {
		return false
	}
	rangeEnd := r.current
	rangeStart := rangeEnd - r.selRange
	popExpiredPoints(r.window, rangeStart)
	r.load(rangeStart, rangeEnd)
	return r.err == nil
}

func (r *hashKeyedBatchRangeVectorIterator) Close() error {
	return r.iter.Close()
}

func (r *hashKeyedBatchRangeVectorIterator) Error() error {
	if r.err != nil {
		return r.err
	}
	return r.iter.Err()
}

// metric returns the labels to keep for the series with label string lbs: all the labels of a
// series that carries __error__, and otherwise only those that the parent reads.
func (r *hashKeyedBatchRangeVectorIterator) metric(lbs string) (labels.Labels, error) {
	// A label value that contains the text parses to a series without the label, which costs a
	// parse and nothing else.
	if r.keep.None() && !strings.Contains(lbs, logqlerr.ErrorLabel) {
		return labels.EmptyLabels(), nil
	}
	metric, err := syntax.ParseMetric(lbs)
	if err != nil {
		return labels.EmptyLabels(), fmt.Errorf("failed to parse metric %q: %w", lbs, err)
	}
	if metric.Has(logqlerr.ErrorLabel) {
		return metric, nil
	}
	r.builder.Reset()
	metric.Range(func(l labels.Label) {
		if r.keep.Reads(l.Name) {
			r.builder.Add(l.Name, l.Value)
		}
	})
	return r.builder.Labels(), nil
}

// load the next sample range window.
func (r *hashKeyedBatchRangeVectorIterator) load(start, end int64) {
	for lbs, sample, hasNext := r.iter.Peek(); hasNext; lbs, sample, hasNext = r.iter.Peek() {
		if sample.Timestamp > end {
			// not consuming the iterator as this belong to another range.
			return
		}
		// the lower bound of the range is not inclusive
		if sample.Timestamp <= start {
			_ = r.iter.Next()
			continue
		}
		key := r.hasher.key(lbs)
		series, ok := r.window[key]
		if !ok {
			metric, ok := r.metrics[key]
			if !ok {
				if r.err = r.limits.validateMaxSeries(len(r.metrics) + 1); r.err != nil {
					return
				}
				var err error
				if metric, err = r.metric(lbs); err != nil {
					r.err = err
					return
				}
				r.metrics[key] = metric
			}
			series = getSeries()
			series.Metric = metric
			r.window[key] = series
		}
		series.Floats = append(series.Floats, promql.FPoint{T: sample.Timestamp, F: sample.Value})
		_ = r.iter.Next()
	}
}

func (r *hashKeyedBatchRangeVectorIterator) At() (int64, StepResult) {
	if r.at == nil {
		r.at = make([]promql.Sample, 0, len(r.window))
	}
	r.at = r.at[:0]
	// convert ts from nano to milli seconds as the iterator work with nanoseconds
	ts := (r.current + r.offset) / 1e+6
	for _, series := range r.window {
		r.at = append(r.at, promql.Sample{
			F:      r.agg(series.Floats, r.current),
			T:      ts,
			Metric: series.Metric,
		})
	}
	return ts, SampleVector(r.at)
}
