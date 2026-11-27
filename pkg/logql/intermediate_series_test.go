package logql

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

func TestIntermediateSeriesCounter_ShouldTrackCurrentAndPeak(t *testing.T) {
	c := newIntermediateSeriesCounter(0)

	require.NoError(t, c.Reserve(3))
	require.NoError(t, c.Reserve(2))
	require.Equal(t, int64(5), c.Current())
	require.Equal(t, int64(5), c.Peak())

	c.Release(4)
	require.Equal(t, int64(1), c.Current())
	require.Equal(t, int64(5), c.Peak())

	require.NoError(t, c.Reserve(1))
	require.Equal(t, int64(2), c.Current())
	require.Equal(t, int64(5), c.Peak())
}

func TestIntermediateSeriesCounter_ShouldNeverFailWithoutALimit(t *testing.T) {
	c := newIntermediateSeriesCounter(0)

	require.NoError(t, c.Reserve(1_000_000))
	require.Equal(t, int64(1_000_000), c.Peak())
}

func TestIntermediateSeriesCounter_ShouldFailWithoutCountingAReservationOverTheLimit(t *testing.T) {
	c := newIntermediateSeriesCounter(3)

	require.NoError(t, c.Reserve(3))
	require.ErrorIs(t, c.Reserve(1), errIntermediateSeriesLimit)
	require.Equal(t, int64(3), c.Current())
	require.Equal(t, int64(3), c.Peak())

	c.Release(1)
	require.NoError(t, c.Reserve(1))
}

func TestIntermediateSeriesCounter_ShouldCountNothingWhenNil(t *testing.T) {
	var c *intermediateSeriesCounter

	require.NoError(t, c.Reserve(5))
	c.Release(5)
	require.Zero(t, c.Current())
	require.Zero(t, c.Peak())
}

func TestIntermediateSeriesCounter_ShouldBeSafeForConcurrentUse(t *testing.T) {
	const (
		workers = 8
		each    = 1000
	)
	c := newIntermediateSeriesCounter(0)

	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range each {
				require.NoError(t, c.Reserve(1))
			}
		}()
	}
	wg.Wait()

	require.Equal(t, int64(workers*each), c.Current())
	require.Equal(t, int64(workers*each), c.Peak())
}

func TestIntermediateSeriesCounter_ShouldNotRejectAReservationThatFitsUnderContention(t *testing.T) {
	const (
		workers = 8
		each    = 50_000
	)
	c := newIntermediateSeriesCounter(1)

	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range each {
				require.ErrorIs(t, c.Reserve(2), errIntermediateSeriesLimit)
			}
		}()
	}

	// Only this goroutine ever holds a reservation, so there is always room for it.
	for range each {
		require.NoError(t, c.Reserve(1))
		c.Release(1)
	}
	wg.Wait()

	require.Zero(t, c.Current())
	require.Equal(t, int64(1), c.Peak(), "a rejected reservation must not move the peak")
}

func TestIntermediateSeriesCounterFromContext_ShouldReturnTheCounterOfTheContext(t *testing.T) {
	c := newIntermediateSeriesCounter(0)

	require.Same(t, c, intermediateSeriesCounterFromContext(withIntermediateSeriesCounter(context.Background(), c)))
	require.Nil(t, intermediateSeriesCounterFromContext(context.Background()))
}

// repeatedSeriesIterator returns an iterator over n series with a sample at each of the timestamps.
func repeatedSeriesIterator(n int, timestamps ...time.Time) iter.PeekingSampleIterator {
	series := make([]iter.SampleIterator, 0, n)
	for i := range n {
		lbs := labels.FromStrings("app", "a", "id", fmt.Sprint(i))
		samples := make([]logproto.Sample, 0, len(timestamps))
		for j, ts := range timestamps {
			samples = append(samples, logproto.Sample{Timestamp: ts.UnixNano(), Hash: uint64(j), Value: 1})
		}
		series = append(series, iter.NewSeriesIterator(logproto.Series{
			Labels:     lbs.String(),
			Samples:    samples,
			StreamHash: labels.StableHash(lbs),
		}))
	}
	return iter.NewPeekingSampleIterator(iter.NewTimestampFirstSortSampleIterator(series))
}

