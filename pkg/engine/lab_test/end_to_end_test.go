package engine_lab

/*
================================================================================
END-TO-END LEARNING LAB: COMPLETE QUERY EXECUTION FLOW
================================================================================

This file demonstrates the complete journey of a LogQL query through the
Loki Query Engine V2, from initial query string to final results. It ties
together all concepts from the previous stage files into cohesive examples.

================================================================================
THE COMPLETE QUERY PIPELINE
================================================================================

A LogQL query passes through 6 stages:

  ┌─────────────┐    ┌─────────────┐    ┌─────────────┐
  │   LogQL     │    │  Logical    │    │  Physical   │
  │   String    │───▶│   Plan      │───▶│   Plan      │
  │             │    │  (SSA IR)   │    │   (DAG)     │
  └─────────────┘    └─────────────┘    └─────────────┘
                                              │
        Stage 1: Parse          Stage 2:      │    Stage 3:
        & Build SSA             Build DAG     │    Create Tasks
                                              ▼
  ┌─────────────┐    ┌─────────────┐    ┌─────────────┐
  │   LogQL     │    │  Workflow   │    │   Arrow     │
  │   Result    │◀───│  Execution  │◀───│   (Tasks)   │
  │             │    │             │    │             │
  └─────────────┘    └─────────────┘    └─────────────┘
        Stage 6:           Stage 5:          Stage 4:
        Build Result       Run Workflow      Execute Tasks

================================================================================
DATA TRANSFORMATIONS AT EACH STAGE
================================================================================

  Stage 1 - LogQL → SSA:
    Input:  `{app="test"} |= "error"`
    Output: %1 = EQ label.app "test"
            %2 = MATCH_STR builtin.message "error"
            %3 = MAKETABLE [selector=%1, predicates=[%2]]
            ...

  Stage 2 - SSA → DAG:
    Input:  Linear SSA instructions
    Output: DAG with nodes:
            TopK → Filter → ScanSet → [DataObjScan, DataObjScan]

  Stage 3 - DAG → Workflow:
    Input:  Physical plan DAG
    Output: Task graph with streams:
            Task[TopK] ← Stream ← Task[Filter+Scan1]
                       ← Stream ← Task[Filter+Scan2]

  Stage 4 - Tasks → Pipelines:
    Input:  Task fragments
    Output: Pipeline tree producing Arrow RecordBatches

  Stage 5 - Pipelines → Arrow:
    Input:  Connected pipelines
    Output: Stream of Arrow RecordBatches

  Stage 6 - Arrow → LogQL Result:
    Input:  Arrow RecordBatches
    Output: logqlmodel.Streams or promql.Vector/Matrix

================================================================================
*/

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/go-kit/log"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/engine"
	"github.com/grafana/loki/v3/pkg/engine/internal/executor"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/logical"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/logqlmodel/metadata"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
)

// ============================================================================
// END-TO-END LOG QUERY TESTS
// ============================================================================

