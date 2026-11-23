package logql

import (
	"fmt"
	"math"
	"runtime/debug"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/histogram"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

// histogramFuzzLimit is how long what the frontend does with a histogram may take. It takes microseconds.
const histogramFuzzLimit = 5 * time.Second

// runGuarded runs f and returns what a panic of it was, or that it did not return within limit. A hang is what the
// sum of two histograms did for a NaN zero threshold, and a test that waits for it never fails. The goroutine of
// a function that hangs is left behind.
func runGuarded(limit time.Duration, f func()) (panicked string, timedOut bool) {
	done := make(chan string, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- fmt.Sprintf("%v\n%s", r, debug.Stack())
				return
			}
			done <- ""
		}()
		f()
	}()
	select {
	case msg := <-done:
		return msg, false
	case <-time.After(limit):
		return "", true
	}
}

// histogramFuzzDo runs f and fails the test when it panics, or does not return.
func histogramFuzzDo(t *testing.T, what string, f func()) {
	t.Helper()
	panicked, timedOut := runGuarded(histogramFuzzLimit, f)
	if panicked != "" {
		t.Fatalf("%s panicked: %s", what, panicked)
	}
	if timedOut {
		t.Fatalf("%s did not return in %s", what, histogramFuzzLimit)
	}
}

// What the fuzz test reports has to be seen to work: it is the only way that a hang or a panic of the sum is found.
func TestHistogramFuzzGuard(t *testing.T) {
	const limit = 50 * time.Millisecond

	panicked, timedOut := runGuarded(limit, func() {})
	require.Empty(t, panicked)
	require.False(t, timedOut)

	panicked, timedOut = runGuarded(limit, func() { var s []int; _ = s[len(s)] })
	require.Contains(t, panicked, "index out of range")
	require.False(t, timedOut)

	release := make(chan struct{})
	defer close(release)
	panicked, timedOut = runGuarded(limit, func() { <-release })
	require.Empty(t, panicked)
	require.True(t, timedOut)
}

// histogramFuzzResult is what checkHistogramProtoFuzz did with its input.
type histogramFuzzResult struct {
	accepted [2]bool // whether the decoder accepted the histogram
	summed   bool    // whether the two were added up, which needs both to be accepted and to fit together
}

// checkHistogramProtoFuzz is what the frontend does with the histograms of two shards, for the bytes of two
// responses that are not trusted: it reads them, adds them up, and reads the sum, which it must do without
// a panic or a hang whatever they hold. What the decoder accepts must also be what the rest relies on, so the
// sum has the observations of both.
func checkHistogramProtoFuzz(t *testing.T, rawA, rawB []byte) histogramFuzzResult {
	t.Helper()
	var (
		res histogramFuzzResult
		hs  [2]*histogram.FloatHistogram
	)
	for i, raw := range [...][]byte{rawA, rawB} {
		var p logproto.NativeHistogram
		if p.Unmarshal(raw) != nil {
			continue
		}
		var (
			h   *histogram.FloatHistogram
			err error
		)
		histogramFuzzDo(t, "reading a histogram", func() { h, err = histogramFromProto(&p) })
		if err != nil {
			continue
		}
		res.accepted[i] = true
		hs[i] = h
		checkAcceptedHistogram(t, h)
	}
	if hs[0] == nil || hs[1] == nil {
		return res
	}

	res.summed = checkHistogramSum(t, hs[0], hs[1])
	// The same histogram added to itself has the same schema, and the same zero threshold, so it is a sum that
	// does not fail for the way the two fit together.
	checkHistogramSum(t, hs[0], hs[0].Copy())
	return res
}

