package logql

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/logqlmodel/metadata"
	"github.com/grafana/loki/v3/pkg/util/httpreq"
)

func TestStreamFirstRangeVectorIterator(t *testing.T) {
	var (
		foo      = `{app="foo"}`
		bar      = `{app="bar"}`
		sec      = func(s int64) int64 { return s * int64(time.Second) }
		countOne = mustRangeAggregation(t, `count_over_time({app="foo"}[10s])`)
		byApp    = &syntax.Grouping{Groups: []string{"app"}}
	)

	t.Run("counts the samples of each window over a range query", func(t *testing.T) {
		input := []labeledSample{
			{foo, sec(1)}, {foo, sec(10)}, {foo, sec(11)}, {foo, sec(20)}, {bar, sec(15)},
		}
		it, err := newStreamFirstRangeVectorIterator(context.Background(), newLabeledSampleIterator(input), countOne, sec(10), sec(10), sec(10), sec(30), 0, byApp, 0, false)
		require.NoError(t, err)

		require.Equal(t, map[int64]map[string]float64{
			10_000: {foo: 2},
			20_000: {foo: 2, bar: 1},
		}, drainRangeVector(t, it))
	})

	t.Run("excludes a sample on the window start and includes one on the window end", func(t *testing.T) {
		input := []labeledSample{{foo, sec(10)}, {foo, sec(20)}}
		it, err := newStreamFirstRangeVectorIterator(context.Background(), newLabeledSampleIterator(input), countOne, sec(10), sec(10), sec(20), sec(20), 0, byApp, 0, false)
		require.NoError(t, err)

		require.Equal(t, map[int64]map[string]float64{20_000: {foo: 1}}, drainRangeVector(t, it))
	})

	t.Run("evaluates an instant query as one step", func(t *testing.T) {
		input := []labeledSample{{foo, sec(5)}, {foo, sec(9)}, {bar, sec(1)}}
		it, err := newStreamFirstRangeVectorIterator(context.Background(), newLabeledSampleIterator(input), countOne, sec(10), 0, sec(10), sec(10), 0, byApp, 0, false)
		require.NoError(t, err)

		require.Equal(t, map[int64]map[string]float64{10_000: {foo: 2, bar: 1}}, drainRangeVector(t, it))
	})

	t.Run("reports steps at their unshifted timestamp when the query has an offset", func(t *testing.T) {
		input := []labeledSample{{foo, sec(5)}}
		it, err := newStreamFirstRangeVectorIterator(context.Background(), newLabeledSampleIterator(input), countOne, sec(10), 0, sec(20), sec(20), sec(10), byApp, 0, false)
		require.NoError(t, err)

		require.Equal(t, map[int64]map[string]float64{20_000: {foo: 1}}, drainRangeVector(t, it))
	})

	t.Run("emits no sample for a window without samples", func(t *testing.T) {
		input := []labeledSample{{foo, sec(5)}}
		it, err := newStreamFirstRangeVectorIterator(context.Background(), newLabeledSampleIterator(input), countOne, sec(10), sec(10), sec(10), sec(30), 0, byApp, 0, false)
		require.NoError(t, err)

		steps := 0
		for it.Next() {
			ts, vec := it.At()
			if ts == 10_000 {
				require.Len(t, vec.SampleVector(), 1)
			} else {
				require.Empty(t, vec.SampleVector())
			}
			steps++
		}
		require.NoError(t, it.Error())
		require.Equal(t, 3, steps)
	})

	t.Run("matches the timestamp-first iterator on random input in random order", func(t *testing.T) {
		for seed := int64(0); seed < 200; seed++ {
			rnd := rand.New(rand.NewSource(seed))
			input := randomLabeledSamples(rnd)

			selRange := sec(1 + rnd.Int63n(30))
			step := sec(1 + rnd.Int63n(20))
			if rnd.Intn(4) == 0 {
				step = 0
			}
			start := sec(rnd.Int63n(60))
			end := start
			if step != 0 {
				end += sec(rnd.Int63n(120))
			}
			offset := sec(rnd.Int63n(3)) * 5
			expr := mustRangeAggregation(t, fmt.Sprintf(`count_over_time({app="foo"}[%s])`, time.Duration(selRange)))

			want, err := newTimestampFirstRangeVectorIterator(newTimestampOrderedIterator(input), expr, selRange, step, start, end, offset)
			require.NoError(t, err)

			rnd.Shuffle(len(input), func(i, j int) { input[i], input[j] = input[j], input[i] })
			got, err := newStreamFirstRangeVectorIterator(context.Background(), newLabeledSampleIterator(input), expr, selRange, step, start, end, offset, byApp, 0, false)
			require.NoError(t, err)

			require.Equal(t, drainRangeVector(t, want), drainRangeVector(t, got), "seed %d", seed)
		}
	})

	t.Run("fails when the input fails", func(t *testing.T) {
		readErr := errors.New("read failed")
		input := failingSampleIterator{SampleIterator: newLabeledSampleIterator([]labeledSample{{foo, sec(5)}}), err: readErr}
		it, err := newStreamFirstRangeVectorIterator(context.Background(), iter.NewPeekingSampleIterator(input), countOne, sec(10), 0, sec(10), sec(10), 0, byApp, 0, false)
		require.NoError(t, err)

		require.False(t, it.Next())
		require.ErrorIs(t, it.Error(), readErr)
	})

	t.Run("fails with the cancellation error the input reports", func(t *testing.T) {
		input := failingSampleIterator{SampleIterator: newLabeledSampleIterator([]labeledSample{{foo, sec(5)}}), err: context.Canceled}
		it, err := newStreamFirstRangeVectorIterator(context.Background(), iter.NewPeekingSampleIterator(input), countOne, sec(10), 0, sec(10), sec(10), 0, byApp, 0, false)
		require.NoError(t, err)

		require.False(t, it.Next())
		require.ErrorIs(t, it.Error(), context.Canceled)
	})

	t.Run("fails when a new series exceeds the series limit", func(t *testing.T) {
		input := []labeledSample{{foo, sec(5)}, {bar, sec(6)}}
		it, err := newStreamFirstRangeVectorIterator(context.Background(), newLabeledSampleIterator(input), countOne, sec(10), 0, sec(10), sec(10), 0, byApp, 1, false)
		require.NoError(t, err)

		require.False(t, it.Next())
		require.ErrorIs(t, it.Error(), logqlmodel.ErrLimit)
		require.EqualError(t, it.Error(), logqlmodel.NewSeriesLimitError(1).Error())
	})

	t.Run("ignores a series without samples in any window when enforcing the series limit", func(t *testing.T) {
		input := []labeledSample{{bar, sec(50)}, {foo, sec(5)}}
		it, err := newStreamFirstRangeVectorIterator(context.Background(), newLabeledSampleIterator(input), countOne, sec(10), 0, sec(10), sec(10), 0, byApp, 1, false)
		require.NoError(t, err)

		require.Equal(t, map[int64]map[string]float64{10_000: {foo: 1}}, drainRangeVector(t, it))
	})

	t.Run("fails when the labels of a series do not parse", func(t *testing.T) {
		input := []labeledSample{{foo, sec(4)}, {`{app=`, sec(5)}}
		it, err := newStreamFirstRangeVectorIterator(context.Background(), newLabeledSampleIterator(input), countOne, sec(10), 0, sec(10), sec(10), 0, byApp, 0, false)
		require.NoError(t, err)

		require.False(t, it.Next())
		require.ErrorContains(t, it.Error(), "parsing series labels")
	})

	t.Run("fails with the pipeline error when a series over the limit carries an error label in a Logs Drilldown request", func(t *testing.T) {
		md, ctx := metadata.NewContext(context.Background())
		ctx = httpreq.InjectQueryTags(ctx, "Source=grafana-lokiexplore-app")
		input := []labeledSample{{foo, sec(5)}, {`{__error__="JSONParserErr", app="bar"}`, sec(6)}}
		it, err := newStreamFirstRangeVectorIterator(ctx, newLabeledSampleIterator(input), countOne, sec(10), 0, sec(10), sec(10), 0, byApp, 1, false)
		require.NoError(t, err)

		require.False(t, it.Next())
		var pipelineErr *logqlmodel.PipelineError
		require.ErrorAs(t, it.Error(), &pipelineErr)
		require.Empty(t, md.Warnings())
	})

	t.Run("returns the first series with a warning when a Logs Drilldown request exceeds the series limit", func(t *testing.T) {
		md, ctx := metadata.NewContext(context.Background())
		ctx = httpreq.InjectQueryTags(ctx, "Source=grafana-lokiexplore-app")
		input := []labeledSample{{foo, sec(5)}, {bar, sec(6)}, {foo, sec(7)}}
		it, err := newStreamFirstRangeVectorIterator(ctx, newLabeledSampleIterator(input), countOne, sec(10), 0, sec(10), sec(10), 0, byApp, 1, false)
		require.NoError(t, err)

		require.Equal(t, map[int64]map[string]float64{10_000: {foo: 2}}, drainRangeVector(t, it))
		require.Equal(t, []string{"maximum number of series (1) reached for a single query; returning partial results"}, md.Warnings())
	})

	t.Run("counts series that differ only in a label the sum drops as one output series", func(t *testing.T) {
		input := []labeledSample{{`{app="foo", pod="1"}`, sec(5)}, {`{app="foo", pod="2"}`, sec(6)}}
		it, err := newStreamFirstRangeVectorIterator(context.Background(), newLabeledSampleIterator(input), countOne, sec(10), 0, sec(10), sec(10), 0, byApp, 1, false)
		require.NoError(t, err)

		require.Equal(t, map[int64]map[string]float64{10_000: {`{app="foo", pod="1"}`: 1, `{app="foo", pod="2"}`: 1}}, drainRangeVector(t, it))
	})

	t.Run("counts the same output series of two tenants once", func(t *testing.T) {
		input := []labeledSample{{`{__tenant_id__="a", app="foo"}`, sec(5)}, {`{__tenant_id__="b", app="foo"}`, sec(6)}}
		it, err := newStreamFirstRangeVectorIterator(context.Background(), newLabeledSampleIterator(input), countOne, sec(10), 0, sec(10), sec(10), 0, byApp, 1, false)
		require.NoError(t, err)

		require.Equal(t, map[int64]map[string]float64{10_000: {`{__tenant_id__="a", app="foo"}`: 1, `{__tenant_id__="b", app="foo"}`: 1}}, drainRangeVector(t, it))
	})

	t.Run("fails when series differ in a label a without grouping keeps", func(t *testing.T) {
		withoutPod := &syntax.Grouping{Groups: []string{"pod"}, Without: true}
		input := []labeledSample{{`{app="foo", pod="1"}`, sec(5)}, {`{app="bar", pod="1"}`, sec(6)}}
		it, err := newStreamFirstRangeVectorIterator(context.Background(), newLabeledSampleIterator(input), countOne, sec(10), 0, sec(10), sec(10), 0, withoutPod, 1, false)
		require.NoError(t, err)

		require.False(t, it.Next())
		require.ErrorIs(t, it.Error(), logqlmodel.ErrLimit)
	})

	t.Run("keeps a series of an output series it already has when a Logs Drilldown request reached the series limit", func(t *testing.T) {
		_, ctx := metadata.NewContext(context.Background())
		ctx = httpreq.InjectQueryTags(ctx, "Source=grafana-lokiexplore-app")
		input := []labeledSample{{`{__tenant_id__="a", app="foo"}`, sec(5)}, {`{__tenant_id__="a", app="bar"}`, sec(6)}, {`{__tenant_id__="b", app="foo"}`, sec(7)}}
		it, err := newStreamFirstRangeVectorIterator(ctx, newLabeledSampleIterator(input), countOne, sec(10), 0, sec(10), sec(10), 0, byApp, 1, false)
		require.NoError(t, err)

		require.Equal(t, map[int64]map[string]float64{10_000: {`{__tenant_id__="a", app="foo"}`: 1, `{__tenant_id__="b", app="foo"}`: 1}}, drainRangeVector(t, it))
	})

	t.Run("rejects a range aggregation other than count_over_time", func(t *testing.T) {
		_, err := newStreamFirstRangeVectorIterator(context.Background(), newLabeledSampleIterator(nil), mustRangeAggregation(t, `rate({app="foo"}[10s])`), sec(10), 0, sec(10), sec(10), 0, byApp, 0, false)
		require.ErrorContains(t, err, "rate")
	})
}