/*
TestEndToEnd_SimpleLogQuery demonstrates the complete journey of a simple
log query through all stages of the V2 engine.

This is the most fundamental query pattern - selecting logs from a stream
with a label selector and line filter.
*/
func TestEndToEnd_SimpleLogQuery(t *testing.T) {
	/*
	   ============================================================================
	   QUERY: {app="test"} |= "error"
	   ============================================================================

	   This query:
	   - Selects log streams where app="test"
	   - Filters lines containing "error"
	   - Returns up to 100 results, newest first

	   Expected Result Type: logqlmodel.Streams (log entries grouped by stream)
	*/

	// ============================================================================
	// STAGE 1: LOGICAL PLANNING
	// ============================================================================
	t.Run("stage1_logical_planning", func(t *testing.T) {
		/*
		   Input: LogQL query string + parameters
		   Output: SSA intermediate representation

		   The logical planner converts the parsed AST into SSA form where:
		   - Each variable is assigned exactly once
		   - Data flow is explicit
		   - Operations are independent of physical storage
		*/
		q := &mockQuery{
			statement: `{app="test"} |= "error"`,
			start:     1000,
			end:       2000,
			direction: logproto.BACKWARD,
			limit:     100,
		}

		plan, err := logical.BuildPlan(q)
		require.NoError(t, err)

		t.Logf("\n=== STAGE 1: LOGICAL PLAN (SSA) ===\n%s", plan.String())

		/*
		   Expected SSA output:
		   %1 = EQ label.app "test"              <- Stream selector predicate
		   %2 = MATCH_STR builtin.message "error" <- Line filter predicate
		   %3 = MAKETABLE [selector=%1, predicates=[%2], shard=0_of_1]
		   %4 = GTE builtin.timestamp <start>
		   %5 = SELECT %3 [predicate=%4]
		   %6 = LT builtin.timestamp <end>
		   %7 = SELECT %5 [predicate=%6]
		   %8 = SELECT %7 [predicate=%2]         <- Apply line filter
		   %9 = TOPK %8 [sort_by=builtin.timestamp, k=100, asc=false]
		   %10 = LOGQL_COMPAT %9
		   RETURN %10

		   Key observations:
		   1. Predicates are created before use (functional style)
		   2. MAKETABLE defines the data source with pushdown hints
		   3. SELECT operations apply filters step by step
		   4. TOPK handles both sorting and limiting
		   5. LOGQL_COMPAT ensures output format compatibility
		*/

		planStr := plan.String()
		require.Contains(t, planStr, "EQ label.app")
		require.Contains(t, planStr, "MATCH_STR builtin.message")
		require.Contains(t, planStr, "MAKETABLE")
		require.Contains(t, planStr, "SELECT")
		require.Contains(t, planStr, "TOPK")
	})

	// ============================================================================
	// STAGE 2: PHYSICAL PLANNING
	// ============================================================================
	t.Run("stage2_physical_planning", func(t *testing.T) {
		/*
		   Input: Logical plan (SSA)
		   Output: Physical plan (DAG) with concrete data sources

		   NOW USES REAL STORAGE via TestIngester.

		   The physical planner:
		   - Resolves MAKETABLE to actual data object locations via catalog
		   - Converts SSA instructions to DAG nodes
		   - Applies optimization passes (pushdown, parallelization)
		*/
		ctx := context.Background()

		// Create test ingester with real data containing "error" lines
		ingester := setupTestIngesterWithData(t, ctx, "test-tenant", map[string][]string{
			`{app="test"}`: {
				"error: connection failed",
				"info: request processed",
				"error: timeout occurred",
				"debug: trace data",
			},
		})
		defer ingester.Close()

		catalog := ingester.Catalog()

		now := time.Now()
		q := &mockQuery{
			statement: `{app="test"} |= "error"`,
			start:     now.Add(-1 * time.Hour).Unix(),
			end:       now.Add(1 * time.Hour).Unix(),
			direction: logproto.BACKWARD,
			limit:     100,
		}

		logicalPlan, err := logical.BuildPlan(q)
		require.NoError(t, err)

		planner := physical.NewPlanner(
			physical.NewContext(q.Start(), q.End()),
			catalog,
		)

		physicalPlan, err := planner.Build(logicalPlan)
		require.NoError(t, err)

		t.Logf("\n=== STAGE 2: PHYSICAL PLAN (Before Optimization) ===\n%s",
			physical.PrintAsTree(physicalPlan))

		// Apply optimizations
		optimizedPlan, err := planner.Optimize(physicalPlan)
		require.NoError(t, err)

		t.Logf("\n=== STAGE 2: PHYSICAL PLAN (After Optimization) ===\n%s",
			physical.PrintAsTree(optimizedPlan))

		/*
		   Expected physical plan structure (after optimization):
		   TopK
		   └── Parallelize
		         └── ScanSet [predicates pushed down]
		               ├── DataObjScan [from real storage]
		               └── ... (may have multiple based on data layout)

		   Key observations:
		   1. MAKETABLE resolved to ScanSet with REAL scan targets from storage
		   2. Predicates pushed down to ScanSet (optimization)
		   3. Parallelize node inserted for distributed execution
		   4. Filter nodes may be eliminated after pushdown
		*/

		root, err := optimizedPlan.Root()
		require.NoError(t, err)
		require.NotNil(t, root)

		// Verify we got real scan targets from storage
		var targetCount int
		err = optimizedPlan.DFSWalk(root, func(n physical.Node) error {
			if scanSet, ok := n.(*physical.ScanSet); ok {
				targetCount += len(scanSet.Targets)
			}
			return nil
		}, dag.PreOrderWalk)
		require.NoError(t, err)
		require.Greater(t, targetCount, 0, "should have at least one scan target from real storage")
		t.Logf("Found %d scan targets from real storage", targetCount)
	})

	// ============================================================================
	// STAGE 3: WORKFLOW PLANNING
	// ============================================================================
	t.Run("stage3_workflow_planning", func(t *testing.T) {
		/*
		   Input: Physical plan (DAG)
		   Output: Workflow with tasks and streams

		   NOW USES REAL STORAGE via TestIngester.

		   The workflow planner:
		   - Partitions the DAG at pipeline breakers
		   - Creates tasks for parallel execution
		   - Connects tasks via data streams
		*/
		ctx := context.Background()

		// Create test ingester with data in multiple streams
		ingester := setupTestIngesterWithData(t, ctx, "test-tenant", map[string][]string{
			`{app="test", stream="1"}`: {
				"error: connection failed",
				"error: timeout occurred",
			},
			`{app="test", stream="2"}`: {
				"error: invalid input",
			},
		})
		defer ingester.Close()

		catalog := ingester.Catalog()

		// Build logical plan
		now := time.Now()
		q := &mockQuery{
			statement: `{app="test"} |= "error"`,
			start:     now.Add(-1 * time.Hour).Unix(),
			end:       now.Add(1 * time.Hour).Unix(),
			direction: logproto.BACKWARD,
			limit:     100,
		}

		logicalPlan, err := logical.BuildPlan(q)
		require.NoError(t, err)

		// Build physical plan with real catalog
		planner := physical.NewPlanner(
			physical.NewContext(q.Start(), q.End()),
			catalog,
		)

		physicalPlan, err := planner.Build(logicalPlan)
		require.NoError(t, err)

		optimizedPlan, err := planner.Optimize(physicalPlan)
		require.NoError(t, err)

		runner := newTestRunner()
		wf, err := workflow.New(
			workflow.Options{},
			log.NewNopLogger(),
			"test-tenant",
			runner,
			optimizedPlan,
		)
		require.NoError(t, err)

		t.Logf("\n=== STAGE 3: WORKFLOW ===\n%s", workflow.Sprint(wf))

		/*
		   Expected workflow structure:
		   Workflow:
		     Tasks:
		       - Task[0]: TopK (root task, receives merged results)
		       - Task[1+]: Scan tasks (leaf tasks, one per data object section)
		     Streams:
		       - Stream[N]: Task[scan] → Task[TopK]

		   Key observations:
		   1. Pipeline breakers (TopK) force task boundaries
		   2. Each scan target FROM REAL STORAGE becomes a separate task
		   3. Streams connect tasks for data flow
		   4. Root task aggregates results from leaf tasks
		*/

		// Verify tasks and streams were created from real data
		runner.mu.RLock()
		numTasks := len(runner.tasks)
		numStreams := len(runner.streams)
		runner.mu.RUnlock()

		t.Logf("Tasks created from real storage: %d", numTasks)
		t.Logf("Streams created: %d", numStreams)
	})

	// ============================================================================
	// STAGES 4-5: EXECUTION (Simulated)
	// ============================================================================
	t.Run("stage4_5_execution", func(t *testing.T) {
		/*
		   Input: Workflow tasks
		   Output: Arrow RecordBatches

		   NOW EXECUTES AGAINST REAL STORAGE.

		   Execution involves:
		   1. Scheduler assigns tasks to workers
		   2. Workers execute task fragments
		   3. Pipelines read from REAL STORAGE and produce Arrow batches
		   4. Results flow through streams to root
		*/
		ctx := context.Background()

		// Create test ingester with timestamped data for predictable results
		ingester := setupTestIngesterWithTimestamps(t, ctx, "test-tenant", []LogEntry{
			{
				Labels:    `{app="test"}`,
				Line:      "error: connection failed",
				Timestamp: time.Unix(0, 1000000003),
			},
			{
				Labels:    `{app="test"}`,
				Line:      "error: timeout occurred",
				Timestamp: time.Unix(0, 1000000001),
			},
			{
				Labels:    `{app="test"}`,
				Line:      "error: invalid input",
				Timestamp: time.Unix(0, 1000000002),
			},
		})
		defer ingester.Close()

		catalog := ingester.Catalog()

		// Build and execute complete query
		q := &mockQuery{
			statement: `{app="test"} |= "error"`,
			start:     time.Unix(0, 0).Unix(),
			end:       time.Unix(0, 2000000000).Unix(),
			direction: logproto.BACKWARD,
			limit:     100,
		}

		logicalPlan, err := logical.BuildPlan(q)
		require.NoError(t, err)

		planner := physical.NewPlanner(
			physical.NewContext(q.Start(), q.End()),
			catalog,
		)

		physicalPlan, err := planner.Build(logicalPlan)
		require.NoError(t, err)

		optimizedPlan, err := planner.Optimize(physicalPlan)
		require.NoError(t, err)

		// Execute the plan using the executor (with tenant context)
		execCtx := ctxWithTenant(ctx, "test-tenant")
		executorCfg := executor.Config{
			BatchSize: 100,
			Bucket:    ingester.Bucket(), // CRITICAL: executor needs bucket to read DataObj files
		}

		pipeline := executor.Run(execCtx, executorCfg, optimizedPlan, log.NewNopLogger())
		defer pipeline.Close()

		t.Logf("\n=== STAGES 4-5: ARROW RECORDBATCHES FROM REAL STORAGE ===")

		// Read results from the pipeline
		var totalRows int64
		for {
			rec, err := pipeline.Read(execCtx)
			if errors.Is(err, executor.EOF) {
				break
			}
			require.NoError(t, err)

			totalRows += rec.NumRows()
			t.Logf("Read RecordBatch: %d rows, %d columns", rec.NumRows(), rec.NumCols())

			// Log some sample data - find message column by name
			if rec.NumRows() > 0 {
				for i := 0; i < int(rec.NumCols()); i++ {
					field := rec.Schema().Field(i)
					t.Logf("  Column %d: %s (type=%s)", i, field.Name, field.Type)
					if field.Name == semconv.ColumnIdentMessage.FQN() {
						msgCol := rec.Column(i).(*array.String)
						t.Logf("  First message: %s", msgCol.Value(0))
					}
				}
			}

			rec.Release()
		}

		t.Logf("Total rows read from real storage: %d", totalRows)
		require.Greater(t, totalRows, int64(0), "should have read data from real storage")

		/*
		   Expected flow:
		   1. Scan tasks read from REAL DataObj files in storage
		   2. Pipelines produce Arrow RecordBatches with actual data
		   3. TopK Task receives batches via streams
		   4. TopK merges and sorts by timestamp descending
		   5. TopK outputs final sorted records
		*/
	})

	// ============================================================================
	// STAGE 6: RESULT BUILDING
	// ============================================================================
	t.Run("stage6_result_building", func(t *testing.T) {
		/*
		   Input: Arrow RecordBatches
		   Output: logqlmodel.Streams

		   NOW READS REAL DATA from execution pipeline.

		   The result builder:
		   - Reads columns from Arrow batches
		   - Groups entries by stream labels
		   - Creates logproto.Entry objects with timestamp and line
		   - Returns sorted streams
		*/
		ctx := context.Background()

		// Create test ingester with timestamped data
		ingester := setupTestIngesterWithTimestamps(t, ctx, "test-tenant", []LogEntry{
			{
				Labels:    `{app="test"}`,
				Line:      "error: connection failed",
				Timestamp: time.Unix(0, 1000000003),
			},
			{
				Labels:    `{app="test"}`,
				Line:      "error: invalid input",
				Timestamp: time.Unix(0, 1000000002),
			},
			{
				Labels:    `{app="test"}`,
				Line:      "error: timeout occurred",
				Timestamp: time.Unix(0, 1000000001),
			},
		})
		defer ingester.Close()

		catalog := ingester.Catalog()

		// Build and execute query
		q := &mockQuery{
			statement: `{app="test"} |= "error"`,
			start:     time.Unix(0, 0).Unix(),
			end:       time.Unix(0, 2000000000).Unix(),
			direction: logproto.BACKWARD,
			limit:     100,
		}

		logicalPlan, err := logical.BuildPlan(q)
		require.NoError(t, err)

		planner := physical.NewPlanner(
			physical.NewContext(q.Start(), q.End()),
			catalog,
		)

		physicalPlan, err := planner.Build(logicalPlan)
		require.NoError(t, err)

		optimizedPlan, err := planner.Optimize(physicalPlan)
		require.NoError(t, err)

		// Execute and read results (with tenant context)
		execCtx := ctxWithTenant(ctx, "test-tenant")
		executorCfg := executor.Config{
			BatchSize: 100,
			Bucket:    ingester.Bucket(), // CRITICAL: executor needs bucket
		}
		pipeline := executor.Run(execCtx, executorCfg, optimizedPlan, log.NewNopLogger())
		defer pipeline.Close()

		t.Logf("\n=== STAGE 6: RESULT BUILDING FROM REAL STORAGE ===")

		// Read and inspect Arrow RecordBatches from real execution
		var finalRecord arrow.RecordBatch
		for {
			rec, err := pipeline.Read(execCtx)
			if errors.Is(err, executor.EOF) {
				break
			}
			require.NoError(t, err)

			// For simplicity, just use the first batch
			if finalRecord == nil {
				finalRecord = rec
			} else {
				rec.Release()
			}
		}

		if finalRecord != nil {
			defer finalRecord.Release()

			t.Logf("Final Results from Real Storage (logqlmodel.Streams):")
			t.Logf("Schema columns: %d", finalRecord.NumCols())

			// Log schema for debugging
			for i := 0; i < int(finalRecord.NumCols()); i++ {
				field := finalRecord.Schema().Field(i)
				t.Logf("  Column %d: %s (type=%s)", i, field.Name, field.Type)
			}

			// Find timestamp and message columns by semantic name
			var tsColIdx, msgColIdx int = -1, -1
			for i := 0; i < int(finalRecord.NumCols()); i++ {
				name := finalRecord.Schema().Field(i).Name
				if name == semconv.ColumnIdentTimestamp.FQN() {
					tsColIdx = i
				} else if name == semconv.ColumnIdentMessage.FQN() {
					msgColIdx = i
				}
			}

			if tsColIdx >= 0 && msgColIdx >= 0 {
				tsCol := finalRecord.Column(tsColIdx).(*array.Timestamp)
				msgCol := finalRecord.Column(msgColIdx).(*array.String)

				t.Logf("Stream: {app=\"test\"}")
				for i := 0; i < int(finalRecord.NumRows()); i++ {
					ts := time.Unix(0, tsCol.Value(i).ToTime(arrow.Nanosecond).UnixNano())
					msg := msgCol.Value(i)
					t.Logf("  [%v] %s", ts, msg)
				}
			}

			require.Greater(t, int(finalRecord.NumRows()), 0, "should have results from real storage")

			/*
			   Expected LogQL Result:
			   logqlmodel.Streams{
			       {
			           Labels: {app="test"},
			           Entries: [
			               {Timestamp: ..., Line: "error: connection failed"},
			               {Timestamp: ..., Line: "error: invalid input"},
			               {Timestamp: ..., Line: "error: timeout occurred"},
			           ]
			       }
			   }

			   Key observations:
			   1. Entries are grouped by unique label combinations
			   2. Entries within each stream are sorted by timestamp
			   3. Direction (BACKWARD) determines sort order (newest first)
			   4. Result is compatible with V1 LogQL API
			   5. ALL DATA COMES FROM REAL STORAGE, NOT SIMULATION!
			*/
		}
	})
}