// drain steps the iterator to the end of the query and returns its error.
func drain(it RangeVectorIterator) error {
	for it.Next() {
		it.At()
	}
	return it.Error()
}

func TestBatchRangeVectorIterator_ShouldCountEachNewLabelSetOnce(t *testing.T) {
	var (
		start    = time.Unix(10, 0)
		end      = time.Unix(30, 0)
		step     = 5 * time.Second
		selRange = (10 * time.Second).Nanoseconds()
		count    = &syntax.RangeAggregationExpr{Operation: syntax.OpRangeTypeCount}
	)

	for name, tc := range map[string]struct {
		build func(it iter.PeekingSampleIterator, limits iteratorLimits) (RangeVectorIterator, error)
	}{
		"batch": {
			build: func(it iter.PeekingSampleIterator, limits iteratorLimits) (RangeVectorIterator, error) {
				return newTimestampFirstRangeVectorIterator(it, count, selRange, step.Nanoseconds(), start.UnixNano(), end.UnixNano(), 0, limits)
			},
		},
		"first with timestamp": {
			build: func(it iter.PeekingSampleIterator, limits iteratorLimits) (RangeVectorIterator, error) {
				return newFirstWithTimestampIterator(it, selRange, step.Nanoseconds(), start.UnixNano(), end.UnixNano(), 0, limits), nil
			},
		},
		"last with timestamp": {
			build: func(it iter.PeekingSampleIterator, limits iteratorLimits) (RangeVectorIterator, error) {
				return newLastWithTimestampIterator(it, selRange, step.Nanoseconds(), start.UnixNano(), end.UnixNano(), 0, limits), nil
			},
		},
		"quantile sketch": {
			build: func(it iter.PeekingSampleIterator, limits iteratorLimits) (RangeVectorIterator, error) {
				return newQuantileSketchIterator(it, selRange, step.Nanoseconds(), start.UnixNano(), end.UnixNano(), 0, limits), nil
			},
		},
		"count distinct": {
			build: func(it iter.PeekingSampleIterator, limits iteratorLimits) (RangeVectorIterator, error) {
				return newCountDistinctIterator(it, selRange, step.Nanoseconds(), start.UnixNano(), end.UnixNano(), 0, false, limits), nil
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			for caseName, c := range map[string]struct {
				it   iter.PeekingSampleIterator
				want int64
			}{
				"repeated label sets count once": {
					it:   repeatedSeriesIterator(3, start.Add(time.Second), start.Add(6*time.Second), start.Add(11*time.Second), start.Add(16*time.Second)),
					want: 3,
				},
				"churning label sets count each new one": {
					it:   distinctSeriesIterator(7, start.Add(time.Second)),
					want: 7,
				},
				"empty windows count nothing": {
					it:   iter.NewPeekingSampleIterator(iter.NoopSampleIterator),
					want: 0,
				},
			} {
				t.Run(caseName, func(t *testing.T) {
					counter := newIntermediateSeriesCounter(0)
					rangeIt, err := tc.build(c.it, iteratorLimits{counter: counter})
					require.NoError(t, err)

					require.NoError(t, drain(rangeIt))
					require.Equal(t, c.want, counter.Peak())
					require.Equal(t, c.want, counter.Current(), "the batch iterator never prunes its label sets")
				})
			}
		})
	}
}