func TestStreamFirstRangeVectorIterator_windowRange(t *testing.T) {
	sec := func(s int64) int64 { return s * int64(time.Second) }
	countOverTime := mustRangeAggregation(t, `count_over_time({app="foo"}[10s])`)

	for _, tc := range []struct {
		name                               string
		selRange, step, start, end, offset int64
		ts                                 int64
		wantLo, wantHi                     int
		wantOK                             bool
	}{
		{
			name:     "returns only the step whose timestamp equals ts",
			selRange: sec(10), step: sec(10), start: sec(10), end: sec(40),
			ts:     sec(20),
			wantLo: 1, wantHi: 1, wantOK: true,
		},
		{
			name:     "returns the next step for ts one nanosecond after a step timestamp",
			selRange: sec(10), step: sec(10), start: sec(10), end: sec(40),
			ts:     sec(20) + 1,
			wantLo: 2, wantHi: 2, wantOK: true,
		},
		{
			name:     "excludes the step whose window starts exactly at ts",
			selRange: sec(10), step: sec(10), start: sec(10), end: sec(40),
			ts:     sec(10),
			wantLo: 0, wantHi: 0, wantOK: true,
		},
		{
			name:     "returns every step whose window holds ts when the range spans several steps",
			selRange: sec(25), step: sec(10), start: sec(10), end: sec(40),
			ts:     sec(12),
			wantLo: 1, wantHi: 2, wantOK: true,
		},
		{
			name:     "clamps lo to the first step when ts is before the query start",
			selRange: sec(35), step: sec(10), start: sec(10), end: sec(40),
			ts:     -sec(20),
			wantLo: 0, wantHi: 0, wantOK: true,
		},
		{
			name:     "clamps hi to the last step when the window extends past the query end",
			selRange: sec(30), step: sec(10), start: sec(10), end: sec(40),
			ts:     sec(35),
			wantLo: 3, wantHi: 3, wantOK: true,
		},
		{
			name:     "returns false for ts on the start of the first window",
			selRange: sec(10), step: sec(10), start: sec(10), end: sec(40),
			ts:     0,
			wantOK: false,
		},
		{
			name:     "returns false for ts after the last step",
			selRange: sec(10), step: sec(10), start: sec(10), end: sec(40),
			ts:     sec(40) + 1,
			wantOK: false,
		},
		{
			name:     "returns false for ts between two windows that do not overlap",
			selRange: sec(5), step: sec(10), start: sec(10), end: sec(40),
			ts:     sec(15),
			wantOK: false,
		},
		{
			name:     "returns the only step of an instant query",
			selRange: sec(10), step: 0, start: sec(10), end: sec(10),
			ts:     sec(5),
			wantLo: 0, wantHi: 0, wantOK: true,
		},
		{
			name:     "shifts the windows back by the offset",
			selRange: sec(10), step: sec(10), start: sec(20), end: sec(50), offset: sec(10),
			ts:     sec(10),
			wantLo: 0, wantHi: 0, wantOK: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			it, err := newStreamFirstRangeVectorIterator(context.Background(), newLabeledSampleIterator(nil), countOverTime, tc.selRange, tc.step, tc.start, tc.end, tc.offset, &syntax.Grouping{}, 0, false)
			require.NoError(t, err)

			lo, hi, ok := it.(*streamFirstRangeVectorIterator).windowRange(tc.ts)
			require.Equal(t, tc.wantOK, ok)
			if tc.wantOK {
				require.Equal(t, tc.wantLo, lo)
				require.Equal(t, tc.wantHi, hi)
			}
		})
	}
}

