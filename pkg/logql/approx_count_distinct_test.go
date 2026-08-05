package logql

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/axiomhq/hyperloglog"
	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
)

func TestParseApproxCountDistinct(t *testing.T) {
	tests := []struct {
		in       string
		want     string
		distinct string
		groups   []string
	}{
		{
			in:       `approx_count_distinct(device_id) by (version) ({job="status"} |= "System Status Report" | logfmt)`,
			want:     `approx_count_distinct(device_id) by (version)({job="status"} |= "System Status Report" | logfmt)`,
			distinct: "device_id",
			groups:   []string{"version"},
		},
		{
			in:       `approx_count_distinct(device_id)({job="status"} | logfmt)`,
			want:     `approx_count_distinct(device_id)({job="status"} | logfmt)`,
			distinct: "device_id",
			groups:   nil,
		},
		{
			in:       `approx_count_distinct by (version) (device_id) ({job="status"} | logfmt)`,
			want:     `approx_count_distinct(device_id) by (version)({job="status"} | logfmt)`,
			distinct: "device_id",
			groups:   []string{"version"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			expr, err := syntax.ParseSampleExpr(tt.in)
			require.NoError(t, err)
			acd, ok := expr.(*syntax.ApproxCountDistinctExpr)
			require.True(t, ok)
			require.Equal(t, tt.distinct, acd.DistinctLabel)
			require.Equal(t, tt.groups, acd.Grouping.Groups)
			require.Equal(t, tt.want, acd.String())
		})
	}
}

func TestShardMapperApproxCountDistinct(t *testing.T) {
	in := `approx_count_distinct(device_id) by (version) ({job="status"} | logfmt)`
	expr, err := syntax.ParseExpr(in)
	require.NoError(t, err)

	t.Run("disabled", func(t *testing.T) {
		m := NewShardMapper(NewPowerOfTwoStrategy(ConstantShards(2)), nilShardMetrics, nil)
		_, _, _, err := m.Parse(expr)
		require.Error(t, err)
		require.Contains(t, err.Error(), "approx_count_distinct is not enabled")
	})

	t.Run("enabled", func(t *testing.T) {
		m := NewShardMapper(NewPowerOfTwoStrategy(ConstantShards(2)), nilShardMetrics, []string{SupportApproxCountDistinct})
		_, _, mapped, err := m.Parse(expr)
		require.NoError(t, err)
		eval, ok := mapped.(*CountDistinctEvalExpr)
		require.True(t, ok)
		require.NotNil(t, eval.mergeExpr)
		require.Len(t, eval.mergeExpr.downstreams, 2)
		for _, d := range eval.mergeExpr.downstreams {
			acd, ok := d.SampleExpr.(*syntax.ApproxCountDistinctExpr)
			require.True(t, ok)
			require.True(t, acd.SketchOnly, "sharded downstreams must request sketches for merge")
		}
	})
}

func TestApproxCountDistinctSketchOnlyJSONRoundTrip(t *testing.T) {
	in := `approx_count_distinct(device_id) by (version) ({job="status"} | logfmt)`
	expr, err := syntax.ParseSampleExpr(in)
	require.NoError(t, err)
	acd := expr.(*syntax.ApproxCountDistinctExpr)
	acd.SketchOnly = true

	var buf strings.Builder
	require.NoError(t, syntax.EncodeJSON(acd, &buf))
	decoded, err := syntax.DecodeJSON(buf.String())
	require.NoError(t, err)
	got, ok := decoded.(*syntax.ApproxCountDistinctExpr)
	require.True(t, ok)
	require.True(t, got.SketchOnly)
	require.Equal(t, "device_id", got.DistinctLabel)
	require.Equal(t, []string{"version"}, got.Grouping.Groups)
}

func TestCountDistinctVectorMerge(t *testing.T) {
	left := CountDistinctVector{
		{
			T:      1000,
			F:      hyperloglog.New14(),
			Metric: labels.FromStrings("version", "1.0.0"),
		},
	}
	for i := 0; i < 100; i++ {
		left[0].F.Insert([]byte(fmt.Sprintf("device-%d", i)))
	}

	right := CountDistinctVector{
		{
			T:      1000,
			F:      hyperloglog.New14(),
			Metric: labels.FromStrings("version", "1.0.0"),
		},
		{
			T:      1000,
			F:      hyperloglog.New14(),
			Metric: labels.FromStrings("version", "2.0.0"),
		},
	}
	for i := 50; i < 150; i++ {
		right[0].F.Insert([]byte(fmt.Sprintf("device-%d", i)))
	}
	for i := 0; i < 20; i++ {
		right[1].F.Insert([]byte(fmt.Sprintf("other-%d", i)))
	}

	merged, err := left.Merge(right)
	require.NoError(t, err)
	require.Len(t, merged, 2)

	byVersion := map[string]uint64{}
	for _, s := range merged {
		byVersion[s.Metric.Get("version")] = s.F.Estimate()
	}
	// Combined distinct for version 1.0.0 is ~150
	require.InDelta(t, 150, float64(byVersion["1.0.0"]), 15)
	require.InDelta(t, 20, float64(byVersion["2.0.0"]), 5)
}

