package logql

import (
	"fmt"
	"math/rand"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/iter"
	logqlerr "github.com/grafana/loki/v3/pkg/logql/errors"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

// drainSeriesPresence returns, for every step with at least one series, the metrics of the
// series present at that step, sorted. Values are left out, since the identity-only iterator
// does not compute them.
func drainSeriesPresence(t *testing.T, it RangeVectorIterator) map[int64][]string {
	t.Helper()
	out := map[int64][]string{}
	for it.Next() {
		ts, vec := it.At()
		for _, s := range vec.SampleVector() {
			out[ts] = append(out[ts], s.Metric.String())
		}
		sort.Strings(out[ts])
	}
	require.NoError(t, it.Error())
	require.NoError(t, it.Close())
	return out
}

// drainSeriesCounts returns the number of series present at every step with at least one series.
func drainSeriesCounts(t *testing.T, it RangeVectorIterator) map[int64]int {
	t.Helper()
	out := map[int64]int{}
	for ts, series := range drainSeriesPresence(t, it) {
		out[ts] = len(series)
	}
	return out
}

func TestIdentityBatchRangeVectorIterator_ShouldPresentTheSeriesOfTheBatchIterator(t *testing.T) {
	sec := func(s int64) int64 { return s * int64(time.Second) }
	const errored = `{app="a", __error__="JSONParserErr", __error_details__="boom"}`

	for name, op := range map[string]string{
		"count_over_time": `count_over_time({app="foo"}[%s])`,
		"rate":            `rate({app="foo"}[%s])`,
		"bytes_over_time": `bytes_over_time({app="foo"}[%s])`,
	} {
		t.Run(name, func(t *testing.T) {
			for seed := int64(0); seed < 200; seed++ {
				rnd := rand.New(rand.NewSource(seed))
				input := randomValuedSamples(rnd)
				if rnd.Intn(3) == 0 {
					input = append(input, valuedSample{labels: errored, timestamp: rnd.Int63n(sec(200)), value: 1})
				}

				// The windows overlap, so the batch iterator is the one to compare with.
				step := sec(1 + rnd.Int63n(20))
				selRange := step + sec(rnd.Int63n(30))
				start := sec(rnd.Int63n(60))
				end := start + sec(1+rnd.Int63n(120))
				offset := sec(rnd.Int63n(3)) * 5
				expr := mustRangeAggregation(t, fmt.Sprintf(op, time.Duration(selRange)))

				want, err := newTimestampFirstRangeVectorIterator(newTimestampOrderedIterator(input), expr, selRange, step, start, end, offset, iteratorLimits{})
				require.NoError(t, err)
				got, err := newTimestampFirstIdentityRangeVectorIterator(newTimestampOrderedIterator(input), expr, selRange, step, start, end, offset, iteratorLimits{})
				require.NoError(t, err)

				wantCounts, gotCounts := drainSeriesCounts(t, want), drainSeriesCounts(t, got)
				require.Equal(t, wantCounts, gotCounts, "seed %d", seed)
			}
		})
	}
}

func TestIdentityBatchRangeVectorIterator_ShouldKeepTheLabelsOfErroredSeriesOnly(t *testing.T) {
	sec := func(s int64) int64 { return s * int64(time.Second) }
	const (
		clean   = `{app="a"}`
		errored = `{app="a", __error__="JSONParserErr", __error_details__="boom"}`
	)
	expr := mustRangeAggregation(t, `count_over_time({app="a"} | json [10s])`)
	input := withValue([]labeledSample{{clean, sec(5)}, {errored, sec(6)}}, 1)

	it, err := newTimestampFirstIdentityRangeVectorIterator(newTimestampOrderedIterator(input), expr, sec(10), sec(5), sec(10), sec(15), 0, iteratorLimits{})
	require.NoError(t, err)

	got := drainSeriesPresence(t, it)
	require.Equal(t, map[int64][]string{
		10_000: {`{__error__="JSONParserErr", __error_details__="boom", app="a"}`, `{}`},
		15_000: {`{__error__="JSONParserErr", __error_details__="boom", app="a"}`},
	}, got)

	require.Contains(t, got[10_000][0], logqlerr.ErrorLabel)
}

func TestIdentityBatchRangeVectorIterator_ShouldDropASeriesOnceItsLastSampleLeavesTheWindow(t *testing.T) {
	sec := func(s int64) int64 { return s * int64(time.Second) }
	expr := mustRangeAggregation(t, `count_over_time({app="a"}[10s])`)
	input := withValue([]labeledSample{{`{app="a"}`, sec(5)}, {`{app="b"}`, sec(8)}}, 1)

	it, err := newTimestampFirstIdentityRangeVectorIterator(newTimestampOrderedIterator(input), expr, sec(10), sec(5), sec(5), sec(25), 0, iteratorLimits{})
	require.NoError(t, err)

	// The lower bound of the window is not inclusive, so a sample at 5s is out at 15s.
	require.Equal(t, map[int64]int{
		5_000:  1,
		10_000: 2,
		15_000: 1,
	}, drainSeriesCounts(t, it))
}

func TestIdentityBatchRangeVectorIterator_ShouldFailOnSeriesPastTheLimitLikeTheBatchIterator(t *testing.T) {
	sec := func(s int64) int64 { return s * int64(time.Second) }
	expr := mustRangeAggregation(t, `count_over_time({app="a"}[10s])`)

	for name, tc := range map[string]struct {
		samples   []labeledSample
		maxSeries int
		start     int64
		wantErr   bool
	}{
		"at the limit": {
			samples:   []labeledSample{{`{app="a"}`, sec(6)}, {`{app="b"}`, sec(7)}},
			maxSeries: 2,
		},
		"past the limit": {
			samples:   []labeledSample{{`{app="a"}`, sec(6)}, {`{app="b"}`, sec(7)}, {`{app="c"}`, sec(8)}},
			maxSeries: 2,
			wantErr:   true,
		},
		"past the limit, counting series that left the window": {
			samples:   []labeledSample{{`{app="a"}`, sec(6)}, {`{app="b"}`, sec(26)}, {`{app="c"}`, sec(46)}},
			maxSeries: 2,
			wantErr:   true,
		},
		"samples before the window are not series": {
			samples:   []labeledSample{{`{app="a"}`, sec(1)}, {`{app="b"}`, sec(2)}, {`{app="c"}`, sec(8)}},
			maxSeries: 1,
			start:     15,
		},
	} {
		t.Run(name, func(t *testing.T) {
			input := withValue(tc.samples, 1)
			start := sec(max(tc.start, 5))
			limits := iteratorLimits{maxSeries: tc.maxSeries}

			batch, err := newTimestampFirstRangeVectorIterator(newTimestampOrderedIterator(input), expr, sec(10), sec(5), start, sec(50), 0, limits)
			require.NoError(t, err)
			identity, err := newTimestampFirstIdentityRangeVectorIterator(newTimestampOrderedIterator(input), expr, sec(10), sec(5), start, sec(50), 0, limits)
			require.NoError(t, err)

			for _, it := range []RangeVectorIterator{batch, identity} {
				for it.Next() {
					_, _ = it.At()
				}
				if tc.wantErr {
					require.ErrorContains(t, it.Error(), "maximum number of series (2)")
				} else {
					require.NoError(t, it.Error())
				}
			}
		})
	}
}

func TestIdentityBatchRangeVectorIterator_ShouldFallBackToTheStreamingIteratorWithoutOverlap(t *testing.T) {
	sec := func(s int64) int64 { return s * int64(time.Second) }
	expr := mustRangeAggregation(t, `count_over_time({app="a"}[5s])`)
	input := withValue([]labeledSample{{`{app="a"}`, sec(6)}}, 1)

	it, err := newTimestampFirstIdentityRangeVectorIterator(newTimestampOrderedIterator(input), expr, sec(5), sec(30), sec(10), sec(100), 0, iteratorLimits{})
	require.NoError(t, err)
	require.IsType(t, &streamRangeVectorIterator{}, it)
}

// BenchmarkIdentityBatchRangeVectorIterator compares the batch iterator with the identity-only one
// over many series, for count(count_over_time(...)), which reads only which series exist.
func BenchmarkIdentityBatchRangeVectorIterator(b *testing.B) {
	const (
		numSeries  = 20_000
		numSteps   = 12
		selRange   = int64(time.Minute)
		step       = int64(5 * time.Second)
		sampleStep = int64(time.Second)
	)
	lbs := make([]string, numSeries)
	for i := range lbs {
		lbs[i] = fmt.Sprintf(`{app="app-%d", cluster="prod-eu-west-1", namespace="team-%d", pod="app-%d-7c9d8f6b5-x%04d", container="main"}`, i%500, i%50, i%500, i)
	}
	// The samples come in global timestamp order, as the timestamp-first iterator requires.
	input := make([]valuedSample, 0, numSeries*numSteps*5)
	for ts := int64(0); ts < numSteps*step; ts += sampleStep {
		for _, l := range lbs {
			input = append(input, valuedSample{labels: l, timestamp: ts + 1, value: 1})
		}
	}
	expr := &syntax.RangeAggregationExpr{Operation: syntax.OpRangeTypeCount}
	end := (numSteps - 1) * step

	for name, newIterator := range map[string]func(it iter.PeekingSampleIterator) (RangeVectorIterator, error){
		"batch": func(it iter.PeekingSampleIterator) (RangeVectorIterator, error) {
			return newTimestampFirstRangeVectorIterator(it, expr, selRange, step, 0, end, 0, iteratorLimits{})
		},
		"identity only": func(it iter.PeekingSampleIterator) (RangeVectorIterator, error) {
			return newTimestampFirstIdentityRangeVectorIterator(it, expr, selRange, step, 0, end, 0, iteratorLimits{})
		},
	} {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				b.StopTimer()
				it, err := newIterator(newValuedSampleIterator(input))
				require.NoError(b, err)
				b.StartTimer()
				for it.Next() {
					_, _ = it.At()
				}
				require.NoError(b, it.Error())
			}
		})
	}
}
