package logql

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"regexp"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
	yaml "go.yaml.in/yaml/v4"

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

// crossings returns a counter that records every threshold it is called for, and the record.
func crossings(thresholds ...int) (*intermediateSeriesCounter, *[]int64) {
	var got []int64
	c := newIntermediateSeriesCounter(0).notifyOnCrossing(thresholds, func(threshold, _ int64) {
		got = append(got, threshold)
	})
	return c, &got
}

func TestIntermediateSeriesCounter_ShouldNotifyOnceForEachThresholdCrossed(t *testing.T) {
	c, got := crossings(10, 50, 100)

	for range 60 {
		require.NoError(t, c.Reserve(1))
	}

	require.Equal(t, []int64{10, 50}, *got)
}

func TestIntermediateSeriesCounter_ShouldNotNotifyUnderTheLowestThreshold(t *testing.T) {
	c, got := crossings(10, 50)

	require.NoError(t, c.Reserve(9))

	require.Empty(t, *got)
}

func TestIntermediateSeriesCounter_ShouldNotNotifyTwiceForChurnAroundAThreshold(t *testing.T) {
	c, got := crossings(10)

	for range 5 {
		require.NoError(t, c.Reserve(10))
		c.Release(10)
	}

	require.Equal(t, []int64{10}, *got)
}

func TestIntermediateSeriesCounter_ShouldNotifyOnceForADuplicatedThreshold(t *testing.T) {
	c, got := crossings(50, 10, 50, 10)

	require.NoError(t, c.Reserve(60))

	require.Equal(t, []int64{10, 50}, *got)
}

func TestIntermediateSeriesCounter_ShouldNotifyEveryThresholdOneReservationJumpsOver(t *testing.T) {
	c, got := crossings(50, 10)

	require.NoError(t, c.Reserve(60))

	require.Equal(t, []int64{10, 50}, *got)
}

func TestIntermediateSeriesCounter_ShouldNotifyOnceUnderConcurrentUse(t *testing.T) {
	var calls atomic.Int64
	c := newIntermediateSeriesCounter(0).notifyOnCrossing([]int{100}, func(_, _ int64) { calls.Add(1) })

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 1000 {
				require.NoError(t, c.Reserve(1))
			}
		}()
	}
	wg.Wait()

	require.Equal(t, int64(1), calls.Load())
}

func TestIntermediateSeriesCounter_ShouldNotifyEachThresholdOnceUnderConcurrentUseWithReleases(t *testing.T) {
	var (
		mtx sync.Mutex
		got []int64
	)
	thresholds := []int{10, 100, 1000, 3000}
	c := newIntermediateSeriesCounter(0).notifyOnCrossing(thresholds, func(threshold, _ int64) {
		mtx.Lock()
		defer mtx.Unlock()
		got = append(got, threshold)
	})

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 500 {
				require.NoError(t, c.Reserve(2))
				c.Release(1)
			}
		}()
	}
	wg.Wait()

	require.Equal(t, int64(4000), c.Current())
	require.ElementsMatch(t, []int64{10, 100, 1000, 3000}, got)
}

func TestIntermediateSeriesCounter_ShouldNotNotifyForAReservationOverTheLimit(t *testing.T) {
	var got []int64
	c := newIntermediateSeriesCounter(5).notifyOnCrossing([]int{3, 10}, func(threshold, _ int64) {
		got = append(got, threshold)
	})

	require.NoError(t, c.Reserve(4))
	require.ErrorIs(t, c.Reserve(10), errIntermediateSeriesLimit)

	require.Equal(t, []int64{3}, got)
	require.Equal(t, int64(4), c.Current())
	require.Equal(t, int64(4), c.Peak())
}

