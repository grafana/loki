package logql_test

import (
	"bytes"
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
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/util/marshal"
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
	StepNS  string `json:"step_ns"`
	Entries []struct {
		OffsetNS string `json:"offset_ns"`
		Level    string `json:"level"`
		Line     string `json:"line"`
	} `json:"entries"`
	ExpectedLogs    map[string]int `json:"expected_logs"`
	ExpectedCurrent map[string]int `json:"expected_current"`
}

type volumeBoundaryPart struct {
	Start  time.Time     `json:"start"`
	End    time.Time     `json:"end"`
	Step   time.Duration `json:"step_ns"`
	Window time.Duration `json:"window_ns"`
}

type volumeBoundaryExchange struct {
	Part     volumeBoundaryPart `json:"part"`
	Query    string             `json:"query"`
	Response json.RawMessage    `json:"response"`
}

type volumeBoundaryReport struct {
	OffsetExchanges    []volumeBoundaryExchange `json:"offset_exchanges"`
	OffsetCounts       map[string]int           `json:"offset_1ns_wire"`
	SplitExchanges     []volumeBoundaryExchange `json:"split_exchanges"`
	Scenario           string                   `json:"scenario"`
	Logs               map[string]int           `json:"logs"`
	Current            map[string]int           `json:"current_frontend"`
	Split              map[string]int           `json:"split_without_ns_adjustment"`
	EndOnly            map[string]int           `json:"subtract_ns_from_last_time_only"`
	AdjustedAST        map[string]int           `json:"adjusted_ast_engine_only"`
	AdjustedWireErrors []string                 `json:"adjusted_wire_errors"`
	AdjustedParts      []volumeBoundaryPart     `json:"adjusted_parts"`
}

// TestLogsVolumeBoundaryScenarios uses the real step-alignment middleware,
// LogQL parser and query engine over one shared, deterministic log corpus.
// MockQuerier supplies in-memory storage; neither Loki's evaluator nor parser
// is modified. Set VOLUME_BOUNDARY_VERIFY=current to run the old requests
// against the desired invariant (expected failures), or adjusted-wire to
// require the proposed lookbacks to parse (expected failures for multi-bucket
// ranges). The default run asserts and records these known limitations.
// The AST-only experiment is deliberately separate from the wire
// test: passing it does not establish a deployable frontend-only solution.
func TestLogsVolumeBoundaryScenarios(t *testing.T) {
	raw, err := os.ReadFile("testdata/logs_volume_boundaries.json")
	require.NoError(t, err)
	var fixture volumeBoundaryFixture
	require.NoError(t, json.Unmarshal(raw, &fixture))
	base, err := time.Parse(time.RFC3339Nano, fixture.BaseTime)
	require.NoError(t, err)
	reports := make([]volumeBoundaryReport, 0, len(fixture.Scenarios))
	for _, scenario := range fixture.Scenarios {
		t.Run(scenario.Name, func(t *testing.T) {
			start := base.Add(volumeNS(t, scenario.StartNS))
			end := base.Add(volumeNS(t, scenario.EndNS))
			step := volumeNS(t, scenario.StepNS)
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
			engine := logql.NewEngine(logql.EngineOpts{}, logql.NewMockQuerier(1, streams), logql.NoLimits, log.NewNopLogger())
			ctx := user.InjectOrgID(context.Background(), "volume-boundary-tests")
			logs := map[string]int{}
			for _, direction := range []logproto.Direction{logproto.FORWARD, logproto.BACKWARD} {
				params, err := logql.NewLiteralParams(fixture.Selector, start, end, step, 0, direction, 10000, nil, nil)
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
				require.Equal(t, scenario.ExpectedLogs, got, "log query is the [start,end) reference")
				logs = got
			}

			current, _ := volumeRunParts(t, ctx, engine, fixture.Selector, []volumeBoundaryPart{{start, end, step, step}}, false)
			require.Equal(t, scenario.ExpectedCurrent, current, "reproduce existing frontend coverage")
			if scenario.Name != "start_only" && scenario.Name != "end_only" {
				require.NotEqual(t, logs, current, "current frontend must reproduce the known discrepancy")
			}
			if os.Getenv("VOLUME_BOUNDARY_VERIFY") == "current" {
				require.Equal(t, logs, current, "desired invariant: current frontend is expected to fail")
			}
			splitParts := volumeDatasourceSplit(start, end, step)
			split, exchanges := volumeRunParts(t, ctx, engine, fixture.Selector, splitParts, false)
			offsetCounts, offsetExchanges := volumeRunParts(t, ctx, engine, fixture.Selector, splitParts, false, time.Nanosecond)
			require.Equal(t, logs, offsetCounts, "offset 1ns matches logs using real query text and step alignment")
			t.Logf("offset-1ns=%v logs=%v", offsetCounts, logs)
			endOnlyParts := volumeSplit(start, end, step, false)
			last := &endOnlyParts[len(endOnlyParts)-1]
			last.End = last.End.Add(-time.Nanosecond)
			last.Start = last.End
			endOnly, _ := volumeRunParts(t, ctx, engine, fixture.Selector, endOnlyParts, false)
			adjustedParts := volumeSplit(start, end, step, true)
			adjusted, _ := volumeRunParts(t, ctx, engine, fixture.Selector, adjustedParts, true)
			require.Equal(t, logs, adjusted, "exact outer windows match log counts, including each level")
			var wireErrors []string
			for _, part := range adjustedParts {
				query := fmt.Sprintf("sum by (level) (count_over_time(%s[%s]))", fixture.Selector, part.Window)
				_, err := syntax.ParseExpr(query)
				if os.Getenv("VOLUME_BOUNDARY_VERIFY") == "adjusted-wire" {
					require.NoError(t, err, "desired invariant: new approach must be expressible over the wire")
				}
				if part.Window%time.Millisecond != 0 {
					require.Error(t, err, "nanosecond lookbacks are currently rejected at the wire parser")
					wireErrors = append(wireErrors, err.Error())
				} else {
					require.NoError(t, err)
				}
			}
			if len(wireErrors) == 0 {
				wireCounts, _ := volumeRunParts(t, ctx, engine, fixture.Selector, adjustedParts, false)
				require.Equal(t, logs, wireCounts, "single-bucket solution works without bypassing the parser")
			}
			reports = append(reports, volumeBoundaryReport{offsetExchanges, offsetCounts, exchanges, scenario.Name, logs, current, split, endOnly, adjusted, wireErrors, adjustedParts})
			t.Logf("logs=%v current=%v split=%v end-only=%v adjusted-AST=%v wire-errors=%d", logs, current, split, endOnly, adjusted, len(wireErrors))
		})
	}
	if filename := os.Getenv("VOLUME_BOUNDARY_RESULTS"); filename != "" {
		raw, err := json.MarshalIndent(reports, "", "  ")
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filename, append(raw, '\n'), 0644))
	}
}

