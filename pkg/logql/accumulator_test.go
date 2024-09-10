package logql

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/sketch"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase/definitions"
)

func TestAccumulatedStreams(t *testing.T) {
	lim := 30
	nStreams := 10
	start, end := 0, 10
	// for a logproto.BACKWARD query, we use a min heap based on FORWARD
	// to store the _earliest_ timestamp of the _latest_ entries, up to `limit`
	xs := newStreams(time.Unix(int64(start), 0), time.Unix(int64(end), 0), time.Second, nStreams, logproto.BACKWARD)
	acc := NewStreamAccumulator(LiteralParams{
		direction: logproto.BACKWARD,
		limit:     uint32(lim),
	})
	for _, x := range xs {
		acc.Push(x)
	}

	for i := 0; i < lim; i++ {
		got := acc.Pop().(*logproto.Stream)
		require.Equal(t, fmt.Sprintf(`{n="%d"}`, i%nStreams), got.Labels)
		exp := (nStreams*(end-start) - lim + i) / nStreams
		require.Equal(t, time.Unix(int64(exp), 0), got.Entries[0].Timestamp)
	}

}

func TestDownstreamAccumulatorSimple(t *testing.T) {
	lim := 30
	start, end := 0, 10
	direction := logproto.BACKWARD

	streams := newStreams(time.Unix(int64(start), 0), time.Unix(int64(end), 0), time.Second, 10, direction)
	x := make(logqlmodel.Streams, 0, len(streams))
	for _, s := range streams {
		x = append(x, *s)
	}
	// dummy params. Only need to populate direction & limit
	params, err := NewLiteralParams(
		`{app="foo"}`, time.Time{}, time.Time{}, 0, 0, direction, uint32(lim), nil, nil,
	)
	require.NoError(t, err)

	acc := NewStreamAccumulator(params)
	result := logqlmodel.Result{
		Data: x,
	}

	require.Nil(t, acc.Accumulate(context.Background(), result, 0))

	res := acc.Result()[0]
	got, ok := res.Data.(logqlmodel.Streams)
	require.Equal(t, true, ok)
	require.Equal(t, 10, len(got), "correct number of streams")

	// each stream should have the top 3 entries
	for i := 0; i < 10; i++ {
		require.Equal(t, 3, len(got[i].Entries), "correct number of entries in stream")
		for j := 0; j < 3; j++ {
			require.Equal(t, time.Unix(int64(9-j), 0), got[i].Entries[j].Timestamp, "correct timestamp")
		}
	}
}

// TestDownstreamAccumulatorMultiMerge simulates merging multiple
// sub-results from different queries.
func TestDownstreamAccumulatorMultiMerge(t *testing.T) {
	for _, direction := range []logproto.Direction{logproto.BACKWARD, logproto.FORWARD} {
		t.Run(direction.String(), func(t *testing.T) {
			nQueries := 10
			delta := 10 // 10 entries per stream, 1s apart
			streamsPerQuery := 10
			lim := 30

			payloads := make([]logqlmodel.Streams, 0, nQueries)
			for i := 0; i < nQueries; i++ {
				start := i * delta
				end := start + delta
				streams := newStreams(time.Unix(int64(start), 0), time.Unix(int64(end), 0), time.Second, streamsPerQuery, direction)
				var res logqlmodel.Streams
				for i := range streams {
					res = append(res, *streams[i])
				}
				payloads = append(payloads, res)

			}

			// queries are always dispatched in the correct order.
			// oldest time ranges first in the case of logproto.FORWARD
			// and newest time ranges first in the case of logproto.BACKWARD
			if direction == logproto.BACKWARD {
				for i, j := 0, len(payloads)-1; i < j; i, j = i+1, j-1 {
					payloads[i], payloads[j] = payloads[j], payloads[i]
				}
			}

			// dummy params. Only need to populate direction & limit
			params, err := NewLiteralParams(
				`{app="foo"}`, time.Time{}, time.Time{}, 0, 0, direction, uint32(lim), nil, nil,
			)
			require.NoError(t, err)

			acc := NewStreamAccumulator(params)
			for i := 0; i < nQueries; i++ {
				err := acc.Accumulate(context.Background(), logqlmodel.Result{
					Data: payloads[i],
				}, i)
				require.Nil(t, err)
			}

			got, ok := acc.Result()[0].Data.(logqlmodel.Streams)
			require.Equal(t, true, ok)
			require.Equal(t, int64(nQueries), acc.Result()[0].Statistics.Summary.Shards)

			// each stream should have the top 3 entries
			for i := 0; i < streamsPerQuery; i++ {
				stream := got[i]
				require.Equal(t, fmt.Sprintf(`{n="%d"}`, i), stream.Labels, "correct labels")
				ln := lim / streamsPerQuery
				require.Equal(t, ln, len(stream.Entries), "correct number of entries in stream")
				switch direction {
				case logproto.BACKWARD:
					for i := 0; i < ln; i++ {
						offset := delta*nQueries - 1 - i
						require.Equal(t, time.Unix(int64(offset), 0), stream.Entries[i].Timestamp, "correct timestamp")
					}
				default:
					for i := 0; i < ln; i++ {
						offset := i
						require.Equal(t, time.Unix(int64(offset), 0), stream.Entries[i].Timestamp, "correct timestamp")
					}
				}
			}
		})
	}
}