func TestIntermediateSeriesBreadcrumbThresholds_ShouldRoundTripThroughAFlag(t *testing.T) {
	for name, tc := range map[string]struct {
		in      string
		want    IntermediateSeriesBreadcrumbThresholds
		wantErr bool
	}{
		"list":     {in: "10,50, 100", want: IntermediateSeriesBreadcrumbThresholds{10, 50, 100}},
		"empty":    {in: "", want: nil},
		"not int":  {in: "10,x", wantErr: true},
		"negative": {in: "-1", wantErr: true},
	} {
		t.Run(name, func(t *testing.T) {
			var got IntermediateSeriesBreadcrumbThresholds
			err := got.Set(tc.in)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestEngineOpts_ShouldReadBreadcrumbThresholdsFromYAML(t *testing.T) {
	var opts EngineOpts
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	opts.RegisterFlags(fs)
	require.Equal(t, IntermediateSeriesBreadcrumbThresholds{10_000, 50_000, 100_000, 500_000}, opts.IntermediateSeriesBreadcrumbs)

	require.NoError(t, yaml.Unmarshal([]byte("intermediate_series_breadcrumbs: \"5,7\"\n"), &opts))

	require.Equal(t, IntermediateSeriesBreadcrumbThresholds{5, 7}, opts.IntermediateSeriesBreadcrumbs)
}

func TestIntermediateSeriesBreadcrumbThresholds_ShouldUnmarshalFromYAML(t *testing.T) {
	for name, tc := range map[string]struct {
		in      string
		want    IntermediateSeriesBreadcrumbThresholds
		wantErr bool
	}{
		"list":              {in: "[10, 50, 100]", want: IntermediateSeriesBreadcrumbThresholds{10, 50, 100}},
		"block list":        {in: "\n  - 10\n  - 50", want: IntermediateSeriesBreadcrumbThresholds{10, 50}},
		"empty list":        {in: "[]", want: nil},
		"csv string":        {in: "\"10,50\"", want: IntermediateSeriesBreadcrumbThresholds{10, 50}},
		"empty string":      {in: "\"\"", want: nil},
		"single number":     {in: "10", want: IntermediateSeriesBreadcrumbThresholds{10}},
		"negative in list":  {in: "[10, -1]", wantErr: true},
		"zero in list":      {in: "[0]", wantErr: true},
		"non-int in list":   {in: "[10, x]", wantErr: true},
		"null in list":      {in: "[null]", wantErr: true},
		"null among values": {in: "[10, null, 50]", wantErr: true},
		"blank block item":  {in: "\n  - 10\n  -\n  - 50", wantErr: true},
		"negative in csv":   {in: "\"10,-1\"", wantErr: true},
		"map is not a list": {in: "{a: 1}", wantErr: true},
	} {
		t.Run(name, func(t *testing.T) {
			got := IntermediateSeriesBreadcrumbThresholds{1}
			err := yaml.Unmarshal([]byte("v: "+tc.in+"\n"), &struct {
				V *IntermediateSeriesBreadcrumbThresholds `yaml:"v"`
			}{V: &got})
			if tc.wantErr {
				require.Error(t, err)
				require.Equal(t, IntermediateSeriesBreadcrumbThresholds{1}, got, "an invalid value must not change the thresholds")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestEngine_ShouldLogOneBreadcrumbForEachThresholdAJobCrosses(t *testing.T) {
	var (
		start      = time.Unix(60, 0)
		end        = time.Unix(120, 0)
		breadcrumb = regexp.MustCompile(`threshold=(\d+) intermediate_series=(\d+)`)
	)
	for name, tc := range map[string]struct {
		thresholds []int
		want       []int64
	}{
		"crosses two thresholds":  {thresholds: []int{2, 4, 100}, want: []int64{2, 4}},
		"under the lowest":        {thresholds: []int{100}, want: nil},
		"turned off with no list": {thresholds: nil, want: nil},
	} {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			eng := NewEngine(EngineOpts{IntermediateSeriesBreadcrumbs: tc.thresholds}, NewMockQuerier(1, distinctStreams(5, end)), NoLimits, log.NewLogfmtLogger(&buf))
			params, err := NewLiteralParams(`count(count_over_time({app="a"}[1m]))`, start, end, 30*time.Second, 0, logproto.FORWARD, 1000, nil, nil)
			require.NoError(t, err)

			_, err = eng.Query(params).Exec(user.InjectOrgID(context.Background(), "fake"))
			require.NoError(t, err)

			var thresholds []int64
			for _, m := range breadcrumb.FindAllStringSubmatch(buf.String(), -1) {
				threshold, err := strconv.ParseInt(m[1], 10, 64)
				require.NoError(t, err)
				current, err := strconv.ParseInt(m[2], 10, 64)
				require.NoError(t, err)
				require.GreaterOrEqual(t, current, threshold, buf.String())
				thresholds = append(thresholds, threshold)
			}
			require.Equal(t, tc.want, thresholds, buf.String())
			if len(tc.want) > 0 {
				require.Contains(t, buf.String(), "tenant=fake")
				require.Contains(t, buf.String(), "count_over_time")
			}
		})
	}
}

func TestEngine_ShouldNotFailAJobThatCrossesAThresholdWithoutALogger(t *testing.T) {
	end := time.Unix(120, 0)
	eng := NewEngine(EngineOpts{IntermediateSeriesBreadcrumbs: []int{2}}, NewMockQuerier(1, distinctStreams(5, end)), NoLimits, nil)
	params, err := NewLiteralParams(`count(count_over_time({app="a"}[1m]))`, time.Unix(60, 0), end, 30*time.Second, 0, logproto.FORWARD, 1000, nil, nil)
	require.NoError(t, err)

	_, err = eng.Query(params).Exec(user.InjectOrgID(context.Background(), "fake"))

	require.NoError(t, err)
}
