package engine_lab

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/loki/v3/pkg/engine"
	"github.com/grafana/loki/v3/pkg/engine/internal/executor"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/logical"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/logqlmodel/metadata"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"
)

/*
================================================================================
ENGINE V2 FEATURE SUPPORT TEST
================================================================================

This file provides comprehensive tests of LogQL features supported in Engine V2.
Each feature has its own subtest for easy individual execution.

Each test executes against REAL STORAGE using TestIngester and attempts
the complete query flow: logical planning → physical planning → execution.

All features that have plan support are FULLY EXECUTABLE - no partial support!
If we can build a logical plan for it, we can execute it.

Feature Categories Tested:
  - Stream Selectors
  - Line Filters
  - Parsers (json, logfmt, regexp, pattern)
  - Label Filters
  - Label Manipulation (drop, keep, line_format, label_format)
  - Query Direction (forward, backward)
  - Metric Queries (count_over_time, rate, bytes_rate, etc.)
  - Aggregations (sum, avg, min, max, count, etc.)
  - Unwrap and Range Vector functions

FULLY SUPPORTED (Logical → Physical → Execution):
--------------------------------------------------
✓ Stream selectors (=, !=, =~, !~) - Working correctly!
✓ Multiple label selectors (AND)
✓ Line filters (|=, !=, |~, !~) - Working correctly!
✓ Multiple line filters (chaining)
✓ JSON parsing (| json) with label filters
✓ Logfmt parsing (| logfmt) with label filters
✓ Drop labels (| drop)
✓ BACKWARD direction
✓ Aggregations: sum, sum by, min, max

NOT SUPPORTED:
--------------
✗ FORWARD direction
✗ Instant vector queries (count_over_time, rate, bytes_over_time, bytes_rate)
✗ Keep labels (| keep)
✗ Line format (| line_format)
✗ Label format (| label_format)
✗ Decolorize (| decolorize)
✗ Regexp parser (| regexp)
✗ Pattern parser (| pattern)
✗ Unwrap expressions
✗ Label replace
✗ avg, count aggregations

================================================================================
*/

// ============================================================================
// HELPER METHODS
// ============================================================================

// testLogQuery executes a log query and returns the result streams
func testLogQuery(t *testing.T, ctx context.Context, query string, testData []LogEntry) logqlmodel.Streams {
	t.Helper()

	ingester := setupTestIngesterWithTimestamps(t, ctx, "test-tenant", testData)
	defer ingester.Close()

	catalog := ingester.Catalog()
	now := time.Now()

	q := &mockQuery{
		statement: query,
		start:     now.Add(-1 * time.Hour).Unix(),
		end:       now.Add(1 * time.Hour).Unix(),
		direction: logproto.BACKWARD,
		limit:     100,
	}

	// Logical planning
	logicalPlan, err := logical.BuildPlan(q)
	require.NoError(t, err, "logical planning should succeed")
	t.Logf("✓ Logical planning succeeded")

	// Physical planning
	planner := physical.NewPlanner(
		physical.NewContext(q.Start(), q.End()),
		catalog,
	)

	physicalPlan, err := planner.Build(logicalPlan)
	require.NoError(t, err, "physical planning should succeed")

	optimizedPlan, err := planner.Optimize(physicalPlan)
	require.NoError(t, err, "optimization should succeed")
	t.Logf("✓ Physical planning succeeded")

	// Execution
	execCtx := ctxWithTenant(ctx, "test-tenant")
	executorCfg := executor.Config{
		BatchSize: 100,
		Bucket:    ingester.Bucket(),
	}

	pipeline := executor.Run(execCtx, executorCfg, optimizedPlan, log.NewNopLogger())
	defer pipeline.Close()

	// Build result using the production streamResultBuilder
	builder := engine.NewStreamsResultBuilder(logproto.BACKWARD, false)

	for {
		rec, readErr := pipeline.Read(execCtx)
		if errors.Is(readErr, executor.EOF) {
			break
		}
		require.NoError(t, readErr, "execution should succeed")

		if rec != nil {
			builder.CollectRecord(rec)
			rec.Release()
		}
	}

	t.Logf("✓ Execution succeeded")

	// Build the logqlmodel.Result and extract streams
	metaCtx, _ := metadata.NewContext(context.Background())
	result := builder.Build(stats.Result{}, metaCtx)
	streams, ok := result.Data.(logqlmodel.Streams)
	require.True(t, ok, "result data should be logqlmodel.Streams, got %T", result.Data)

	// Print results
	t.Logf("\n=== QUERY RESULTS ===")
	t.Logf("Query: %s", query)
	t.Logf("Streams: %d", len(streams))
	for i, stream := range streams {
		t.Logf("  Stream %d: %s (%d entries)", i, stream.Labels, len(stream.Entries))
		for j, entry := range stream.Entries {
			if j < 5 { // Limit to first 5 entries per stream
				t.Logf("    [%s] %s", entry.Timestamp.Format("15:04:05.000"), entry.Line)
			}
		}
		if len(stream.Entries) > 5 {
			t.Logf("    ... and %d more entries", len(stream.Entries)-5)
		}
	}

	return streams
}