func TestQuantileSketchDownstreamAccumulatorSimple(t *testing.T) {
	acc := newQuantileSketchAccumulator()
	downstreamResult := newQuantileSketchResults()[0]

	require.Nil(t, acc.Accumulate(context.Background(), downstreamResult, 0))

	res := acc.Result()[0]
	got, ok := res.Data.(ProbabilisticQuantileMatrix)
	require.Equal(t, true, ok)
	require.Equal(t, 10, len(got), "correct number of vectors")

	require.Equal(t, res.Headers[0].Name, "HeaderA")
	require.Equal(t, res.Warnings, []string{"warning"})
	require.Equal(t, int64(33), res.Statistics.Summary.Shards)
}

func BenchmarkAccumulator(b *testing.B) {

	// dummy params. Only need to populate direction & limit
	lim := 30
	params, err := NewLiteralParams(
		`{app="foo"}`, time.Time{}, time.Time{}, 0, 0, logproto.BACKWARD, uint32(lim), nil, nil,
	)
	require.NoError(b, err)

	for acc, tc := range map[string]struct {
		results []logqlmodel.Result
		newAcc  func(Params, []logqlmodel.Result) Accumulator
		params  Params
	}{
		"streams": {
			newStreamResults(),
			func(p Params, _ []logqlmodel.Result) Accumulator {
				return NewStreamAccumulator(p)
			},
			params,
		},
		"quantile sketches": {
			newQuantileSketchResults(),
			func(_ Params, _ []logqlmodel.Result) Accumulator {
				return newQuantileSketchAccumulator()
			},
			params,
		},
	} {
		b.Run(acc, func(b *testing.B) {
			b.ResetTimer()
			b.ReportAllocs()
			for n := 0; n < b.N; n++ {

				acc := tc.newAcc(params, tc.results)
				for i, r := range tc.results {
					err := acc.Accumulate(context.Background(), r, i)
					require.Nil(b, err)
				}

				acc.Result()
			}
		})
	}
}

func newStreamResults() []logqlmodel.Result {
	nQueries := 50
	delta := 100 // 10 entries per stream, 1s apart
	streamsPerQuery := 50

	results := make([]logqlmodel.Result, nQueries)
	for i := 0; i < nQueries; i++ {
		start := i * delta
		end := start + delta
		streams := newStreams(time.Unix(int64(start), 0), time.Unix(int64(end), 0), time.Second, streamsPerQuery, logproto.BACKWARD)
		var res logqlmodel.Streams
		for i := range streams {
			res = append(res, *streams[i])
		}
		results[i] = logqlmodel.Result{Data: res}

	}

	return results
}

func newQuantileSketchResults() []logqlmodel.Result {
	results := make([]logqlmodel.Result, 100)
	statistics := stats.Result{
		Summary: stats.Summary{Shards: 33},
	}

	for r := range results {
		vectors := make([]ProbabilisticQuantileVector, 10)
		for i := range vectors {
			vectors[i] = make(ProbabilisticQuantileVector, 10)
			for j := range vectors[i] {
				vectors[i][j] = ProbabilisticQuantileSample{
					T:      int64(i),
					F:      newRandomSketch(),
					Metric: []labels.Label{{Name: "foo", Value: fmt.Sprintf("bar-%d", j)}},
				}
			}
		}
		results[r] = logqlmodel.Result{Data: ProbabilisticQuantileMatrix(vectors), Headers: []*definitions.PrometheusResponseHeader{{Name: "HeaderA", Values: []string{"ValueA"}}}, Warnings: []string{"warning"}, Statistics: statistics}
	}

	return results
}

func newStreamWithDirection(start, end time.Time, delta time.Duration, ls string, direction logproto.Direction) *logproto.Stream {
	s := &logproto.Stream{
		Labels: ls,
	}
	for t := start; t.Before(end); t = t.Add(delta) {
		s.Entries = append(s.Entries, logproto.Entry{
			Timestamp: t,
			Line:      fmt.Sprintf("%d", t.Unix()),
		})
	}
	if direction == logproto.BACKWARD {
		// simulate data coming in reverse order (logproto.BACKWARD)
		for i, j := 0, len(s.Entries)-1; i < j; i, j = i+1, j-1 {
			s.Entries[i], s.Entries[j] = s.Entries[j], s.Entries[i]
		}
	}
	return s
}

func newStreams(start, end time.Time, delta time.Duration, n int, direction logproto.Direction) (res []*logproto.Stream) {
	for i := 0; i < n; i++ {
		res = append(res, newStreamWithDirection(start, end, delta, fmt.Sprintf(`{n="%d"}`, i), direction))
	}
	return res
}

func newRandomSketch() sketch.QuantileSketch {
	r := rand.New(rand.NewSource(42))
	s := sketch.NewDDSketch()
	for i := 0; i < 1000; i++ {
		_ = s.Add(r.Float64())
	}
	return s
}
