package logql

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	promql_parser "github.com/prometheus/prometheus/promql/parser"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
)

// streamFirstTestSamples is a per-stream sample set with varied values and timestamps, so
// value-sensitive aggregations (sum/avg/min/max/rate-values) are actually exercised.
var streamFirstTestSamples = []logproto.Sample{
	{Timestamp: time.Unix(2, 0).UnixNano(), Hash: 1, Value: 3},
	{Timestamp: time.Unix(5, 0).UnixNano(), Hash: 2, Value: 7},
	{Timestamp: time.Unix(6, 0).UnixNano(), Hash: 3, Value: 2},
	{Timestamp: time.Unix(10, 0).UnixNano(), Hash: 4, Value: 5},
	{Timestamp: time.Unix(11, 0).UnixNano(), Hash: 5, Value: 9},
	{Timestamp: time.Unix(35, 0).UnixNano(), Hash: 6, Value: 4},
	{Timestamp: time.Unix(40, 0).UnixNano(), Hash: 7, Value: 8},
	{Timestamp: time.Unix(70, 0).UnixNano(), Hash: 8, Value: 1},
	{Timestamp: time.Unix(100, 0).UnixNano(), Hash: 9, Value: 6},
	{Timestamp: time.Unix(105, 0).UnixNano(), Hash: 10, Value: 2},
}

type rangeVectorStep struct {
	ts     int64
	values map[string]float64
}

// drainRangeVector collects every step emitted by a RangeVectorIterator.
func drainRangeVector(t *testing.T, it RangeVectorIterator) []rangeVectorStep {
	t.Helper()
	var steps []rangeVectorStep
	for it.Next() {
		ts, vec := it.At()
		values := map[string]float64{}
		for _, s := range vec.SampleVector() {
			values[s.Metric.String()] = s.F
		}
		steps = append(steps, rangeVectorStep{ts: ts, values: values})
	}
	require.NoError(t, it.Error())
	require.NoError(t, it.Close())
	return steps
}

func parseRangeAgg(t *testing.T, query string) *syntax.RangeAggregationExpr {
	t.Helper()
	expr := syntax.MustParseExpr(query)
	rangeExpr, ok := expr.(*syntax.RangeAggregationExpr)
	require.Truef(t, ok, "query %q did not parse to a RangeAggregationExpr (got %T)", query, expr)
	return rangeExpr
}

