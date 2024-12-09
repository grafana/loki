package logql

import (
	"container/heap"
	"context"
	"fmt"
	"maps"
	"slices"
	"sort"
	"time"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/logqlmodel/metadata"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase/definitions"
	"github.com/grafana/loki/v3/pkg/util/math"
)

// NewBufferedAccumulator returns an accumulator which aggregates all query
// results in a slice. This is useful for metric queries, which are generally
// small payloads and the memory overhead for buffering is negligible.
func NewBufferedAccumulator(n int) *BufferedAccumulator {
	return &BufferedAccumulator{
		results: make([]logqlmodel.Result, n),
	}
}

type BufferedAccumulator struct {
	results []logqlmodel.Result
}

func (a *BufferedAccumulator) Accumulate(_ context.Context, acc logqlmodel.Result, i int) error {
	a.results[i] = acc
	return nil
}

func (a *BufferedAccumulator) Result() []logqlmodel.Result {
	return a.results
}

type QuantileSketchAccumulator struct {
	matrix ProbabilisticQuantileMatrix

	stats    stats.Result        // for accumulating statistics from downstream requests
	headers  map[string][]string // for accumulating headers from downstream requests
	warnings map[string]struct{} // for accumulating warnings from downstream requests}
}

// newQuantileSketchAccumulator returns an accumulator for sharded
// probabilistic quantile queries that merges results as they come in.
func newQuantileSketchAccumulator() *QuantileSketchAccumulator {
	return &QuantileSketchAccumulator{
		headers:  make(map[string][]string),
		warnings: make(map[string]struct{}),
	}
}

func (a *QuantileSketchAccumulator) Accumulate(_ context.Context, res logqlmodel.Result, _ int) error {
	if res.Data.Type() != QuantileSketchMatrixType {
		return fmt.Errorf("unexpected matrix data type: got (%s), want (%s)", res.Data.Type(), QuantileSketchMatrixType)
	}
	data, ok := res.Data.(ProbabilisticQuantileMatrix)
	if !ok {
		return fmt.Errorf("unexpected matrix type: got (%T), want (ProbabilisticQuantileMatrix)", res.Data)
	}

	// TODO(owen-d/ewelch): Shard counts should be set by the querier
	// so we don't have to do it in tricky ways in multiple places.
	// See pkg/logql/downstream.go:DownstreamEvaluator.Downstream
	// for another example.
	if res.Statistics.Summary.Shards == 0 {
		res.Statistics.Summary.Shards = 1
	}
	a.stats.Merge(res.Statistics)
	metadata.ExtendHeaders(a.headers, res.Headers)

	for _, w := range res.Warnings {
		a.warnings[w] = struct{}{}
	}

	if a.matrix == nil {
		a.matrix = data
		return nil
	}

	var err error
	a.matrix, err = a.matrix.Merge(data)
	a.stats.Merge(res.Statistics)
	return err
}

func (a *QuantileSketchAccumulator) Result() []logqlmodel.Result {
	headers := make([]*definitions.PrometheusResponseHeader, 0, len(a.headers))
	for name, vals := range a.headers {
		headers = append(
			headers,
			&definitions.PrometheusResponseHeader{
				Name:   name,
				Values: vals,
			},
		)
	}

	warnings := slices.Sorted(maps.Keys(a.warnings))

	return []logqlmodel.Result{
		{
			Data:       a.matrix,
			Headers:    headers,
			Warnings:   warnings,
			Statistics: a.stats,
		},
	}
}

type CountMinSketchAccumulator struct {
	vec *CountMinSketchVector

	stats    stats.Result        // for accumulating statistics from downstream requests
	headers  map[string][]string // for accumulating headers from downstream requests
	warnings map[string]struct{} // for accumulating warnings from downstream requests}
}

// newCountMinSketchAccumulator returns an accumulator for sharded
// count min sketch queries that merges results as they come in.
func newCountMinSketchAccumulator() *CountMinSketchAccumulator {
	return &CountMinSketchAccumulator{
		headers:  make(map[string][]string),
		warnings: make(map[string]struct{}),
	}
}