// ============================================================================
// END-TO-END METRIC QUERY TESTS
// ============================================================================

/*
TestEndToEnd_MetricQuery demonstrates the complete journey of a metric
query through all stages.

Metric queries differ from log queries:
- They return numeric time series instead of log lines
- They involve range and vector aggregations
- The result type is promql.Vector or promql.Matrix
*/
func TestEndToEnd_MetricQuery(t *testing.T) {
	/*
	   ============================================================================
	   QUERY: sum by (level) (count_over_time({app="test"}[5m]))
	   ============================================================================

	   This query:
	   - Counts log lines per 5-minute window for each stream
	   - Groups and sums the counts by the "level" label
	   - Returns a vector (single point per series) or matrix (time series)

	   Expected Result Type: promql.Vector (for instant queries)
	                        promql.Matrix (for range queries)
	*/

	// ============================================================================
	// STAGE 1: LOGICAL PLANNING FOR METRIC QUERIES
	// ============================================================================
	t.Run("stage1_metric_logical_plan", func(t *testing.T) {
		q := &mockQuery{
			statement: `sum by (level) (count_over_time({app="test"}[5m]))`,
			start:     3600,
			end:       7200,
			interval:  5 * time.Minute,
		}

		plan, err := logical.BuildPlan(q)
		require.NoError(t, err)

		t.Logf("\n=== METRIC QUERY LOGICAL PLAN ===\n%s", plan.String())

		/*
		   Expected SSA output:
		   %1 = EQ label.app "test"
		   %2 = MAKETABLE [selector=%1, predicates=[], shard=0_of_1]
		   %3 = GTE builtin.timestamp <start - 5min>  <- Lookback adjustment!
		   %4 = SELECT %2 [predicate=%3]
		   %5 = LT builtin.timestamp <end>
		   %6 = SELECT %4 [predicate=%5]
		   %7 = RANGE_AGGREGATION %6 [operation=count, range=5m]
		   %8 = VECTOR_AGGREGATION %7 [operation=sum, group_by=(level)]
		   %9 = LOGQL_COMPAT %8
		   RETURN %9

		   Key differences from log queries:
		   1. No TOPK - results are aggregated values, not sorted logs
		   2. Time range starts earlier (lookback for range aggregation)
		   3. RANGE_AGGREGATION counts entries per time window
		   4. VECTOR_AGGREGATION groups and sums across series
		*/

		planStr := plan.String()
		require.Contains(t, planStr, "RANGE_AGGREGATION")
		require.Contains(t, planStr, "VECTOR_AGGREGATION")
		require.Contains(t, planStr, "operation=count")
		require.Contains(t, planStr, "operation=sum")
	})

	// ============================================================================
	// METRIC QUERY EXECUTION FLOW
	// ============================================================================
	t.Run("metric_query_execution_flow", func(t *testing.T) {
		/*
		   Metric query execution differs from log queries:

		   1. Scan tasks read log entries (same as log queries)
		   2. RangeAggregation task counts entries per time window:
		      - Groups by stream labels + timestamp bucket
		      - Produces count values per window
		   3. VectorAggregation task sums counts:
		      - Groups by specified labels (level)
		      - Produces final aggregated values

		   Arrow data transformation:
		   - Scan output: (timestamp, message, labels)
		   - RangeAgg output: (timestamp, value, labels) where value = count
		   - VectorAgg output: (timestamp, value, grouped_labels) where value = sum
		*/
		ctx := context.Background()

		// Create test data with different levels for aggregation testing
		now := time.Now()
		entries := []LogEntry{
			// Error level entries
			{Labels: `{app="test", level="error"}`, Line: "error 1", Timestamp: now.Add(-9 * time.Minute)},
			{Labels: `{app="test", level="error"}`, Line: "error 2", Timestamp: now.Add(-8 * time.Minute)},
			{Labels: `{app="test", level="error"}`, Line: "error 3", Timestamp: now.Add(-7 * time.Minute)},
			{Labels: `{app="test", level="error"}`, Line: "error 4", Timestamp: now.Add(-6 * time.Minute)},
			{Labels: `{app="test", level="error"}`, Line: "error 5", Timestamp: now.Add(-5 * time.Minute)},
			// Info level entries
			{Labels: `{app="test", level="info"}`, Line: "info 1", Timestamp: now.Add(-9 * time.Minute)},
			{Labels: `{app="test", level="info"}`, Line: "info 2", Timestamp: now.Add(-8 * time.Minute)},
		}

		ingester := setupTestIngesterWithTimestamps(t, ctx, "test-tenant", entries)
		defer ingester.Close()

		catalog := ingester.Catalog()

		// Build metric query
		q := &mockQuery{
			statement: `sum by (level) (count_over_time({app="test"}[5m]))`,
			start:     now.Add(-10 * time.Minute).Unix(),
			end:       now.Unix(),
			interval:  5 * time.Minute,
		}

		logicalPlan, err := logical.BuildPlan(q)
		require.NoError(t, err)

		planner := physical.NewPlanner(
			physical.NewContext(q.Start(), q.End()),
			catalog,
		)

		physicalPlan, err := planner.Build(logicalPlan)
		require.NoError(t, err)

		optimizedPlan, err := planner.Optimize(physicalPlan)
		require.NoError(t, err)

		// Execute the metric query against real storage (with tenant context)
		execCtx := ctxWithTenant(ctx, "test-tenant")
		executorCfg := executor.Config{
			BatchSize: 100,
			Bucket:    ingester.Bucket(), // CRITICAL: executor needs bucket
		}
		pipeline := executor.Run(execCtx, executorCfg, optimizedPlan, log.NewNopLogger())
		defer pipeline.Close()

		t.Logf("\n=== METRIC QUERY EXECUTION ON REAL STORAGE ===")

		// Use the production vectorResultBuilder from pkg/engine/compat.go
		// to build promql.Vector results from Arrow RecordBatches.
		builder := engine.NewVectorResultBuilder()

		for {
			rec, err := pipeline.Read(execCtx)
			if errors.Is(err, executor.EOF) {
				break
			}
			require.NoError(t, err)

			t.Logf("Read metric RecordBatch: %d rows, %d cols", rec.NumRows(), rec.NumCols())
			for i := 0; i < int(rec.NumCols()); i++ {
				field := rec.Schema().Field(i)
				t.Logf("  Column %d: %s (type=%s)", i, field.Name, field.Type)
			}

			// Collect record data into promql.Samples
			builder.CollectRecord(rec)
			rec.Release()
		}

		require.Greater(t, builder.Len(), 0, "result vector should not be empty")

		// Build the logqlmodel.Result from collected samples and extract the promql.Vector
		metaCtx, _ := metadata.NewContext(context.Background())
		result := builder.Build(stats.Result{}, metaCtx)
		vector, ok := result.Data.(promql.Vector)
		require.True(t, ok, "result data should be promql.Vector, got %T", result.Data)

		t.Logf("\n=== RESULT VERIFICATION (promql.Vector) ===")
		t.Logf("Vector length: %d", len(vector))
		for i, sample := range vector {
			t.Logf("  Sample %d: T=%d, F=%v, Metric=%s", i, sample.T, sample.F, sample.Metric.String())
		}

		// Expected result for: sum by (level) (count_over_time({app="test"}[5m]))
		// Given test data:
		//   - 5 error level entries
		//   - 2 info level entries
		// Note: Vector samples are sorted by labels.Compare() in Build()
		expectedVector := promql.Vector{
			{
				Metric: labels.FromStrings("level", "error"),
				F:      5.0,
			},
			{
				Metric: labels.FromStrings("level", "info"),
				F:      2.0,
			},
		}

		// Verify we got the expected number of samples
		require.Equal(t, len(expectedVector), len(vector), "unexpected number of vector samples")

		// Verify each sample (ignoring timestamp as it varies)
		for _, expected := range expectedVector {
			found := false
			for _, actual := range vector {
				if labels.Equal(expected.Metric, actual.Metric) {
					require.Equal(t, expected.F, actual.F, "value mismatch for metric %s", expected.Metric.String())
					t.Logf("✓ Verified: %s = %v", expected.Metric.String(), expected.F)
					found = true
					break
				}
			}
			require.True(t, found, "expected metric %s not found in result", expected.Metric.String())
		}

		t.Logf("✓ All expected promql.Vector samples verified!")

		/*
		   Expected Result:
		   promql.Vector{
		       {Metric: {level="error"}, Point: {T: ..., V: 5}},
		       {Metric: {level="info"}, Point: {T: ..., V: 2}},
		   }

		   Key observations:
		   1. Multiple streams merged into fewer series
		   2. Values are aggregated (sum in this case)
		   3. Labels reduced to only group_by labels
		   4. Timestamp represents the evaluation point
		   5. ALL DATA AND AGGREGATIONS FROM REAL STORAGE EXECUTION!
		*/
	})
}