func TestNewTimestampFirstRangeAggEvaluator_ShouldCountAgainstTheCounterOfTheContext(t *testing.T) {
	var (
		start  = time.Unix(10, 0)
		params = LiteralParams{start: start, end: time.Unix(30, 0), step: 5 * time.Second}
	)

	for name, op := range map[string]string{
		"count":                syntax.OpRangeTypeCount,
		"first with timestamp": syntax.OpRangeTypeFirstWithTimestamp,
		"last with timestamp":  syntax.OpRangeTypeLastWithTimestamp,
		"quantile sketch":      syntax.OpRangeTypeQuantileSketch,
	} {
		t.Run(name, func(t *testing.T) {
			counter := newIntermediateSeriesCounter(0)
			ctx := withIntermediateSeriesCounter(context.Background(), counter)
			expr := &syntax.RangeAggregationExpr{Left: &syntax.LogRangeExpr{Interval: 10 * time.Second}, Operation: op}

			ev, err := newTimestampFirstRangeAggEvaluator(ctx, distinctSeriesIterator(4, start.Add(time.Second)), expr, params, 0, false, iteratorLimits{})
			require.NoError(t, err)
			defer ev.Close()
			for {
				if next, _, _ := ev.Next(); !next {
					break
				}
			}
			require.NoError(t, ev.Error())

			require.Equal(t, int64(4), counter.Peak())
		})
	}
}

func TestDefaultEvaluator_ShouldCountBothLegsOfABinaryOperationAgainstOneCounter(t *testing.T) {
	const numSeries = 3
	var (
		start = time.Unix(60, 0)
		end   = time.Unix(120, 0)
	)
	expr, err := syntax.ParseSampleExpr(`sum(count_over_time({app="a"}[1m]) + rate({app="a"}[1m]))`)
	require.NoError(t, err)
	params, err := NewLiteralParams(expr.String(), start, end, 30*time.Second, 0, logproto.FORWARD, 1000, nil, nil)
	require.NoError(t, err)

	counter := newIntermediateSeriesCounter(0)
	ctx := withIntermediateSeriesCounter(user.InjectOrgID(context.Background(), "fake"), counter)
	ev := NewDefaultEvaluator(NewMockQuerier(1, distinctStreams(numSeries, end)), 0, 0, NoLimits)
	stepEv, err := ev.NewStepEvaluator(ctx, ev, expr, params, RootContext(expr, 0))
	require.NoError(t, err)
	defer stepEv.Close()
	for {
		if next, _, _ := stepEv.Next(); !next {
			break
		}
	}
	require.NoError(t, stepEv.Error())

	require.Equal(t, int64(2*numSeries), counter.Peak())
}

func TestBatchRangeVectorIterator_ShouldNotCountALabelSetThatFailsToParse(t *testing.T) {
	var (
		start = time.Unix(10, 0)
		end   = time.Unix(30, 0)
		count = &syntax.RangeAggregationExpr{Operation: syntax.OpRangeTypeCount}
	)
	it := iter.NewPeekingSampleIterator(iter.NewSeriesIterator(logproto.Series{
		Labels:     "not a label set",
		Samples:    []logproto.Sample{{Timestamp: start.Add(time.Second).UnixNano(), Value: 1}},
		StreamHash: 1,
	}))

	counter := newIntermediateSeriesCounter(0)
	rangeIt, err := newTimestampFirstRangeVectorIterator(it, count, (10 * time.Second).Nanoseconds(), (5 * time.Second).Nanoseconds(), start.UnixNano(), end.UnixNano(), 0, iteratorLimits{counter: counter})
	require.NoError(t, err)

	require.ErrorContains(t, drain(rangeIt), "failed to parse metric")
	require.Zero(t, counter.Current())
	require.Zero(t, counter.Peak())
}

