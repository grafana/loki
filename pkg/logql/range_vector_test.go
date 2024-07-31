package logql

import (
	"context"
	"fmt"
	"math/rand"
	"sort"
	"testing"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/sketch"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logql/vector"
)

var samples = []logproto.Sample{
	{Timestamp: time.Unix(2, 0).UnixNano(), Hash: 1, Value: 1.},
	{Timestamp: time.Unix(5, 0).UnixNano(), Hash: 2, Value: 1.},
	{Timestamp: time.Unix(6, 0).UnixNano(), Hash: 3, Value: 1.},
	{Timestamp: time.Unix(10, 0).UnixNano(), Hash: 4, Value: 1.},
	{Timestamp: time.Unix(10, 1).UnixNano(), Hash: 5, Value: 1.},
	{Timestamp: time.Unix(11, 0).UnixNano(), Hash: 6, Value: 1.},
	{Timestamp: time.Unix(35, 0).UnixNano(), Hash: 7, Value: 1.},
	{Timestamp: time.Unix(35, 1).UnixNano(), Hash: 8, Value: 1.},
	{Timestamp: time.Unix(40, 0).UnixNano(), Hash: 9, Value: 1.},
	{Timestamp: time.Unix(100, 0).UnixNano(), Hash: 10, Value: 1.},
	{Timestamp: time.Unix(100, 1).UnixNano(), Hash: 11, Value: 1.},
}

var (
	labelFoo, _ = syntax.ParseLabels("{app=\"foo\"}")
	labelBar, _ = syntax.ParseLabels("{app=\"bar\"}")
)

func newSampleIterator(samples []logproto.Sample) iter.SampleIterator {
	return iter.NewSortSampleIterator([]iter.SampleIterator{
		iter.NewSeriesIterator(logproto.Series{
			Labels:     labelFoo.String(),
			Samples:    samples,
			StreamHash: labelFoo.Hash(),
		}),
		iter.NewSeriesIterator(logproto.Series{
			Labels:     labelBar.String(),
			Samples:    samples,
			StreamHash: labelBar.Hash(),
		}),
	})
}

func newfakePeekingSampleIterator(samples []logproto.Sample) iter.PeekingSampleIterator {
	return iter.NewPeekingSampleIterator(newSampleIterator(samples))
}

func newSample(t time.Time, v float64, metric labels.Labels) promql.Sample {
	return promql.Sample{Metric: metric, T: t.UnixMilli(), F: v}
}

func newPoint(t time.Time, v float64) promql.FPoint {
	return promql.FPoint{T: t.UnixMilli(), F: v}
}

func Benchmark_RangeVectorIteratorCompare(b *testing.B) {

	// no overlap test case.
	buildStreamingIt := func() (RangeVectorIterator, error) {
		tt := struct {
			selRange   int64
			step       int64
			offset     int64
			start, end time.Time
		}{
			(5 * time.Second).Nanoseconds(),
			(30 * time.Second).Nanoseconds(),
			0,
			time.Unix(10, 0),
			time.Unix(100, 0),
		}

		iter := newfakePeekingSampleIterator(samples)
		expr := &syntax.RangeAggregationExpr{Operation: syntax.OpRangeTypeCount}
		selRange := tt.selRange
		step := tt.step
		start := tt.start.UnixNano()
		end := tt.end.UnixNano()
		offset := tt.offset
		if step == 0 {
			step = 1
		}
		if offset != 0 {
			start = start - offset
			end = end - offset
		}

		it := &streamRangeVectorIterator{
			iter:     iter,
			step:     step,
			end:      end,
			selRange: selRange,
			metrics:  map[string]labels.Labels{},
			r:        expr,
			current:  start - step, // first loop iteration will set it to start
			offset:   offset,
		}
		return it, nil
	}

	buildBatchIt := func() (RangeVectorIterator, error) {
		tt := struct {
			selRange   int64
			step       int64
			offset     int64
			start, end time.Time
		}{
			(5 * time.Second).Nanoseconds(), // no overlap
			(30 * time.Second).Nanoseconds(),
			0,
			time.Unix(10, 0),
			time.Unix(100, 0),
		}

		iter := newfakePeekingSampleIterator(samples)
		expr := &syntax.RangeAggregationExpr{Operation: syntax.OpRangeTypeCount}
		selRange := tt.selRange
		step := tt.step
		start := tt.start.UnixNano()
		end := tt.end.UnixNano()
		offset := tt.offset
		if step == 0 {
			step = 1
		}
		if offset != 0 {
			start = start - offset
			end = end - offset
		}

		vectorAggregator, err := aggregator(expr)
		if err != nil {
			return nil, err
		}

		return &batchRangeVectorIterator{
			iter:     iter,
			step:     step,
			end:      end,
			selRange: selRange,
			metrics:  map[string]labels.Labels{},
			window:   map[string]*promql.Series{},
			agg:      vectorAggregator,
			current:  start - step, // first loop iteration will set it to start
			offset:   offset,
		}, nil
	}

	b.Run("streaming agg", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			it, err := buildStreamingIt()
			if err != nil {
				b.Fatal(err)
			}
			for it.Next() {
				_, _ = it.At()
			}
		}
	})

	b.Run("batch agg", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			it, err := buildBatchIt()
			if err != nil {
				b.Fatal(err)
			}
			for it.Next() {
				_, _ = it.At()
			}
		}
	})

}

