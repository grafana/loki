package logql

import (
	"github.com/grafana/loki/pkg/iter"
	"github.com/prometheus/prometheus/promql"
)

// RangeVectorAggregator aggregates samples for a given range of samples.
// It receives the current milliseconds timestamp and the list of point within
// the range.
type RangeVectorAggregator func(int64, []promql.Point) float64

// RangeVectorIterator iterates through a range of samples.
// To fetch the current vector use `At` with a `RangeVectorAggregator`.
type RangeVectorIterator interface {
	Next() bool
	At(aggregator RangeVectorAggregator) (int64, promql.Vector)
	Close() error
}

type rangeVectorIterator struct {
	iter                         iter.PeekingEntryIterator
	selRange, step, end, current int64
	window                       map[string]*promql.Series
}

func newRangeVectorIterator(
	it iter.EntryIterator,
	selRange, step, start, end int64) *rangeVectorIterator {
	// forces at least one step.
	if step == 0 {
		step = 1
	}
	return &rangeVectorIterator{
		iter:     iter.NewPeekingIterator(it),
		step:     step,
		end:      end,
		selRange: selRange,
		current:  start - step, // first loop iteration will set it to start
		window:   map[string]*promql.Series{},
	}
}

func (r *rangeVectorIterator) Next() bool {
	// slides the range window to the next position
	r.current = r.current + r.step
	if r.current > r.end {
		return false
	}
	rangeEnd := r.current
	rangeStart := r.current - r.selRange
	// load samples
	r.popBack(rangeStart)
	r.load(rangeStart, rangeEnd)
	return true
}

func (r *rangeVectorIterator) Close() error {
	return r.iter.Close()
}

// popBack removes all entries out of the current window from the back.
func (r *rangeVectorIterator) popBack(newStart int64) {
	// possible improvement: if there is no overlap we can just remove all.
	for fp := range r.window {
		lastPoint := 0
		for i, p := range r.window[fp].Points {
			if p.T <= newStart {
				lastPoint = i
				continue
			}
			break
		}
		r.window[fp].Points = r.window[fp].Points[lastPoint+1:]
		if len(r.window[fp].Points) == 0 {
			delete(r.window, fp)
		}
	}
}

// load the next sample range window.
func (r *rangeVectorIterator) load(start, end int64) {
	for lbs, entry, hasNext := r.iter.Peek(); hasNext; lbs, entry, hasNext = r.iter.Peek() {
		if entry.Timestamp.UnixNano() > end {
			// not consuming the iterator as this belong to another range.
			return
		}
		// the lower bound of the range is not inclusive
		if entry.Timestamp.UnixNano() <= start {
			_ = r.iter.Next()
			continue
		}
		// adds the sample.
		var series *promql.Series
		var ok bool
		series, ok = r.window[lbs]
		if !ok {
			series = &promql.Series{
				Points: []promql.Point{},
			}
			r.window[lbs] = series
		}
		series.Points = append(series.Points, promql.Point{
			T: entry.Timestamp.UnixNano(),
			V: 1,
		})
		_ = r.iter.Next()
	}
}

func (r *rangeVectorIterator) At(aggregator RangeVectorAggregator) (int64, promql.Vector) {
	result := make([]promql.Sample, 0, len(r.window))
	// convert ts from nano to milli seconds as the iterator work with nanoseconds
	ts := r.current / 1e+6
	for lbs, series := range r.window {
		labels, err := promql.ParseMetric(lbs)
		if err != nil {
			continue
		}

		result = append(result, promql.Sample{
			Point: promql.Point{
				V: aggregator(ts, series.Points),
				T: ts,
			},
			Metric: labels,
		})

	}
	return ts, result
}
