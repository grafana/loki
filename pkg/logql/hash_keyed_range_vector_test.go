package logql

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

// drainProjectedValues returns, for every step with at least one series, the sorted strings of the
// labels the parent reads and the value of each series present at that step. A NaN is written as
// NaN, so that equal results compare equal.
func drainProjectedValues(t *testing.T, it RangeVectorIterator, keep LabelReads) map[int64][]string {
	t.Helper()
	out := map[int64][]string{}
	for it.Next() {
		ts, vec := it.At()
		for _, s := range vec.SampleVector() {
			b := labels.NewScratchBuilder(s.Metric.Len())
			s.Metric.Range(func(l labels.Label) {
				if keep.Reads(l.Name) {
					b.Add(l.Name, l.Value)
				}
			})
			out[ts] = append(out[ts], fmt.Sprintf("%s=%v", b.Labels().String(), s.F))
		}
		sort.Strings(out[ts])
	}
	require.NoError(t, it.Error())
	require.NoError(t, it.Close())
	return out
}

func TestHashKeyedBatchRangeVectorIterator_ShouldPresentTheValuesOfTheBatchIterator(t *testing.T) {
	sec := func(s int64) int64 { return s * int64(time.Second) }
	const errored = `{app="a", __error__="JSONParserErr", __error_details__="boom"}`

	for name, op := range map[string]string{
		"count_over_time":    `count_over_time({app="foo"}[%s])`,
		"rate":               `rate({app="foo"}[%s])`,
		"bytes_over_time":    `bytes_over_time({app="foo"}[%s])`,
		"bytes_rate":         `bytes_rate({app="foo"}[%s])`,
		"rate unwrapped":     `rate({app="foo"} | unwrap v [%s])`,
		"rate_counter":       `rate_counter({app="foo"} | unwrap v [%s])`,
		"sum_over_time":      `sum_over_time({app="foo"} | unwrap v [%s])`,
		"avg_over_time":      `avg_over_time({app="foo"} | unwrap v [%s])`,
		"max_over_time":      `max_over_time({app="foo"} | unwrap v [%s])`,
		"min_over_time":      `min_over_time({app="foo"} | unwrap v [%s])`,
		"stddev_over_time":   `stddev_over_time({app="foo"} | unwrap v [%s])`,
		"stdvar_over_time":   `stdvar_over_time({app="foo"} | unwrap v [%s])`,
		"quantile_over_time": `quantile_over_time(0.9, {app="foo"} | unwrap v [%s])`,
		"first_over_time":    `first_over_time({app="foo"} | unwrap v [%s])`,
		"last_over_time":     `last_over_time({app="foo"} | unwrap v [%s])`,
	} {
		for keepName, keep := range map[string]LabelReads{
			"no labels": noLabels(),
			"app":       onlyLabels("app"),
			"pod":       onlyLabels("pod", "missing"),
		} {
			t.Run(name+"/"+keepName, func(t *testing.T) {
				for seed := int64(0); seed < 150; seed++ {
					rnd := rand.New(rand.NewSource(seed))
					input := randomValuedSamples(rnd)
					for i := range input {
						switch rnd.Intn(25) {
						case 0:
							input[i].value = math.NaN()
						case 1:
							input[i].value = math.Inf(1)
						case 2:
							input[i].value = math.Inf(-1)
						}
					}
					if rnd.Intn(3) == 0 {
						input = append(input, valuedSample{labels: errored, timestamp: rnd.Int63n(sec(200)), value: 1})
					}

					step := sec(1 + rnd.Int63n(20))
					selRange := step + sec(rnd.Int63n(30))
					start := sec(rnd.Int63n(60))
					end := start + sec(1+rnd.Int63n(120))
					offset := sec(rnd.Int63n(3)) * 5
					expr := mustRangeAggregation(t, fmt.Sprintf(op, time.Duration(selRange)))

					want, err := newTimestampFirstRangeVectorIterator(newTimestampOrderedIterator(input), expr, selRange, step, start, end, offset, iteratorLimits{})
					require.NoError(t, err)
					got, err := newTimestampFirstHashKeyedRangeVectorIterator(newTimestampOrderedIterator(input), expr, keep, selRange, step, start, end, offset, iteratorLimits{})
					require.NoError(t, err)
					require.IsType(t, &hashKeyedBatchRangeVectorIterator{}, got)

					require.Equal(t, drainProjectedValues(t, want, keep), drainProjectedValues(t, got, keep), "seed %d", seed)
				}
			})
		}
	}
}