// testMetricQuery executes a metric query and returns the result vector
func testMetricQuery(t *testing.T, ctx context.Context, query string, testData []LogEntry) promql.Vector {
	t.Helper()

	ingester := setupTestIngesterWithTimestamps(t, ctx, "test-tenant", testData)
	defer ingester.Close()

	catalog := ingester.Catalog()
	now := time.Now()

	q := &mockQuery{
		statement: query,
		start:     now.Add(-10 * time.Minute).Unix(),
		end:       now.Unix(),
		interval:  5 * time.Minute,
	}

	// Logical planning
	logicalPlan, err := logical.BuildPlan(q)
	require.NoError(t, err, "logical planning should succeed")
	t.Logf("✓ Logical planning succeeded")

	// Physical planning
	planner := physical.NewPlanner(
		physical.NewContext(q.Start(), q.End()),
		catalog,
	)

	physicalPlan, err := planner.Build(logicalPlan)
	require.NoError(t, err, "physical planning should succeed")

	optimizedPlan, err := planner.Optimize(physicalPlan)
	require.NoError(t, err, "optimization should succeed")
	t.Logf("✓ Physical planning succeeded")

	// Execution
	execCtx := ctxWithTenant(ctx, "test-tenant")
	executorCfg := executor.Config{
		BatchSize: 100,
		Bucket:    ingester.Bucket(),
	}

	pipeline := executor.Run(execCtx, executorCfg, optimizedPlan, log.NewNopLogger())
	defer pipeline.Close()

	// Build result using the production vectorResultBuilder
	builder := engine.NewVectorResultBuilder()

	for {
		rec, readErr := pipeline.Read(execCtx)
		if errors.Is(readErr, executor.EOF) {
			break
		}
		require.NoError(t, readErr, "execution should succeed")

		if rec != nil {
			builder.CollectRecord(rec)
			rec.Release()
		}
	}

	t.Logf("✓ Execution succeeded")

	// Build the logqlmodel.Result and extract vector
	metaCtx, _ := metadata.NewContext(context.Background())
	result := builder.Build(stats.Result{}, metaCtx)
	vector, ok := result.Data.(promql.Vector)
	require.True(t, ok, "result data should be promql.Vector, got %T", result.Data)

	// Print results
	t.Logf("\n=== QUERY RESULTS ===")
	t.Logf("Query: %s", query)
	t.Logf("Vector samples: %d", len(vector))
	for i, sample := range vector {
		t.Logf("  Sample %d: %s = %v (t=%d)", i, sample.Metric.String(), sample.F, sample.T)
	}

	return vector
}

// testUnsupportedQuery verifies that a query fails at logical planning
func testUnsupportedQuery(t *testing.T, ctx context.Context, query string, testData []LogEntry) {
	t.Helper()

	ingester := setupTestIngesterWithTimestamps(t, ctx, "test-tenant", testData)
	defer ingester.Close()

	now := time.Now()

	// Determine if metric or log query
	isMetricQuery := len(query) >= 3 && (query[:3] == "sum" ||
		query[:3] == "avg" ||
		query[:3] == "min" ||
		query[:3] == "max") ||
		len(query) >= 5 && (query[:5] == "count" ||
			query[:5] == "bytes") ||
		len(query) >= 4 && query[:4] == "rate"

	var q *mockQuery
	if isMetricQuery {
		q = &mockQuery{
			statement: query,
			start:     now.Add(-10 * time.Minute).Unix(),
			end:       now.Unix(),
			interval:  5 * time.Minute,
		}
	} else {
		q = &mockQuery{
			statement: query,
			start:     now.Add(-1 * time.Hour).Unix(),
			end:       now.Add(1 * time.Hour).Unix(),
			direction: logproto.BACKWARD,
			limit:     100,
		}
	}

	_, err := logical.BuildPlan(q)
	require.Error(t, err, "expected error for unsupported feature")
	t.Logf("✗ NOT SUPPORTED (planning failed): %v", err)
}