// TestStreamFirstMatchesTimestampFirst is the primary correctness test: for every eligible
// (decomposable) range aggregation and a range of query shapes (instant, overlapping and
// non-overlapping range queries, with and without offset), the stream-first iterator must
// produce the same result as the default timestamp-first (batch/stream) iterator over identical
// input.
func TestStreamFirstMatchesTimestampFirst(t *testing.T) {
	ops := []struct {
		name   string
		fn     string
		unwrap bool
	}{
		{"count_over_time", "count_over_time", false},
		{"sum_over_time", "sum_over_time", true},
		{"bytes_over_time", "bytes_over_time", false},
		{"rate_count", "rate", false},
		{"rate_values", "rate", true},
		{"bytes_rate", "bytes_rate", false},
		{"avg_over_time", "avg_over_time", true},
		{"min_over_time", "min_over_time", true},
		{"max_over_time", "max_over_time", true},
	}

	shapes := []struct {
		name     string
		interval time.Duration
		step     time.Duration
		start    time.Time
		end      time.Time
		offset   time.Duration
	}{
		{"instant-long-range", 120 * time.Second, 0, time.Unix(105, 0), time.Unix(105, 0), 0},
		{"range-overlap", 30 * time.Second, 10 * time.Second, time.Unix(40, 0), time.Unix(120, 0), 0},
		{"range-nonoverlap", 20 * time.Second, 40 * time.Second, time.Unix(40, 0), time.Unix(200, 0), 0},
		{"range-overlap-offset", 30 * time.Second, 15 * time.Second, time.Unix(60, 0), time.Unix(160, 0), 10 * time.Second},
	}

	for _, op := range ops {
		for _, shape := range shapes {
			t.Run(fmt.Sprintf("%s/%s", op.name, shape.name), func(t *testing.T) {
				intervalStr := fmt.Sprintf("%ds", int(shape.interval.Seconds()))
				var query string
				if op.unwrap {
					query = fmt.Sprintf(`%s({app="foo"} | unwrap foo [%s])`, op.fn, intervalStr)
				} else {
					query = fmt.Sprintf(`%s({app="foo"}[%s])`, op.fn, intervalStr)
				}
				expr := parseRangeAgg(t, query)

				selRange := expr.Left.Interval.Nanoseconds()
				step := shape.step.Nanoseconds()
				start := shape.start.UnixNano()
				end := shape.end.UnixNano()
				offset := shape.offset.Nanoseconds()

				def, err := newRangeVectorIterator(
					newfakePeekingSampleIterator(streamFirstTestSamples),
					expr, selRange, step, start, end, offset, logproto.SAMPLE_ORDER_BY_TIMESTAMP)
				require.NoError(t, err)

				sf, err := newRangeVectorIterator(
					newfakePeekingSampleIterator(streamFirstTestSamples),
					expr, selRange, step, start, end, offset, logproto.SAMPLE_ORDER_BY_STREAM)
				require.NoError(t, err)

				want := drainRangeVector(t, def)
				got := drainRangeVector(t, sf)

				require.Equal(t, len(want), len(got), "different number of steps")
				for i := range want {
					require.Equal(t, want[i].ts, got[i].ts, "step %d timestamp mismatch", i)
					require.Equal(t, len(want[i].values), len(got[i].values),
						"step %d (ts=%d) series count mismatch: want=%v got=%v", i, want[i].ts, want[i].values, got[i].values)
					for lbs, wantV := range want[i].values {
						gotV, ok := got[i].values[lbs]
						require.Truef(t, ok, "step %d missing series %q", i, lbs)
						require.InDeltaf(t, wantV, gotV, 1e-6, "step %d series %q value mismatch", i, lbs)
					}
				}
			})
		}
	}
}

// TestStreamFirstEmptyAndSparse covers windows with no samples and single-sample windows.
func TestStreamFirstEmptyAndSparse(t *testing.T) {
	expr := parseRangeAgg(t, `count_over_time({app="foo"}[10s])`)
	selRange := expr.Left.Interval.Nanoseconds()

	// A query window entirely before any sample: no series emitted at any step.
	single := []logproto.Sample{{Timestamp: time.Unix(50, 0).UnixNano(), Hash: 1, Value: 1}}

	def, err := newRangeVectorIterator(
		newfakePeekingSampleIterator(single),
		expr, selRange, (10 * time.Second).Nanoseconds(),
		time.Unix(10, 0).UnixNano(), time.Unix(100, 0).UnixNano(), 0, logproto.SAMPLE_ORDER_BY_TIMESTAMP)
	require.NoError(t, err)
	sf, err := newRangeVectorIterator(
		newfakePeekingSampleIterator(single),
		expr, selRange, (10 * time.Second).Nanoseconds(),
		time.Unix(10, 0).UnixNano(), time.Unix(100, 0).UnixNano(), 0, logproto.SAMPLE_ORDER_BY_STREAM)
	require.NoError(t, err)

	want := drainRangeVector(t, def)
	got := drainRangeVector(t, sf)
	require.Equal(t, want, got)
}

// singleSampleIter is a SampleIterator that yields one sample carrying an arbitrary (possibly
// invalid) labels string, so the stream-first iterator's label parsing can be exercised.
type singleSampleIter struct {
	labels string
	sample logproto.Sample
	done   bool
}

func (s *singleSampleIter) Next() bool {
	if s.done {
		return false
	}
	s.done = true
	return true
}
func (s *singleSampleIter) At() logproto.Sample { return s.sample }
func (s *singleSampleIter) Labels() string      { return s.labels }
func (s *singleSampleIter) StreamHash() uint64  { return 0 }
func (s *singleSampleIter) Err() error          { return nil }
func (s *singleSampleIter) Close() error        { return nil }

