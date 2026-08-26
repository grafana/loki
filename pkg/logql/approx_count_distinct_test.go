package logql

import (
	"context"
	"testing"
	"time"

	"github.com/axiomhq/hyperloglog"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

func TestCountDistinctVectorMerge(t *testing.T) {
	left := CountDistinctVector{
		{
			T:      1,
			F:      hyperloglog.New14(),
			Metric: labels.FromStrings("version", "1"),
		},
	}
	left[0].F.Insert([]byte("a"))
	left[0].F.Insert([]byte("b"))

	right := CountDistinctVector{
		{
			T:      1,
			F:      hyperloglog.New14(),
			Metric: labels.FromStrings("version", "1"),
		},
		{
			T:      1,
			F:      hyperloglog.New14(),
			Metric: labels.FromStrings("version", "2"),
		},
	}
	right[0].F.Insert([]byte("b"))
	right[0].F.Insert([]byte("c"))
	right[1].F.Insert([]byte("x"))

	merged, err := left.Merge(right)
	require.NoError(t, err)
	require.Len(t, merged, 2)

	byVersion := map[string]uint64{}
	for _, sample := range merged {
		byVersion[sample.Metric.Get("version")] = sample.F.Estimate()
	}
	require.Equal(t, uint64(3), byVersion["1"])
	require.Equal(t, uint64(1), byVersion["2"])
}

func TestCountDistinctMatrixMerge(t *testing.T) {
	left := CountDistinctMatrix{
		{{T: 1, F: hyperloglog.New14(), Metric: labels.FromStrings("version", "1")}},
		{{T: 2, F: hyperloglog.New14(), Metric: labels.FromStrings("version", "1")}},
	}
	left[0][0].F.Insert([]byte("a"))
	left[1][0].F.Insert([]byte("b"))

	right := CountDistinctMatrix{
		{{T: 1, F: hyperloglog.New14(), Metric: labels.FromStrings("version", "1")}},
		{{T: 2, F: hyperloglog.New14(), Metric: labels.FromStrings("version", "1")}},
	}
	right[0][0].F.Insert([]byte("c"))
	right[1][0].F.Insert([]byte("b"))

	merged, err := left.Merge(right)
	require.NoError(t, err)
	require.Equal(t, uint64(2), merged[0][0].F.Estimate())
	require.Equal(t, uint64(1), merged[1][0].F.Estimate())

	_, err = left.Merge(CountDistinctMatrix{left[0]})
	require.Error(t, err)
}

func TestCountDistinctValue_String(t *testing.T) {
	require.Equal(t, "CountDistinctVector()", CountDistinctVector{}.String())
	require.Equal(t, "CountDistinctMatrix()", CountDistinctMatrix{}.String())
}

func TestCountDistinctExpr_String(t *testing.T) {
	sketch := mustCountDistinctSketch(t, `approx_count_distinct(mac, {foo="bar"}[5m]) by (version)`)

	t.Run("merge empty", func(t *testing.T) {
		require.Equal(t, "CountDistinctMerge<>", (&CountDistinctMergeExpr{}).String())
	})

	t.Run("merge one downstream", func(t *testing.T) {
		expr := &CountDistinctMergeExpr{
			downstreams: []DownstreamSampleExpr{{SampleExpr: sketch}},
		}
		require.Equal(t,
			`CountDistinctMerge<downstream<__count_distinct_sketch__(mac,{foo="bar"}[5m]) by (version), shard=<nil>>>`,
			expr.String(),
		)
	})

	t.Run("merge concatenates downstreams", func(t *testing.T) {
		expr := &CountDistinctMergeExpr{
			downstreams: []DownstreamSampleExpr{
				{SampleExpr: sketch, shard: NewPowerOfTwoShard(index.ShardAnnotation{Shard: 0, Of: 2}).Bind(nil)},
				{SampleExpr: sketch, shard: NewPowerOfTwoShard(index.ShardAnnotation{Shard: 1, Of: 2}).Bind(nil)},
			},
		}
		require.Equal(t,
			`CountDistinctMerge<downstream<__count_distinct_sketch__(mac,{foo="bar"}[5m]) by (version), shard=0_of_2> ++ downstream<__count_distinct_sketch__(mac,{foo="bar"}[5m]) by (version), shard=1_of_2>>`,
			expr.String(),
		)
	})

	t.Run("merge caps downstreams at defaultMaxDepth", func(t *testing.T) {
		old := defaultMaxDepth
		defaultMaxDepth = 2
		t.Cleanup(func() { defaultMaxDepth = old })

		expr := &CountDistinctMergeExpr{
			downstreams: []DownstreamSampleExpr{
				{SampleExpr: sketch, shard: NewPowerOfTwoShard(index.ShardAnnotation{Shard: 0, Of: 3}).Bind(nil)},
				{SampleExpr: sketch, shard: NewPowerOfTwoShard(index.ShardAnnotation{Shard: 1, Of: 3}).Bind(nil)},
				{SampleExpr: sketch, shard: NewPowerOfTwoShard(index.ShardAnnotation{Shard: 2, Of: 3}).Bind(nil)},
			},
		}
		require.Equal(t,
			`CountDistinctMerge<downstream<__count_distinct_sketch__(mac,{foo="bar"}[5m]) by (version), shard=0_of_3> ++ downstream<__count_distinct_sketch__(mac,{foo="bar"}[5m]) by (version), shard=1_of_3>>`,
			expr.String(),
		)
		require.NotContains(t, expr.String(), "2_of_3")
	})

	t.Run("eval nil merge", func(t *testing.T) {
		require.Equal(t, "CountDistinctEval<>", (&CountDistinctEvalExpr{}).String())
	})

	t.Run("eval wraps merge", func(t *testing.T) {
		expr := &CountDistinctEvalExpr{
			mergeExpr: &CountDistinctMergeExpr{
				downstreams: []DownstreamSampleExpr{{SampleExpr: sketch}},
			},
		}
		require.Equal(t,
			`CountDistinctEval<CountDistinctMerge<downstream<__count_distinct_sketch__(mac,{foo="bar"}[5m]) by (version), shard=<nil>>>>`,
			expr.String(),
		)
	})
}

func TestCountDistinctEvalNilMergeExpr(t *testing.T) {
	ev := NewDownstreamEvaluator(nil)
	params, err := NewLiteralParams(
		`count_over_time({foo="bar"}[1m])`,
		time.Unix(0, 0),
		time.Unix(0, 0),
		0,
		0,
		logproto.FORWARD,
		1000,
		nil,
		nil,
	)
	require.NoError(t, err)

	_, err = ev.NewStepEvaluator(context.Background(), nil, &CountDistinctEvalExpr{}, params)
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing merge expression")
}

func mustCountDistinctSketch(t *testing.T, query string) *syntax.CountDistinctSketchExpr {
	t.Helper()
	expr, err := syntax.ParseExpr(query)
	require.NoError(t, err)
	agg, ok := expr.(*syntax.LabelAggregationExpr)
	require.True(t, ok)
	return syntax.NewCountDistinctSketchFromLabelAggregation(agg)
}

func TestCountDistinctProtoRoundTrip(t *testing.T) {
	original := CountDistinctMatrix{
		{
			{
				T:      123,
				F:      hyperloglog.New14(),
				Metric: labels.FromStrings("version", "1"),
			},
		},
		{},
	}
	original[0][0].F.Insert([]byte("aa:bb"))
	original[0][0].F.Insert([]byte("cc:dd"))

	proto, err := original.ToProto()
	require.NoError(t, err)
	require.Len(t, proto.Values, 2)
	require.Empty(t, proto.Values[1].Samples)
	roundTrip, err := CountDistinctMatrixFromProto(proto)
	require.NoError(t, err)
	require.Len(t, roundTrip, 2)
	require.Empty(t, roundTrip[1])
	require.Equal(t, original[0][0].T, roundTrip[0][0].T)
	require.Equal(t, original[0][0].Metric, roundTrip[0][0].Metric)
	require.Equal(t, original[0][0].F.Estimate(), roundTrip[0][0].F.Estimate())
}

func TestApproxCountDistinctEval(t *testing.T) {
	start := time.Unix(100, 0)
	end := start.Add(60 * time.Second)
	step := 30 * time.Second
	streams := []logproto.Stream{
		{
			Labels: `{job="devices", version="1"}`,
			Entries: []logproto.Entry{
				// T=100 window (40s, 100s] only.
				{Timestamp: start.Add(-50 * time.Second), Line: `mac="early"`},
				// T=100 and T=130 windows.
				{Timestamp: start.Add(-10 * time.Second), Line: `mac="mid"`},
				// T=160 window (100s, 160s] only.
				{Timestamp: start.Add(40 * time.Second), Line: `mac="late"`},
			},
		},
		{
			Labels: `{job="devices", version="2"}`,
			Entries: []logproto.Entry{
				// T=100 and T=130 windows.
				{Timestamp: start.Add(-15 * time.Second), Line: `mac="other"`},
			},
		},
	}

	type seriesExpect struct {
		metric  labels.Labels
		instant float64
		rangeTs []float64
	}

	tests := []struct {
		name    string
		query   string
		instant bool
		series  []seriesExpect
	}{
		{
			name:    "instant default grouped",
			query:   `approx_count_distinct(mac, {job="devices"} | logfmt [1m])`,
			instant: true,
			series: []seriesExpect{
				{metric: labels.FromStrings("job", "devices", "version", "1"), instant: 2},
				{metric: labels.FromStrings("job", "devices", "version", "2"), instant: 1},
			},
		},
		{
			name:    "instant ungrouped",
			query:   `approx_count_distinct(mac, {job="devices"} | logfmt [1m]) by ()`,
			instant: true,
			series: []seriesExpect{
				{metric: labels.EmptyLabels(), instant: 3},
			},
		},
		{
			name:    "instant grouped",
			query:   `approx_count_distinct(mac, {job="devices"} | logfmt [1m]) by (version)`,
			instant: true,
			series: []seriesExpect{
				{metric: labels.FromStrings("version", "1"), instant: 2},
				{metric: labels.FromStrings("version", "2"), instant: 1},
			},
		},
		{
			name:    "range default grouped",
			query:   `approx_count_distinct(mac, {job="devices"} | logfmt [1m])`,
			instant: false,
			series: []seriesExpect{
				{metric: labels.FromStrings("job", "devices", "version", "1"), rangeTs: []float64{2, 1, 1}},
				{metric: labels.FromStrings("job", "devices", "version", "2"), rangeTs: []float64{1, 1}},
			},
		},
		{
			name:    "range ungrouped",
			query:   `approx_count_distinct(mac, {job="devices"} | logfmt [1m]) by ()`,
			instant: false,
			series: []seriesExpect{
				{metric: labels.EmptyLabels(), rangeTs: []float64{3, 2, 1}},
			},
		},
		{
			name:    "range grouped",
			query:   `approx_count_distinct(mac, {job="devices"} | logfmt [1m]) by (version)`,
			instant: false,
			series: []seriesExpect{
				{metric: labels.FromStrings("version", "1"), rangeTs: []float64{2, 1, 1}},
				{metric: labels.FromStrings("version", "2"), rangeTs: []float64{1, 1}},
			},
		},
	}

	eng := NewEngine(EngineOpts{}, NewMockQuerier(1, streams), NoLimits, nil)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			qStart, qEnd, qStep := start, start, time.Duration(0)
			if !tc.instant {
				qEnd, qStep = end, step
			}
			params, err := NewLiteralParams(
				tc.query, qStart, qEnd, qStep, 0, logproto.FORWARD, 1000, nil, nil,
			)
			require.NoError(t, err)

			res, err := eng.Query(params).Exec(user.InjectOrgID(context.Background(), "fake"))
			require.NoError(t, err)

			if tc.instant {
				vec, ok := res.Data.(promql.Vector)
				require.True(t, ok)
				require.Len(t, vec, len(tc.series))
				got := map[uint64]float64{}
				for _, sample := range vec {
					got[labels.StableHash(sample.Metric)] = sample.F
				}
				for _, exp := range tc.series {
					require.InDelta(t, exp.instant, got[labels.StableHash(exp.metric)], 0.01, exp.metric.String())
				}
				return
			}

			matrix, ok := res.Data.(promql.Matrix)
			require.True(t, ok)
			require.Len(t, matrix, len(tc.series))
			got := map[uint64][]promql.FPoint{}
			for _, series := range matrix {
				got[labels.StableHash(series.Metric)] = series.Floats
			}
			for _, exp := range tc.series {
				points := got[labels.StableHash(exp.metric)]
				require.Len(t, points, len(exp.rangeTs), exp.metric.String())
				for i, want := range exp.rangeTs {
					require.Equal(t, start.Add(time.Duration(i)*step).UnixMilli(), points[i].T)
					require.InDelta(t, want, points[i].F, 0.01, exp.metric.String())
				}
			}
		})
	}
}