// assertStreamsEqual compares two stream results
func assertStreamsEqual(t *testing.T, expected, actual logqlmodel.Streams) {
	t.Helper()

	require.Equal(t, len(expected), len(actual), "stream count mismatch")

	for i := range expected {
		require.Equal(t, expected[i].Labels, actual[i].Labels, "stream %d labels mismatch", i)
		require.Equal(t, len(expected[i].Entries), len(actual[i].Entries), "stream %d entry count mismatch", i)

		for j := range expected[i].Entries {
			require.Equal(t, expected[i].Entries[j].Line, actual[i].Entries[j].Line,
				"stream %d entry %d line mismatch", i, j)
			// Note: We don't compare timestamps exactly as they may vary slightly
		}
	}
}

// assertVectorEqual compares two vector results (ignoring timestamps)
func assertVectorEqual(t *testing.T, expected, actual promql.Vector) {
	t.Helper()

	require.Equal(t, len(expected), len(actual), "vector length mismatch")

	// Sort both vectors by labels for consistent comparison
	expectedSorted := make(promql.Vector, len(expected))
	copy(expectedSorted, expected)
	actualSorted := make(promql.Vector, len(actual))
	copy(actualSorted, actual)

	for i := range expectedSorted {
		found := false
		for j := range actualSorted {
			if labels.Equal(expectedSorted[i].Metric, actualSorted[j].Metric) {
				require.InDelta(t, expectedSorted[i].F, actualSorted[j].F, 0.001,
					"value mismatch for metric %s", expectedSorted[i].Metric.String())
				found = true
				break
			}
		}
		require.True(t, found, "expected metric %s not found in result", expectedSorted[i].Metric.String())
	}
}

// ============================================================================
// STREAM SELECTOR TESTS
// ============================================================================

func TestStreamSelectors(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	t.Run("simple_label_selector", func(t *testing.T) {
		testData := []LogEntry{
			{Labels: `{app="test"}`, Line: "log 1", Timestamp: now.Add(-3 * time.Minute)},
			{Labels: `{app="test"}`, Line: "log 2", Timestamp: now.Add(-2 * time.Minute)},
			{Labels: `{app="other"}`, Line: "log 3", Timestamp: now.Add(-1 * time.Minute)},
		}

		streams := testLogQuery(t, ctx, `{app="test"}`, testData)

		// Label selector works correctly - returns only matching streams
		var totalEntries int
		for _, stream := range streams {
			totalEntries += len(stream.Entries)
		}
		require.Equal(t, 2, totalEntries, "should return 2 logs with app=test")
		require.Equal(t, 1, len(streams), "should have 1 stream")
		require.Equal(t, `{app="test"}`, streams[0].Labels, "stream should have app=test label")
		t.Logf("✓ Label selector works correctly: got %d entries in %d stream", totalEntries, len(streams))
	})

	t.Run("multiple_label_selectors", func(t *testing.T) {
		testData := []LogEntry{
			{Labels: `{app="test", level="error"}`, Line: "error log", Timestamp: now.Add(-3 * time.Minute)},
			{Labels: `{app="test", level="info"}`, Line: "info log", Timestamp: now.Add(-2 * time.Minute)},
			{Labels: `{app="other", level="error"}`, Line: "other error", Timestamp: now.Add(-1 * time.Minute)},
		}

		streams := testLogQuery(t, ctx, `{app="test", level="error"}`, testData)

		// Multiple label selectors work correctly
		var totalEntries int
		for _, stream := range streams {
			totalEntries += len(stream.Entries)
		}
		require.Equal(t, 1, totalEntries, "should return 1 log with app=test AND level=error")
		require.Equal(t, 1, len(streams), "should have 1 stream")
		t.Logf("✓ Multiple label selectors work correctly: got %d entry in %d stream", totalEntries, len(streams))
	})

	t.Run("regex_label_selector", func(t *testing.T) {
		testData := []LogEntry{
			{Labels: `{app="test-1"}`, Line: "log 1", Timestamp: now.Add(-3 * time.Minute)},
			{Labels: `{app="test-2"}`, Line: "log 2", Timestamp: now.Add(-2 * time.Minute)},
			{Labels: `{app="other"}`, Line: "log 3", Timestamp: now.Add(-1 * time.Minute)},
		}

		streams := testLogQuery(t, ctx, `{app=~"test.*"}`, testData)

		// Regex label selector works correctly
		var totalEntries int
		for _, stream := range streams {
			totalEntries += len(stream.Entries)
		}
		require.Equal(t, 2, totalEntries, "should return 2 logs matching test.*")
		require.Equal(t, 2, len(streams), "should have 2 streams")
		t.Logf("✓ Regex label selector works correctly: got %d entries in %d streams", totalEntries, len(streams))
	})

	t.Run("negative_label_selector", func(t *testing.T) {
		testData := []LogEntry{
			{Labels: `{app="test", level="error"}`, Line: "error", Timestamp: now.Add(-3 * time.Minute)},
			{Labels: `{app="test", level="debug"}`, Line: "debug", Timestamp: now.Add(-2 * time.Minute)},
			{Labels: `{app="other", level="info"}`, Line: "info", Timestamp: now.Add(-1 * time.Minute)},
		}

		streams := testLogQuery(t, ctx, `{app="test", level!="debug"}`, testData)

		// Negative label selector works correctly
		var totalEntries int
		for _, stream := range streams {
			totalEntries += len(stream.Entries)
		}
		require.Equal(t, 1, totalEntries, "should return 1 log with app=test AND level!=debug")
		require.Equal(t, 1, len(streams), "should have 1 stream")
		t.Logf("✓ Negative label selector works correctly: got %d entry in %d stream", totalEntries, len(streams))
	})

	t.Run("negative_regex_selector", func(t *testing.T) {
		testData := []LogEntry{
			{Labels: `{app="test", level="error"}`, Line: "error", Timestamp: now.Add(-3 * time.Minute)},
			{Labels: `{app="test", level="debug"}`, Line: "debug", Timestamp: now.Add(-2 * time.Minute)},
			{Labels: `{app="test", level="trace"}`, Line: "trace", Timestamp: now.Add(-1 * time.Minute)},
		}

		streams := testLogQuery(t, ctx, `{app="test", level!~"debug|trace"}`, testData)

		// Negative regex selector works correctly
		var totalEntries int
		for _, stream := range streams {
			totalEntries += len(stream.Entries)
		}
		require.Equal(t, 1, totalEntries, "should return 1 log with level=error (not debug or trace)")
		require.Equal(t, 1, len(streams), "should have 1 stream")
		t.Logf("✓ Negative regex selector works correctly: got %d entry in %d stream", totalEntries, len(streams))
	})
}