// Keep first and last buckets separate even when the requested bounds are
// aligned. Those full-width outer buckets also need the endpoint correction.
func volumeSplit(start, end time.Time, step time.Duration, adjusted bool) []volumeBoundaryPart {
	firstEnd := time.Unix(0, (start.UnixNano()/int64(step)+1)*int64(step))
	if !firstEnd.Before(end) {
		eval := end
		if adjusted {
			eval = eval.Add(-time.Nanosecond)
		}
		return []volumeBoundaryPart{{eval, eval, 0, end.Sub(start)}}
	}
	firstWindow := firstEnd.Sub(start)
	if adjusted {
		firstWindow += time.Nanosecond
	}
	parts := []volumeBoundaryPart{{firstEnd, firstEnd, 0, firstWindow}}
	lastStart := time.Unix(0, end.UnixNano()/int64(step)*int64(step))
	if lastStart.Equal(end) {
		lastStart = lastStart.Add(-step)
	}
	if firstEnd.Before(lastStart) {
		parts = append(parts, volumeBoundaryPart{firstEnd.Add(step), lastStart, step, step})
	}
	lastEnd := end
	if adjusted {
		lastEnd = lastEnd.Add(-time.Nanosecond)
	}
	return append(parts, volumeBoundaryPart{lastEnd, lastEnd, 0, lastEnd.Sub(lastStart)})
}