func Benchmark_RangeVectorIterator(b *testing.B) {
	b.ReportAllocs()
	tt := struct {
		selRange   int64
		step       int64
		offset     int64
		start, end time.Time
	}{
		(5 * time.Second).Nanoseconds(), // no overlap
		(30 * time.Second).Nanoseconds(),
		0,
		time.Unix(10, 0),
		time.Unix(100, 0),
	}

	for i := 0; i < b.N; i++ {
		i := 0
		it, err := newRangeVectorIterator(newfakePeekingSampleIterator(samples),
			&syntax.RangeAggregationExpr{Operation: syntax.OpRangeTypeCount}, tt.selRange,
			tt.step, tt.start.UnixNano(), tt.end.UnixNano(), tt.offset)
		if err != nil {
			panic(err)
		}
		for it.Next() {
			_, _ = it.At()
			i++
		}
	}

}

func Test_RangeVectorIterator_InstantQuery(t *testing.T) {
	now := time.Date(2024, 7, 3, 0, 0, 0, 0, time.UTC)

	samples := []logproto.Sample{
		{Timestamp: now.Add(-2 * time.Hour).UnixNano(), Value: 1.23},
		{Timestamp: now.Add(-1 * time.Hour).UnixNano(), Value: 2.34},
		{Timestamp: now.UnixNano(), Value: 3.45},
	}

	tests := []struct {
		selRange       time.Duration
		now            time.Time
		expectedVector promql.Vector
		expectedTs     time.Time
	}{
		{
			// query range:    (----]
			// samples:        x        x        x
			selRange:       30 * time.Minute,
			now:            now.Add(-90 * time.Minute),
			expectedVector: []promql.Sample{},
			expectedTs:     now.Add(-90 * time.Minute),
		},
		{
			// query range:    (--------]
			// samples:        x        x        x
			selRange: 60 * time.Minute,
			now:      now.Add(-60 * time.Minute),
			expectedVector: []promql.Sample{
				newSample(now.Add(-60*time.Minute), 1, labelFoo),
				newSample(now.Add(-60*time.Minute), 1, labelBar),
			},
			expectedTs: now.Add(-60 * time.Minute),
		},
		{
			// query range:        (----]
			// samples:        x        x        x
			selRange: 30 * time.Minute,
			now:      now.Add(-60 * time.Minute),
			expectedVector: []promql.Sample{
				newSample(now.Add(-60*time.Minute), 1, labelFoo),
				newSample(now.Add(-60*time.Minute), 1, labelBar),
			},
			expectedTs: now.Add(-60 * time.Minute),
		},
		{
			// query range:        (---------]
			// samples:        x        x        x
			selRange: 60 * time.Minute,
			now:      now.Add(-30 * time.Minute),
			expectedVector: []promql.Sample{
				newSample(now.Add(-30*time.Minute), 1, labelFoo),
				newSample(now.Add(-30*time.Minute), 1, labelBar),
			},
			expectedTs: now.Add(-30 * time.Minute),
		},
		{
			// query range:             (----]
			// samples:        x        x        x
			selRange:       30 * time.Minute,
			now:            now.Add(-30 * time.Minute),
			expectedVector: []promql.Sample{},
			expectedTs:     now.Add(-30 * time.Minute),
		},
		{
			// query range:             (--------]
			// samples:        x        x        x
			selRange: 60 * time.Minute,
			now:      now,
			expectedVector: []promql.Sample{
				newSample(now, 1, labelFoo),
				newSample(now, 1, labelBar),
			},
			expectedTs: now,
		},
		{
			// query range:                 (----]
			// samples:        x        x        x
			selRange: 30 * time.Minute,
			now:      now,
			expectedVector: []promql.Sample{
				newSample(now, 1, labelFoo),
				newSample(now, 1, labelBar),
			},
			expectedTs: now,
		},
		{
			// query range:    (-----------------]
			// samples:        x        x        x
			selRange: 120 * time.Minute,
			now:      now,
			expectedVector: []promql.Sample{
				newSample(now, 2, labelFoo),
				newSample(now, 2, labelBar),
			},
			expectedTs: now,
		},
		{
			// query range:               (----]
			// samples:        x        x        x
			selRange:       30 * time.Minute,
			now:            now.Add(-15 * time.Minute),
			expectedVector: []promql.Sample{},
			expectedTs:     now.Add(-15 * time.Minute),
		},
	}

	for i, tt := range tests {
		t.Run(
			fmt.Sprintf("%d logs[%s] - start %s - end %s", i, tt.selRange, tt.now.Add(-tt.selRange), tt.now),
			func(t *testing.T) {
				it, err := newRangeVectorIterator(
					newfakePeekingSampleIterator(samples),
					&syntax.RangeAggregationExpr{Operation: syntax.OpRangeTypeCount},
					tt.selRange.Nanoseconds(),
					0,                 // step
					tt.now.UnixNano(), // start
					tt.now.UnixNano(), // end
					0,                 // offset
				)
				require.NoError(t, err)

				hasNext := it.Next()
				require.True(t, hasNext)

				ts, v := it.At()
				require.ElementsMatch(t, tt.expectedVector, v)
				require.Equal(t, tt.expectedTs.UnixMilli(), ts)
			})
	}
}