// checkAcceptedHistogram checks what a histogram that was read must be, and that the functions that read a
// histogram return for it.
func checkAcceptedHistogram(t *testing.T, h *histogram.FloatHistogram) {
	t.Helper()
	require.NoError(t, h.Validate())
	// It is what a shard could have sent, so it is what a shard sends: it goes through the wire, and is the same.
	p, err := histogramToProto(h)
	require.NoError(t, err)
	again, err := histogramFromProto(p)
	require.NoError(t, err)
	requireSameHistogram(t, h, again)

	before := h.Copy()
	histogramFuzzDo(t, "reading a histogram with the functions", func() { checkReaders(h) })
	requireSameHistogram(t, before, h)
}

// checkReaders runs every function that reads a histogram on h, with arguments that PromQL accepts.
func checkReaders(h *histogram.FloatHistogram) {
	inf := math.Inf(1)
	for _, e := range []*syntax.HistogramFuncExpr{
		{Operation: syntax.OpHistogramQuantile, Params: []float64{0.5}},
		{Operation: syntax.OpHistogramQuantile, Params: []float64{0}},
		{Operation: syntax.OpHistogramQuantile, Params: []float64{1}},
		{Operation: syntax.OpHistogramQuantile, Params: []float64{-1}},
		{Operation: syntax.OpHistogramQuantile, Params: []float64{2}},
		{Operation: syntax.OpHistogramQuantiles, Label: "q", Params: []float64{0.01, 0.5, 0.99}},
		{Operation: syntax.OpHistogramFraction, Params: []float64{0, 1}},
		{Operation: syntax.OpHistogramFraction, Params: []float64{-inf, inf}},
		{Operation: syntax.OpHistogramFraction, Params: []float64{1, 0}},
		{Operation: syntax.OpHistogramFraction, Params: []float64{1e-300, 1e300}},
		{Operation: syntax.OpHistogramCount},
		{Operation: syntax.OpHistogramSum},
		{Operation: syntax.OpHistogramAvg},
	} {
		ev := &HistogramFuncEvaluator{expr: e}
		ev.appendSamples(nil, 0, promql.Sample{H: h})
	}
}

// checkHistogramSum adds a and b up as the frontend does, which is with the aggregation of sum, and reads the sum.
// It returns whether the two fit together, they do not when one has custom buckets and the other has not.
func checkHistogramSum(t *testing.T, a, b *histogram.FloatHistogram) bool {
	t.Helper()
	beforeA, beforeB := a.Copy(), b.Copy()

	var (
		sum *histogram.FloatHistogram
		err error
	)
	histogramFuzzDo(t, "adding up two histograms", func() {
		var g groupedAggregation
		if err = g.addToSum(promql.Sample{H: a}); err != nil {
			return
		}
		if err = g.addToSum(promql.Sample{H: b}); err != nil {
			return
		}
		sum, err = g.histogramSum()
	})
	// The histograms of the shards are the samples of the sum, and they are not changed by it.
	requireSameHistogram(t, beforeA, a)
	requireSameHistogram(t, beforeB, b)
	if err != nil {
		return false
	}

	// The observations of both are in the sum, in the buckets as well as in the count.
	requireSameObservations(t, "count", a.Count+b.Count, sum.Count)
	requireSameObservations(t, "buckets", observationsInBuckets(a)+observationsInBuckets(b), observationsInBuckets(sum))
	require.NoError(t, sum.Validate())
	histogramFuzzDo(t, "reading the sum of two histograms", func() { checkReaders(sum) })
	return true
}

// observationsInBuckets returns the observations in the buckets of h, the zero bucket included.
func observationsInBuckets(h *histogram.FloatHistogram) float64 {
	total := h.ZeroCount
	for _, c := range h.PositiveBuckets {
		total += c
	}
	for _, c := range h.NegativeBuckets {
		total += c
	}
	return total
}

// requireSameObservations compares counts as floats, which differ in the last digits when they are summed in
// another order.
func requireSameObservations(t *testing.T, what string, want, got float64) {
	t.Helper()
	require.InEpsilon(t, math.Max(want, 1), math.Max(got, 1), 1e-9, "%s of the sum: want %v, got %v", what, want, got)
}