// ============================================================================
// LINE FILTER TESTS
// ============================================================================

func TestLineFilters(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	t.Run("line_contains_filter", func(t *testing.T) {
		testData := []LogEntry{
			{Labels: `{app="test"}`, Line: "error: connection failed", Timestamp: now.Add(-3 * time.Minute)},
			{Labels: `{app="test"}`, Line: "info: request completed", Timestamp: now.Add(-2 * time.Minute)},
			{Labels: `{app="test"}`, Line: "error: timeout", Timestamp: now.Add(-1 * time.Minute)},
		}

		streams := testLogQuery(t, ctx, `{app="test"} |= "error"`, testData)

		// Line filter works correctly! Label selector filters to app="test" first, then line filter
		var totalEntries int
		for _, stream := range streams {
			totalEntries += len(stream.Entries)
		}
		require.Equal(t, 2, totalEntries, "should return 2 logs containing 'error'")

		// Verify content
		for _, stream := range streams {
			for _, entry := range stream.Entries {
				require.Contains(t, entry.Line, "error", "all entries should contain 'error'")
			}
		}
		t.Logf("✓ Line filter works correctly: got %d entries containing 'error'", totalEntries)
	})

	t.Run("line_not_contains_filter", func(t *testing.T) {
		testData := []LogEntry{
			{Labels: `{app="test"}`, Line: "info log", Timestamp: now.Add(-3 * time.Minute)},
			{Labels: `{app="test"}`, Line: "debug log", Timestamp: now.Add(-2 * time.Minute)},
			{Labels: `{app="test"}`, Line: "error log", Timestamp: now.Add(-1 * time.Minute)},
		}

		streams := testLogQuery(t, ctx, `{app="test"} != "debug"`, testData)

		var totalEntries int
		for _, stream := range streams {
			totalEntries += len(stream.Entries)
		}
		require.Equal(t, 2, totalEntries, "should return 2 logs not containing 'debug'")

		// Verify content
		for _, stream := range streams {
			for _, entry := range stream.Entries {
				require.NotContains(t, entry.Line, "debug", "no entries should contain 'debug'")
			}
		}
		t.Logf("✓ Line filter works correctly: got %d entries not containing 'debug'", totalEntries)
	})

	t.Run("line_regex_filter", func(t *testing.T) {
		testData := []LogEntry{
			{Labels: `{app="test"}`, Line: "error occurred", Timestamp: now.Add(-4 * time.Minute)},
			{Labels: `{app="test"}`, Line: "warn: high latency", Timestamp: now.Add(-3 * time.Minute)},
			{Labels: `{app="test"}`, Line: "info: success", Timestamp: now.Add(-2 * time.Minute)},
			{Labels: `{app="test"}`, Line: "debug: trace", Timestamp: now.Add(-1 * time.Minute)},
		}

		streams := testLogQuery(t, ctx, `{app="test"} |~ "error|warn"`, testData)

		var totalEntries int
		for _, stream := range streams {
			totalEntries += len(stream.Entries)
		}
		require.Equal(t, 2, totalEntries, "should return 2 logs matching 'error|warn'")
		t.Logf("✓ Line regex filter works correctly: got %d entries", totalEntries)
	})

	t.Run("negative_line_regex_filter", func(t *testing.T) {
		testData := []LogEntry{
			{Labels: `{app="test"}`, Line: "info log", Timestamp: now.Add(-3 * time.Minute)},
			{Labels: `{app="test"}`, Line: "debug log", Timestamp: now.Add(-2 * time.Minute)},
			{Labels: `{app="test"}`, Line: "trace log", Timestamp: now.Add(-1 * time.Minute)},
		}

		streams := testLogQuery(t, ctx, `{app="test"} !~ "debug|trace"`, testData)

		var totalEntries int
		for _, stream := range streams {
			totalEntries += len(stream.Entries)
		}
		require.Equal(t, 1, totalEntries, "should return 1 log not matching 'debug|trace'")
		t.Logf("✓ Negative line regex filter works correctly: got %d entry", totalEntries)
	})
}