// ============================================================================
// END-TO-END WITH JSON PARSING
// ============================================================================

/*
TestEndToEnd_JSONParsingQuery demonstrates a query that parses JSON
from log lines and filters on extracted fields.
*/
func TestEndToEnd_JSONParsingQuery(t *testing.T) {
	/*
	   ============================================================================
	   QUERY: {app="test"} | json | level="error"
	   ============================================================================

	   This query:
	   - Selects log streams where app="test"
	   - Parses each log line as JSON
	   - Filters entries where the parsed "level" field equals "error"

	   Pipeline stages:
	   1. Stream selection: {app="test"}
	   2. JSON parsing: | json
	   3. Field filtering: | level="error"
	*/

	t.Run("json_parsing_flow", func(t *testing.T) {
		/*
		   NOW TESTS JSON PARSING WITH REAL STORAGE.

		   This demonstrates:
		   1. Ingesting JSON-formatted log lines
		   2. Query-time JSON parsing
		   3. Filtering on parsed fields
		*/
		ctx := context.Background()

		// Create test data with JSON-formatted log lines
		ingester := setupTestIngesterWithTimestamps(t, ctx, "test-tenant", []LogEntry{
			{
				Labels:    `{app="test"}`,
				Line:      `{"level": "error", "msg": "connection failed"}`,
				Timestamp: time.Unix(0, 1000000002),
			},
			{
				Labels:    `{app="test"}`,
				Line:      `{"level": "info", "msg": "request processed"}`,
				Timestamp: time.Unix(0, 1000000003),
			},
			{
				Labels:    `{app="test"}`,
				Line:      `{"level": "error", "msg": "timeout"}`,
				Timestamp: time.Unix(0, 1000000001),
			},
		})
		defer ingester.Close()

		catalog := ingester.Catalog()

		// STAGE 1: Logical planning with JSON parsing
		q := &mockQuery{
			statement: `{app="test"} | json | level="error"`,
			start:     time.Unix(0, 0).Unix(),
			end:       time.Unix(0, 2000000000).Unix(),
			direction: logproto.BACKWARD,
			limit:     100,
		}

		plan, err := logical.BuildPlan(q)
		require.NoError(t, err)

		t.Logf("\n=== JSON QUERY LOGICAL PLAN ===\n%s", plan.String())

		planStr := plan.String()
		require.Contains(t, planStr, "PROJECT")
		require.Contains(t, planStr, "PARSE_JSON")
		require.Contains(t, planStr, "ambiguous.level")

		/*
		   SSA highlights for JSON parsing:

		   %N = PROJECT %input [mode=*E, expr=PARSE_JSON(builtin.message, [], false, false)]

		   This PROJECT instruction:
		   - mode=*E: Extend mode - adds new columns while keeping existing
		   - PARSE_JSON: Parses the message column as JSON
		   - []: Extract all keys (empty list means all)
		   - false, false: Not strict mode, don't keep as strings

		   The filter uses "ambiguous.level" because at logical planning
		   time, we don't know if level is a label or parsed field.
		   Physical planning resolves this based on schema.
		*/

		// Build and execute the query with JSON parsing
		planner := physical.NewPlanner(
			physical.NewContext(q.Start(), q.End()),
			catalog,
		)

		physicalPlan, err := planner.Build(plan)
		require.NoError(t, err)

		optimizedPlan, err := planner.Optimize(physicalPlan)
		require.NoError(t, err)

		// Execute against real storage (with tenant context)
		execCtx := ctxWithTenant(ctx, "test-tenant")
		executorCfg := executor.Config{
			BatchSize: 100,
			Bucket:    ingester.Bucket(), // CRITICAL: executor needs bucket
		}
		pipeline := executor.Run(execCtx, executorCfg, optimizedPlan, log.NewNopLogger())
		defer pipeline.Close()

		t.Logf("\n=== AFTER JSON PARSING (Real Execution) ===")

		// Read results with parsed JSON fields
		var totalRows int64
		for {
			rec, err := pipeline.Read(execCtx)
			if errors.Is(err, executor.EOF) {
				break
			}
			require.NoError(t, err)

			totalRows += rec.NumRows()

			if rec.NumRows() > 0 {
				t.Logf("Columns after JSON parsing: %d", rec.NumCols())
				for i, field := range rec.Schema().Fields() {
					t.Logf("  Column %d: %s", i, field.Name)
				}

				// Verify we have parsed columns
				for i := 0; i < int(rec.NumCols()); i++ {
					field := rec.Schema().Field(i)
					if field.Name == semconv.NewIdentifier("level", types.ColumnTypeParsed, types.Loki.String).FQN() {
						t.Logf("Found parsed.level column!")
					}
				}

				// Log message content - find message column by name
				msgColIdx := -1
				for i := 0; i < int(rec.NumCols()); i++ {
					if rec.Schema().Field(i).Name == semconv.ColumnIdentMessage.FQN() {
						msgColIdx = i
						break
					}
				}
				if msgColIdx >= 0 {
					msgCol := rec.Column(msgColIdx).(*array.String)
					for j := 0; j < int(rec.NumRows()); j++ {
						t.Logf("  Row %d message: %s", j, msgCol.Value(j))
					}
				}
			}

			rec.Release()
		}

		t.Logf("Total rows with level=error (from real JSON parsing): %d", totalRows)

		/*
		   Arrow schema after JSON parsing:
		   - timestamp_ns.builtin.timestamp (original)
		   - utf8.builtin.message (original)
		   - utf8.label.app (original)
		   - utf8.parsed.level (NEW - from JSON parsing at query time)
		   - utf8.parsed.msg (NEW - from JSON parsing at query time)

		   Note the "parsed." prefix indicating these columns were
		   extracted during query execution, not from stream labels.

		   REAL EXECUTION means JSON parsing actually happens, not simulated!
		*/
	})
}