// histogramSeedProto is the wire form of the histogram of the values, which is what a shard sends.
func histogramSeedProto(spec *syntax.BucketSpec, values ...float64) *logproto.NativeHistogram {
	b := newHistogramBuilder(spec)
	for _, v := range values {
		b.observe(v)
	}
	h, err := b.build()
	if err != nil {
		panic(err)
	}
	p, err := histogramToProto(h)
	if err != nil {
		panic(err)
	}
	return p
}

// histogramFuzzSeed is a pair of responses that are not trusted. A histogram that the frontend must refuse is
// marked, and it is refused for the reason of the seed: the responses of the other seeds are as they should be.
type histogramFuzzSeed struct {
	name    string
	a, b    *logproto.NativeHistogram
	refused [2]bool
	summed  bool // whether the pair is added up, when it is not refused
}

func histogramFuzzSeeds() []histogramFuzzSeed {
	good := func() *logproto.NativeHistogram { return histogramSeedProto(nil, 1, 2, 2, -3, 0, 100) }
	other := func() *logproto.NativeHistogram { return histogramSeedProto(nil, 0.5, 2, 9, -3, -300) }
	custom := func(values ...float64) *logproto.NativeHistogram {
		return histogramSeedProto(&syntax.BucketSpec{Bounds: []float64{1, 10, 100}}, values...)
	}
	// with returns the histogram that good has after f.
	with := func(f func(p *logproto.NativeHistogram)) *logproto.NativeHistogram {
		p := good()
		f(p)
		return p
	}
	// one is a histogram with a single bucket, 2 observations at the index of 4.
	one := func(f func(p *logproto.NativeHistogram)) *logproto.NativeHistogram {
		p := &logproto.NativeHistogram{
			Schema: histogramSchema, ZeroThreshold: prometheus.DefNativeHistogramZeroThreshold, Count: 2, Sum: 3,
			PositiveSpans: []*logproto.BucketSpan{{Offset: 4, Length: 1}}, PositiveDeltas: []int64{2},
		}
		f(p)
		return p
	}
	setThreshold := func(v float64) func(p *logproto.NativeHistogram) {
		return func(p *logproto.NativeHistogram) { p.ZeroThreshold = v }
	}
	setOffset := func(v int32) func(p *logproto.NativeHistogram) {
		return func(p *logproto.NativeHistogram) { p.PositiveSpans[0].Offset = v }
	}

	return []histogramFuzzSeed{
		// What a query makes.
		{name: "exponential buckets", a: good(), b: other(), summed: true},
		{name: "custom buckets of the same bounds", a: custom(0.5, 5, 50, 500), b: custom(5, 5, 7), summed: true},
		{name: "custom buckets and exponential ones", a: custom(5), b: good()},
		{name: "an empty histogram", a: histogramSeedProto(nil), b: good(), summed: true},
		{name: "a bucket far from the others", a: one(setOffset(-300)), b: one(setOffset(3000)), summed: true},
		// The lowest bucket is the one above the zero bucket, which has the upper bound 2^-128, and the highest is
		// the one of the largest float.
		{name: "the lowest and the highest buckets", a: one(setOffset(-1023)), b: one(setOffset(8192)), summed: true},

		// The schema and the zero threshold are the ones of the histograms of a query. A pair of them that differ is
		// added up wrongly by Prometheus: a fuzz test found a sum with more observations than its two parts.
		{name: "another zero threshold", a: one(setThreshold(1e-5)), b: one(func(_ *logproto.NativeHistogram) {}), refused: [2]bool{true}},
		{name: "a zero threshold that is zero", a: one(setThreshold(0)), b: good(), refused: [2]bool{true}},
		{name: "another schema", a: one(func(p *logproto.NativeHistogram) { p.Schema = 2; p.PositiveSpans[0].Offset = 2 }), b: good(), refused: [2]bool{true}},
		{name: "a schema that has more buckets", a: one(func(p *logproto.NativeHistogram) { p.Schema = 4 }), b: good(), refused: [2]bool{true}},
		{
			name: "the pair of a fuzz test, that was added up to more observations than it has",
			a:    &logproto.NativeHistogram{Schema: 2, ZeroThreshold: 1.011764705882353},
			b: &logproto.NativeHistogram{
				Schema: 3, ZeroCount: 1, Count: 6,
				PositiveSpans: []*logproto.BucketSpan{{Length: 1}, {Length: 1}, {Length: 1}}, PositiveDeltas: []int64{1, 1, -1},
				NegativeSpans: []*logproto.BucketSpan{{Length: 1}}, NegativeDeltas: []int64{1},
			},
			refused: [2]bool{true, true},
		},

		// The zero threshold is finite, and not negative.
		{name: "NaN zero threshold", a: one(setThreshold(math.NaN())), b: good(), refused: [2]bool{true}},
		{name: "NaN zero thresholds", a: one(setThreshold(math.NaN())), b: one(setThreshold(math.NaN())), refused: [2]bool{true, true}},
		{name: "negative zero threshold", a: one(setThreshold(-5)), b: good(), refused: [2]bool{true}},
		{name: "infinite zero threshold", a: one(setThreshold(math.Inf(1))), b: good(), refused: [2]bool{true}},
		{name: "negative infinite zero threshold", a: one(setThreshold(math.Inf(-1))), b: good(), refused: [2]bool{true}},

		// What the zero bucket covers is in it.
		// The bucket 1024*-8 has the upper bound 2^-128, which is the zero threshold.
		{name: "a bucket inside the zero bucket", a: one(setOffset(-2000)), b: good(), refused: [2]bool{true}},
		{
			name: "a bucket of the negative side inside the zero bucket",
			a: one(func(p *logproto.NativeHistogram) {
				p.NegativeSpans, p.NegativeDeltas, p.PositiveSpans, p.PositiveDeltas = p.PositiveSpans, p.PositiveDeltas, nil, nil
				p.NegativeSpans[0].Offset = -2000
			}),
			b: good(), refused: [2]bool{true},
		},
		{name: "a bucket that is at the zero threshold", a: one(setOffset(-1024)), b: good(), refused: [2]bool{true}},
		{name: "a bucket that is above the zero threshold", a: one(setOffset(-1023)), b: good(), summed: true},
		{
			name: "no observation in a bucket inside the zero bucket",
			a: one(func(p *logproto.NativeHistogram) {
				p.PositiveSpans = []*logproto.BucketSpan{{Offset: -2000, Length: 1}, {Offset: 2003, Length: 1}}
				p.PositiveDeltas = []int64{0, 2}
			}),
			b: good(), summed: true,
		},

		// The index of a bucket is one that a float has.
		{name: "offset at the lowest int32", a: one(setOffset(math.MinInt32)), b: good(), refused: [2]bool{true}},
		{name: "offsets at both ends of the int32", a: one(setOffset(math.MinInt32)), b: one(setOffset(math.MaxInt32)), refused: [2]bool{true, true}},
		{name: "offset at the highest int32", a: one(setOffset(math.MaxInt32)), b: good(), refused: [2]bool{true}},
		{name: "offset past the highest index", a: one(setOffset(1 << 20)), b: good(), refused: [2]bool{true}},
		{name: "offset before the lowest index", a: one(setOffset(-(1 << 20))), b: good(), refused: [2]bool{true}},
		{
			name: "offsets that add up past the highest index",
			a: one(func(p *logproto.NativeHistogram) {
				p.PositiveSpans = []*logproto.BucketSpan{{Offset: 0, Length: 1}, {Offset: math.MaxInt32, Length: 1}, {Offset: math.MaxInt32, Length: 1}}
				p.PositiveDeltas = []int64{1, 0, 0}
				p.Count = 3
			}),
			b: good(), refused: [2]bool{true},
		},
		{
			name: "a span with no bucket and an offset past the highest index",
			a: one(func(p *logproto.NativeHistogram) {
				p.PositiveSpans = append(p.PositiveSpans, &logproto.BucketSpan{Offset: math.MaxInt32})
			}),
			b: good(), refused: [2]bool{true},
		},

		// A span holds a bucket. Added to a histogram that has a bucket at its index, a span that has none panicked
		// in the sum, which a fuzz test found.
		{
			name: "a span that holds no bucket, at the index of a bucket of the other",
			a: one(func(p *logproto.NativeHistogram) {
				p.PositiveSpans, p.PositiveDeltas, p.Count = []*logproto.BucketSpan{{Offset: 4, Length: 0}}, nil, 0
			}),
			b: one(func(_ *logproto.NativeHistogram) {}), refused: [2]bool{true},
		},
		{
			name: "a span that holds no bucket between two that do",
			a: one(func(p *logproto.NativeHistogram) {
				p.PositiveSpans = []*logproto.BucketSpan{{Offset: 4, Length: 1}, {Offset: 1, Length: 0}, {Offset: 1, Length: 1}}
				p.PositiveDeltas = []int64{1, 0}
				p.Count = 2
			}),
			b: good(), refused: [2]bool{true},
		},
		{
			name: "a negative bucket far out",
			a: one(func(p *logproto.NativeHistogram) {
				p.NegativeSpans, p.NegativeDeltas, p.PositiveSpans, p.PositiveDeltas = p.PositiveSpans, p.PositiveDeltas, nil, nil
				p.NegativeSpans[0].Offset = math.MinInt32
			}),
			b: good(), refused: [2]bool{true},
		},

		// The count is the observations in the buckets, and it is a count that a float holds. The NaNs of exponential
		// buckets are the exception: they are in no bucket, and the sum is NaN.
		{name: "NaN sum and a count that is more than the buckets", a: one(func(p *logproto.NativeHistogram) { p.Sum = math.NaN(); p.Count = 99 }), b: good(), summed: true},
		{name: "NaNs added to a histogram that has some", a: histogramSeedProto(nil, 1, math.NaN()), b: histogramSeedProto(nil, 2, math.NaN(), math.NaN()), summed: true},
		{name: "what a query makes of NaN and the infinities", a: histogramSeedProto(nil, 1, math.NaN(), math.Inf(1), math.Inf(-1)), b: good(), summed: true},
		{name: "custom buckets that have a NaN", a: custom(5, math.NaN(), math.Inf(1), math.Inf(-1)), b: custom(5, 5, 7), summed: true},
		{
			name: "custom buckets, a NaN sum and a count that is more than the buckets",
			a:    func() *logproto.NativeHistogram { p := custom(5, math.NaN()); p.Count += 3; return p }(), b: custom(5), refused: [2]bool{true},
		},
		// The NaNs are not in the buckets, so nothing but the count limits them. A fuzz test found a count that the
		// wire format does not hold, which the shard that sent it could not have sent.
		{name: "NaN sum and a count that a float does not hold", a: one(func(p *logproto.NativeHistogram) { p.Sum = math.NaN(); p.Count = 1<<53 + 1 }), b: good(), refused: [2]bool{true}},
		{name: "NaN sum and the most that a float holds", a: one(func(p *logproto.NativeHistogram) { p.Sum = math.NaN(); p.Count = 1 << 53 }), b: good(), summed: true},
		{name: "NaN sum and a count over the integers", a: one(func(p *logproto.NativeHistogram) { p.Sum = math.NaN(); p.Count = math.MaxUint64 }), b: good(), refused: [2]bool{true}},
		{name: "NaN sum and a count that is less", a: one(func(p *logproto.NativeHistogram) { p.Sum = math.NaN(); p.Count = 1 }), b: good(), refused: [2]bool{true}},
		// The bucket of the largest number is 8192, and the infinities are in the one above it.
		{name: "the bucket of the infinities", a: one(setOffset(8193)), b: one(setOffset(8192)), summed: true},
		{name: "the bucket above the one of the infinities", a: one(setOffset(8194)), b: good(), refused: [2]bool{true}},
		{name: "NaN sum and the count of the buckets", a: one(func(p *logproto.NativeHistogram) { p.Sum = math.NaN() }), b: good(), summed: true},
		{name: "infinite sums", a: one(func(p *logproto.NativeHistogram) { p.Sum = math.Inf(1) }), b: one(func(p *logproto.NativeHistogram) { p.Sum = math.Inf(-1) }), summed: true},
		{name: "a count that is more than the buckets", a: with(func(p *logproto.NativeHistogram) { p.Count += 5 }), b: good(), refused: [2]bool{true}},
		{
			name: "buckets that add up past the integers",
			a: one(func(p *logproto.NativeHistogram) {
				p.PositiveSpans = []*logproto.BucketSpan{{Offset: 0, Length: 3}}
				p.PositiveDeltas = []int64{math.MaxInt64 / 2, 0, 0}
				p.Count = 1 // what the sum of the buckets is, after it wraps around
			}),
			b: good(), refused: [2]bool{true},
		},
		{name: "a count that a float does not hold", a: one(func(p *logproto.NativeHistogram) { p.Count = 1<<53 + 1; p.PositiveDeltas = []int64{1<<53 + 1} }), b: good(), refused: [2]bool{true}},
		{name: "the most that a float holds", a: one(func(p *logproto.NativeHistogram) { p.Count = 1 << 53; p.PositiveDeltas = []int64{1 << 53} }), b: good(), summed: true},

		// The bounds of custom buckets are finite.
		{
			name: "a custom bound that is -Inf",
			a: &logproto.NativeHistogram{
				Schema: histogram.CustomBucketsSchema, Count: 1, Sum: 1, CustomValues: []float64{math.Inf(-1), 1},
				PositiveSpans: []*logproto.BucketSpan{{Offset: 1, Length: 1}}, PositiveDeltas: []int64{1},
			},
			b: custom(5), refused: [2]bool{true},
		},
	}
}