// TestStreamFirstErrorsOnUnparsableLabels asserts the stream-first iterator surfaces (rather than
// silently drops) a sample whose series labels cannot be parsed: it yields no steps and Error()
// returns the parse failure.
func TestStreamFirstErrorsOnUnparsableLabels(t *testing.T) {
	expr := parseRangeAgg(t, `count_over_time({app="foo"}[10s])`)
	selRange := expr.Left.Interval.Nanoseconds()

	src := iter.NewPeekingSampleIterator(&singleSampleIter{
		labels: "{invalid", // not a valid label set: ParseMetric must reject it
		sample: logproto.Sample{Timestamp: time.Unix(15, 0).UnixNano(), Hash: 1, Value: 1},
	})

	it, err := newRangeVectorIterator(
		src, expr, selRange, (10 * time.Second).Nanoseconds(),
		time.Unix(10, 0).UnixNano(), time.Unix(100, 0).UnixNano(), 0, logproto.SAMPLE_ORDER_BY_STREAM)
	require.NoError(t, err)

	require.False(t, it.Next(), "iterator must not yield steps when a sample's labels cannot be parsed")
	require.Error(t, it.Error(), "the label parse error must surface via Error()")
	require.NoError(t, it.Close())
}

// fixedSamplesQuerier is a logql.Querier that returns a fixed sample set for every
// SelectSamples call. It honors the requested SampleOrder (returning stream-first input when
// SAMPLE_ORDER_BY_STREAM is set, mimicking the real querier) and records the orders it saw, so an
// end-to-end test can both compare flag-off vs flag-on results and confirm the evaluator
// propagates the order via the request field.
type fixedSamplesQuerier struct {
	samples   []logproto.Sample
	sawOrders *[]logproto.SampleOrder
}

func (q fixedSamplesQuerier) SelectLogs(context.Context, SelectLogParams) (iter.EntryIterator, error) {
	return nil, errors.New("SelectLogs not implemented")
}

func (q fixedSamplesQuerier) SelectSamples(ctx context.Context, params SelectSampleParams) (iter.SampleIterator, error) {
	sampleOrder := params.Order
	if q.sawOrders != nil {
		*q.sawOrders = append(*q.sawOrders, sampleOrder)
	}
	foo := iter.NewSeriesIterator(logproto.Series{
		Labels: labelFoo.String(), StreamHash: labels.StableHash(labelFoo), Samples: q.samples,
	})
	bar := iter.NewSeriesIterator(logproto.Series{
		Labels: labelBar.String(), StreamHash: labels.StableHash(labelBar), Samples: q.samples,
	})
	if sampleOrder == logproto.SAMPLE_ORDER_BY_STREAM {
		// Stream-first: group per stream, ordered by streamHash — what the real querier returns.
		return iter.NewStreamFirstMergeSampleIterator(ctx, []iter.SampleIterator{foo, bar}), nil
	}
	return iter.NewSortSampleIterator([]iter.SampleIterator{foo, bar}), nil
}

// normalizeResult flattens a matrix/vector result into labels -> points for comparison.
func normalizeResult(t *testing.T, data promql_parser.Value) map[string][]promql.FPoint {
	t.Helper()
	out := map[string][]promql.FPoint{}
	switch v := data.(type) {
	case promql.Matrix:
		for _, s := range v {
			out[s.Metric.String()] = append([]promql.FPoint{}, s.Floats...)
		}
	case promql.Vector:
		for _, s := range v {
			out[s.Metric.String()] = []promql.FPoint{{T: s.T, F: s.F}}
		}
	default:
		t.Fatalf("unexpected result type %T", data)
	}
	return out
}