func (a *CountMinSketchAccumulator) Accumulate(_ context.Context, res logqlmodel.Result, _ int) error {
	if res.Data.Type() != CountMinSketchVectorType {
		return fmt.Errorf("unexpected matrix data type: got (%s), want (%s)", res.Data.Type(), CountMinSketchVectorType)
	}
	data, ok := res.Data.(CountMinSketchVector)
	if !ok {
		return fmt.Errorf("unexpected matrix type: got (%T), want (CountMinSketchVector)", res.Data)
	}

	// TODO(owen-d/ewelch): Shard counts should be set by the querier
	// so we don't have to do it in tricky ways in multiple places.
	// See pkg/logql/downstream.go:DownstreamEvaluator.Downstream
	// for another example.
	if res.Statistics.Summary.Shards == 0 {
		res.Statistics.Summary.Shards = 1
	}
	a.stats.Merge(res.Statistics)
	metadata.ExtendHeaders(a.headers, res.Headers)

	for _, w := range res.Warnings {
		a.warnings[w] = struct{}{}
	}

	if a.vec == nil {
		a.vec = &data // TODO: maybe the matrix should already be a pointeer
		return nil
	}

	var err error
	a.vec, err = a.vec.Merge(&data)
	a.stats.Merge(res.Statistics)
	return err
}

func (a *CountMinSketchAccumulator) Result() []logqlmodel.Result {
	headers := make([]*definitions.PrometheusResponseHeader, 0, len(a.headers))
	for name, vals := range a.headers {
		headers = append(
			headers,
			&definitions.PrometheusResponseHeader{
				Name:   name,
				Values: vals,
			},
		)
	}

	warnings := slices.Sorted(maps.Keys(a.warnings))

	return []logqlmodel.Result{
		{
			Data:       a.vec,
			Headers:    headers,
			Warnings:   warnings,
			Statistics: a.stats,
		},
	}
}

// heap impl for keeping only the top n results across m streams
// importantly, AccumulatedStreams is _bounded_, so it will only
// store the top `limit` results across all streams.
// To implement this, we use a min-heap when looking
// for the max values (logproto.FORWARD)
// and vice versa for logproto.BACKWARD.
// This allows us to easily find the 'worst' value
// and replace it with a better one.
// Once we've fully processed all log lines,
// we return the heap in opposite order and then reverse it
// to get the correct order.
// Heap implements container/heap.Interface
// solely to use heap.Interface as a library.
// It is not intended for the heap pkg functions
// to otherwise call this type.
type AccumulatedStreams struct {
	count, limit int
	labelmap     map[string]int
	streams      []*logproto.Stream
	order        logproto.Direction

	stats    stats.Result        // for accumulating statistics from downstream requests
	headers  map[string][]string // for accumulating headers from downstream requests
	warnings map[string]struct{} // for accumulating warnings from downstream requests
}

// NewStreamAccumulator returns an accumulator for limited log queries.
// Log queries, sharded thousands of times and each returning <limit>
// results, can be _considerably_ larger. In this case, we eagerly
// accumulate the results into a logsAccumulator, discarding values
// over the limit to keep memory pressure down while other subqueries
// are executing.
func NewStreamAccumulator(params Params) *AccumulatedStreams {
	// the stream accumulator stores a heap with reversed order
	// from the results we expect, so we need to reverse the direction
	order := logproto.FORWARD
	if params.Direction() == logproto.FORWARD {
		order = logproto.BACKWARD
	}

	return &AccumulatedStreams{
		labelmap: make(map[string]int),
		order:    order,
		limit:    int(params.Limit()),

		headers:  make(map[string][]string),
		warnings: make(map[string]struct{}),
	}
}

// returns the top priority
func (acc *AccumulatedStreams) top() (time.Time, bool) {
	if len(acc.streams) > 0 && len(acc.streams[0].Entries) > 0 {
		return acc.streams[0].Entries[len(acc.streams[0].Entries)-1].Timestamp, true
	}
	return time.Time{}, false
}

func (acc *AccumulatedStreams) Find(labels string) (int, bool) {
	i, ok := acc.labelmap[labels]
	return i, ok
}

// number of streams
func (acc *AccumulatedStreams) Len() int { return len(acc.streams) }

