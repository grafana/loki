package engine_lab

/*
============================================================================
LOKI QUERY ENGINE V2 - STAGE 2: PHYSICAL PLANNING
============================================================================

This file covers Stage 2 of query processing: Physical Planning.
Physical planning converts the logical SSA plan into an executable DAG.

============================================================================
STAGE OVERVIEW
============================================================================

Input:  logical.Plan (SSA form)
Output: physical.Plan (DAG of executable nodes)

Physical planning performs several key transformations:

1. DATA SOURCE RESOLUTION
   - MAKETABLE → DataObjScan/ScanSet nodes
   - The catalog resolves stream selectors to actual data object locations
   - Each data object section becomes a scan node

2. NODE TYPE CONVERSION
   - SSA instructions → Physical plan nodes
   - SELECT → Filter nodes
   - PROJECT → Projection nodes
   - TOPK → TopK nodes
   - RANGE_AGGREGATION → RangeAggregation nodes
   - VECTOR_AGGREGATION → VectorAggregation nodes

3. GRAPH STRUCTURE
   - Physical plan is a DAG (Directed Acyclic Graph), not a linear list
   - Supports multiple inputs (for joins, binary operations)
   - Parent nodes consume output from child nodes
   - Data flows from leaves (scans) up to root

============================================================================
PHYSICAL PLAN NODE TYPES
============================================================================

DATA SOURCE NODES:
  - DataObjScan: Reads from a single data object section
    Fields: Location, Section, StreamIDs, MaxTimeRange, RowFilter, etc.

  - ScanSet: Collection of scan targets for parallel execution
    Fields: Targets (list of ScanTarget), Predicates, etc.

PROCESSING NODES:
  - Filter: Filters rows based on predicates
    Fields: Predicates (list of expressions)

  - Projection: Transforms/adds/removes columns
    Fields: Columns (list of column expressions)

  - TopK: Sorts and limits rows
    Fields: SortColumn, K, Ascending, NullsFirst

  - Limit: Skip and fetch rows
    Fields: Skip, Fetch

AGGREGATION NODES:
  - RangeAggregation: Aggregates over time windows
    Fields: Operation, Start, End, Range, Step, PartitionBy
    Operations: count, sum, avg, min, max, first, last, bytes_over_time, etc.

  - VectorAggregation: Aggregates across label groups
    Fields: Operation, GroupBy, Without
    Operations: sum, avg, min, max, count, topk, bottomk, etc.

STRUCTURAL NODES:
  - Parallelize: Hint for parallel execution
    Marks where the plan can be parallelized

  - ColumnCompat: Column type compatibility conversion
    Resolves ambiguous column types (label vs metadata vs parsed)

  - Join: Joins multiple inputs (for binary operations)
    Fields: JoinType, LeftOn, RightOn

============================================================================
OPTIMIZATION PASSES
============================================================================

After building the physical plan, several optimization passes are applied:

1. PREDICATE PUSHDOWN
   - Pushes filter predicates as close to data sources as possible
   - Reduces data read from storage
   - Before: Scan → Filter → ...
   - After:  Scan[with predicates] → ...

2. LIMIT PUSHDOWN
   - Pushes limits to scans when safe
   - Reduces data read and processed
   - Only safe when no aggregations or grouping between scan and limit

3. PROJECTION PUSHDOWN
   - Pushes column selections to scans
   - Only reads necessary columns from storage
   - Reduces I/O and memory usage

4. PARALLEL PUSHDOWN
   - Inserts Parallelize hints at appropriate points
   - Enables distributed execution of independent scan operations

============================================================================
CATALOG INTERFACE
============================================================================

The Catalog resolves logical stream selectors to physical data locations:

    type Catalog interface {
        ResolveShardDescriptors(expr Expression, from, through time.Time)
            ([]FilteredShardDescriptor, error)
    }

FilteredShardDescriptor contains:
  - Location: Object storage path (e.g., "bucket/tenant/obj1")
  - Streams: List of stream IDs matching the selector
  - Sections: Index sections to scan
  - TimeRange: Time range covered by this shard

In production, the catalog queries the metastore to find data objects.
In tests, we use mockCatalog with predefined data.

============================================================================
*/

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/logical"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
	"github.com/grafana/loki/v3/pkg/logproto"
)

// ============================================================================
// BASIC PHYSICAL PLANNING TESTS
// ============================================================================