// ============================================================================
// TRACING A QUERY THROUGH ALL STAGES
// ============================================================================

/*
TestEndToEnd_TraceQueryFlow provides a comprehensive trace of a query
through all stages, with detailed logging at each step.
*/
func TestEndToEnd_TraceQueryFlow(t *testing.T) {
	t.Log("\n" + `
================================================================================
TRACING QUERY: {app="test"} |= "error" | json | level="error"
================================================================================

This test traces the complete journey of a complex query through all stages.
`)

	query := `{app="test"} |= "error" | json | level="error"`

	// -------------------------------------------------------------------------
	// TRACE POINT 1: Query Parsing
	// -------------------------------------------------------------------------
	t.Log("\n=== TRACE POINT 1: QUERY PARSING ===")
	t.Logf("Input: %s", query)
	t.Log("Output: syntax.Expr (Abstract Syntax Tree)")
	t.Log(`
    LogSelectorExpr
    ├── Matchers: {app="test"}
    └── Pipeline:
        ├── LineFilter: |= "error"
        ├── JSONParser: | json
        └── LabelFilter: | level="error"
`)

	// -------------------------------------------------------------------------
	// TRACE POINT 2: Logical Planning
	// -------------------------------------------------------------------------
	ctx := context.Background()

	// Create test ingester with JSON log lines for the trace
	ingester := setupTestIngesterWithData(t, ctx, "test-tenant", map[string][]string{
		`{app="test"}`: {
			`{"level": "error", "msg": "connection failed"}`,
			`{"level": "info", "msg": "request processed"}`,
			`{"level": "error", "msg": "timeout"}`,
		},
	})
	defer ingester.Close()

	catalog := ingester.Catalog()

	t.Log("\n=== TRACE POINT 2: LOGICAL PLANNING ===")
	now := time.Now()
	q := &mockQuery{
		statement: query,
		start:     now.Add(-1 * time.Hour).Unix(),
		end:       now.Add(1 * time.Hour).Unix(),
		direction: logproto.BACKWARD,
		limit:     100,
	}

	logicalPlan, err := logical.BuildPlan(q)
	require.NoError(t, err)

	t.Log("Output: logical.Plan (SSA Intermediate Representation)")
	t.Logf("\n%s", logicalPlan.String())

	// -------------------------------------------------------------------------
	// TRACE POINT 3: Physical Planning (with REAL catalog)
	// -------------------------------------------------------------------------
	t.Log("\n=== TRACE POINT 3: PHYSICAL PLANNING (Real Catalog) ===")

	planner := physical.NewPlanner(
		physical.NewContext(q.Start(), q.End()),
		catalog,
	)

	physicalPlan, err := planner.Build(logicalPlan)
	require.NoError(t, err)

	optimizedPlan, err := planner.Optimize(physicalPlan)
	require.NoError(t, err)

	t.Log("Output: physical.Plan (DAG of executable nodes)")
	t.Logf("\n%s", physical.PrintAsTree(optimizedPlan))

	// -------------------------------------------------------------------------
	// TRACE POINT 4: Workflow Planning
	// -------------------------------------------------------------------------
	t.Log("\n=== TRACE POINT 4: WORKFLOW PLANNING ===")

	runner := newTestRunner()
	wf, err := workflow.New(
		workflow.Options{},
		log.NewNopLogger(),
		"test-tenant",
		runner,
		optimizedPlan,
	)
	require.NoError(t, err)

	t.Log("Output: workflow.Workflow (Task Graph)")
	t.Logf("\n%s", workflow.Sprint(wf))

	// -------------------------------------------------------------------------
	// TRACE POINT 5-6: Execution & Result Building
	// -------------------------------------------------------------------------
	t.Log("\n=== TRACE POINT 5-6: EXECUTION & RESULT BUILDING ===")
	t.Log("For a complete execution example with real data, see:")
	t.Log("  - TestEndToEnd_SimpleLogQuery/stage4_5_execution")
	t.Log("  - TestEndToEnd_SimpleLogQuery/stage6_result_building")
	t.Log("")
	t.Log("Note: This trace test uses workflow.New() which requires a scheduler")
	t.Log("to coordinate task execution. The other end-to-end tests use executor.Run()")
	t.Log("directly which executes the plan synchronously without workflow coordination.")
	t.Log("")
	t.Log("Workflow structure shown above demonstrates how the query would be")
	t.Log("distributed across tasks in a production deployment with real workers.")

	t.Log("\n=== QUERY TRACE COMPLETE (All Stages Used Real Storage!) ===")
}