func TestApproxCountDistinctRangeOffset(t *testing.T) {
	now := time.Unix(100, 0)
	streams := []logproto.Stream{
		{
			Labels: `{job="devices", version="1"}`,
			Entries: []logproto.Entry{
				// Inside the offset window (T-1m-30s, T-30s] = (10s, 70s].
				{Timestamp: now.Add(-50 * time.Second), Line: `mac="shifted"`},
				// After the offset window; excluded.
				{Timestamp: now.Add(-10 * time.Second), Line: `mac="too-new"`},
			},
		},
	}

	eng := NewEngine(EngineOpts{}, NewMockQuerier(1, streams), NoLimits, nil)
	params, err := NewLiteralParams(
		`approx_count_distinct(mac, {job="devices"} | logfmt [1m] offset 30s) by (version)`,
		now, now, 0, 0, logproto.FORWARD, 1000, nil, nil,
	)
	require.NoError(t, err)

	res, err := eng.Query(params).Exec(user.InjectOrgID(context.Background(), "fake"))
	require.NoError(t, err)
	vec := res.Data.(promql.Vector)
	require.Len(t, vec, 1)
	require.Equal(t, now.UnixMilli(), vec[0].T)
	require.InDelta(t, 1, vec[0].F, 0.01)
}

func TestApproxCountDistinctBoundaries(t *testing.T) {
	now := time.Unix(100, 0)
	streams := []logproto.Stream{
		{
			Labels: `{job="devices", version="1"}`,
			Entries: []logproto.Entry{
				// Exactly at lower bound (T-D): excluded (open lower).
				{Timestamp: now.Add(-time.Minute), Line: `mac="lower"`},
				// Inside range.
				{Timestamp: now.Add(-30 * time.Second), Line: `mac="inside"`},
				// Exactly at T: included (closed upper via +1ns on End).
				{Timestamp: now, Line: `mac="upper"`},
			},
		},
	}

	eng := NewEngine(EngineOpts{}, NewMockQuerier(1, streams), NoLimits, nil)
	params, err := NewLiteralParams(
		`approx_count_distinct(mac, {job="devices"} | logfmt [1m]) by (version)`,
		now, now, 0, 0, logproto.FORWARD, 1000, nil, nil,
	)
	require.NoError(t, err)

	res, err := eng.Query(params).Exec(user.InjectOrgID(context.Background(), "fake"))
	require.NoError(t, err)
	vec := res.Data.(promql.Vector)
	require.Len(t, vec, 1)
	require.InDelta(t, 2, vec[0].F, 0.01)
}

