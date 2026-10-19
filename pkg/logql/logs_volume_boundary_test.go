package logql_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
)

type volumeBoundaryFixture struct {
	BaseTime  string                   `json:"base_time"`
	Selector  string                   `json:"selector"`
	Scenarios []volumeBoundaryScenario `json:"scenarios"`
}

type volumeBoundaryScenario struct {
	Name    string `json:"name"`
	StartNS string `json:"start_ns"`
	EndNS   string `json:"end_ns"`
	Entries []struct {
		OffsetNS string `json:"offset_ns"`
		Level    string `json:"level"`
		Line     string `json:"line"`
	} `json:"entries"`
	ExpectedLogs map[string]int       `json:"expected_logs"`
	Parts        []volumeBoundaryPart `json:"parts"`
}

type volumeBoundaryPart struct {
	Name     string                 `json:"name"`
	Query    string                 `json:"query"`
	Start    string                 `json:"start"`
	End      string                 `json:"end"`
	Step     string                 `json:"step"`
	Expected []volumeBoundaryBucket `json:"expected"`
}

// Bucket times are offsets from the base time. Loki reports the
// offset-adjusted evaluation timestamp at millisecond precision, so a request
// at 60s with offset 1ns yields a sample timestamp of 60s.
type volumeBoundaryBucket struct {
	Time   string             `json:"time"`
	Counts map[string]float64 `json:"counts"`
}

// TestLogsVolumeBoundaryScenarios evaluates explicit request parts against
// boundary-heavy logs data using Loki's parser, step alignment, and engine.
func TestLogsVolumeBoundaryScenarios(t *testing.T) {
	fixture, base := loadVolumeBoundaryFixture(t)
	ctx := user.InjectOrgID(context.Background(), "volume-boundary-tests")

	for _, scenario := range fixture.Scenarios {
		t.Run(scenario.Name, func(t *testing.T) {
			engine := newVolumeBoundaryEngine(t, base, scenario)
			start := base.Add(volumeNS(t, scenario.StartNS))
			end := base.Add(volumeNS(t, scenario.EndNS))

			for _, direction := range []logproto.Direction{logproto.FORWARD, logproto.BACKWARD} {
				t.Run(direction.String(), func(t *testing.T) {
					counts := volumeLogCounts(ctx, t, engine, fixture.Selector, start, end, direction)
					require.Equal(t, scenario.ExpectedLogs, counts, "log query covers [start,end)")
				})
			}

			totals := map[string]int{}
			require.NotEmpty(t, scenario.Parts)
			for _, part := range scenario.Parts {
				t.Run(part.Name, func(t *testing.T) {
					buckets := volumeRunPart(ctx, t, engine, base, part)
					require.Equal(t, part.expectedBuckets(t, base), buckets, "bucket timestamps and counts")
					buckets.addTo(totals)
				})
			}
			require.Equal(t, scenario.ExpectedLogs, totals, "query parts together cover [start,end)")
		})
	}
}

func loadVolumeBoundaryFixture(t *testing.T) (volumeBoundaryFixture, time.Time) {
	t.Helper()
	raw, err := os.ReadFile("testdata/logs_volume_boundaries.json")
	require.NoError(t, err)
	var fixture volumeBoundaryFixture
	require.NoError(t, json.Unmarshal(raw, &fixture))
	base, err := time.Parse(time.RFC3339Nano, fixture.BaseTime)
	require.NoError(t, err)
	return fixture, base
}

// newVolumeBoundaryEngine groups the corpus into sorted streams for in-memory storage.
func newVolumeBoundaryEngine(t *testing.T, base time.Time, scenario volumeBoundaryScenario) *logql.QueryEngine {
	t.Helper()
	streamsByLevel := map[string]*logproto.Stream{}
	for _, entry := range scenario.Entries {
		stream := streamsByLevel[entry.Level]
		if stream == nil {
			stream = &logproto.Stream{Labels: fmt.Sprintf("{app=%q, level=%q}", "volume-boundary", entry.Level)}
			streamsByLevel[entry.Level] = stream
		}
		stream.Entries = append(stream.Entries, logproto.Entry{
			Timestamp: base.Add(volumeNS(t, entry.OffsetNS)), Line: entry.Line,
		})
	}
	var streams []logproto.Stream
	for _, stream := range streamsByLevel {
		sort.SliceStable(stream.Entries, func(i, j int) bool { return stream.Entries[i].Timestamp.Before(stream.Entries[j].Timestamp) })
		streams = append(streams, *stream)
	}
	return logql.NewEngine(logql.EngineOpts{}, logql.NewMockQuerier(1, streams), logql.NoLimits, log.NewNopLogger())
}