// TestEngineStreamFirstEndToEnd runs metric queries through the engine with the stream-ordered
// execution flag off and on, over identical input, and asserts the results match. This validates
// the wiring (NewStepEvaluator -> newRangeAggEvaluator -> stream-first iterator) end to end,
// including the sum push-down path and the fallback for non-decomposable aggregations.
func TestEngineStreamFirstEndToEnd(t *testing.T) {
	var offOrders, onOrders []logproto.SampleOrder
	offQ := fixedSamplesQuerier{samples: streamFirstTestSamples, sawOrders: &offOrders}
	onQ := fixedSamplesQuerier{samples: streamFirstTestSamples, sawOrders: &onOrders}

	off := NewEngine(EngineOpts{}, offQ, NoLimits, log.NewNopLogger())
	on := NewEngine(EngineOpts{StreamOrderedExecutionEnabled: true}, onQ, NoLimits, log.NewNopLogger())

	queries := []string{
		`count_over_time({app="foo"}[30s])`,
		`sum(count_over_time({app="foo"}[30s]))`,
		`sum by (app) (rate({app="foo"}[30s]))`,
		`bytes_over_time({app="foo"}[30s])`,
		`max_over_time({app="foo"} | unwrap foo [30s])`,
		`avg_over_time({app="foo"} | unwrap foo [30s])`,
		// non-decomposable: flag-on must fall back to the default path and match.
		`quantile_over_time(0.9, {app="foo"} | unwrap foo [30s])`,
	}

	run := func(eng *QueryEngine, qs string, start, end time.Time, step time.Duration) promql_parser.Value {
		params, err := NewLiteralParams(qs, start, end, step, 0, logproto.FORWARD, 0, nil, nil)
		require.NoError(t, err)
		res, err := eng.Query(params).Exec(user.InjectOrgID(context.Background(), "fake"))
		require.NoError(t, err)
		return res.Data
	}

	shapes := []struct {
		name       string
		start, end time.Time
		step       time.Duration
	}{
		{"instant", time.Unix(105, 0), time.Unix(105, 0), 0},
		{"range", time.Unix(40, 0), time.Unix(120, 0), 10 * time.Second},
	}

	for _, qs := range queries {
		for _, sh := range shapes {
			t.Run(fmt.Sprintf("%s/%s", qs, sh.name), func(t *testing.T) {
				offOrders, onOrders = offOrders[:0], onOrders[:0]

				want := normalizeResult(t, run(off, qs, sh.start, sh.end, sh.step))
				got := normalizeResult(t, run(on, qs, sh.start, sh.end, sh.step))

				// The default engine never requests stream ordering.
				for _, o := range offOrders {
					require.Equal(t, logproto.SAMPLE_ORDER_BY_TIMESTAMP, o, "flag-off must not request stream order")
				}
				// With the flag on, the router (getSampleOrderForExpr) engages stream-first execution
				// for every decomposable op, on both range and instant shapes; only non-decomposable
				// ops (quantile_over_time) fall back to timestamp order.
				expectStreamFirst := !strings.Contains(qs, "quantile_over_time")
				require.NotEmpty(t, onOrders)
				for _, o := range onOrders {
					if expectStreamFirst {
						require.Equal(t, logproto.SAMPLE_ORDER_BY_STREAM, o, "flag-on must request stream order for %q (%s)", qs, sh.name)
					} else {
						require.Equal(t, logproto.SAMPLE_ORDER_BY_TIMESTAMP, o, "flag-on must fall back for %q (%s)", qs, sh.name)
					}
				}

				require.Equal(t, len(want), len(got), "series count mismatch")
				for lbs, wantPts := range want {
					gotPts, ok := got[lbs]
					require.Truef(t, ok, "missing series %q", lbs)
					require.Equalf(t, len(wantPts), len(gotPts), "series %q point count mismatch", lbs)
					for i := range wantPts {
						require.Equal(t, wantPts[i].T, gotPts[i].T, "series %q point %d ts mismatch", lbs, i)
						require.InDeltaf(t, wantPts[i].F, gotPts[i].F, 1e-6, "series %q point %d value mismatch", lbs, i)
					}
				}
			})
		}
	}
}