func TestCountDistinctSketchExprReturnsSketches(t *testing.T) {
	now := time.Unix(100, 0)
	streams := []logproto.Stream{
		{
			Labels: `{job="devices", version="1"}`,
			Entries: []logproto.Entry{
				{Timestamp: now.Add(-10 * time.Second), Line: `mac="aa:bb"`},
			},
		},
	}

	q := NewMockQuerier(1, streams)
	params, err := NewLiteralParams(
		`approx_count_distinct(mac, {job="devices"} | logfmt [1m]) by (version)`,
		now, now, 0, 0, logproto.FORWARD, 1000, nil, nil,
	)
	require.NoError(t, err)

	parsed, ok := params.GetExpression().(*syntax.LabelAggregationExpr)
	require.True(t, ok)
	sketch := syntax.NewCountDistinctSketchFromLabelAggregation(parsed)

	ev := NewDefaultEvaluator(q, 5*time.Minute, 10_000)
	sketchParams := ParamsWithExpressionOverride{
		Params:             params,
		ExpressionOverride: sketch,
	}
	step, err := ev.NewStepEvaluator(user.InjectOrgID(context.Background(), "fake"), ev, sketch, sketchParams)
	require.NoError(t, err)
	t.Cleanup(func() { _ = step.Close() })
	okNext, _, result := step.Next()
	require.True(t, okNext)
	sketches := result.CountDistinctVec()
	require.Len(t, sketches, 1)
	require.Equal(t, uint64(1), sketches[0].F.Estimate())
}