/*
TestPhysicalPlanning demonstrates conversion from logical to physical plans.

This test suite shows how the physical planner:
1. Converts SSA instructions to DAG nodes
2. Resolves data sources via the catalog
3. Applies optimization passes
*/
func TestPhysicalPlanning(t *testing.T) {
	t.Run("log query physical plan with multiple data objects", func(t *testing.T) {
		/*
		   ============================================================================
		   TEST: Physical Plan with Multiple Data Objects
		   ============================================================================

		   This test demonstrates how a simple log query is transformed from
		   logical to physical plan, with multiple data objects from the catalog.

		   Logical Plan:
		   -------------
		     MAKETABLE → SELECT → SELECT → TOPK → LOGQL_COMPAT → RETURN

		   Physical Plan (after transformation):
		   -------------------------------------
		     Parallelize
		       └── TopK
		             └── Filter (time range)
		                   └── ColumnCompat
		                         └── ScanSet
		                               ├── DataObjScan [obj1]
		                               └── DataObjScan [obj2]

		   Key Transformations:
		   --------------------
		   1. MAKETABLE resolves to ScanSet via catalog
		   2. ScanSet contains multiple DataObjScan targets
		   3. SELECT nodes become Filter nodes
		   4. TOPK becomes TopK node
		   5. Parallelize is inserted as optimization hint
		*/
		ctx := context.Background()

		// Create test ingester with real data across multiple streams
		ingester := setupTestIngesterWithData(t, ctx, "test-tenant", map[string][]string{
			`{app="test", env="prod"}`: {
				"log line from prod 1",
				"log line from prod 2",
			},
			`{app="test", env="dev"}`: {
				"log line from dev 1",
				"log line from dev 2",
			},
		})
		defer ingester.Close()

		// Get real catalog from ingester
		catalog := ingester.Catalog()

		// Create logical plan
		now := time.Now()
		q := &mockQuery{
			statement: `{app="test"}`,
			start:     now.Add(-1 * time.Hour).Unix(),
			end:       now.Add(1 * time.Hour).Unix(),
			direction: logproto.BACKWARD,
			limit:     100,
		}

		logicalPlan, err := logical.BuildPlan(q)
		require.NoError(t, err)

		t.Logf("Logical Plan:\n%s", logicalPlan.String())

		// Build physical plan with REAL catalog
		planner := physical.NewPlanner(
			physical.NewContext(q.Start(), q.End()),
			catalog,
		)

		physicalPlan, err := planner.Build(logicalPlan)
		require.NoError(t, err)
		require.NotNil(t, physicalPlan)

		t.Logf("Physical Plan (before optimization):\n%s", physical.PrintAsTree(physicalPlan))

		// Verify structure
		root, err := physicalPlan.Root()
		require.NoError(t, err)
		require.NotNil(t, root, "physical plan should have a root")

		// Apply optimizations
		optimizedPlan, err := planner.Optimize(physicalPlan)
		require.NoError(t, err)

		t.Logf("Physical Plan (after optimization):\n%s", physical.PrintAsTree(optimizedPlan))

		// Verify the catalog resolved to real data objects
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

	t.Run("physical plan with filter pushdown", func(t *testing.T) {
		/*
		   ============================================================================
		   TEST: Filter Pushdown Optimization
		   ============================================================================

		   This test demonstrates the predicate pushdown optimization pass.

		   LogQL: {app="test"} |= "error"

		   Before Optimization:
		   --------------------
		     TopK
		       └── Filter [predicate: message contains "error"]
		             └── Filter [predicate: timestamp range]
		                   └── ScanSet

		   After Optimization (Predicate Pushdown):
		   -----------------------------------------
		     TopK
		       └── ScanSet [predicates pushed down: time range, "error"]

		   Benefits of Pushdown:
		   ---------------------
		   1. Less data read from storage (bloom filters, indexes)
		   2. Fewer rows processed by operators
		   3. Better cache utilization
		*/
		ctx := context.Background()

		// Create test ingester with real data containing "error" lines
		ingester := setupTestIngesterWithData(t, ctx, "test-tenant", map[string][]string{
			`{app="test"}`: {
				"error: connection failed",
				"info: request processed",
				"error: timeout occurred",
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

		// Before optimization - filters are separate nodes
		t.Logf("Before optimization:\n%s", physical.PrintAsTree(physicalPlan))

		// After optimization - filters pushed into scan
		optimizedPlan, err := planner.Optimize(physicalPlan)
		require.NoError(t, err)

		t.Logf("After optimization:\n%s", physical.PrintAsTree(optimizedPlan))

		// Verify predicates were pushed down to scan nodes
		var foundPredicatesInScan bool
		root, _ := optimizedPlan.Root()
		err = optimizedPlan.DFSWalk(root, func(n physical.Node) error {
			if scanSet, ok := n.(*physical.ScanSet); ok {
				if len(scanSet.Predicates) > 0 {
					foundPredicatesInScan = true
					t.Logf("Found %d predicates pushed to ScanSet", len(scanSet.Predicates))
				}
			}
			return nil
		}, dag.PreOrderWalk)
		require.NoError(t, err)
		require.True(t, foundPredicatesInScan, "predicates should be pushed down to scan")
	})

	t.Run("metric query physical plan", func(t *testing.T) {
		/*
		   ============================================================================
		   TEST: Metric Query Physical Plan
		   ============================================================================

		   LogQL: sum by (level) (count_over_time({app="test"}[5m]))

		   Logical Plan:
		   -------------
		     MAKETABLE → SELECT → SELECT → RANGE_AGGREGATION → VECTOR_AGGREGATION

		   Physical Plan:
		   --------------
		     VectorAggregation [operation=sum, group_by=(level)]
		       └── RangeAggregation [operation=count, range=5m]
		             └── Parallelize
		                   └── ScanSet

		   Node Responsibilities:
		   ----------------------
		   1. ScanSet: Read log entries from storage
		   2. RangeAggregation: Count entries per time window (5 min)
		   3. VectorAggregation: Sum counts grouped by level label
		*/
		ctx := context.Background()

		// Create test ingester with real data across different levels
		ingester := setupTestIngesterMultiStream(t, ctx, "test-tenant", []struct {
			labels string
			lines  []string
		}{
			{`{app="test", level="error"}`, []string{"error log 1", "error log 2"}},
			{`{app="test", level="info"}`, []string{"info log 1", "info log 2", "info log 3"}},
			{`{app="test", level="warn"}`, []string{"warn log 1"}},
		})
		defer ingester.Close()

		catalog := ingester.Catalog()

		now := time.Now()
		q := &mockQuery{
			statement: `sum by (level) (count_over_time({app="test"}[5m]))`,
			start:     now.Add(-10 * time.Minute).Unix(),
			end:       now.Add(10 * time.Minute).Unix(),
			interval:  5 * time.Minute,
		}

		logicalPlan, err := logical.BuildPlan(q)
		require.NoError(t, err)

		t.Logf("Logical Plan:\n%s", logicalPlan.String())

		planner := physical.NewPlanner(
			physical.NewContext(q.Start(), q.End()),
			catalog,
		)

		physicalPlan, err := planner.Build(logicalPlan)
		require.NoError(t, err)

		t.Logf("Physical Plan:\n%s", physical.PrintAsTree(physicalPlan))

		// Verify aggregation nodes are present
		var hasRangeAgg, hasVectorAgg bool
		root, _ := physicalPlan.Root()
		err = physicalPlan.DFSWalk(root, func(n physical.Node) error {
			switch n.Type() {
			case physical.NodeTypeRangeAggregation:
				hasRangeAgg = true
				t.Logf("Found RangeAggregation node")
			case physical.NodeTypeVectorAggregation:
				hasVectorAgg = true
				t.Logf("Found VectorAggregation node")
			}
			return nil
		}, dag.PreOrderWalk)
		require.NoError(t, err)
		require.True(t, hasRangeAgg, "should have RangeAggregation node")
		require.True(t, hasVectorAgg, "should have VectorAggregation node")
	})
}

// ============================================================================
// PHYSICAL PLAN NODE TYPES TESTS
// ============================================================================

/*
TestPhysicalPlanNodeTypes demonstrates the various physical plan node types.

Each test creates nodes manually to show their structure and fields.
*/
func TestPhysicalPlanNodeTypes(t *testing.T) {
	t.Run("DataObjScan structure", func(t *testing.T) {
		/*
		   ============================================================================
		   NODE TYPE: DataObjScan
		   ============================================================================

		   DataObjScan reads data from a single data object section in storage.

		   Structure:
		   ----------
		   type DataObjScan struct {
		       Location     DataObjLocation  // Object storage path
		       Section      int              // Section index within object
		       StreamIDs    []int64          // Stream IDs to read
		       MaxTimeRange TimeRange        // Time range covered
		       RowFilter    Expression       // Optional row-level filter
		       Columns      []ColumnExpr     // Columns to project
		   }

		   The DataObjScan is the leaf node in physical plans - it's where
		   data enters the execution pipeline.
		*/
		scan := &physical.DataObjScan{
			Location:  "bucket/tenant/dataobj-2024-01-01-001",
			Section:   0,
			StreamIDs: []int64{1, 2, 3, 4, 5},
			MaxTimeRange: physical.TimeRange{
				Start: time.Now(),
				End:   time.Now().Add(time.Hour),
			},
		}

		require.Equal(t, physical.NodeTypeDataObjScan, scan.Type())
		require.Equal(t, physical.DataObjLocation("bucket/tenant/dataobj-2024-01-01-001"), scan.Location)
		require.Len(t, scan.StreamIDs, 5)

		t.Logf("DataObjScan: location=%s, section=%d, streams=%v",
			scan.Location, scan.Section, scan.StreamIDs)
	})

	t.Run("ScanSet with multiple targets", func(t *testing.T) {
		/*
		   ============================================================================
		   NODE TYPE: ScanSet
		   ============================================================================

		   ScanSet is a collection of scan targets for parallel execution.
		   When multiple data objects match a query, they're grouped into a ScanSet.

		   Structure:
		   ----------
		   type ScanSet struct {
		       Targets    []*ScanTarget     // Individual scan targets
		       Predicates []Expression      // Shared predicates
		       Columns    []ColumnExpr      // Shared column projections
		   }

		   type ScanTarget struct {
		       Type       ScanType          // DataObject or other source
		       DataObject *DataObjScan      // The actual scan spec
		   }

		   Parallelization:
		   ----------------
		   Each ScanTarget can be executed in parallel. The workflow planner
		   creates individual tasks for each target.
		*/
		scanSet := &physical.ScanSet{
			Targets: []*physical.ScanTarget{
				{
					Type: physical.ScanTypeDataObject,
					DataObject: &physical.DataObjScan{
						Location:  "bucket/tenant/obj1",
						Section:   0,
						StreamIDs: []int64{1, 2, 3},
						MaxTimeRange: physical.TimeRange{
							Start: time.Now(),
							End:   time.Now().Add(time.Hour),
						},
					},
				},
				{
					Type: physical.ScanTypeDataObject,
					DataObject: &physical.DataObjScan{
						Location:  "bucket/tenant/obj2",
						Section:   0,
						StreamIDs: []int64{4, 5, 6},
						MaxTimeRange: physical.TimeRange{
							Start: time.Now(),
							End:   time.Now().Add(time.Hour),
						},
					},
				},
			},
		}

		require.Equal(t, physical.NodeTypeScanSet, scanSet.Type())
		require.Len(t, scanSet.Targets, 2, "should have two scan targets")

		for i, target := range scanSet.Targets {
			require.Equal(t, physical.ScanTypeDataObject, target.Type)
			require.NotNil(t, target.DataObject)
			t.Logf("Target %d: location=%s, section=%d, streams=%v",
				i, target.DataObject.Location, target.DataObject.Section, target.DataObject.StreamIDs)
		}
	})

	t.Run("Filter node structure", func(t *testing.T) {
		/*
		   ============================================================================
		   NODE TYPE: Filter
		   ============================================================================

		   Filter applies predicates to filter rows from its input.

		   Structure:
		   ----------
		   type Filter struct {
		       Predicates []Expression    // Conditions to apply (ANDed)
		   }

		   Filter nodes are inserted by the physical planner for:
		   - Time range filters (GTE/LT on timestamp)
		   - Line filters (MATCH_STR, NOT_MATCH_STR, MATCH_RE)
		   - Label filters (EQ, NEQ on parsed/label columns)

		   After optimization, many filters are pushed down to ScanSet.
		*/
		filter := &physical.Filter{
			Predicates: []physical.Expression{
				&physical.BinaryExpr{
					Left: &physical.ColumnExpr{
						Ref: types.ColumnRef{
							Column: "level",
							Type:   types.ColumnTypeAmbiguous,
						},
					},
					Op:    types.BinaryOpEq,
					Right: physical.NewLiteral("error"),
				},
			},
		}

		require.Equal(t, physical.NodeTypeFilter, filter.Type())
		require.Len(t, filter.Predicates, 1)

		t.Logf("Filter node with %d predicates", len(filter.Predicates))
	})

	t.Run("RangeAggregation node structure", func(t *testing.T) {
		/*
		   ============================================================================
		   NODE TYPE: RangeAggregation
		   ============================================================================

		   RangeAggregation aggregates data over time windows.
		   This is the core of LogQL's *_over_time functions.

		   Structure:
		   ----------
		   type RangeAggregation struct {
		       Operation   RangeAggregationType  // count, sum, avg, etc.
		       Start       time.Time             // Query start time
		       End         time.Time             // Query end time
		       Range       time.Duration         // Window size (e.g., 5m)
		       Step        time.Duration         // Evaluation interval
		       PartitionBy []ColumnExpression    // Grouping columns
		   }

		   Supported Operations:
		   ---------------------
		   - count:          Count log lines in window
		   - sum:            Sum values (for numeric extraction)
		   - avg:            Average values
		   - min/max:        Min/max values
		   - first/last:     First/last values in window
		   - bytes_over_time: Sum of log line bytes
		   - rate:           count / range_seconds

		   Windowing:
		   ----------
		   For each evaluation point at time T, the aggregation considers
		   all data in the range [T - range, T).

		   Example: count_over_time(...[5m]) at T=10:00
		   Window: [09:55, 10:00) - 5 minute window ending at 10:00
		*/
		now := time.Now()
		rangeAgg := &physical.RangeAggregation{
			Operation: types.RangeAggregationTypeCount,
			Start:     now,
			End:       now.Add(time.Hour),
			Range:     5 * time.Minute,
			Step:      time.Minute,
			PartitionBy: []physical.ColumnExpression{
				&physical.ColumnExpr{
					Ref: types.ColumnRef{
						Column: "level",
						Type:   types.ColumnTypeAmbiguous,
					},
				},
			},
		}

		require.Equal(t, physical.NodeTypeRangeAggregation, rangeAgg.Type())
		require.Equal(t, types.RangeAggregationTypeCount, rangeAgg.Operation)
		require.Equal(t, 5*time.Minute, rangeAgg.Range)
		require.Equal(t, time.Minute, rangeAgg.Step)

		t.Logf("RangeAggregation: op=%v, range=%v, step=%v",
			rangeAgg.Operation, rangeAgg.Range, rangeAgg.Step)
	})

	t.Run("VectorAggregation node structure", func(t *testing.T) {
		/*
		   ============================================================================
		   NODE TYPE: VectorAggregation
		   ============================================================================

		   VectorAggregation aggregates across series (label groups).
		   This implements LogQL's vector aggregation operators.

		   Structure:
		   ----------
		   type VectorAggregation struct {
		       Operation  VectorAggregationType  // sum, avg, count, etc.
		       GroupBy    []ColumnExpression     // "by (...)" labels
		       Without    bool                   // "without (...)" mode
		       Parameter  *float64               // For topk/bottomk (k value)
		   }

		   Supported Operations:
		   ---------------------
		   - sum:     Sum values across series
		   - avg:     Average across series
		   - count:   Count number of series
		   - min/max: Minimum/maximum across series
		   - topk:    Top k series by value
		   - bottomk: Bottom k series by value
		   - stddev:  Standard deviation
		   - stdvar:  Variance

		   Grouping Modes:
		   ---------------
		   - by (label1, label2): Group by specified labels only
		   - without (label1): Group by all labels except specified

		   Example: sum by (level) (count_over_time(...))
		   Groups series by "level" label and sums their values.
		   Series {level="error"} and {level="error", app="x"} would be
		   grouped together into {level="error"}.
		*/
		vectorAgg := &physical.VectorAggregation{
			Operation: types.VectorAggregationTypeSum,
			GroupBy: []physical.ColumnExpression{
				&physical.ColumnExpr{
					Ref: types.ColumnRef{
						Column: "level",
						Type:   types.ColumnTypeAmbiguous,
					},
				},
			},
		}

		require.Equal(t, physical.NodeTypeVectorAggregation, vectorAgg.Type())
		require.Equal(t, types.VectorAggregationTypeSum, vectorAgg.Operation)
		require.Len(t, vectorAgg.GroupBy, 1)

		t.Logf("VectorAggregation: op=%v, groupBy=%d labels",
			vectorAgg.Operation, len(vectorAgg.GroupBy))
	})

	t.Run("Parallelize node structure", func(t *testing.T) {
		/*
		   ============================================================================
		   NODE TYPE: Parallelize
		   ============================================================================

		   Parallelize is a structural node that marks parallelization points.

		   Structure:
		   ----------
		   type Parallelize struct {
		       // No additional fields - it's a marker node
		   }

		   Purpose:
		   --------
		   Parallelize nodes are inserted by optimization passes to indicate
		   where the plan can be split for parallel execution.

		   The workflow planner uses these markers to create separate tasks
		   that can run concurrently on different workers.

		   Typical Placement:
		   ------------------
		   Parallelize is inserted above ScanSet nodes:

		     RangeAggregation
		       └── Parallelize  <-- Tasks below can run in parallel
		             └── ScanSet
		                   ├── DataObjScan[obj1]  <-- Task 1
		                   ├── DataObjScan[obj2]  <-- Task 2
		                   └── DataObjScan[obj3]  <-- Task 3
		*/
		parallelize := &physical.Parallelize{}

		require.Equal(t, physical.NodeTypeParallelize, parallelize.Type())

		t.Logf("Parallelize node - marks parallel execution boundary")
	})
}

// ============================================================================
// DAG STRUCTURE TESTS
// ============================================================================

/*
TestPhysicalPlanDAG demonstrates the DAG structure of physical plans.

Physical plans form a Directed Acyclic Graph where:
- Edges represent data flow
- Children produce data consumed by parents
- Multiple children enable joins and parallel execution
*/
func TestPhysicalPlanDAG(t *testing.T) {
	t.Run("build DAG manually", func(t *testing.T) {
		/*
		   ============================================================================
		   TEST: Manual DAG Construction
		   ============================================================================

		   This test builds a physical plan DAG manually to demonstrate
		   the graph structure and edge semantics.

		   Graph Structure:
		   ----------------
		       VectorAgg
		           │
		       RangeAgg
		           │
		      Parallelize
		           │
		       ScanSet

		   Edge Direction:
		   ---------------
		   Edges point from parent → child (data consumer → data producer).
		   Data flows in the opposite direction: child → parent.
		*/
		var graph dag.Graph[physical.Node]

		// Create nodes (bottom to top)
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

		// Add edges (parent → child)
		require.NoError(t, graph.AddEdge(dag.Edge[physical.Node]{Parent: parallelize, Child: scanSet}))
		require.NoError(t, graph.AddEdge(dag.Edge[physical.Node]{Parent: rangeAgg, Child: parallelize}))
		require.NoError(t, graph.AddEdge(dag.Edge[physical.Node]{Parent: vectorAgg, Child: rangeAgg}))

		// Create plan from graph
		plan := physical.FromGraph(graph)

		t.Logf("Manual DAG:\n%s", physical.PrintAsTree(plan))

		// Verify structure
		root, err := plan.Root()
		require.NoError(t, err)
		require.Equal(t, physical.NodeTypeVectorAggregation, root.Type())

		// Walk the plan
		var nodeTypes []physical.NodeType
		err = plan.DFSWalk(root, func(n physical.Node) error {
			nodeTypes = append(nodeTypes, n.Type())
			return nil
		}, dag.PreOrderWalk)
		require.NoError(t, err)

		t.Logf("Node types in DFS order: %v", nodeTypes)
	})

	t.Run("multiple inputs to join node", func(t *testing.T) {
		/*
		   ============================================================================
		   TEST: DAG with Multiple Inputs
		   ============================================================================

		   This demonstrates a DAG where a node has multiple inputs,
		   which is common for join operations or binary expressions.

		   Graph Structure:
		   ----------------
		           BinaryOp
		            /    \
		        Scan1   Scan2

		   Note: While we don't have a Join node in this simplified example,
		   the structure shows how multiple children feed into a parent.
		*/
		var graph dag.Graph[physical.Node]

		scan1 := graph.Add(&physical.DataObjScan{Location: "obj1", StreamIDs: []int64{1}})
		scan2 := graph.Add(&physical.DataObjScan{Location: "obj2", StreamIDs: []int64{2}})
		merge := graph.Add(&physical.Parallelize{}) // Using Parallelize as placeholder

		// Multiple children for one parent
		require.NoError(t, graph.AddEdge(dag.Edge[physical.Node]{Parent: merge, Child: scan1}))
		require.NoError(t, graph.AddEdge(dag.Edge[physical.Node]{Parent: merge, Child: scan2}))

		plan := physical.FromGraph(graph)

		t.Logf("Multi-input DAG:\n%s", physical.PrintAsTree(plan))

		// The parent node should have 2 children
		root, err := plan.Root()
		require.NoError(t, err)
		children := plan.Graph().Children(root)
		require.Len(t, children, 2, "merge node should have 2 children")
	})
}