func (acc *AccumulatedStreams) Swap(i, j int) {
	// for i=0, j=1

	// {'a': 0, 'b': 1}
	// [a, b]
	acc.streams[i], acc.streams[j] = acc.streams[j], acc.streams[i]
	// {'a': 0, 'b': 1}
	// [b, a]
	acc.labelmap[acc.streams[i].Labels] = i
	acc.labelmap[acc.streams[j].Labels] = j
	// {'a': 1, 'b': 0}
	// [b, a]
}

// first order by timestamp, then by labels
func (acc *AccumulatedStreams) Less(i, j int) bool {
	// order by the 'oldest' entry in the stream
	if a, b := acc.streams[i].Entries[len(acc.streams[i].Entries)-1].Timestamp, acc.streams[j].Entries[len(acc.streams[j].Entries)-1].Timestamp; !a.Equal(b) {
		return acc.less(a, b)
	}
	return acc.streams[i].Labels <= acc.streams[j].Labels
}

func (acc *AccumulatedStreams) less(a, b time.Time) bool {
	// use after for stable sort
	if acc.order == logproto.FORWARD {
		return !a.After(b)
	}
	return !b.After(a)
}

func (acc *AccumulatedStreams) Push(x any) {
	s := x.(*logproto.Stream)
	if len(s.Entries) == 0 {
		return
	}

	if room := acc.limit - acc.count; room >= len(s.Entries) {
		if i, ok := acc.Find(s.Labels); ok {
			// stream already exists, append entries

			// these are already guaranteed to be sorted
			// Reasoning: we shard subrequests so each stream exists on only one
			// shard. Therefore, the only time a stream should already exist
			// is in successive splits, which are already guaranteed to be ordered
			// and we can just append.
			acc.appendTo(acc.streams[i], s)

			return
		}

		// new stream
		acc.addStream(s)
		return
	}

	// there's not enough room for all the entries,
	// so we need to
	acc.push(s)
}

// there's not enough room for all the entries.
// since we store them in a reverse heap relative to what we _want_
// (i.e. the max value for FORWARD, the min value for BACKWARD),
// we test if the new entry is better than the worst entry,
// swapping them if so.
func (acc *AccumulatedStreams) push(s *logproto.Stream) {
	worst, ok := acc.top()
	room := math.Min(acc.limit-acc.count, len(s.Entries))

	if !ok {
		if room == 0 {
			// special case: limit must be zero since there's no room and no worst entry
			return
		}
		s.Entries = s.Entries[:room]
		// special case: there are no entries in the heap. Push entries up to the limit
		acc.addStream(s)
		return
	}

	// since entries are sorted by timestamp from best -> worst,
	// we can discard the entire stream if the incoming best entry
	// is worse than the worst entry in the heap.
	cutoff := sort.Search(len(s.Entries), func(i int) bool {
		// TODO(refactor label comparison -- should be in another fn)
		if worst.Equal(s.Entries[i].Timestamp) {
			return acc.streams[0].Labels < s.Labels
		}
		return acc.less(s.Entries[i].Timestamp, worst)
	})
	s.Entries = s.Entries[:cutoff]

	for i := 0; i < len(s.Entries) && acc.less(worst, s.Entries[i].Timestamp); i++ {

		// push one entry at a time
		room = acc.limit - acc.count
		// pop if there's no room to make the heap small enough for an append;
		// in the short path of Push() we know that there's room for at least one entry
		if room == 0 {
			acc.Pop()
		}

		cpy := *s
		cpy.Entries = []logproto.Entry{s.Entries[i]}
		acc.Push(&cpy)

		// update worst
		worst, _ = acc.top()
	}
}

func (acc *AccumulatedStreams) addStream(s *logproto.Stream) {
	// ensure entries conform to order we expect
	// TODO(owen-d): remove? should be unnecessary since we insert in appropriate order
	// but it's nice to have the safeguard
	sort.Slice(s.Entries, func(i, j int) bool {
		return acc.less(s.Entries[j].Timestamp, s.Entries[i].Timestamp)
	})

	acc.streams = append(acc.streams, s)
	i := len(acc.streams) - 1
	acc.labelmap[s.Labels] = i
	acc.count += len(s.Entries)
	heap.Fix(acc, i)
}