func Test_RangeVectorIterator(t *testing.T) {
	tests := []struct {
		selRange        int64
		step            int64
		offset          int64
		expectedVectors []promql.Vector
		expectedTs      []time.Time
		start, end      time.Time
	}{
		{
			(5 * time.Second).Nanoseconds(), // no overlap
			(30 * time.Second).Nanoseconds(),
			0,
			[]promql.Vector{
				[]promql.Sample{
					newSample(time.Unix(10, 0), 2, labelBar),
					newSample(time.Unix(10, 0), 2, labelFoo),
				},
				[]promql.Sample{
					newSample(time.Unix(40, 0), 2, labelBar),
					newSample(time.Unix(40, 0), 2, labelFoo),
				},
				{},
				[]promql.Sample{
					newSample(time.Unix(100, 0), 1, labelBar),
					newSample(time.Unix(100, 0), 1, labelFoo),
				},
			},
			[]time.Time{time.Unix(10, 0), time.Unix(40, 0), time.Unix(70, 0), time.Unix(100, 0)},
			time.Unix(10, 0), time.Unix(100, 0),
		},
		{
			(35 * time.Second).Nanoseconds(), // will overlap by 5 sec
			(30 * time.Second).Nanoseconds(),
			0,
			[]promql.Vector{
				[]promql.Sample{
					newSample(time.Unix(10, 0), 4, labelBar),
					newSample(time.Unix(10, 0), 4, labelFoo),
				},
				[]promql.Sample{
					newSample(time.Unix(40, 0), 7, labelBar),
					newSample(time.Unix(40, 0), 7, labelFoo),
				},
				[]promql.Sample{
					newSample(time.Unix(70, 0), 2, labelBar),
					newSample(time.Unix(70, 0), 2, labelFoo),
				},
				[]promql.Sample{
					newSample(time.Unix(100, 0), 1, labelBar),
					newSample(time.Unix(100, 0), 1, labelFoo),
				},
			},
			[]time.Time{time.Unix(10, 0), time.Unix(40, 0), time.Unix(70, 0), time.Unix(100, 0)},
			time.Unix(10, 0), time.Unix(100, 0),
		},
		{
			(30 * time.Second).Nanoseconds(), // same range
			(30 * time.Second).Nanoseconds(),
			0,
			[]promql.Vector{
				[]promql.Sample{
					newSample(time.Unix(10, 0), 4, labelBar),
					newSample(time.Unix(10, 0), 4, labelFoo),
				},
				[]promql.Sample{
					newSample(time.Unix(40, 0), 5, labelBar),
					newSample(time.Unix(40, 0), 5, labelFoo),
				},
				[]promql.Sample{},
				[]promql.Sample{
					newSample(time.Unix(100, 0), 1, labelBar),
					newSample(time.Unix(100, 0), 1, labelFoo),
				},
			},
			[]time.Time{time.Unix(10, 0), time.Unix(40, 0), time.Unix(70, 0), time.Unix(100, 0)},
			time.Unix(10, 0), time.Unix(100, 0),
		},
		{
			(50 * time.Second).Nanoseconds(), // all step are overlapping
			(10 * time.Second).Nanoseconds(),
			0,
			[]promql.Vector{
				[]promql.Sample{
					newSample(time.Unix(110, 0), 2, labelBar),
					newSample(time.Unix(110, 0), 2, labelFoo),
				},
				[]promql.Sample{
					newSample(time.Unix(120, 0), 2, labelBar),
					newSample(time.Unix(120, 0), 2, labelFoo),
				},
			},
			[]time.Time{time.Unix(110, 0), time.Unix(120, 0)},
			time.Unix(110, 0), time.Unix(120, 0),
		},
		{
			(5 * time.Second).Nanoseconds(), // no overlap
			(30 * time.Second).Nanoseconds(),
			(10 * time.Second).Nanoseconds(),
			[]promql.Vector{
				[]promql.Sample{
					newSample(time.Unix(20, 0), 2, labelBar),
					newSample(time.Unix(20, 0), 2, labelFoo),
				},
				[]promql.Sample{
					newSample(time.Unix(50, 0), 2, labelBar),
					newSample(time.Unix(50, 0), 2, labelFoo),
				},
				{},
				[]promql.Sample{
					newSample(time.Unix(110, 0), 1, labelBar),
					newSample(time.Unix(110, 0), 1, labelFoo),
				},
			},
			[]time.Time{time.Unix(20, 0), time.Unix(50, 0), time.Unix(80, 0), time.Unix(110, 0)},
			time.Unix(20, 0), time.Unix(110, 0),
		},
	}

	for _, tt := range tests {
		t.Run(
			fmt.Sprintf("logs[%s] - step: %s - offset: %s", time.Duration(tt.selRange), time.Duration(tt.step), time.Duration(tt.offset)),
			func(t *testing.T) {
				it, err := newRangeVectorIterator(newfakePeekingSampleIterator(samples),
					&syntax.RangeAggregationExpr{Operation: syntax.OpRangeTypeCount}, tt.selRange,
					tt.step, tt.start.UnixNano(), tt.end.UnixNano(), tt.offset)
				require.NoError(t, err)

				i := 0
				for it.Next() {
					ts, v := it.At()
					require.ElementsMatch(t, tt.expectedVectors[i], v)
					require.Equal(t, tt.expectedTs[i].UnixNano()/1e+6, ts)
					i++
				}
				require.Equal(t, len(tt.expectedTs), i)
				require.Equal(t, len(tt.expectedVectors), i)
			})
	}
}

