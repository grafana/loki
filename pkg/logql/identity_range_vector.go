package logql

import (
	"fmt"
	"hash/maphash"
	"strings"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"

	"github.com/grafana/loki/v3/pkg/iter"
	logqlerr "github.com/grafana/loki/v3/pkg/logql/errors"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

// seriesKey identifies a series by two 64-bit hashes of its label string, taken with different
// seeds. The seeds are random for each iterator, so a collision is as unlikely as one of a 128-bit
// hash, and a query cannot be written to force one.
type seriesKey struct {
	hi, lo uint64
}

// identityBatchRangeVectorIterator is the batch range-vector iterator for a parent that reads only
// which series exist at each step: no labels, no values, and no merging of series. It keeps the
// last timestamp of each series in place of its parsed labels and points, and returns one sample
// without labels for each series in the window. Series that carry __error__ keep their parsed
// labels, since the evaluator fails the query on them unless it was asked to keep errored lines.
type identityBatchRangeVectorIterator struct {
	iter                                 iter.PeekingSampleIterator
	selRange, step, end, current, offset int64
	seeds                                [2]maphash.Seed
	// seen holds every series seen so far, as the batch iterator does with its metrics, so that
	// the series limit counts the same series.
	seen map[seriesKey]struct{}
	// lastSeen holds the series with a sample in the current window, with the timestamp of their
	// last one. A series leaves it once that sample leaves the window, so a step costs the series
	// it presents and not every series the query has seen.
	lastSeen map[seriesKey]int64
	errored  map[seriesKey]labels.Labels
	at       []promql.Sample
	limits   iteratorLimits
	err      error
}

// newTimestampFirstIdentityRangeVectorIterator returns the range-vector iterator of expr for a
// parent that reads only which series exist. The input iterator must return samples in global
// timestamp order. Without overlapping windows it returns the streaming iterator, which keeps no
// points either.
func newTimestampFirstIdentityRangeVectorIterator(
	it iter.PeekingSampleIterator,
	expr *syntax.RangeAggregationExpr,
	selRange, step, start, end, offset int64,
	limits iteratorLimits,
) (RangeVectorIterator, error) {
	if !windowsOverlap(selRange, step, start, end) {
		return newTimestampFirstRangeVectorIterator(it, expr, selRange, step, start, end, offset, limits)
	}
	// forces at least one step.
	if step == 0 {
		step = 1
	}
	if offset != 0 {
		start = start - offset
		end = end - offset
	}
	return &identityBatchRangeVectorIterator{
		iter:     it,
		step:     step,
		end:      end,
		selRange: selRange,
		seeds:    [2]maphash.Seed{maphash.MakeSeed(), maphash.MakeSeed()},
		seen:     map[seriesKey]struct{}{},
		lastSeen: map[seriesKey]int64{},
		errored:  map[seriesKey]labels.Labels{},
		current:  start - step, // first loop iteration will set it to start
		offset:   offset,
		limits:   limits,
	}, nil
}

func (r *identityBatchRangeVectorIterator) Next() bool {
	if r.err != nil {
		return false
	}
	// slides the range window to the next position
	r.current = r.current + r.step
	if r.current > r.end {
		return false
	}
	r.load(r.current-r.selRange, r.current)
	return r.err == nil
}

func (r *identityBatchRangeVectorIterator) Close() error {
	return r.iter.Close()
}

func (r *identityBatchRangeVectorIterator) Error() error {
	if r.err != nil {
		return r.err
	}
	return r.iter.Err()
}

// key returns the seriesKey of the series with label string lbs.
func (r *identityBatchRangeVectorIterator) key(lbs string) seriesKey {
	return seriesKey{hi: maphash.String(r.seeds[0], lbs), lo: maphash.String(r.seeds[1], lbs)}
}

// load the next sample range window.
func (r *identityBatchRangeVectorIterator) load(start, end int64) {
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
		key := r.key(lbs)
		if _, ok := r.seen[key]; !ok {
			if r.err = r.limits.validateMaxSeries(len(r.seen) + 1); r.err != nil {
				return
			}
			r.seen[key] = struct{}{}
			// A label value that contains the text parses to a series without the label, which
			// costs a parse and nothing else.
			if strings.Contains(lbs, logqlerr.ErrorLabel) {
				metric, err := syntax.ParseMetric(lbs)
				if err != nil {
					r.err = fmt.Errorf("failed to parse metric %q: %w", lbs, err)
					return
				}
				r.errored[key] = metric
			}
		}
		// The input is in timestamp order, so the latest sample is the last one.
		r.lastSeen[key] = sample.Timestamp
		_ = r.iter.Next()
	}
}

func (r *identityBatchRangeVectorIterator) At() (int64, StepResult) {
	if r.at == nil {
		r.at = make([]promql.Sample, 0, len(r.lastSeen))
	}
	r.at = r.at[:0]
	rangeStart := r.current - r.selRange
	// convert ts from nano to milli seconds as the iterator work with nanoseconds
	ts := (r.current + r.offset) / 1e+6
	for key, last := range r.lastSeen {
		if last <= rangeStart {
			// The window only moves forward, so the series cannot come back into it.
			delete(r.lastSeen, key)
			continue
		}
		metric, ok := r.errored[key]
		if !ok {
			metric = labels.EmptyLabels()
		}
		r.at = append(r.at, promql.Sample{F: 1, T: ts, Metric: metric})
	}
	return ts, SampleVector(r.at)
}