// ============================================================================
// PARSER TESTS
// ============================================================================

func TestParsers(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	t.Run("json_parser", func(t *testing.T) {
		testData := []LogEntry{
			{Labels: `{app="json-app"}`, Line: `{"level":"error","msg":"timeout"}`, Timestamp: now.Add(-2 * time.Minute)},
			{Labels: `{app="json-app"}`, Line: `{"level":"info","msg":"success"}`, Timestamp: now.Add(-1 * time.Minute)},
		}

		streams := testLogQuery(t, ctx, `{app="json-app"} | json`, testData)

		var totalEntries int
		for _, stream := range streams {
			totalEntries += len(stream.Entries)
		}
		require.Equal(t, 2, totalEntries, "should return 2 logs")
		t.Logf("✓ JSON parser works: got %d entries", totalEntries)
	})

	t.Run("json_parser_with_label_filter", func(t *testing.T) {
		testData := []LogEntry{
			{Labels: `{app="json-app"}`, Line: `{"level":"error","msg":"timeout"}`, Timestamp: now.Add(-3 * time.Minute)},
			{Labels: `{app="json-app"}`, Line: `{"level":"info","msg":"success"}`, Timestamp: now.Add(-2 * time.Minute)},
			{Labels: `{app="json-app"}`, Line: `{"level":"error","msg":"failed"}`, Timestamp: now.Add(-1 * time.Minute)},
		}

		streams := testLogQuery(t, ctx, `{app="json-app"} | json | level="error"`, testData)

		var totalEntries int
		for _, stream := range streams {
			totalEntries += len(stream.Entries)
		}
		require.Equal(t, 2, totalEntries, "should return 2 logs with level=error after parsing")
		t.Logf("✓ JSON parser with label filter works: got %d entries", totalEntries)
	})

	t.Run("logfmt_parser", func(t *testing.T) {
		testData := []LogEntry{
			{Labels: `{app="logfmt-app"}`, Line: `level=error msg="connection refused"`, Timestamp: now.Add(-3 * time.Minute)},
			{Labels: `{app="logfmt-app"}`, Line: `level=info msg="request ok"`, Timestamp: now.Add(-2 * time.Minute)},
			{Labels: `{app="logfmt-app"}`, Line: `level=info msg="database" trace="foo"`, Timestamp: now.Add(-1 * time.Minute)},
		}

		streams := testLogQuery(t, ctx, `{app="logfmt-app"} | logfmt | trace="foo"`, testData)

		var totalEntries int
		for _, stream := range streams {
			totalEntries += len(stream.Entries)
		}
		require.Equal(t, 1, totalEntries, "should return 1 logs")
		t.Logf("✓ Logfmt parser works: got %d entries", totalEntries)
	})

	t.Run("logfmt_parser_with_label_filter", func(t *testing.T) {
		testData := []LogEntry{
			{Labels: `{app="logfmt-app"}`, Line: `level=error msg="connection refused"`, Timestamp: now.Add(-3 * time.Minute)},
			{Labels: `{app="logfmt-app"}`, Line: `level=info msg="request ok"`, Timestamp: now.Add(-2 * time.Minute)},
			{Labels: `{app="logfmt-app"}`, Line: `level=error msg="timeout"`, Timestamp: now.Add(-1 * time.Minute)},
		}

		streams := testLogQuery(t, ctx, `{app="logfmt-app"} | logfmt | level="error"`, testData)

		var totalEntries int
		for _, stream := range streams {
			totalEntries += len(stream.Entries)
		}
		require.Equal(t, 2, totalEntries, "should return 2 logs with level=error after parsing")
		t.Logf("✓ Logfmt parser with label filter works: got %d entries", totalEntries)
	})
}