func TestGetSampleOrderForExpr(t *testing.T) {
	// All cases run with stream-ordered execution enabled: decomposable ops route to stream order,
	// non-decomposable ops stay on the default per-timestamp order. (The disabled-flag gate is
	// covered end-to-end by TestEngineStreamFirstEndToEnd.)
	cases := map[string]struct {
		query string
		want  logproto.SampleOrder
	}{
		"count_over_time (decomposable) -> stream order":           {`count_over_time({app="foo"}[30s])`, logproto.SAMPLE_ORDER_BY_STREAM},
		"sum_over_time via unwrap (decomposable) -> stream order":  {`sum_over_time({app="foo"} | unwrap v [30s])`, logproto.SAMPLE_ORDER_BY_STREAM},
		"bytes_over_time (decomposable) -> stream order":           {`bytes_over_time({app="foo"}[30s])`, logproto.SAMPLE_ORDER_BY_STREAM},
		"bytes_rate (decomposable) -> stream order":                {`bytes_rate({app="foo"}[30s])`, logproto.SAMPLE_ORDER_BY_STREAM},
		"rate (decomposable) -> stream order":                      {`rate({app="foo"}[30s])`, logproto.SAMPLE_ORDER_BY_STREAM},
		"avg_over_time via unwrap (decomposable) -> stream order":  {`avg_over_time({app="foo"} | unwrap v [30s])`, logproto.SAMPLE_ORDER_BY_STREAM},
		"min_over_time via unwrap (decomposable) -> stream order":  {`min_over_time({app="foo"} | unwrap v [30s])`, logproto.SAMPLE_ORDER_BY_STREAM},
		"max_over_time via unwrap (decomposable) -> stream order":  {`max_over_time({app="foo"} | unwrap v [30s])`, logproto.SAMPLE_ORDER_BY_STREAM},
		"quantile_over_time (non-decomposable) -> timestamp order": {`quantile_over_time(0.9, {app="foo"} | unwrap v [30s])`, logproto.SAMPLE_ORDER_BY_TIMESTAMP},
		"stddev_over_time (non-decomposable) -> timestamp order":   {`stddev_over_time({app="foo"} | unwrap v [30s])`, logproto.SAMPLE_ORDER_BY_TIMESTAMP},
		"stdvar_over_time (non-decomposable) -> timestamp order":   {`stdvar_over_time({app="foo"} | unwrap v [30s])`, logproto.SAMPLE_ORDER_BY_TIMESTAMP},
		"first_over_time (non-decomposable) -> timestamp order":    {`first_over_time({app="foo"} | unwrap v [30s])`, logproto.SAMPLE_ORDER_BY_TIMESTAMP},
		"last_over_time (non-decomposable) -> timestamp order":     {`last_over_time({app="foo"} | unwrap v [30s])`, logproto.SAMPLE_ORDER_BY_TIMESTAMP},
		"absent_over_time (non-decomposable) -> timestamp order":   {`absent_over_time({app="foo"}[30s])`, logproto.SAMPLE_ORDER_BY_TIMESTAMP},
		"rate_counter (non-decomposable) -> timestamp order":       {`rate_counter({app="foo"} | unwrap v [30s])`, logproto.SAMPLE_ORDER_BY_TIMESTAMP},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			expr := parseRangeAgg(t, c.query)
			require.Equal(t, c.want, getSampleOrderForExpr(true, expr))
		})
	}
}

func TestCeilFloorDivInt64(t *testing.T) {
	cases := []struct{ a, b, expectedCeil, expectedFloor int64 }{
		{10, 3, 4, 3},
		{9, 3, 3, 3},
		{-1, 3, 0, -1},
		{-3, 3, -1, -1},
		{-4, 3, -1, -2},
		{0, 3, 0, 0},
	}
	for _, c := range cases {
		require.Equalf(t, c.expectedCeil, ceilDivInt64(c.a, c.b), "ceilDivInt64(%d,%d)", c.a, c.b)
		require.Equalf(t, c.expectedFloor, floorDivInt64(c.a, c.b), "floorDivInt64(%d,%d)", c.a, c.b)
	}
}

// genSampleIterator lazily generates samples for numStreams streams, each with n samples
// evenly spread across [0, spanNs], WITHOUT materializing them all. Samples are emitted in
// global timestamp order (all streams at t0, then all streams at t1, ...), which the default
// range-vector iterator requires. Being lazy is essential for the memory comparison: it
// mimics storage streaming samples, so peak memory reflects each evaluator's own working set
// (a full window of raw samples for the default path vs. one accumulator per series for the
// stream-first path) rather than a pre-materialized input dominating both.
type genSampleIterator struct {
	labelStrs  []string
	hashes     []uint64
	n          int
	numStreams int
	spanNs     int64

	// collapse models the sum() label-reduction push-down: every sample is emitted under a
	// single empty-labelled series, so the range-vector iterator sees exactly one series.
	collapse   bool
	emptyLabel string
	emptyHash  uint64

	started   bool
	i, s      int
	curSample logproto.Sample
	curLabel  string
	curHash   uint64
}