// volumeLogCounts counts the actual log query results by level.
func volumeLogCounts(ctx context.Context, t *testing.T, engine *logql.QueryEngine, selector string, start, end time.Time, direction logproto.Direction) map[string]int {
	t.Helper()
	params, err := logql.NewLiteralParams(selector, start, end, 0, 0, direction, 10000, nil, nil)
	require.NoError(t, err)
	result, err := engine.Query(params).Exec(ctx)
	require.NoError(t, err)
	got := map[string]int{}
	for _, stream := range result.Data.(logqlmodel.Streams) {
		matchers, err := syntax.ParseMatchers(stream.Labels, true)
		require.NoError(t, err)
		for _, matcher := range matchers {
			if matcher.Name == "level" {
				got[matcher.Value] += len(stream.Entries)
			}
		}
	}
	return got
}

// volumeBuckets indexes nonzero counts by sample timestamp (Unix milliseconds), then level.
type volumeBuckets map[int64]map[string]float64

func (part volumeBoundaryPart) expectedBuckets(t *testing.T, base time.Time) volumeBuckets {
	t.Helper()
	buckets := volumeBuckets{}
	for _, bucket := range part.Expected {
		if len(bucket.Counts) > 0 {
			buckets[base.Add(volumeDuration(t, bucket.Time)).UnixMilli()] = bucket.Counts
		}
	}
	return buckets
}

func (buckets volumeBuckets) addTo(totals map[string]int) {
	for _, bucket := range buckets {
		for level, count := range bucket {
			totals[level] += int(count)
		}
	}
}

func volumeRunPart(ctx context.Context, t *testing.T, engine *logql.QueryEngine, base time.Time, part volumeBoundaryPart) volumeBuckets {
	t.Helper()
	start := base.Add(volumeDuration(t, part.Start))
	end := base.Add(volumeDuration(t, part.End))
	step := volumeDuration(t, part.Step)
	if step > 0 {
		handler := queryrangebase.StepAlignMiddleware.Wrap(queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
			start, end = req.GetStart(), req.GetEnd()
			return nil, nil
		}))
		_, err := handler.Do(ctx, &queryrangebase.PrometheusRequest{Start: start, End: end, Step: step.Milliseconds()})
		require.NoError(t, err)
	}
	// Exercise parser serialization as well as execution, without modifying the AST.
	expr, err := syntax.ParseExpr(part.Query)
	require.NoError(t, err)
	params, err := logql.NewLiteralParams(expr.String(), start, end, step, 0, logproto.FORWARD, 10000, nil, nil)
	require.NoError(t, err)
	result, err := engine.Query(params).Exec(ctx)
	require.NoError(t, err)

	buckets := volumeBuckets{}
	add := func(timestamp int64, level string, count float64) {
		// Missing series and zero-valued samples both represent an empty bucket.
		if count == 0 {
			return
		}
		if buckets[timestamp] == nil {
			buckets[timestamp] = map[string]float64{}
		}
		buckets[timestamp][level] += count
	}
	if step == 0 {
		values, ok := result.Data.(promql.Vector)
		require.True(t, ok, "instant part must return a vector, got %T", result.Data)
		for _, sample := range values {
			add(sample.T, sample.Metric.Get("level"), sample.F)
		}
	} else {
		values, ok := result.Data.(promql.Matrix)
		require.True(t, ok, "range part must return a matrix, got %T", result.Data)
		for _, series := range values {
			for _, sample := range series.Floats {
				add(sample.T, series.Metric.Get("level"), sample.F)
			}
		}
	}
	return buckets
}

func volumeDuration(t *testing.T, value string) time.Duration {
	t.Helper()
	duration, err := time.ParseDuration(value)
	require.NoError(t, err)
	return duration
}

func volumeNS(t *testing.T, value string) time.Duration {
	t.Helper()
	ns, err := strconv.ParseInt(value, 10, 64)
	require.NoError(t, err)
	return time.Duration(ns)
}