func TestBatchRangeVectorIterator_ShouldNotRaiseThePeakForALabelSetThatFailsToParseAfterValidOnes(t *testing.T) {
	var (
		start = time.Unix(10, 0)
		end   = time.Unix(30, 0)
		count = &syntax.RangeAggregationExpr{Operation: syntax.OpRangeTypeCount}
	)
	good := iter.NewSeriesIterator(logproto.Series{
		Labels:     `{app="a"}`,
		Samples:    []logproto.Sample{{Timestamp: start.Add(time.Second).UnixNano(), Value: 1}},
		StreamHash: 1,
	})
	bad := iter.NewSeriesIterator(logproto.Series{
		Labels:     "not a label set",
		Samples:    []logproto.Sample{{Timestamp: start.Add(2 * time.Second).UnixNano(), Value: 1}},
		StreamHash: 2,
	})
	it := iter.NewPeekingSampleIterator(iter.NewTimestampFirstSortSampleIterator([]iter.SampleIterator{good, bad}))

	counter := newIntermediateSeriesCounter(0)
	rangeIt, err := newTimestampFirstRangeVectorIterator(it, count, (10 * time.Second).Nanoseconds(), (5 * time.Second).Nanoseconds(), start.UnixNano(), end.UnixNano(), 0, iteratorLimits{counter: counter})
	require.NoError(t, err)

	require.ErrorContains(t, drain(rangeIt), "failed to parse metric")
	require.Equal(t, int64(1), counter.Current())
	require.Equal(t, int64(1), counter.Peak())
}

func TestBatchRangeVectorIterator_ShouldReleaseItsLabelSetsOnClose(t *testing.T) {
	var (
		start = time.Unix(10, 0)
		end   = time.Unix(30, 0)
		count = &syntax.RangeAggregationExpr{Operation: syntax.OpRangeTypeCount}
	)
	counter := newIntermediateSeriesCounter(0)
	newIterator := func(n int) RangeVectorIterator {
		rangeIt, err := newTimestampFirstRangeVectorIterator(distinctSeriesIterator(n, start.Add(time.Second)), count, (10 * time.Second).Nanoseconds(), (5 * time.Second).Nanoseconds(), start.UnixNano(), end.UnixNano(), 0, iteratorLimits{counter: counter})
		require.NoError(t, err)
		return rangeIt
	}

	first := newIterator(4)
	require.NoError(t, drain(first))
	require.Equal(t, int64(4), counter.Current())
	require.NoError(t, first.Close())
	require.Zero(t, counter.Current())

	second := newIterator(3)
	require.NoError(t, drain(second))
	require.NoError(t, second.Close())
	require.Zero(t, counter.Current())
	require.Equal(t, int64(4), counter.Peak(), "sequential iterators must not add up")

	// A repeated Close must not release what another iterator still holds.
	third := newIterator(2)
	require.NoError(t, drain(third))
	require.NoError(t, third.Close())
	other := newIterator(1)
	require.NoError(t, drain(other))
	_ = third.Close()
	require.Equal(t, int64(1), counter.Current())
}

func TestStreamRangeVectorIterator_ShouldNotReleaseAReservationItNeverMade(t *testing.T) {
	var (
		start = time.Unix(10, 0)
		end   = time.Unix(30, 0)
		count = &syntax.RangeAggregationExpr{Operation: syntax.OpRangeTypeCount}
	)
	it := iter.NewPeekingSampleIterator(iter.NewSeriesIterator(logproto.Series{
		Labels:     "not a label set",
		Samples:    []logproto.Sample{{Timestamp: start.Add(-time.Second).UnixNano(), Value: 1}},
		StreamHash: 1,
	}))

	counter := newIntermediateSeriesCounter(0)
	require.NoError(t, counter.Reserve(1))
	// A range shorter than the step does not overlap, so the iterator streams.
	rangeIt, err := newTimestampFirstRangeVectorIterator(it, count, (2 * time.Second).Nanoseconds(), (5 * time.Second).Nanoseconds(), start.UnixNano(), end.UnixNano(), 0, iteratorLimits{counter: counter})
	require.NoError(t, err)
	require.IsType(t, &streamRangeVectorIterator{}, rangeIt)

	require.ErrorContains(t, drain(rangeIt), "failed to parse metric")
	require.NoError(t, rangeIt.Close())
	require.NoError(t, rangeIt.Close())
	require.Equal(t, int64(1), counter.Current(), "the reservation belongs to another iterator")
}