func TestHashKeyedBatchRangeVectorIterator_ShouldKeepOnlyTheLabelsTheParentReads(t *testing.T) {
	sec := func(s int64) int64 { return s * int64(time.Second) }
	expr := mustRangeAggregation(t, `max_over_time({app="a"} | unwrap v [10s])`)
	input := withValue([]labeledSample{{`{app="a", pod="1"}`, sec(5)}, {`{app="a", pod="2"}`, sec(6)}}, 3)

	for name, tc := range map[string]struct {
		keep LabelReads
		want []string
	}{
		"no labels": {keep: noLabels(), want: []string{`{}`, `{}`}},
		"one label": {keep: onlyLabels("pod"), want: []string{`{pod="1"}`, `{pod="2"}`}},
	} {
		t.Run(name, func(t *testing.T) {
			it, err := newTimestampFirstHashKeyedRangeVectorIterator(newTimestampOrderedIterator(input), expr, tc.keep, sec(10), sec(5), sec(10), sec(15), 0, iteratorLimits{})
			require.NoError(t, err)
			require.True(t, it.Next())
			_, vec := it.At()
			var got []string
			for _, s := range vec.SampleVector() {
				require.Equal(t, 3.0, s.F)
				got = append(got, s.Metric.String())
			}
			sort.Strings(got)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestHashKeyedBatchRangeVectorIterator_ShouldKeepAllTheLabelsOfErroredSeries(t *testing.T) {
	sec := func(s int64) int64 { return s * int64(time.Second) }
	const (
		clean   = `{app="a"}`
		errored = `{app="a", __error__="JSONParserErr", __error_details__="boom"}`
		// The text in a value is not an error.
		lookalike = `{app="a", msg="has __error__ in it"}`
	)
	expr := mustRangeAggregation(t, `max_over_time({app="a"} | json | unwrap v [10s])`)
	input := withValue([]labeledSample{{clean, sec(5)}, {errored, sec(6)}, {lookalike, sec(7)}}, 2)

	for name, keep := range map[string]LabelReads{"no labels": noLabels(), "app": onlyLabels("app")} {
		t.Run(name, func(t *testing.T) {
			it, err := newTimestampFirstHashKeyedRangeVectorIterator(newTimestampOrderedIterator(input), expr, keep, sec(10), sec(5), sec(10), sec(15), 0, iteratorLimits{})
			require.NoError(t, err)
			require.True(t, it.Next())
			_, vec := it.At()
			var got []string
			for _, s := range vec.SampleVector() {
				got = append(got, s.Metric.String())
			}
			sort.Strings(got)
			wantClean, wantLookalike := `{}`, `{}`
			if name == "app" {
				wantClean, wantLookalike = `{app="a"}`, `{app="a"}`
			}
			require.Equal(t, []string{`{__error__="JSONParserErr", __error_details__="boom", app="a"}`, wantClean, wantLookalike}, got)
		})
	}
}

func TestHashKeyedBatchRangeVectorIterator_ShouldFailOnSeriesPastTheLimitLikeTheBatchIterator(t *testing.T) {
	sec := func(s int64) int64 { return s * int64(time.Second) }
	expr := mustRangeAggregation(t, `max_over_time({app="a"} | unwrap v [10s])`)

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
		"series that differ only in labels the parent does not read": {
			samples:   []labeledSample{{`{app="a", pod="1"}`, sec(6)}, {`{app="a", pod="2"}`, sec(7)}, {`{app="a", pod="3"}`, sec(8)}},
			maxSeries: 2,
			wantErr:   true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			input := withValue(tc.samples, 1)
			start := sec(max(tc.start, 5))
			limits := iteratorLimits{maxSeries: tc.maxSeries}

			batch, err := newTimestampFirstRangeVectorIterator(newTimestampOrderedIterator(input), expr, sec(10), sec(5), start, sec(50), 0, limits)
			require.NoError(t, err)
			hashKeyed, err := newTimestampFirstHashKeyedRangeVectorIterator(newTimestampOrderedIterator(input), expr, noLabels(), sec(10), sec(5), start, sec(50), 0, limits)
			require.NoError(t, err)

			for _, it := range []RangeVectorIterator{batch, hashKeyed} {
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

func TestHashKeyedBatchRangeVectorIterator_ShouldFallBackToTheStreamingIteratorWithoutOverlap(t *testing.T) {
	sec := func(s int64) int64 { return s * int64(time.Second) }
	expr := mustRangeAggregation(t, `max_over_time({app="a"} | unwrap v [5s])`)
	input := withValue([]labeledSample{{`{app="a"}`, sec(6)}}, 1)

	it, err := newTimestampFirstHashKeyedRangeVectorIterator(newTimestampOrderedIterator(input), expr, noLabels(), sec(5), sec(30), sec(10), sec(100), 0, iteratorLimits{})
	require.NoError(t, err)
	require.IsType(t, &streamRangeVectorIterator{}, it)
}

func TestHashKeyedBatchRangeVectorIterator_ShouldFailOnAnUnsupportedOperationLikeTheBatchIterator(t *testing.T) {
	expr := &syntax.RangeAggregationExpr{Operation: "unsupported"}
	_, err := newTimestampFirstRangeVectorIterator(newTimestampOrderedIterator(nil), expr, 10, 5, 0, 50, 0, iteratorLimits{})
	require.Error(t, err)
	_, err = newTimestampFirstHashKeyedRangeVectorIterator(newTimestampOrderedIterator(nil), expr, noLabels(), 10, 5, 0, 50, 0, iteratorLimits{})
	require.Error(t, err)
}

// BenchmarkHashKeyedBatchRangeVectorIterator compares the batch iterator with the hash-keyed one
// over many series, for avg(max_over_time(...)), which reads values but no label.
func BenchmarkHashKeyedBatchRangeVectorIterator(b *testing.B) {
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
	input := make([]valuedSample, 0, numSeries*numSteps*5)
	for ts := int64(0); ts < numSteps*step; ts += sampleStep {
		for i, l := range lbs {
			input = append(input, valuedSample{labels: l, timestamp: ts + 1, value: float64(i)})
		}
	}
	expr := &syntax.RangeAggregationExpr{Operation: syntax.OpRangeTypeMax}
	end := (numSteps - 1) * step

	for name, newIterator := range map[string]func(it iter.PeekingSampleIterator) (RangeVectorIterator, error){
		"batch": func(it iter.PeekingSampleIterator) (RangeVectorIterator, error) {
			return newTimestampFirstRangeVectorIterator(it, expr, selRange, step, 0, end, 0, iteratorLimits{})
		},
		"hash keyed": func(it iter.PeekingSampleIterator) (RangeVectorIterator, error) {
			return newTimestampFirstHashKeyedRangeVectorIterator(it, expr, noLabels(), selRange, step, 0, end, 0, iteratorLimits{})
		},
		"hash keyed by app": func(it iter.PeekingSampleIterator) (RangeVectorIterator, error) {
			return newTimestampFirstHashKeyedRangeVectorIterator(it, expr, onlyLabels("app"), selRange, step, 0, end, 0, iteratorLimits{})
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