func TestApproxCountDistinctEval(t *testing.T) {
	const (
		nVersions = 5
		perVer    = 200
	)
	streams := makeStatusReportStreams(nVersions, perVer, 1)
	querier := MockQuerier{streams: streams}
	lookback := time.Duration(nVersions*perVer+10) * time.Second
	eng := NewEngine(EngineOpts{MaxLookBackPeriod: lookback}, querier, fakeLimits{maxSeries: 1000, timeout: time.Hour}, log.NewNopLogger())

	// Instant query: start == end, window covered by MaxLookBackPeriod.
	end := time.Unix(int64(nVersions*perVer)+1, 0)
	params, err := NewLiteralParams(
		`approx_count_distinct(device_id) by (version) ({job="status"} |= "System Status Report" | logfmt)`,
		end, end, 0, 0, logproto.FORWARD, 0, nil, nil,
	)
	require.NoError(t, err)

	res, err := eng.Query(params).Exec(user.InjectOrgID(context.Background(), "fake"))
	require.NoError(t, err)

	vec, ok := res.Data.(promql.Vector)
	require.True(t, ok)
	require.Len(t, vec, nVersions)

	for _, s := range vec {
		require.InDelta(t, float64(perVer), s.F, float64(perVer)*0.05)
	}
}

func TestApproxCountDistinctSeriesLimit(t *testing.T) {
	streams := makeStatusReportStreams(10, 5, 1)
	querier := MockQuerier{streams: streams}
	eng := NewEngine(EngineOpts{MaxLookBackPeriod: time.Hour}, querier, fakeLimits{maxSeries: 3, timeout: time.Hour}, log.NewNopLogger())

	end := time.Unix(100, 0)
	params, err := NewLiteralParams(
		`approx_count_distinct(device_id) by (version) ({job="status"} |= "System Status Report" | logfmt)`,
		end, end, 0, 0, logproto.FORWARD, 0, nil, nil,
	)
	require.NoError(t, err)

	_, err = eng.Query(params).Exec(user.InjectOrgID(context.Background(), "fake"))
	require.Error(t, err)
	var limitErr *logqlmodel.LimitError
	require.ErrorAs(t, err, &limitErr)
}

func TestDistinctValueExtractorNoSeriesExplosion(t *testing.T) {
	expr, err := syntax.ParseSampleExpr(`approx_count_distinct(device_id) by (version) ({job="status"} | logfmt)`)
	require.NoError(t, err)
	extractors, err := expr.Extractors()
	require.NoError(t, err)
	require.Len(t, extractors, 1)

	ex := extractors[0].ForStream(labels.FromStrings("job", "status"))
	seen := map[string]struct{}{}
	for i := 0; i < 1000; i++ {
		line := fmt.Sprintf(`System Status Report: device_id=device-%d version=1.0.%d`, i, i%3)
		samples, ok := ex.Process(int64(i), []byte(line), labels.EmptyLabels())
		require.True(t, ok)
		require.Len(t, samples, 1)
		seen[samples[0].Labels.String()] = struct{}{}
	}
	// Only 3 version labels — device_id must not create series.
	require.Len(t, seen, 3)
}

func TestCountDistinctProtoRoundTrip(t *testing.T) {
	sk := hyperloglog.New14()
	sk.Insert([]byte("a"))
	sk.Insert([]byte("b"))
	vec := CountDistinctVector{{
		T:      42,
		F:      sk,
		Metric: labels.FromStrings("version", "1.2.3"),
	}}
	proto, err := vec.ToProto()
	require.NoError(t, err)
	back, err := CountDistinctVectorFromProto(proto)
	require.NoError(t, err)
	require.Len(t, back, 1)
	require.Equal(t, uint64(2), back[0].F.Estimate())
	require.Equal(t, "1.2.3", back[0].Metric.Get("version"))
}

// makeStatusReportStreams builds log streams with the line format:
//
//	System Status Report: device_id=<id> version=<x.y.z>
func makeStatusReportStreams(nVersions, perVersion, nShards int) []logproto.Stream {
	_ = nShards
	var streams []logproto.Stream
	ts := time.Unix(1, 0)
	entries := make([]logproto.Entry, 0, nVersions*perVersion)
	for v := 0; v < nVersions; v++ {
		version := fmt.Sprintf("1.0.%d", v)
		for d := 0; d < perVersion; d++ {
			deviceID := fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", v, d, d, d, d)
			entries = append(entries, logproto.Entry{
				Timestamp: ts,
				Line:      fmt.Sprintf(`System Status Report: device_id=%s version=%s`, deviceID, version),
			})
			ts = ts.Add(time.Second)
		}
	}
	streams = append(streams, logproto.Stream{
		Labels:  `{job="status"}`,
		Entries: entries,
	})
	return streams
}

func TestApproxCountDistinctRangeRejected(t *testing.T) {
	streams := makeStatusReportStreams(2, 10, 1)
	querier := MockQuerier{streams: streams}
	eng := NewEngine(EngineOpts{MaxLookBackPeriod: time.Hour}, querier, fakeLimits{maxSeries: 1000, timeout: time.Hour}, log.NewNopLogger())

	start := time.Unix(0, 0)
	end := time.Unix(100, 0)
	params, err := NewLiteralParams(
		`approx_count_distinct(device_id) by (version) ({job="status"} |= "System Status Report" | logfmt)`,
		start, end, time.Second*10, 0, logproto.FORWARD, 0, nil, nil,
	)
	require.NoError(t, err)

	_, err = eng.Query(params).Exec(user.InjectOrgID(context.Background(), "fake"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "only supported on instant queries")
}