func TestCountDistinctSketchExprRangeSteps(t *testing.T) {
	start := time.Unix(100, 0)
	end := start.Add(60 * time.Second)
	step := 30 * time.Second
	streams := []logproto.Stream{
		{
			Labels: `{job="devices", version="1"}`,
			Entries: []logproto.Entry{
				{Timestamp: start.Add(-50 * time.Second), Line: `mac="early"`},
				{Timestamp: start.Add(-10 * time.Second), Line: `mac="mid"`},
				{Timestamp: start.Add(40 * time.Second), Line: `mac="late"`},
			},
		},
	}

	q := NewMockQuerier(1, streams)
	params, err := NewLiteralParams(
		`approx_count_distinct(mac, {job="devices"} | logfmt [1m]) by (version)`,
		start, end, step, 0, logproto.FORWARD, 1000, nil, nil,
	)
	require.NoError(t, err)

	parsed, ok := params.GetExpression().(*syntax.LabelAggregationExpr)
	require.True(t, ok)
	sketch := syntax.NewCountDistinctSketchFromLabelAggregation(parsed)

	ev := NewDefaultEvaluator(q, 5*time.Minute, 10_000)
	sketchParams := ParamsWithExpressionOverride{
		Params:             params,
		ExpressionOverride: sketch,
	}
	stepEv, err := ev.NewStepEvaluator(user.InjectOrgID(context.Background(), "fake"), ev, sketch, sketchParams)
	require.NoError(t, err)
	t.Cleanup(func() { _ = stepEv.Close() })

	var estimates []uint64
	for {
		okNext, _, result := stepEv.Next()
		if !okNext {
			break
		}
		sketches := result.CountDistinctVec()
		require.Len(t, sketches, 1)
		estimates = append(estimates, sketches[0].F.Estimate())
	}
	require.Equal(t, []uint64{2, 1, 1}, estimates)
}