func TestCeilDiv(t *testing.T) {
	for _, tc := range []struct {
		name string
		a, b int64
		want int64
	}{
		{name: "returns the exact quotient when b divides a", a: 20, b: 10, want: 2},
		{name: "rounds a positive quotient up", a: 21, b: 10, want: 3},
		{name: "rounds a positive quotient just below an integer up", a: 29, b: 10, want: 3},
		{name: "returns zero for a zero dividend", a: 0, b: 10, want: 0},
		{name: "rounds a positive fraction below one up to one", a: 1, b: 10, want: 1},
		{name: "returns the exact quotient when b divides a negative a", a: -20, b: 10, want: -2},
		{name: "rounds a negative quotient up toward zero", a: -21, b: 10, want: -2},
		{name: "rounds a negative fraction above minus one up to zero", a: -1, b: 10, want: 0},
		{name: "returns a for a divisor of one", a: -7, b: 1, want: -7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, ceilDiv(tc.a, tc.b))
		})
	}
}

func TestFloorDiv(t *testing.T) {
	for _, tc := range []struct {
		name string
		a, b int64
		want int64
	}{
		{name: "returns the exact quotient when b divides a", a: 20, b: 10, want: 2},
		{name: "rounds a positive quotient down", a: 29, b: 10, want: 2},
		{name: "rounds a positive quotient just above an integer down", a: 21, b: 10, want: 2},
		{name: "returns zero for a zero dividend", a: 0, b: 10, want: 0},
		{name: "rounds a positive fraction below one down to zero", a: 9, b: 10, want: 0},
		{name: "returns the exact quotient when b divides a negative a", a: -20, b: 10, want: -2},
		{name: "rounds a negative quotient down away from zero", a: -21, b: 10, want: -3},
		{name: "rounds a negative fraction above minus one down to minus one", a: -1, b: 10, want: -1},
		{name: "returns a for a divisor of one", a: -7, b: 1, want: -7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, floorDiv(tc.a, tc.b))
		})
	}
}