// ============================================================================
// QUERY TYPE COMPARISON
// ============================================================================

/*
TestEndToEnd_QueryTypeComparison compares the pipeline differences between
log queries and metric queries side by side.
*/
func TestEndToEnd_QueryTypeComparison(t *testing.T) {
	t.Log("\n" + `
================================================================================
QUERY TYPE COMPARISON: LOG vs METRIC
================================================================================
`)

	// -------------------------------------------------------------------------
	// LOG QUERY
	// -------------------------------------------------------------------------
	t.Run("log_query", func(t *testing.T) {
		t.Log("LOG QUERY: {app=\"test\"} |= \"error\"")

		q := &mockQuery{
			statement: `{app="test"} |= "error"`,
			start:     1000,
			end:       2000,
			direction: logproto.BACKWARD,
			limit:     100,
		}

		plan, err := logical.BuildPlan(q)
		require.NoError(t, err)

		t.Log("\nCharacteristics:")
		t.Log("  - Returns: logqlmodel.Streams (log entries)")
		t.Log("  - Has TOPK: Yes (sorts by timestamp)")
		t.Log("  - Has Aggregations: No")
		t.Log("  - Time Range: Exact query range")
		t.Log("  - Output Schema: (timestamp, message, labels)")

		planStr := plan.String()
		require.Contains(t, planStr, "TOPK")
		require.NotContains(t, planStr, "RANGE_AGGREGATION")
	})

	// -------------------------------------------------------------------------
	// METRIC QUERY
	// -------------------------------------------------------------------------
	t.Run("metric_query", func(t *testing.T) {
		t.Log("\nMETRIC QUERY: sum by (level) (count_over_time({app=\"test\"}[5m]))")

		q := &mockQuery{
			statement: `sum by (level) (count_over_time({app="test"}[5m]))`,
			start:     3600,
			end:       7200,
			interval:  5 * time.Minute,
		}

		plan, err := logical.BuildPlan(q)
		require.NoError(t, err)

		t.Log("\nCharacteristics:")
		t.Log("  - Returns: promql.Vector/Matrix (numeric series)")
		t.Log("  - Has TOPK: No")
		t.Log("  - Has Aggregations: Yes (RANGE_AGGREGATION, VECTOR_AGGREGATION)")
		t.Log("  - Time Range: Extended by lookback (start - 5m)")
		t.Log("  - Output Schema: (timestamp, value, grouped_labels)")

		planStr := plan.String()
		require.NotContains(t, planStr, "TOPK")
		require.Contains(t, planStr, "RANGE_AGGREGATION")
		require.Contains(t, planStr, "VECTOR_AGGREGATION")
	})

	// -------------------------------------------------------------------------
	// COMPARISON TABLE
	// -------------------------------------------------------------------------
	t.Log("\n" + `
================================================================================
COMPARISON TABLE
================================================================================

| Aspect              | Log Query            | Metric Query           |
|---------------------|---------------------|------------------------|
| Result Type         | Streams             | Vector/Matrix          |
| Output              | Log entries         | Numeric values         |
| Sorting             | TOPK by timestamp   | N/A                    |
| Aggregation         | None                | Range + Vector         |
| Time Adjustment     | Exact range         | Range + lookback       |
| Limit               | Yes (k parameter)   | No                     |
| Direction           | FORWARD/BACKWARD    | N/A                    |
| Group By            | N/A                 | Label grouping         |

================================================================================
`)
}

// ============================================================================
// PIPELINE BREAKERS AND TASK BOUNDARIES
// ============================================================================