// ============================================================================
// LABEL MANIPULATION TESTS
// ============================================================================

func TestLabelManipulation(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	t.Run("drop_labels", func(t *testing.T) {
		testData := []LogEntry{
			{Labels: `{app="test", env="prod"}`, Line: "log 1", Timestamp: now.Add(-2 * time.Minute)},
			{Labels: `{app="test", env="dev"}`, Line: "log 2", Timestamp: now.Add(-1 * time.Minute)},
		}

		streams := testLogQuery(t, ctx, `{app="test"} | drop app`, testData)

		var totalEntries int
		for _, stream := range streams {
			totalEntries += len(stream.Entries)
		}
		require.Equal(t, 2, totalEntries, "should return 2 logs (drop doesn't filter)")
		t.Logf("✓ Drop labels works: got %d entries", totalEntries)
	})

	t.Run("drop_labels_with_matchers", func(t *testing.T) {
		testData := []LogEntry{
			{Labels: `{app="test", env="prod"}`, Line: "log 1", Timestamp: now.Add(-2 * time.Minute)},
			{Labels: `{app="test", env="dev"}`, Line: "log 2", Timestamp: now.Add(-1 * time.Minute)},
		}

		testUnsupportedQuery(t, ctx, `{app="test"} | drop env="dev"`, testData)
	})

	t.Run("keep_labels_not_supported", func(t *testing.T) {
		testData := []LogEntry{
			{Labels: `{app="test", level="info"}`, Line: "log", Timestamp: now},
		}

		testUnsupportedQuery(t, ctx, `{app="test"} | keep level`, testData)
	})

	t.Run("line_format_not_supported", func(t *testing.T) {
		testData := []LogEntry{
			{Labels: `{app="test"}`, Line: "log", Timestamp: now},
		}

		testUnsupportedQuery(t, ctx, `{app="test"} | line_format "{{.level}}: {{.msg}}"`, testData)
	})

	t.Run("label_format_not_supported", func(t *testing.T) {
		testData := []LogEntry{
			{Labels: `{app="test"}`, Line: "log", Timestamp: now},
		}

		testUnsupportedQuery(t, ctx, `{app="test"} | label_format new_label="{{.app}}"`, testData)
	})

	t.Run("decolorize_not_supported", func(t *testing.T) {
		testData := []LogEntry{
			{Labels: `{app="test"}`, Line: "\x1b[31merror\x1b[0m", Timestamp: now},
		}

		testUnsupportedQuery(t, ctx, `{app="test"} | decolorize`, testData)
	})
}

// ============================================================================
// AGGREGATION TESTS
// ============================================================================