// A test that skips what it is given checks nothing, and a fuzz test cannot tell. Every seed is read as it should
// be here, the responses that must be refused are, and the pairs that are added up are checked by the sum.
func TestHistogramProtoFuzzSeedsAreChecked(t *testing.T) {
	var summed, refused int
	for _, seed := range histogramFuzzSeeds() {
		t.Run(seed.name, func(t *testing.T) {
			a, err := seed.a.Marshal()
			require.NoError(t, err)
			b, err := seed.b.Marshal()
			require.NoError(t, err)

			res := checkHistogramProtoFuzz(t, a, b)
			for i := range seed.refused {
				require.Equal(t, seed.refused[i], !res.accepted[i], "the histogram %d is refused", i)
			}
			require.Equal(t, seed.summed, res.summed, "the pair is added up")
			if seed.summed {
				summed++
			}
			for _, r := range seed.refused {
				if r {
					refused++
				}
			}
		})
	}
	require.NotZero(t, summed)
	require.NotZero(t, refused)
}

// FuzzHistogramFromProto runs checkHistogramProtoFuzz on mutations of the seeds, which are two responses.
// Run it with
//
//	go test -run '^$' -fuzz FuzzHistogramFromProto -fuzztime 1m ./pkg/logql
//
// Without -fuzz it runs the seeds as a regular test.
func FuzzHistogramFromProto(f *testing.F) {
	for _, seed := range histogramFuzzSeeds() {
		a, err := seed.a.Marshal()
		require.NoError(f, err)
		b, err := seed.b.Marshal()
		require.NoError(f, err)
		f.Add(a, b)
	}
	f.Fuzz(func(t *testing.T, a, b []byte) {
		checkHistogramProtoFuzz(t, a, b)
	})
}