func overlapStreams(now time.Time) []logproto.Stream {
	return []logproto.Stream{
		{
			Labels: `{job="devices", version="1", shard="a"}`,
			Entries: []logproto.Entry{
				{Timestamp: now.Add(-20 * time.Second), Line: `mac="shared"`},
				{Timestamp: now.Add(-10 * time.Second), Line: `mac="only-a"`},
			},
		},
		{
			Labels: `{job="devices", version="1", shard="b"}`,
			Entries: []logproto.Entry{
				{Timestamp: now.Add(-15 * time.Second), Line: `mac="shared"`},
				{Timestamp: now.Add(-5 * time.Second), Line: `mac="only-b"`},
			},
		},
	}
}

func execShardedApproxCountDistinct(t *testing.T, query string, start, end time.Time, step time.Duration) (logqlmodel.Result, logqlmodel.Result) {
	t.Helper()
	q := NewMockQuerier(2, overlapStreams(start))
	opts := EngineOpts{}
	regular := NewEngine(opts, q, NoLimits, nil)
	sharded := NewDownstreamEngine(opts, MockDownstreamer{regular}, NoLimits, nil)

	params, err := NewLiteralParams(query, start, end, step, 0, logproto.FORWARD, 1000, nil, nil)
	require.NoError(t, err)

	mapper := NewShardMapper(NewPowerOfTwoStrategy(ConstantShards(2)), nilShardMetrics, []string{SupportApproxCountDistinct})
	_, _, mapped, err := mapper.Parse(params.GetExpression())
	require.NoError(t, err)

	ctx := user.InjectOrgID(context.Background(), "fake")
	localRes, err := regular.Query(params).Exec(ctx)
	require.NoError(t, err)

	shardedRes, err := sharded.Query(ctx, ParamsWithExpressionOverride{
		Params:             params,
		ExpressionOverride: mapped.(syntax.SampleExpr),
	}).Exec(ctx)
	require.NoError(t, err)
	return localRes, shardedRes
}