func TestStreamRangeVectorIterator_ShouldNotRaiseThePeakForALabelSetThatFailsToParse(t *testing.T) {
	var (
		start = time.Unix(10, 0)
		end   = time.Unix(30, 0)
		count = &syntax.RangeAggregationExpr{Operation: syntax.OpRangeTypeCount}
	)
	newSeries := func(lbs string, hash uint64, ts time.Time) iter.SampleIterator {
		return iter.NewSeriesIterator(logproto.Series{
			Labels:     lbs,
			Samples:    []logproto.Sample{{Timestamp: ts.UnixNano(), Value: 1}},
			StreamHash: hash,
		})
	}
	good := func() iter.SampleIterator { return newSeries(`{app="a"}`, 1, start.Add(-time.Second)) }
	bad := func() iter.SampleIterator { return newSeries("not a label set", 2, start.Add(-500*time.Millisecond)) }

	for name, tc := range map[string]struct {
		series   func() []iter.SampleIterator
		wantPeak int64
	}{
		"as the first label":  {series: func() []iter.SampleIterator { return []iter.SampleIterator{bad()} }, wantPeak: 0},
		"after a valid label": {series: func() []iter.SampleIterator { return []iter.SampleIterator{good(), bad()} }, wantPeak: 1},
	} {
		t.Run(name, func(t *testing.T) {
			counter := newIntermediateSeriesCounter(0)
			it := iter.NewPeekingSampleIterator(iter.NewTimestampFirstSortSampleIterator(tc.series()))
			rangeIt, err := newTimestampFirstRangeVectorIterator(it, count, (2 * time.Second).Nanoseconds(), (5 * time.Second).Nanoseconds(), start.UnixNano(), end.UnixNano(), 0, iteratorLimits{counter: counter})
			require.NoError(t, err)
			require.IsType(t, &streamRangeVectorIterator{}, rangeIt)

			require.ErrorContains(t, drain(rangeIt), "failed to parse metric")
			require.Equal(t, tc.wantPeak, counter.Current())
			require.Equal(t, tc.wantPeak, counter.Peak())

			require.NoError(t, rangeIt.Close())
			require.Zero(t, counter.Current())
		})
	}
}

func TestStreamRangeVectorIterator_ShouldCountLabelSetsPerStep(t *testing.T) {
	const numSeries = 4
	var (
		start = time.Unix(10, 0)
		count = &syntax.RangeAggregationExpr{Operation: syntax.OpRangeTypeCount}
	)

	for name, tc := range map[string]struct {
		selRange time.Duration
		step     time.Duration
		end      time.Time
		it       func() iter.PeekingSampleIterator
		wantPeak int64
	}{
		"a stable label set does not grow across steps": {
			selRange: 2 * time.Second, step: 5 * time.Second, end: time.Unix(30, 0),
			it: func() iter.PeekingSampleIterator {
				return repeatedSeriesIterator(numSeries, time.Unix(10, 0).Add(-time.Second), time.Unix(15, 0).Add(-time.Second), time.Unix(20, 0).Add(-time.Second), time.Unix(25, 0).Add(-time.Second))
			},
			wantPeak: numSeries,
		},
		"step greater than range": {
			selRange: time.Second, step: 10 * time.Second, end: time.Unix(30, 0),
			it: func() iter.PeekingSampleIterator {
				return repeatedSeriesIterator(numSeries, time.Unix(10, 0).Add(-500*time.Millisecond), time.Unix(20, 0).Add(-500*time.Millisecond))
			},
			wantPeak: numSeries,
		},
		"instant query": {
			selRange: 2 * time.Second, step: 0, end: start,
			it:       func() iter.PeekingSampleIterator { return distinctSeriesIterator(numSeries, start.Add(-time.Second)) },
			wantPeak: numSeries,
		},
	} {
		t.Run(name, func(t *testing.T) {
			counter := newIntermediateSeriesCounter(0)
			rangeIt, err := newTimestampFirstRangeVectorIterator(tc.it(), count, tc.selRange.Nanoseconds(), tc.step.Nanoseconds(), start.UnixNano(), tc.end.UnixNano(), 0, iteratorLimits{counter: counter})
			require.NoError(t, err)
			require.IsType(t, &streamRangeVectorIterator{}, rangeIt)

			for rangeIt.Next() {
				rangeIt.At()
				require.LessOrEqual(t, counter.Current(), int64(numSeries))
			}
			require.NoError(t, rangeIt.Error())
			require.Equal(t, tc.wantPeak, counter.Peak())

			require.NoError(t, rangeIt.Close())
			require.Zero(t, counter.Current())
		})
	}
}