func TestAggregations(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	t.Run("sum_aggregation", func(t *testing.T) {
		testData := []LogEntry{
			{Labels: `{app="test", level="error"}`, Line: "log 1", Timestamp: now.Add(-3 * time.Minute)},
			{Labels: `{app="test", level="info"}`, Line: "log 2", Timestamp: now.Add(-2 * time.Minute)},
		}

		vector := testMetricQuery(t, ctx, `sum(count_over_time({app="test"}[5m]))`, testData)

		require.Equal(t, 1, len(vector), "should return single aggregated value")
		require.Greater(t, vector[0].F, 0.0, "sum should be greater than 0")
		t.Logf("✓ Sum aggregation works: value=%v", vector[0].F)
	})

	t.Run("sum_by_aggregation", func(t *testing.T) {
		testData := []LogEntry{
			{Labels: `{app="test", level="error"}`, Line: "log 1", Timestamp: now.Add(-4 * time.Minute)},
			{Labels: `{app="test", level="error"}`, Line: "log 2", Timestamp: now.Add(-3 * time.Minute)},
			{Labels: `{app="test", level="info"}`, Line: "log 3", Timestamp: now.Add(-2 * time.Minute)},
			{Labels: `{app="test", level="warn"}`, Line: "log 4", Timestamp: now.Add(-1 * time.Minute)},
		}

		vector := testMetricQuery(t, ctx, `sum by (level) (count_over_time({app="test"}[5m]))`, testData)

		require.Equal(t, 3, len(vector), "should return 3 levels")

		// Verify we have the expected labels
		labelSets := make(map[string]float64)
		for _, sample := range vector {
			level := sample.Metric.Get("level")
			labelSets[level] = sample.F
		}

		require.Contains(t, labelSets, "error", "should have error level")
		require.Contains(t, labelSets, "info", "should have info level")
		require.Contains(t, labelSets, "warn", "should have warn level")

		t.Logf("✓ Sum by aggregation works: %d series", len(vector))
		for level, value := range labelSets {
			t.Logf("  level=%s: count=%v", level, value)
		}
	})

	t.Run("min_aggregation", func(t *testing.T) {
		testData := []LogEntry{
			{Labels: `{app="test", stream="1"}`, Line: "log 1", Timestamp: now.Add(-2 * time.Minute)},
			{Labels: `{app="test", stream="2"}`, Line: "log 2", Timestamp: now.Add(-1 * time.Minute)},
		}

		vector := testMetricQuery(t, ctx, `min(count_over_time({app="test"}[5m]))`, testData)

		require.Equal(t, 1, len(vector), "should return single min value")
		t.Logf("✓ Min aggregation works: value=%v", vector[0].F)
	})

	t.Run("max_aggregation", func(t *testing.T) {
		testData := []LogEntry{
			{Labels: `{app="test", stream="1"}`, Line: "log 1", Timestamp: now.Add(-2 * time.Minute)},
			{Labels: `{app="test", stream="2"}`, Line: "log 2", Timestamp: now.Add(-1 * time.Minute)},
		}

		vector := testMetricQuery(t, ctx, `max(count_over_time({app="test"}[5m]))`, testData)

		require.Equal(t, 1, len(vector), "should return single max value")
		t.Logf("✓ Max aggregation works: value=%v", vector[0].F)
	})

	t.Run("avg_aggregation_not_supported", func(t *testing.T) {
		testData := []LogEntry{
			{Labels: `{app="test"}`, Line: "log", Timestamp: now},
		}

		testUnsupportedQuery(t, ctx, `avg(count_over_time({app="test"}[5m]))`, testData)
	})

	t.Run("count_aggregation_not_supported", func(t *testing.T) {
		testData := []LogEntry{
			{Labels: `{app="test"}`, Line: "log", Timestamp: now},
		}

		testUnsupportedQuery(t, ctx, `count(count_over_time({app="test"}[5m]))`, testData)
	})
}

// ============================================================================
// INSTANT VECTOR QUERY TESTS (NOT SUPPORTED)
// ============================================================================

func TestInstantVectorQueries(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	testData := []LogEntry{
		{Labels: `{app="test"}`, Line: "log", Timestamp: now},
	}

	t.Run("count_over_time_not_supported", func(t *testing.T) {
		testUnsupportedQuery(t, ctx, `count_over_time({app="test"}[5m])`, testData)
	})

	t.Run("rate_not_supported", func(t *testing.T) {
		testUnsupportedQuery(t, ctx, `rate({app="test"}[5m])`, testData)
	})

	t.Run("bytes_over_time_not_supported", func(t *testing.T) {
		testUnsupportedQuery(t, ctx, `bytes_over_time({app="test"}[5m])`, testData)
	})

	t.Run("bytes_rate_not_supported", func(t *testing.T) {
		testUnsupportedQuery(t, ctx, `bytes_rate({app="test"}[5m])`, testData)
	})
}

// ============================================================================
// COMPLEX QUERY TESTS
// ============================================================================