func newGenSampleIterator(numStreams, samplesPerStream, spanSeconds int, collapse bool) *genSampleIterator {
	labelStrs := make([]string, numStreams)
	hashes := make([]uint64, numStreams)
	for s := 0; s < numStreams; s++ {
		lbls := labels.FromStrings("app", "foo", "instance", fmt.Sprintf("i-%d", s))
		labelStrs[s] = lbls.String()
		hashes[s] = labels.StableHash(lbls)
	}
	return &genSampleIterator{
		labelStrs:  labelStrs,
		hashes:     hashes,
		n:          samplesPerStream,
		numStreams: numStreams,
		spanNs:     int64(spanSeconds) * int64(time.Second),
		collapse:   collapse,
		emptyLabel: labels.EmptyLabels().String(),
		emptyHash:  labels.StableHash(labels.EmptyLabels()),
	}
}

func (g *genSampleIterator) Next() bool {
	if !g.started {
		g.started, g.i, g.s = true, 0, -1
	}
	g.s++
	if g.s >= g.numStreams {
		g.s = 0
		g.i++
	}
	if g.i >= g.n {
		return false
	}
	ts := int64(g.i) * g.spanNs / int64(g.n)
	// Globally-unique per-sample hash (safe whether or not series are collapsed).
	hash := uint64(g.i)*uint64(g.numStreams) + uint64(g.s)
	g.curSample = logproto.Sample{Timestamp: ts, Hash: hash, Value: float64(g.i%97 + 1)}
	if g.collapse {
		g.curLabel = g.emptyLabel
		g.curHash = g.emptyHash
	} else {
		g.curLabel = g.labelStrs[g.s]
		g.curHash = g.hashes[g.s]
	}
	return true
}

func (g *genSampleIterator) At() logproto.Sample { return g.curSample }
func (g *genSampleIterator) Labels() string      { return g.curLabel }
func (g *genSampleIterator) StreamHash() uint64  { return g.curHash }
func (g *genSampleIterator) Err() error          { return nil }
func (g *genSampleIterator) Close() error        { return nil }

// buildLazySampleFactory returns a factory producing a fresh lazy iterator on each call.
func buildLazySampleFactory(numStreams, samplesPerStream, spanSeconds int) func() iter.PeekingSampleIterator {
	return func() iter.PeekingSampleIterator {
		return iter.NewPeekingSampleIterator(newGenSampleIterator(numStreams, samplesPerStream, spanSeconds, false))
	}
}

func TestStreamFirstWindowRange(t *testing.T) {
	// Steps 0..10 at s_k = start + k*step = 0,10,...,100; each step k covers the window
	// (s_k-selRange, s_k]. windowRange reports the inclusive step indices whose window contains ts.
	it := &streamFirstRangeVectorIterator{start: 0, step: 10, selRange: 30, lastStep: 10}

	cases := map[string]struct {
		ts     int64
		lo, hi int
		ok     bool
	}{
		"at start covers the first three windows": {ts: 0, lo: 0, hi: 2, ok: true},
		"mid-step shifts the range up":            {ts: 5, lo: 1, hi: 3, ok: true},
		"on a step boundary":                      {ts: 10, lo: 1, hi: 3, ok: true},
		"before start but within lookback":        {ts: -5, lo: 0, hi: 2, ok: true},
		"far before start matches no window":      {ts: -40, ok: false},
		"on the last step, clamped to lastStep":   {ts: 100, lo: 10, hi: 10, ok: true},
		"past the last window matches nothing":    {ts: 101, ok: false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			lo, hi, ok := it.windowRange(c.ts)
			require.Equal(t, c.ok, ok, "ok")
			if c.ok {
				require.Equal(t, c.lo, lo, "lo")
				require.Equal(t, c.hi, hi, "hi")
			}
		})
	}
}