func TestStreamRangeVectorIterator_ShouldReleaseTheStepBeforeTheNext(t *testing.T) {
	start := time.Unix(10, 0)
	count := &syntax.RangeAggregationExpr{Operation: syntax.OpRangeTypeCount}
	// 3 label sets in the first step, 1 in the second.
	series := []iter.SampleIterator{}
	for i, ts := range []time.Time{start.Add(-time.Second), start.Add(-time.Second), start.Add(-time.Second), start.Add(4 * time.Second)} {
		lbs := labels.FromStrings("app", "a", "id", fmt.Sprint(i))
		series = append(series, iter.NewSeriesIterator(logproto.Series{Labels: lbs.String(), StreamHash: labels.StableHash(lbs),
			Samples: []logproto.Sample{{Timestamp: ts.UnixNano(), Hash: uint64(i), Value: 1}}}))
	}
	it := iter.NewPeekingSampleIterator(iter.NewTimestampFirstSortSampleIterator(series))
	counter := newIntermediateSeriesCounter(0)
	rangeIt, err := newTimestampFirstRangeVectorIterator(it, count, (2 * time.Second).Nanoseconds(), (5 * time.Second).Nanoseconds(), start.UnixNano(), start.Add(5*time.Second).UnixNano(), 0, iteratorLimits{counter: counter})
	require.NoError(t, err)

	require.True(t, rangeIt.Next())
	require.Equal(t, int64(3), counter.Current())
	require.True(t, rangeIt.Next())
	require.Equal(t, int64(1), counter.Current())
	require.Equal(t, int64(3), counter.Peak())
}

func TestEngine_ShouldReportTheIntermediateSeriesPeakInTheStatistics(t *testing.T) {
	const numSeries = 5
	var (
		start = time.Unix(60, 0)
		end   = time.Unix(120, 0)
	)

	for name, tc := range map[string]struct {
		maxSeries int
		wantErr   bool
	}{
		"success": {maxSeries: 100},
		"failure": {maxSeries: 2, wantErr: true},
	} {
		t.Run(name, func(t *testing.T) {
			eng := NewEngine(EngineOpts{}, NewMockQuerier(1, distinctStreams(numSeries, end)), &fakeLimits{maxSeries: tc.maxSeries, timeout: time.Minute}, log.NewNopLogger())
			params, err := NewLiteralParams(`count_over_time({app="a"}[1m])`, start, end, 30*time.Second, 0, logproto.FORWARD, 1000, nil, nil)
			require.NoError(t, err)

			res, err := eng.Query(params).Exec(user.InjectOrgID(context.Background(), "fake"))

			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Positive(t, res.Statistics.Summary.IntermediateSeriesPeak)
			require.LessOrEqual(t, res.Statistics.Summary.IntermediateSeriesPeak, int64(numSeries))
		})
	}
}