func Test_RangeVectorIteratorBadLabels(t *testing.T) {
	badIterator := iter.NewPeekingSampleIterator(
		iter.NewSeriesIterator(logproto.Series{
			Labels:  "{badlabels=}",
			Samples: samples,
		}))
	it, err := newRangeVectorIterator(badIterator,
		&syntax.RangeAggregationExpr{Operation: syntax.OpRangeTypeCount}, (30 * time.Second).Nanoseconds(),
		(30 * time.Second).Nanoseconds(), time.Unix(10, 0).UnixNano(), time.Unix(100, 0).UnixNano(), 0)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		defer cancel()
		//nolint:revive
		for it.Next() {
		}
	}()
	select {
	case <-time.After(1 * time.Second):
		require.Fail(t, "goroutine took too long")
	case <-ctx.Done():
	}
}

func Test_InstantQueryRangeVectorAggregations(t *testing.T) {
	tests := []struct {
		name          string
		expectedValue float64
		op            string
		negative      bool
	}{
		{"rate", 1.5e+09, syntax.OpRangeTypeRate, false},
		{"rate counter", 9.999999999999999e+08, syntax.OpRangeTypeRateCounter, false},
		{"count", 3., syntax.OpRangeTypeCount, false},
		{"bytes rate", 3e+09, syntax.OpRangeTypeBytesRate, false},
		{"bytes", 6., syntax.OpRangeTypeBytes, false},
		{"sum", 6., syntax.OpRangeTypeSum, false},
		{"avg", 2., syntax.OpRangeTypeAvg, false},
		{"max", -1, syntax.OpRangeTypeMax, true},
		{"min", 1., syntax.OpRangeTypeMin, false},
		{"std dev", 0.816496580927726, syntax.OpRangeTypeStddev, false},
		{"std vara", 0.6666666666666666, syntax.OpRangeTypeStdvar, false},
		{"quantile", 2.98, syntax.OpRangeTypeQuantile, false},
		{"first", 1., syntax.OpRangeTypeFirst, false},
		{"last", 3., syntax.OpRangeTypeLast, false},
		{"absent", 1., syntax.OpRangeTypeAbsent, false},
	}

	var start, end int64 = 4, 4 // Instant query
	for _, tt := range tests {
		t.Run(fmt.Sprintf("testing aggregation %s", tt.name), func(t *testing.T) {
			it, err := newRangeVectorIterator(sampleIter(tt.negative),
				&syntax.RangeAggregationExpr{Left: &syntax.LogRange{Interval: 2}, Params: proto.Float64(0.99), Operation: tt.op},
				3, 1, start, end, 0)
			require.NoError(t, err)

			//nolint:revive
			for it.Next() {
			}
			_, value := it.At()
			require.Equal(t, tt.expectedValue, value.SampleVector()[0].F)
		})
	}
}

