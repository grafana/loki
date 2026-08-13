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

func TestCountDistinctProtoRoundTrip(t *testing.T) {
	original := CountDistinctVector{
		{
			T:      123,
			F:      hyperloglog.New14(),
			Metric: labels.FromStrings("version", "1"),
		},
	}
	original[0].F.Insert([]byte("aa:bb"))
	original[0].F.Insert([]byte("cc:dd"))

	proto, err := original.ToProto()
	require.NoError(t, err)
	roundTrip, err := CountDistinctVectorFromProto(proto)
	require.NoError(t, err)
	require.Len(t, roundTrip, 1)
	require.Equal(t, original[0].T, roundTrip[0].T)
	require.Equal(t, original[0].Metric, roundTrip[0].Metric)
	require.Equal(t, original[0].F.Estimate(), roundTrip[0].F.Estimate())
}

func TestApproxCountDistinctLocalEval(t *testing.T) {
	now := time.Unix(100, 0)
	streams := []logproto.Stream{
		{
			Labels: `{job="devices", version="1"}`,
			Entries: []logproto.Entry{
				{Timestamp: now.Add(-30 * time.Second), Line: `mac="aa:bb"`},
				{Timestamp: now.Add(-20 * time.Second), Line: `mac="aa:bb"`},
				{Timestamp: now.Add(-10 * time.Second), Line: `mac="cc:dd"`},
			},
		},
		{
			Labels: `{job="devices", version="2"}`,
			Entries: []logproto.Entry{
				{Timestamp: now.Add(-15 * time.Second), Line: `mac="ee:ff"`},
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

	vec, ok := res.Data.(promql.Vector)
	require.True(t, ok)
	require.Len(t, vec, 2)

	byVersion := map[string]float64{}
	for _, sample := range vec {
		byVersion[sample.Metric.Get("version")] = sample.F
	}
	require.InDelta(t, 2, byVersion["1"], 0.01)
	require.InDelta(t, 1, byVersion["2"], 0.01)
}

func TestApproxCountDistinctInstantOnly(t *testing.T) {
	eng := NewEngine(EngineOpts{}, NewMockQuerier(1, nil), NoLimits, nil)
	params, err := NewLiteralParams(
		`approx_count_distinct(mac, {job="devices"}[1m]) by (version)`,
		time.Unix(0, 0), time.Unix(60, 0), time.Second, 0, logproto.FORWARD, 1000, nil, nil,
	)
	require.NoError(t, err)

	_, err = eng.Query(params).Exec(user.InjectOrgID(context.Background(), "fake"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "only supported on instant queries")
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
	okNext, _, result := step.Next()
	require.True(t, okNext)
	sketches := result.CountDistinctVec()
	require.Len(t, sketches, 1)
	require.Equal(t, uint64(1), sketches[0].F.Estimate())
}

func TestApproxCountDistinctShardedOverlap(t *testing.T) {
	now := time.Unix(100, 0)
	streams := []logproto.Stream{
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

	q := NewMockQuerier(2, streams)
	opts := EngineOpts{}
	regular := NewEngine(opts, q, NoLimits, nil)
	sharded := NewDownstreamEngine(opts, MockDownstreamer{regular}, NoLimits, nil)

	params, err := NewLiteralParams(
		`approx_count_distinct(mac, {job="devices"} | logfmt [1m]) by (version)`,
		now, now, 0, 0, logproto.FORWARD, 1000, nil, nil,
	)
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

	localVec := localRes.Data.(promql.Vector)
	shardedVec := shardedRes.Data.(promql.Vector)
	require.Len(t, localVec, 1)
	require.Len(t, shardedVec, 1)
	// shared + only-a + only-b = 3 distinct values across shards
	require.InDelta(t, 3, localVec[0].F, 0.01)
	require.InDelta(t, localVec[0].F, shardedVec[0].F, 0.01)
}