func TestComplexQueries(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	t.Run("multiple_line_filters", func(t *testing.T) {
		testData := []LogEntry{
			{Labels: `{app="test"}`, Line: "error: connection failed", Timestamp: now.Add(-3 * time.Minute)},
			{Labels: `{app="test"}`, Line: "error: timeout", Timestamp: now.Add(-2 * time.Minute)},
			{Labels: `{app="test"}`, Line: "info: connection ok", Timestamp: now.Add(-1 * time.Minute)},
		}

		streams := testLogQuery(t, ctx, `{app="test"} |= "error" |= "connection"`, testData)

		var totalEntries int
		for _, stream := range streams {
			totalEntries += len(stream.Entries)
		}
		require.Equal(t, 1, totalEntries, "should return 1 log containing both 'error' AND 'connection'")

		// Verify content
		for _, stream := range streams {
			for _, entry := range stream.Entries {
				require.Contains(t, entry.Line, "error")
				require.Contains(t, entry.Line, "connection")
			}
		}
		t.Logf("✓ Multiple line filters work correctly: got %d entry", totalEntries)
	})

	t.Run("line_filter_json_parsing_label_filter", func(t *testing.T) {
		testData := []LogEntry{
			{Labels: `{app="test"}`, Line: `{"level":"error","msg":"timeout"}`, Timestamp: now.Add(-3 * time.Minute)},
			{Labels: `{app="test"}`, Line: `{"level":"info","msg":"success"}`, Timestamp: now.Add(-2 * time.Minute)},
			{Labels: `{app="test"}`, Line: `{"level":"error","msg":"failed"}`, Timestamp: now.Add(-1 * time.Minute)},
		}

		streams := testLogQuery(t, ctx, `{app="test"} |= "level" | json | level="error"`, testData)

		var totalEntries int
		for _, stream := range streams {
			totalEntries += len(stream.Entries)
		}
		require.Equal(t, 2, totalEntries, "should return 2 JSON logs with level=error")
		t.Logf("✓ Complex query works: got %d entries", totalEntries)
	})

	t.Run("regex_selector_line_filter_drop", func(t *testing.T) {
		testData := []LogEntry{
			{Labels: `{app="test-1", env="prod"}`, Line: "error occurred", Timestamp: now.Add(-3 * time.Minute)},
			{Labels: `{app="test-2", env="dev"}`, Line: "error failed", Timestamp: now.Add(-2 * time.Minute)},
			{Labels: `{app="other", env="prod"}`, Line: "info success", Timestamp: now.Add(-1 * time.Minute)},
		}

		streams := testLogQuery(t, ctx, `{app=~"test.*"} |= "error" | drop env`, testData)

		var totalEntries int
		for _, stream := range streams {
			totalEntries += len(stream.Entries)
		}
		require.Equal(t, 2, totalEntries, "should return 2 logs containing 'error'")
		t.Logf("✓ Complex query with drop works: got %d entries", totalEntries)
	})
}

// ============================================================================
// DIRECTION TESTS
// ============================================================================

func TestQueryDirection(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	t.Run("backward_direction", func(t *testing.T) {
		testData := []LogEntry{
			{Labels: `{app="test"}`, Line: "log 1", Timestamp: now.Add(-2 * time.Minute)},
			{Labels: `{app="test"}`, Line: "log 2", Timestamp: now.Add(-1 * time.Minute)},
		}

		streams := testLogQuery(t, ctx, `{app="test"}`, testData)

		var totalEntries int
		for _, stream := range streams {
			totalEntries += len(stream.Entries)
		}
		require.Equal(t, 2, totalEntries, "should return 2 logs")
		t.Logf("✓ Backward direction works: got %d entries", totalEntries)
	})

	t.Run("forward_direction_not_supported", func(t *testing.T) {
		ingester := setupTestIngesterWithData(t, ctx, "test-tenant", map[string][]string{
			`{app="test"}`: {"log 1", "log 2", "log 3"},
		})
		defer ingester.Close()

		q := &mockQuery{
			statement: `{app="test"}`,
			start:     now.Add(-1 * time.Hour).Unix(),
			end:       now.Add(1 * time.Hour).Unix(),
			direction: logproto.FORWARD, // FORWARD not supported
			limit:     100,
		}

		_, err := logical.BuildPlan(q)
		require.Error(t, err, "FORWARD direction should not be supported")
		require.Contains(t, err.Error(), "forward")
		t.Logf("✗ FORWARD direction correctly rejected: %v", err)
	})
}