type labeledSample struct {
	labels    string
	timestamp int64
}

// labeledSampleIterator yields samples in the order of its slice, whatever their series and
// timestamps.
type labeledSampleIterator struct {
	samples []labeledSample
	pos     int
}

func newLabeledSampleIterator(samples []labeledSample) iter.PeekingSampleIterator {
	return iter.NewPeekingSampleIterator(&labeledSampleIterator{samples: samples, pos: -1})
}

func (it *labeledSampleIterator) Next() bool {
	it.pos++
	return it.pos < len(it.samples)
}

func (it *labeledSampleIterator) At() logproto.Sample {
	return logproto.Sample{Timestamp: it.samples[it.pos].timestamp, Value: 1}
}

func (it *labeledSampleIterator) Labels() string { return it.samples[it.pos].labels }

func (it *labeledSampleIterator) StreamHash() uint64 { return 0 }

func (it *labeledSampleIterator) Err() error { return nil }

func (it *labeledSampleIterator) Close() error { return nil }

// failingSampleIterator yields the samples of its inner iterator and reports err from Err.
type failingSampleIterator struct {
	iter.SampleIterator
	err error
}

func (it failingSampleIterator) Err() error { return it.err }

// newTimestampOrderedIterator returns samples in global timestamp order, as the timestamp-first
// iterator requires.
func newTimestampOrderedIterator(samples []labeledSample) iter.PeekingSampleIterator {
	bySeries := map[string][]logproto.Sample{}
	for _, s := range samples {
		bySeries[s.labels] = append(bySeries[s.labels], logproto.Sample{Timestamp: s.timestamp, Value: 1})
	}
	its := make([]iter.SampleIterator, 0, len(bySeries))
	for lbs, series := range bySeries {
		sort.Slice(series, func(i, j int) bool { return series[i].Timestamp < series[j].Timestamp })
		its = append(its, iter.NewSeriesIterator(logproto.Series{Labels: lbs, Samples: series}))
	}
	return iter.NewPeekingSampleIterator(iter.NewTimestampFirstSortSampleIterator(its))
}

