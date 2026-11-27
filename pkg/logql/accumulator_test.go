package logql

import (
	"context"
	"fmt"
	"math/rand"
	"sort"
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

// TestDownstreamAccumulatorFreeRoom verifies that the stream accumulator
// returns `limit` entries when more than `limit` entries are available,
// independent of how the downstream entries are grouped into streams.
//
// The two shapes model the same downstream data with and without the
// X-Loki-Response-Encoding-Flags: categorize-labels header:
//
//   - "collapsed": structured metadata is stripped from the series labels, so
//     all entries of a shard arrive as one stream with many entries.
//   - "per-entry": structured metadata makes every series label string unique,
//     so each entry arrives as its own single-entry stream.
func TestDownstreamAccumulatorFreeRoom(t *testing.T) {
	const limit = 500
	start := time.Unix(1700000000, 0)

	// Entries are returned by the downstream best-first: oldest first for
	// FORWARD, newest first for BACKWARD.
	orderForDirection := func(entries []logproto.Entry, dir logproto.Direction) []logproto.Entry {
		if dir == logproto.BACKWARD {
			for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
				entries[i], entries[j] = entries[j], entries[i]
			}
		}
		return entries
	}

	// batch builds one downstream result holding n entries starting at
	// start+offset, either as a single stream or as one stream per entry.
	batch := func(shape string, dir logproto.Direction, id, n int, offset, step time.Duration) logqlmodel.Result {
		entries := make([]logproto.Entry, 0, n)
		for i := range n {
			entries = append(entries, logproto.Entry{
				Timestamp: start.Add(offset + time.Duration(i)*step),
				Line:      fmt.Sprintf("batch-%d-entry-%d", id, i),
			})
		}
		entries = orderForDirection(entries, dir)

		if shape == "per-entry" {
			streams := make(logqlmodel.Streams, 0, len(entries))
			for i, e := range entries {
				streams = append(streams, logproto.Stream{
					Labels:  fmt.Sprintf(`{app="foo", __stream_shard__="%d", trace_id="%d"}`, id, i),
					Entries: []logproto.Entry{e},
				})
			}
			return logqlmodel.Result{Data: streams}
		}

		return logqlmodel.Result{Data: logqlmodel.Streams{{
			Labels:  fmt.Sprintf(`{app="foo", __stream_shard__="%d"}`, id),
			Entries: entries,
		}}}
	}

	allEntries := func(results []logqlmodel.Result) []logproto.Entry {
		var entries []logproto.Entry
		for _, res := range results {
			for _, s := range res.Data.(logqlmodel.Streams) {
				entries = append(entries, s.Entries...)
			}
		}
		return entries
	}

	// bestFirst sorts entries in the order the query returns them, so that the
	// first `limit` entries are the expected result.
	bestFirst := func(entries []logproto.Entry, dir logproto.Direction) []logproto.Entry {
		sort.Slice(entries, func(i, j int) bool {
			if dir == logproto.BACKWARD {
				return entries[i].Timestamp.After(entries[j].Timestamp)
			}
			return entries[i].Timestamp.Before(entries[j].Timestamp)
		})
		return entries
	}

	lines := func(entries []logproto.Entry) []string {
		res := make([]string, 0, len(entries))
		for _, e := range entries {
			res = append(res, e.Line)
		}
		sort.Strings(res)
		return res
	}

	for _, dir := range []logproto.Direction{logproto.FORWARD, logproto.BACKWARD} {
		for _, shape := range []string{"collapsed", "per-entry"} {
			t.Run(fmt.Sprintf("%s/%s", dir, shape), func(t *testing.T) {
				params, err := NewLiteralParams(`{app="foo"}`, start, start.Add(time.Hour), 0, 0, dir, limit, nil, nil)
				require.NoError(t, err)
				acc := NewStreamAccumulator(params)

				// The first batch is small and holds the best entries, so it
				// establishes `worst` while the accumulator is nearly empty.
				// The following batches hold only entries that are worse than
				// that, while the accumulator still has room for 487 entries.
				var (
					results []logqlmodel.Result
					total   int
				)
				if dir == logproto.FORWARD {
					results = append(results, batch(shape, dir, 0, 13, 0, time.Second))
					total += 13
					for id := 1; id <= 4; id++ {
						results = append(results, batch(shape, dir, id, 200, time.Duration(id)*10*time.Minute, 100*time.Millisecond))
						total += 200
					}
				} else {
					results = append(results, batch(shape, dir, 0, 13, 59*time.Minute, time.Second))
					total += 13
					for id := 1; id <= 4; id++ {
						results = append(results, batch(shape, dir, id, 200, time.Duration(5-id)*10*time.Minute, 100*time.Millisecond))
						total += 200
					}
				}

				downstream := allEntries(results)
				for i, res := range results {
					require.NoError(t, acc.Accumulate(context.Background(), res, i))
				}

				require.Greater(t, total, limit)
				got := allEntries(acc.Result())
				require.Len(t, got, limit)
				// the accumulator must keep the best `limit` entries
				expected := bestFirst(downstream, dir)[:limit]
				require.Equal(t, lines(expected), lines(got))
			})
		}
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
					Metric: labels.FromStrings("foo", fmt.Sprintf("bar-%d", j)),
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