// dst must already exist in acc
func (acc *AccumulatedStreams) appendTo(dst, src *logproto.Stream) {
	// these are already guaranteed to be sorted
	// Reasoning: we shard subrequests so each stream exists on only one
	// shard. Therefore, the only time a stream should already exist
	// is in successive splits, which are already guaranteed to be ordered
	// and we can just append.

	var needsSort bool
	for _, e := range src.Entries {
		// sort if order has broken
		if len(dst.Entries) > 0 && acc.less(dst.Entries[len(dst.Entries)-1].Timestamp, e.Timestamp) {
			needsSort = true
		}
		dst.Entries = append(dst.Entries, e)
	}

	if needsSort {
		sort.Slice(dst.Entries, func(i, j int) bool {
			// store in reverse order so we can more reliably insert without sorting and pop from end
			return acc.less(dst.Entries[j].Timestamp, dst.Entries[i].Timestamp)
		})
	}

	acc.count += len(src.Entries)
	heap.Fix(acc, acc.labelmap[dst.Labels])
}

// Pop returns a stream with one entry. It pops the first entry of the first stream
func (acc *AccumulatedStreams) Pop() any {
	n := acc.Len()
	if n == 0 {
		return nil
	}

	stream := acc.streams[0]
	cpy := *stream
	cpy.Entries = []logproto.Entry{cpy.Entries[len(stream.Entries)-1]}
	stream.Entries = stream.Entries[:len(stream.Entries)-1]

	acc.count--

	if len(stream.Entries) == 0 {
		// remove stream
		acc.Swap(0, n-1)
		acc.streams[n-1] = nil // avoid leaking reference
		delete(acc.labelmap, stream.Labels)
		acc.streams = acc.streams[:n-1]

	}

	if acc.Len() > 0 {
		heap.Fix(acc, 0)
	}

	return &cpy
}

// Note: can only be called once as it will alter stream ordreing.
func (acc *AccumulatedStreams) Result() []logqlmodel.Result {
	// sort streams by label
	sort.Slice(acc.streams, func(i, j int) bool {
		return acc.streams[i].Labels < acc.streams[j].Labels
	})

	streams := make(logqlmodel.Streams, 0, len(acc.streams))

	for _, s := range acc.streams {
		// sort entries by timestamp, inversely based on direction
		sort.Slice(s.Entries, func(i, j int) bool {
			return acc.less(s.Entries[j].Timestamp, s.Entries[i].Timestamp)
		})
		streams = append(streams, *s)
	}

	res := logqlmodel.Result{
		// stats & headers are already aggregated in the context
		Data:       streams,
		Statistics: acc.stats,
		Headers:    make([]*definitions.PrometheusResponseHeader, 0, len(acc.headers)),
	}

	for name, vals := range acc.headers {
		res.Headers = append(
			res.Headers,
			&definitions.PrometheusResponseHeader{
				Name:   name,
				Values: vals,
			},
		)
	}

	res.Warnings = slices.Sorted(maps.Keys(acc.warnings))

	return []logqlmodel.Result{res}
}

func (acc *AccumulatedStreams) Accumulate(_ context.Context, x logqlmodel.Result, _ int) error {
	// TODO(owen-d/ewelch): Shard counts should be set by the querier
	// so we don't have to do it in tricky ways in multiple places.
	// See pkg/logql/downstream.go:DownstreamEvaluator.Downstream
	// for another example.
	if x.Statistics.Summary.Shards == 0 {
		x.Statistics.Summary.Shards = 1
	}
	acc.stats.Merge(x.Statistics)
	metadata.ExtendHeaders(acc.headers, x.Headers)

	for _, w := range x.Warnings {
		acc.warnings[w] = struct{}{}
	}

	switch got := x.Data.(type) {
	case logqlmodel.Streams:
		for i := range got {
			acc.Push(&got[i])
		}
	default:
		return fmt.Errorf("unexpected response type during response result accumulation. Got (%T), wanted %s", got, logqlmodel.ValueTypeStreams)
	}
	return nil
}