func randomLabeledSamples(rnd *rand.Rand) []labeledSample {
	series := []string{`{app="a"}`, `{app="b"}`, `{app="c", pod="1"}`}
	var out []labeledSample
	for _, lbs := range series {
		seen := map[int64]bool{}
		for range rnd.Intn(40) {
			ts := rnd.Int63n(int64(200 * time.Second))
			if rnd.Intn(3) == 0 {
				ts = ts / int64(time.Second) * int64(time.Second)
			}
			if seen[ts] {
				continue
			}
			seen[ts] = true
			out = append(out, labeledSample{labels: lbs, timestamp: ts})
		}
	}
	return out
}

// drainRangeVector returns the value of every series at every step with at least one sample,
// keyed by the step timestamp in milliseconds and the series labels.
func drainRangeVector(t *testing.T, it RangeVectorIterator) map[int64]map[string]float64 {
	t.Helper()
	out := map[int64]map[string]float64{}
	for it.Next() {
		ts, vec := it.At()
		for _, s := range vec.SampleVector() {
			if out[ts] == nil {
				out[ts] = map[string]float64{}
			}
			out[ts][s.Metric.String()] = s.F
		}
	}
	require.NoError(t, it.Error())
	require.NoError(t, it.Close())
	return out
}

func mustRangeAggregation(t *testing.T, query string) *syntax.RangeAggregationExpr {
	t.Helper()
	expr, err := syntax.ParseExpr(query)
	require.NoError(t, err)
	return expr.(*syntax.RangeAggregationExpr)
}