func sampleIter(negative bool) iter.PeekingSampleIterator {
	return iter.NewPeekingSampleIterator(
		iter.NewSortSampleIterator([]iter.SampleIterator{
			iter.NewSeriesIterator(logproto.Series{
				Labels: labelFoo.String(),
				Samples: []logproto.Sample{
					{Timestamp: 2, Hash: 1, Value: value(1., negative)},
					{Timestamp: 3, Hash: 2, Value: value(2., negative)},
					{Timestamp: 4, Hash: 3, Value: value(3., negative)},
				},
				StreamHash: labelFoo.Hash(),
			}),
		}),
	)
}

func value(value float64, negative bool) float64 {
	if negative {
		return -1. * value
	}
	return value
}

func TestQuantiles(t *testing.T) {
	// v controls the distribution of values along the curve, a greater v
	// value means there's a large distance between generated values
	vs := []float64{1.0, 5.0, 10.0}
	// s controls the exponential curve of the distribution
	// the higher the s values the faster the drop off from max value to lesser values
	// s must be > 1.0
	ss := []float64{1.01, 2.0, 3.0, 4.0}

	// T-Digest is too big for 1_000 samples. However, we did not optimize
	// the format for size.
	nSamples := []int{5_000, 10_000, 100_000, 1_000_000}

	factories := []struct {
		newSketch     sketch.QuantileSketchFactory
		name          string
		relativeError float64
	}{
		{newSketch: func() sketch.QuantileSketch { return sketch.NewDDSketch() }, name: "DDSketch", relativeError: 0.02},
		{newSketch: sketch.NewTDigestSketch, name: "T-Digest", relativeError: 0.05},
	}

	for _, tc := range factories {
		for _, samplesCount := range nSamples {
			for _, s := range ss {
				for _, v := range vs {
					t.Run(fmt.Sprintf("sketch=%s, s=%.2f, v=%.2f, events=%d", tc.name, s, v, samplesCount), func(t *testing.T) {
						sk := tc.newSketch()

						r := rand.New(rand.NewSource(42))
						z := rand.NewZipf(r, s, v, 1_000)
						values := make(vector.HeapByMaxValue, 0)
						for i := 0; i < samplesCount; i++ {

							value := float64(z.Uint64())
							values = append(values, promql.Sample{F: value})
							err := sk.Add(value)
							require.NoError(t, err)
						}
						sort.Sort(values)

						// Size
						var buf []byte
						var err error
						switch s := sk.(type) {
						case *sketch.DDSketchQuantile:
							buf, err = proto.Marshal(s.DDSketch.ToProto())
							require.NoError(t, err)
						case *sketch.TDigestQuantile:
							buf, err = proto.Marshal(s.ToProto())
							require.NoError(t, err)
						}
						require.Less(t, len(buf), samplesCount*8)

						// Accuracy
						expected := Quantile(0.99, values)
						actual, err := sk.Quantile(0.99)
						require.NoError(t, err)
						require.InEpsilonf(t, expected, actual, tc.relativeError, "expected quantile %f, actual quantile %f", expected, actual)
					})
				}
			}
		}
	}
}