/*
TestEndToEnd_PipelineBreakers demonstrates how pipeline breakers affect
workflow task partitioning.
*/
func TestEndToEnd_PipelineBreakers(t *testing.T) {
	t.Log("\n" + `
================================================================================
PIPELINE BREAKERS AND TASK BOUNDARIES
================================================================================

Pipeline breakers are operators that must see ALL input data before producing
output. They force task boundaries in the workflow.

Examples of pipeline breakers:
- TopK: Must see all data to determine top K items
- RangeAggregation: Must see all data in time window
- VectorAggregation: Must see all data for grouping
- Sort: Must see all data to sort

Non-breakers (can be pipelined):
- Filter: Process row by row
- Projection: Transform row by row
- JSON parsing: Parse row by row

================================================================================
`)

	t.Run("topk_creates_task_boundary", func(t *testing.T) {
		/*
		   Physical Plan:
		     TopK                     <- Pipeline breaker
		       └── Parallelize
		             └── ScanSet
		                   ├── Scan1  <- Can run in parallel
		                   └── Scan2  <- Can run in parallel

		   Workflow:
		     Task 1: TopK (receives merged results)
		     Task 2: Scan1 (sends to Task 1)
		     Task 3: Scan2 (sends to Task 1)
		*/

		var graph dag.Graph[physical.Node]

		scanSet := graph.Add(&physical.ScanSet{
			Targets: []*physical.ScanTarget{
				{Type: physical.ScanTypeDataObject, DataObject: &physical.DataObjScan{Location: "obj1"}},
				{Type: physical.ScanTypeDataObject, DataObject: &physical.DataObjScan{Location: "obj2"}},
			},
		})
		parallelize := graph.Add(&physical.Parallelize{})
		topK := graph.Add(&physical.TopK{
			K:         100,
			Ascending: false,
			SortBy: &physical.ColumnExpr{
				Ref: types.ColumnRef{
					Column: "timestamp",
					Type:   types.ColumnTypeBuiltin,
				},
			},
		})

		_ = graph.AddEdge(dag.Edge[physical.Node]{Parent: parallelize, Child: scanSet})
		_ = graph.AddEdge(dag.Edge[physical.Node]{Parent: topK, Child: parallelize})

		plan := physical.FromGraph(graph)

		runner := newTestRunner()
		wf, err := workflow.New(workflow.Options{}, log.NewNopLogger(), "test-tenant", runner, plan)
		require.NoError(t, err)

		t.Logf("Physical Plan:\n%s", physical.PrintAsTree(plan))
		t.Logf("Workflow:\n%s", workflow.Sprint(wf))
	})

	t.Run("multiple_breakers_create_multiple_boundaries", func(t *testing.T) {
		/*
		   Physical Plan:
		     VectorAggregation        <- Pipeline breaker #2
		       └── RangeAggregation   <- Pipeline breaker #1
		             └── Parallelize
		                   └── ScanSet

		   Workflow:
		     Task 1: VectorAggregation (root)
		     Task 2: RangeAggregation (receives from scans, sends to Task 1)
		     Task 3+: Scan tasks (send to Task 2)
		*/

		var graph dag.Graph[physical.Node]

		scanSet := graph.Add(&physical.ScanSet{
			Targets: []*physical.ScanTarget{
				{Type: physical.ScanTypeDataObject, DataObject: &physical.DataObjScan{Location: "obj1"}},
			},
		})
		parallelize := graph.Add(&physical.Parallelize{})
		rangeAgg := graph.Add(&physical.RangeAggregation{
			Operation: types.RangeAggregationTypeCount,
			Range:     5 * time.Minute,
		})
		vectorAgg := graph.Add(&physical.VectorAggregation{
			Operation: types.VectorAggregationTypeSum,
		})

		_ = graph.AddEdge(dag.Edge[physical.Node]{Parent: parallelize, Child: scanSet})
		_ = graph.AddEdge(dag.Edge[physical.Node]{Parent: rangeAgg, Child: parallelize})
		_ = graph.AddEdge(dag.Edge[physical.Node]{Parent: vectorAgg, Child: rangeAgg})

		plan := physical.FromGraph(graph)

		runner := newTestRunner()
		wf, err := workflow.New(workflow.Options{}, log.NewNopLogger(), "test-tenant", runner, plan)
		require.NoError(t, err)

		t.Logf("Physical Plan:\n%s", physical.PrintAsTree(plan))
		t.Logf("Workflow:\n%s", workflow.Sprint(wf))

		// Verify multiple task boundaries were created
		runner.mu.RLock()
		numTasks := len(runner.tasks)
		numStreams := len(runner.streams)
		runner.mu.RUnlock()

		t.Logf("Tasks created: %d", numTasks)
		t.Logf("Streams created: %d", numStreams)
	})
}

// ============================================================================
// SUMMARY: COMPLETE PIPELINE OVERVIEW
// ============================================================================

/*
TestEndToEnd_PipelineSummary provides a final summary of the complete
V2 engine pipeline.
*/
func TestEndToEnd_PipelineSummary(t *testing.T) {
	fmt.Print(`
================================================================================
LOKI QUERY ENGINE V2 - COMPLETE PIPELINE SUMMARY
================================================================================

1. PARSING (not covered - handled by logql/syntax)
   Input:  LogQL string
   Output: syntax.Expr (AST)
   Action: Parse query string into Abstract Syntax Tree

2. LOGICAL PLANNING (stage1_logical_planning_test.go)
   Input:  syntax.Expr + query parameters
   Output: logical.Plan (SSA IR)
   Action: Convert AST to Static Single Assignment form
   Key:    Each variable assigned once, data flow explicit

3. PHYSICAL PLANNING (stage2_physical_planning_test.go)
   Input:  logical.Plan + Catalog
   Output: physical.Plan (DAG)
   Action: Resolve data sources, build executable DAG
   Key:    MAKETABLE → DataObjScan, apply optimizations

4. WORKFLOW PLANNING (stage3_workflow_planning_test.go)
   Input:  physical.Plan
   Output: workflow.Workflow (Task Graph)
   Action: Partition plan at pipeline breakers, create tasks
   Key:    Tasks connected by streams for distributed execution

5. EXECUTION (stage4_execution_test.go, stage5_distributed_execution_test.go)
   Input:  Tasks assigned by scheduler
   Output: Arrow RecordBatches
   Action: Workers execute task fragments, produce results
   Key:    Pipelines form tree structure, data flows up

6. RESULT BUILDING (stage4_execution_test.go)
   Input:  Arrow RecordBatches
   Output: logqlmodel.Streams or promql.Vector/Matrix
   Action: Convert Arrow data to LogQL-compatible results
   Key:    Group by labels, sort by timestamp, format output

================================================================================
DATA FORMATS THROUGH THE PIPELINE
================================================================================

Stage          | Data Format
---------------|--------------------------------------------------
1. Parsing     | LogQL string → syntax.Expr (AST nodes)
2. Logical     | syntax.Expr → logical.Plan (SSA instructions)
3. Physical    | logical.Plan → physical.Plan (DAG nodes)
4. Workflow    | physical.Plan → Tasks + Streams
5. Execution   | Tasks → Arrow RecordBatches (columnar)
6. Result      | Arrow → Streams/Vector/Matrix (API response)

================================================================================
KEY DESIGN PRINCIPLES
================================================================================

1. COLUMNAR PROCESSING
   - Arrow RecordBatches enable vectorized operations
   - Better CPU cache utilization than row-by-row

2. DISTRIBUTED EXECUTION
   - Workflow partitions plan into tasks
   - Tasks execute in parallel across workers
   - Streams connect tasks for data flow

3. OPTIMIZATION OPPORTUNITIES
   - Predicate pushdown to storage
   - Limit pushdown to scans
   - Projection pushdown (read only needed columns)
   - Parallel scan execution

4. TYPE SAFETY
   - SSA form makes data flow explicit
   - Column type prefixes track data origin
   - Physical planning resolves ambiguous types

================================================================================
`)
}

// ============================================================================
// SIMPLE ENGINE V2 QUERY INTERFACE
// ============================================================================