func TestApproxCountDistinctShardedOverlap(t *testing.T) {
	now := time.Unix(100, 0)
	localRes, shardedRes := execShardedApproxCountDistinct(
		t,
		`approx_count_distinct(mac, {job="devices"} | logfmt [1m]) by (version)`,
		now, now, 0,
	)

	localVec := localRes.Data.(promql.Vector)
	shardedVec := shardedRes.Data.(promql.Vector)
	require.Len(t, localVec, 1)
	require.Len(t, shardedVec, 1)
	// shared + only-a + only-b = 3 distinct values across shards
	require.InDelta(t, 3, localVec[0].F, 0.01)
	require.InDelta(t, localVec[0].F, shardedVec[0].F, 0.01)
}

func TestApproxCountDistinctShardedUngrouped(t *testing.T) {
	now := time.Unix(100, 0)
	localRes, shardedRes := execShardedApproxCountDistinct(
		t,
		`approx_count_distinct(mac, {job="devices"} | logfmt [1m]) by ()`,
		now, now, 0,
	)

	localVec := localRes.Data.(promql.Vector)
	shardedVec := shardedRes.Data.(promql.Vector)
	require.Len(t, localVec, 1)
	require.True(t, localVec[0].Metric.IsEmpty())
	require.InDelta(t, 3, localVec[0].F, 0.01)
	require.InDelta(t, localVec[0].F, shardedVec[0].F, 0.01)
}

func TestApproxCountDistinctShardedRange(t *testing.T) {
	start := time.Unix(100, 0)
	end := start.Add(30 * time.Second)
	step := 30 * time.Second
	localRes, shardedRes := execShardedApproxCountDistinct(
		t,
		`approx_count_distinct(mac, {job="devices"} | logfmt [1m]) by (version)`,
		start, end, step,
	)

	localMatrix := localRes.Data.(promql.Matrix)
	shardedMatrix := shardedRes.Data.(promql.Matrix)
	require.Len(t, localMatrix, 1)
	require.Len(t, shardedMatrix, 1)
	require.Equal(t, localMatrix[0].Metric, shardedMatrix[0].Metric)
	require.Len(t, localMatrix[0].Floats, 2)
	require.Len(t, shardedMatrix[0].Floats, 2)
	for i := range localMatrix[0].Floats {
		require.Equal(t, localMatrix[0].Floats[i].T, shardedMatrix[0].Floats[i].T)
		require.InDelta(t, localMatrix[0].Floats[i].F, shardedMatrix[0].Floats[i].F, 0.01)
		require.InDelta(t, 3, localMatrix[0].Floats[i].F, 0.01)
	}
}