// BenchmarkRangeVectorIterator compares the default (timestamp-first) and stream-first range-vector
// evaluators across a range of window / step / cardinality shapes. Sub-benchmarks are keyed by mode
// and scenario so each shape can be compared across the two modes.
func BenchmarkRangeVectorIterator(b *testing.B) {
	scenarios := map[string]struct {
		numStreams       int
		samplesPerStream int
		spanSeconds      int
		interval         time.Duration
		step             time.Duration
		start, end       time.Time
	}{
		"window=2h step=30m streams=2000": {
			numStreams:       2000,
			samplesPerStream: 15000,
			spanSeconds:      7200,
			interval:         2 * time.Hour,
			step:             30 * time.Minute,
			start:            time.Unix(7200, 0),
			end:              time.Unix(7200+90*60, 0),
		},
		"window=30m step=1m streams=2000": {
			numStreams:       2000,
			samplesPerStream: 8000,
			spanSeconds:      3600,
			interval:         30 * time.Minute,
			step:             time.Minute,
			start:            time.Unix(0, 0),
			end:              time.Unix(3600, 0),
		},
		"window=1m step=5s streams=6000": {
			numStreams:       6000,
			samplesPerStream: 5000,
			spanSeconds:      25000,
			interval:         time.Minute,
			step:             5 * time.Second,
			start:            time.Unix(0, 0),
			end:              time.Unix(25000, 0),
		},
	}

	modes := []struct {
		name  string
		order logproto.SampleOrder
	}{
		{"timestamp-first", logproto.SAMPLE_ORDER_BY_TIMESTAMP},
		{"stream-first", logproto.SAMPLE_ORDER_BY_STREAM},
	}

	for _, m := range modes {
		b.Run(m.name, func(b *testing.B) {
			for name, sc := range scenarios {
				b.Run(name, func(b *testing.B) {
					factory := buildLazySampleFactory(sc.numStreams, sc.samplesPerStream, sc.spanSeconds)
					expr := &syntax.RangeAggregationExpr{
						Operation: syntax.OpRangeTypeCount,
						Left:      &syntax.LogRangeExpr{Interval: sc.interval},
					}
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						it, err := newRangeVectorIterator(
							factory(), expr,
							sc.interval.Nanoseconds(), sc.step.Nanoseconds(),
							sc.start.UnixNano(), sc.end.UnixNano(), 0, m.order)
						if err != nil {
							b.Fatal(err)
						}
						for it.Next() {
							_, _ = it.At()
						}
						_ = it.Close()
					}
				})
			}
		})
	}
}

// TestEngineRecordsSampleOrderStats asserts the engine records the resolved sample order into the
// query stats: stream-first sub-evaluations when the flag is on for a decomposable op, timestamp-
// first when off. This covers the stats plumbing (evaluator decision -> stats context -> Result).
func TestEngineRecordsSampleOrderStats(t *testing.T) {
	run := func(streamOrdered bool) logqlmodel.Result {
		eng := NewEngine(EngineOpts{StreamOrderedExecutionEnabled: streamOrdered},
			fixedSamplesQuerier{samples: streamFirstTestSamples}, NoLimits, log.NewNopLogger())
		params, err := NewLiteralParams(`count_over_time({app="foo"}[30s])`,
			time.Unix(40, 0), time.Unix(120, 0), 10*time.Second, 0, logproto.FORWARD, 0, nil, nil)
		require.NoError(t, err)
		res, err := eng.Query(params).Exec(user.InjectOrgID(context.Background(), "fake"))
		require.NoError(t, err)
		return res
	}

	on := run(true)
	require.Positive(t, on.Statistics.Summary.StreamFirstSubqueries, "flag on: stream-first sub-evaluations recorded")
	require.Zero(t, on.Statistics.Summary.TimestampFirstSubqueries, "flag on: no timestamp-first sub-evaluations")

	off := run(false)
	require.Positive(t, off.Statistics.Summary.TimestampFirstSubqueries, "flag off: timestamp-first sub-evaluations recorded")
	require.Zero(t, off.Statistics.Summary.StreamFirstSubqueries, "flag off: no stream-first sub-evaluations")
}