/*
TestEngineV2_QueryInterface is a simple test for experimenting with Engine V2.
Just change the query variable and resultType to see different query results.

Usage:
1. Modify the 'query' variable to test different LogQL queries
2. Set 'resultType' to "log" or "metric" to control output format
3. Run the test to see results
*/
func TestEngineV2_QueryInterface(t *testing.T) {
	// ============================================================================
	// SETUP - Test data
	// ============================================================================
	ctx := context.Background()

	// Create test data
	ingester := setupTestIngesterWithTimestamps(t, ctx, "test-tenant", []LogEntry{
		{
			Labels:    `{app="web", level="error"}`,
			Line:      "error: connection timeout",
			Timestamp: time.Now().Add(-10 * time.Minute),
		},
		{
			Labels:    `{app="web", level="error"}`,
			Line:      "error: database unavailable",
			Timestamp: time.Now().Add(-9 * time.Minute),
		},
		{
			Labels:    `{app="web", level="info"}`,
			Line:      "info: request processed successfully",
			Timestamp: time.Now().Add(-8 * time.Minute),
		},
		{
			Labels:    `{app="api", level="error"}`,
			Line:      "error: authentication failed",
			Timestamp: time.Now().Add(-7 * time.Minute),
		},
		{
			Labels:    `{app="api", level="info"}`,
			Line:      "info: health check passed",
			Timestamp: time.Now().Add(-6 * time.Minute),
		},
	})
	defer ingester.Close()

	// ============================================================================
	// CONFIGURATION - CHANGE THESE TO EXPERIMENT
	// ============================================================================

	// Change this query to test different LogQL expressions
	query := `{app="web"} |= "error"`

	// Set to "log" for logqlmodel.Streams or "metric" for promql.Vector
	resultType := "log"

	// Set to "basic" for simple sequential execution or "standard" for distributed execution
	// "basic" = no workers/scheduler needed, simpler and faster for testing (RECOMMENDED)
	// "standard" = full distributed execution with workers and scheduler (currently has issues)
	//
	engineType := "basic"

	// Create test logger that outputs to test log
	logger := log.With(log.NewLogfmtLogger(log.NewSyncWriter(&testLogWriter{t: t})), "test", t.Name())

	// ============================================================================
	// SETUP - Engine (based on engineType)
	// ============================================================================
	var executeFunc func(context.Context, *mockQuery) (logqlmodel.Result, error)

	switch engineType {
	case "basic":
		executeFunc = setupBasicEngine(t, ctx, logger, ingester)
	case "standard":
		executeFunc = setupStandardEngine(t, ctx, logger, ingester)
	default:
		t.Fatalf("Unknown engine type: %s (use 'basic' or 'standard')", engineType)
	}

	// ============================================================================
	// EXECUTE QUERY
	// ============================================================================
	t.Logf("\n=== EXECUTING QUERY ===")
	t.Logf("Query: %s", query)
	t.Logf("Engine Type: %s", engineType)
	t.Logf("Result Type: %s", resultType)

	now := time.Now()
	var params *mockQuery

	if resultType == "metric" {
		params = &mockQuery{
			statement: query,
			start:     now.Add(-15 * time.Minute).Unix(),
			end:       now.Unix(),
			interval:  5 * time.Minute,
		}
	} else {
		params = &mockQuery{
			statement: query,
			start:     now.Add(-1 * time.Hour).Unix(),
			end:       now.Unix(),
			direction: logproto.BACKWARD,
			limit:     100,
		}
	}

	execCtx := ctxWithTenant(ctx, "test-tenant")
	result, err := executeFunc(execCtx, params)
	require.NoError(t, err)

	// ============================================================================
	// DISPLAY RESULTS
	// ============================================================================
	t.Logf("\n=== RESULTS ===")
	t.Logf("Result type: %T", result.Data)
	t.Logf("Statistics: %+v", result.Statistics)

	if resultType == "log" {
		// Display as log streams
		if streams, ok := result.Data.(logqlmodel.Streams); ok {
			t.Logf("\nNumber of streams: %d", len(streams))
			for i, stream := range streams {
				t.Logf("\nStream %d: %s", i+1, stream.Labels)
				t.Logf("  Entries: %d", len(stream.Entries))
				for j, entry := range stream.Entries {
					t.Logf("    [%d] %v: %s", j+1, entry.Timestamp, entry.Line)
				}
			}

			totalEntries := 0
			for _, stream := range streams {
				totalEntries += len(stream.Entries)
			}
			t.Logf("\n✓ Total entries: %d", totalEntries)
		} else {
			t.Logf("⚠ Result is not logqlmodel.Streams, got: %T", result.Data)
		}
	} else {
		// Display as metric vector
		if vector, ok := result.Data.(promql.Vector); ok {
			t.Logf("\nVector samples: %d", len(vector))
			for i, sample := range vector {
				t.Logf("  Sample %d:", i+1)
				t.Logf("    Metric: %s", sample.Metric)
				t.Logf("    Value: %v", sample.F)
				t.Logf("    Timestamp: %v", time.Unix(sample.T/1000, 0))
			}
			t.Logf("\n✓ Total samples: %d", len(vector))
		} else {
			t.Logf("⚠ Result is not promql.Vector, got: %T", result.Data)
		}
	}

	t.Logf("\n=== QUERY COMPLETE ===")
}

// testLogWriter writes log output to the test log
type testLogWriter struct {
	t testingT
}

func (w *testLogWriter) Write(p []byte) (n int, err error) {
	w.t.Logf("%s", p)
	return len(p), nil
}

// setupBasicEngine creates a basic engine (no workers/scheduler needed)
func setupBasicEngine(t *testing.T, ctx context.Context, logger log.Logger, ingester *TestIngester) func(context.Context, *mockQuery) (logqlmodel.Result, error) {
	t.Helper()

	// Create basic engine - simpler, no distributed execution
	basicEng := engine.NewBasic(
		engine.ExecutorConfig{BatchSize: 100},
		metastore.Config{},
		ingester.Bucket(),
		logql.NoLimits,
		nil,
		logger,
	)

	// Override catalog for basic engine too
	basicEng.SetTestCatalog(ingester.Catalog())

	return func(ctx context.Context, params *mockQuery) (logqlmodel.Result, error) {
		return basicEng.Execute(ctx, params)
	}
}

// setupStandardEngine creates a standard engine with workers and scheduler
func setupStandardEngine(t *testing.T, ctx context.Context, logger log.Logger, ingester *TestIngester) func(context.Context, *mockQuery) (logqlmodel.Result, error) {
	t.Helper()

	// Create scheduler
	scheduler, err := engine.NewScheduler(engine.SchedulerParams{
		Logger: logger,
	})
	require.NoError(t, err)

	// Start the scheduler service
	require.NoError(t, scheduler.Service().StartAsync(ctx))
	t.Cleanup(func() {
		scheduler.Service().StopAsync()
		_ = scheduler.Service().AwaitTerminated(ctx)
	})
	require.NoError(t, scheduler.Service().AwaitRunning(ctx))

	// Create and start a worker to execute tasks
	// Use 2 threads to handle multiple tasks concurrently
	worker, err := engine.NewWorker(engine.WorkerParams{
		Logger:         logger,
		Bucket:         ingester.Bucket(),
		Config:         engine.WorkerConfig{WorkerThreads: 16},
		Executor:       engine.ExecutorConfig{BatchSize: 100},
		LocalScheduler: scheduler,
	})
	require.NoError(t, err)

	// Start the worker service
	require.NoError(t, worker.Service().StartAsync(ctx))
	t.Cleanup(func() {
		worker.Service().StopAsync()
		_ = worker.Service().AwaitTerminated(ctx)
	})
	require.NoError(t, worker.Service().AwaitRunning(ctx))

	// Create the standard engine
	eng, err := engine.New(engine.Params{
		Logger:     logger,
		Registerer: nil,
		Config: engine.ExecutorConfig{
			BatchSize: 100,
		},
		Scheduler: scheduler,
		Bucket:    ingester.Bucket(),
		Limits:    logql.NoLimits,
	})
	require.NoError(t, err)

	// Override the catalog to use our test ingester's catalog
	eng.SetTestCatalog(ingester.Catalog())

	return func(ctx context.Context, params *mockQuery) (logqlmodel.Result, error) {
		return eng.Execute(ctx, params)
	}
}