func volumeRunParts(t *testing.T, ctx context.Context, engine *logql.QueryEngine, selector string, parts []volumeBoundaryPart, overrideAST bool, offsets ...time.Duration) (map[string]int, []volumeBoundaryExchange) {
	t.Helper()
	counts := map[string]int{}
	var exchanges []volumeBoundaryExchange
	for _, part := range parts {
		// Parse a supported duration, then override the AST only for the
		// explicitly labelled engine experiment.
		window := part.Window
		if overrideAST && window%time.Millisecond != 0 {
			window = time.Second
		}
		query := fmt.Sprintf("sum by (level) (count_over_time(%s[%s]))", selector, model.Duration(window))
		if len(offsets) > 0 {
			query = fmt.Sprintf("sum by (level) (count_over_time(%s[%s] offset %s))", selector, model.Duration(window), offsets[0])
			expr, err := syntax.ParseExpr(query)
			require.NoError(t, err)
			query = expr.String()
			reparsed, err := syntax.ParseExpr(query)
			require.NoError(t, err)
			reparsed.Walk(func(node syntax.Expr) bool {
				if r, ok := node.(*syntax.LogRangeExpr); ok {
					require.Equal(t, offsets[0], r.Offset)
				}
				return true
			})
		}

		start, end := part.Start, part.End
		if part.Step > 0 {
			handler := queryrangebase.StepAlignMiddleware.Wrap(queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
				start, end = req.GetStart(), req.GetEnd()
				return nil, nil
			}))
			_, err := handler.Do(ctx, &queryrangebase.PrometheusRequest{Start: start, End: end, Step: part.Step.Milliseconds()})
			require.NoError(t, err)
		}
		params, err := logql.NewLiteralParams(query, start, end, part.Step, 0, logproto.FORWARD, 10000, nil, nil)
		require.NoError(t, err)
		if overrideAST {
			params.GetExpression().Walk(func(expr syntax.Expr) bool {
				if window, ok := expr.(*syntax.LogRangeExpr); ok {
					window.Interval = part.Window
				}
				return true
			})
		}
		result, err := engine.Query(params).Exec(ctx)
		require.NoError(t, err)

		switch values := result.Data.(type) {
		case promql.Vector:
			for _, sample := range values {
				if sample.F != 0 {
					counts[sample.Metric.Get("level")] += int(sample.F)
				}
			}
		case promql.Matrix:
			for _, series := range values {
				for _, sample := range series.Floats {
					if sample.F != 0 {
						counts[series.Metric.Get("level")] += int(sample.F)
					}
				}
			}
		default:
			t.Fatalf("unexpected metric result %T", result.Data)
		}
		// Use Loki's HTTP response encoder, not a hand-built approximation.
		// Zero execution statistics to keep the recorded fixtures deterministic.
		var body bytes.Buffer
		require.NoError(t, marshal.WriteQueryResponseJSON(result.Data, result.Warnings, stats.Result{}, &body, nil))
		response := json.RawMessage(body.Bytes())
		exchanges = append(exchanges, volumeBoundaryExchange{part, query, response})
	}
	return counts, exchanges
}

func TestLogsVolumeNanosecondDurationRoundTrip(t *testing.T) {
	for _, window := range []time.Duration{30*time.Second + time.Nanosecond, 30*time.Second - time.Nanosecond, time.Nanosecond} {
		expr, err := syntax.ParseExpr(`count_over_time({app="volume-boundary"}[1s])`)
		require.NoError(t, err)
		expr.(*syntax.RangeAggregationExpr).Left.Interval = window
		reparsed, err := syntax.ParseExpr(expr.String())
		require.NoError(t, err)
		require.Equal(t, window.Truncate(time.Millisecond), reparsed.(*syntax.RangeAggregationExpr).Left.Interval)
		t.Logf("requested=%s serialized=%s", window, expr.String())
	}
}

func volumeNS(t *testing.T, value string) time.Duration {
	t.Helper()
	ns, err := strconv.ParseInt(value, 10, 64)
	require.NoError(t, err)
	return time.Duration(ns)
}

// volumeDatasourceSplit reproduces the proposed datasource request layout before
// the nanosecond correction, including its omission of zero-width edge requests.
func volumeDatasourceSplit(start, end time.Time, step time.Duration) []volumeBoundaryPart {
	var parts []volumeBoundaryPart
	alignedStart := time.Unix(0, start.UnixNano()/int64(step)*int64(step))
	if !start.Equal(alignedStart) {
		edge := alignedStart.Add(step)
		if edge.After(end) {
			edge = end
		}
		parts = append(parts, volumeBoundaryPart{edge, edge, 0, edge.Sub(start)})
		start = edge
	}
	alignedEnd := time.Unix(0, end.UnixNano()/int64(step)*int64(step))
	if start.Before(alignedEnd) {
		parts = append(parts, volumeBoundaryPart{start.Add(step), alignedEnd, step, step})
		start = alignedEnd
	}
	if start.Before(end) {
		parts = append(parts, volumeBoundaryPart{end, end, 0, end.Sub(start)})
	}
	return parts
}

func TestLogsVolumeNanosecondLookbackSyntax(t *testing.T) {
	for _, duration := range []string{"30s1ns", "30000000001ns", "29999999999ns", "1ns"} {
		t.Run(duration, func(t *testing.T) {
			_, err := syntax.ParseExpr(fmt.Sprintf("count_over_time({app=%q}[%s])", "volume-boundary", duration))
			require.Error(t, err, "integer nanosecond spelling also fails, not just decimal seconds")
		})
	}
}
