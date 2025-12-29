package engine_lab

/*
============================================================================
LOKI QUERY ENGINE V2 - TEST UTILITIES
============================================================================

This file contains shared utilities used across all learning test files.
These utilities mock the engine's underlying components to enable testing
and understanding of each stage in isolation.

Shared Components:
  - mockQuery: Implements logql.Params for testing logical planning
  - mockCatalog: Implements physical.Catalog for testing physical planning
  - mockPipeline: Simple pipeline for testing execution
  - testRunner: Implements workflow.Runner for testing distributed execution

============================================================================
*/

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sync"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/engine/internal/executor"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache/resultscache"
	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

// ============================================================================
// mockQuery: Query Parameters Mock
// ============================================================================

/*
mockQuery implements logql.Params for testing logical planning.

This is the entry point to the query engine - it wraps the LogQL string and
query parameters (time range, direction, limit, etc.) that are provided by
the user when making a query.

Usage:

	q := &mockQuery{
	    statement: `{app="test"} |= "error"`,
	    start:     1000,    // Unix timestamp
	    end:       2000,    // Unix timestamp
	    direction: logproto.BACKWARD,
	    limit:     100,
	}
	plan, err := logical.BuildPlan(q)
*/
type mockQuery struct {
	statement string
	start     int64
	end       int64
	step      time.Duration
	interval  time.Duration
	direction logproto.Direction
	limit     uint32
}

func (q *mockQuery) Direction() logproto.Direction { return q.direction }
func (q *mockQuery) End() time.Time                { return time.Unix(q.end, 0) }
func (q *mockQuery) Start() time.Time              { return time.Unix(q.start, 0) }
func (q *mockQuery) Limit() uint32                 { return q.limit }
func (q *mockQuery) QueryString() string           { return q.statement }
func (q *mockQuery) GetExpression() syntax.Expr    { return syntax.MustParseExpr(q.statement) }
func (q *mockQuery) Step() time.Duration           { return q.step }
func (q *mockQuery) Interval() time.Duration       { return q.interval }
func (q *mockQuery) Shards() []string              { return []string{"0_of_1"} }
func (q *mockQuery) CachingOptions() resultscache.CachingOptions {
	panic("unimplemented")
}
func (q *mockQuery) GetStoreChunks() *logproto.ChunkRefGroup {
	panic("unimplemented")
}

var _ logql.Params = (*mockQuery)(nil)

// ============================================================================
// mockCatalog: Data Catalog Mock
// ============================================================================

/*
mockCatalog implements physical.Catalog for testing physical planning.

The catalog is responsible for resolving stream selectors to actual data
object locations in storage. During physical planning, the planner queries
the catalog to find which data objects (Parquet files) contain data matching
the query's stream selector.

The mock catalog returns predefined data object locations without accessing
real storage, enabling isolated testing of the physical planner.

Usage:

	catalog := &mockCatalog{
	    sectionDescriptors: []*metastore.DataobjSectionDescriptor{
	        {
	            SectionKey: metastore.SectionKey{ObjectPath: "tenant/obj1", SectionIdx: 0},
	            StreamIDs:  []int64{1, 2},
	            Start:      now,
	            End:        now.Add(time.Hour),
	        },
	    },
	}
	planner := physical.NewPlanner(physical.NewContext(start, end), catalog)
*/
type mockCatalog struct {
	sectionDescriptors []*metastore.DataobjSectionDescriptor
}

func (c *mockCatalog) ResolveShardDescriptors(e physical.Expression, from, through time.Time) ([]physical.FilteredShardDescriptor, error) {
	return c.ResolveShardDescriptorsWithShard(e, nil, physical.ShardInfo{Shard: 0, Of: 1}, from, through)
}

func (c *mockCatalog) ResolveShardDescriptorsWithShard(_ physical.Expression, _ []physical.Expression, shard physical.ShardInfo, from, through time.Time) ([]physical.FilteredShardDescriptor, error) {
	var result []physical.FilteredShardDescriptor
	for i, desc := range c.sectionDescriptors {
		// Filter by shard
		if shard.Of > 1 && i%int(shard.Of) != int(shard.Shard) {
			continue
		}
		// Filter by time range
		if desc.End.Before(from) || desc.Start.After(through) {
			continue
		}
		result = append(result, physical.FilteredShardDescriptor{
			Location:  physical.DataObjLocation(desc.SectionKey.ObjectPath),
			Streams:   desc.StreamIDs,
			Sections:  []int{int(desc.SectionKey.SectionIdx)},
			TimeRange: physical.TimeRange{Start: desc.Start, End: desc.End},
		})
	}
	return result, nil
}

var _ physical.Catalog = (*mockCatalog)(nil)

// ============================================================================
// mockPipeline: Execution Pipeline Mock
// ============================================================================

/*
mockPipeline is a simple pipeline for testing that yields predefined records.

A pipeline is the execution abstraction in the V2 engine. It provides a
streaming interface for reading Arrow RecordBatches:

	type Pipeline interface {
	    Read(context.Context) (arrow.RecordBatch, error)
	    Close()
	}

The mock pipeline yields predefined records and returns EOF when exhausted.

Usage:

	records := []arrow.RecordBatch{record1, record2}
	pipeline := newMockPipeline(records...)
	defer pipeline.Close()

	for {
	    rec, err := pipeline.Read(ctx)
	    if errors.Is(err, executor.EOF) {
	        break
	    }
	    // Process rec...
	}
*/
type mockPipeline struct {
	records []arrow.RecordBatch
	index   int
	closed  bool
}

func newMockPipeline(records ...arrow.RecordBatch) *mockPipeline {
	return &mockPipeline{records: records}
}

func (p *mockPipeline) Read(_ context.Context) (arrow.RecordBatch, error) {
	if p.index >= len(p.records) {
		return nil, executor.EOF
	}
	rec := p.records[p.index]
	p.index++
	return rec, nil
}

func (p *mockPipeline) Close() {
	p.closed = true
}

// ============================================================================
// testRunner: Workflow Runner Mock (Scheduler Interface)
// ============================================================================

/*
testRunner implements workflow.Runner for testing distributed execution.

The Runner interface is the bridge between the workflow system and the
scheduler. It's responsible for:
  - Registering task manifests with the scheduler
  - Creating streams for data flow between tasks
  - Starting/canceling tasks
  - Listening for task results

The test runner simulates scheduler behavior without actual network
communication or task execution, enabling isolated testing of workflow
planning and task lifecycle management.

Key Concepts:
  - Manifest: Collection of tasks and streams for a workflow
  - Stream: Data channel connecting tasks (sender → receiver)
  - Task: Unit of work with sources (inputs) and sinks (outputs)

Usage:

	runner := &testRunner{
	    streams: make(map[ulid.ULID]*testRunnerStream),
	    tasks:   make(map[ulid.ULID]*testRunnerTask),
	}
	wf, err := workflow.New(workflow.Options{}, logger, "tenant", runner, physicalPlan)
*/
type testRunner struct {
	mu      sync.RWMutex
	streams map[ulid.ULID]*testRunnerStream
	tasks   map[ulid.ULID]*testRunnerTask
}

type testRunnerStream struct {
	Stream       *workflow.Stream
	Handler      workflow.StreamEventHandler
	Sender       ulid.ULID
	TaskReceiver ulid.ULID
	Listener     workflow.RecordWriter
}

type testRunnerTask struct {
	task    *workflow.Task
	handler workflow.TaskEventHandler
}

func (r *testRunner) RegisterManifest(_ context.Context, manifest *workflow.Manifest) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var (
		manifestStreams = make(map[ulid.ULID]*testRunnerStream, len(manifest.Streams))
		manifestTasks   = make(map[ulid.ULID]*testRunnerTask, len(manifest.Tasks))
	)

	for _, stream := range manifest.Streams {
		if _, exist := r.streams[stream.ULID]; exist {
			return fmt.Errorf("stream %s already added", stream.ULID)
		} else if _, exist := manifestStreams[stream.ULID]; exist {
			return fmt.Errorf("stream %s already added in manifest", stream.ULID)
		}

		manifestStreams[stream.ULID] = &testRunnerStream{
			Stream:  stream,
			Handler: manifest.StreamEventHandler,
		}
	}
	for _, task := range manifest.Tasks {
		if _, exist := r.tasks[task.ULID]; exist {
			return fmt.Errorf("task %s already added", task.ULID)
		} else if _, exist := manifestTasks[task.ULID]; exist {
			return fmt.Errorf("task %s already added in manifest", task.ULID)
		}

		for _, streams := range task.Sinks {
			for _, stream := range streams {
				rs, exist := manifestStreams[stream.ULID]
				if !exist {
					return fmt.Errorf("sink stream %s not found in manifest", stream.ULID)
				} else if rs.Sender != ulid.Zero {
					return fmt.Errorf("stream %s already bound to sender %s", stream.ULID, rs.Sender)
				}

				rs.Sender = task.ULID
			}
		}
		for _, streams := range task.Sources {
			for _, stream := range streams {
				rs, exist := manifestStreams[stream.ULID]
				if !exist {
					return fmt.Errorf("source stream %s not found in manifest", stream.ULID)
				} else if rs.TaskReceiver != ulid.Zero {
					return fmt.Errorf("source stream %s already bound to %s", stream.ULID, rs.TaskReceiver)
				} else if rs.Listener != nil {
					return fmt.Errorf("source stream %s already bound to local receiver", stream.ULID)
				}

				rs.TaskReceiver = task.ULID
			}
		}

		manifestTasks[task.ULID] = &testRunnerTask{
			task:    task,
			handler: manifest.TaskEventHandler,
		}
	}

	maps.Copy(r.streams, manifestStreams)
	maps.Copy(r.tasks, manifestTasks)
	return nil
}

func (r *testRunner) UnregisterManifest(_ context.Context, manifest *workflow.Manifest) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, stream := range manifest.Streams {
		if _, exist := r.streams[stream.ULID]; !exist {
			return fmt.Errorf("stream %s not found", stream.ULID)
		}
	}
	for _, task := range manifest.Tasks {
		if _, exist := r.tasks[task.ULID]; !exist {
			return fmt.Errorf("task %s not found", task.ULID)
		}
	}

	for _, stream := range manifest.Streams {
		delete(r.streams, stream.ULID)
	}
	for _, task := range manifest.Tasks {
		delete(r.tasks, task.ULID)
	}

	return nil
}

func (r *testRunner) Listen(_ context.Context, writer workflow.RecordWriter, stream *workflow.Stream) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	rs, exist := r.streams[stream.ULID]
	if !exist {
		return fmt.Errorf("stream %s not found", stream.ULID)
	} else if rs.Listener != nil || rs.TaskReceiver != ulid.Zero {
		return fmt.Errorf("stream %s already bound", stream.ULID)
	}

	rs.Listener = writer
	return nil
}

func (r *testRunner) Start(ctx context.Context, tasks ...*workflow.Task) error {
	var errs []error

	for _, task := range tasks {
		r.mu.Lock()
		rt, exist := r.tasks[task.ULID]
		r.mu.Unlock()

		if !exist {
			errs = append(errs, fmt.Errorf("task %s not registered", task.ULID))
			continue
		}

		rt.handler(ctx, task, workflow.TaskStatus{State: workflow.TaskStatePending})
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

func (r *testRunner) Cancel(ctx context.Context, tasks ...*workflow.Task) error {
	var errs []error

	for _, task := range tasks {
		r.mu.RLock()
		rt, exist := r.tasks[task.ULID]
		r.mu.RUnlock()

		if !exist {
			errs = append(errs, fmt.Errorf("task %s not found", task.ULID))
			continue
		}

		rt.handler(ctx, task, workflow.TaskStatus{State: workflow.TaskStateCancelled})
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

var _ workflow.Runner = (*testRunner)(nil)

// ============================================================================
// Helper Functions
// ============================================================================

// testingT is the interface required for test helpers
type testingT interface {
	require.TestingT
	Logf(format string, args ...interface{})
}

// createTestRecord creates a test Arrow record with timestamp and message columns.
// This is useful for testing pipelines and result builders.
func createTestRecord(t testingT, numRows int) arrow.RecordBatch {
	alloc := memory.NewGoAllocator()

	colTs := semconv.ColumnIdentTimestamp
	colMsg := semconv.ColumnIdentMessage

	schema := arrow.NewSchema(
		[]arrow.Field{
			semconv.FieldFromIdent(colTs, false),
			semconv.FieldFromIdent(colMsg, false),
		},
		nil,
	)

	rows := make(arrowtest.Rows, numRows)
	for i := 0; i < numRows; i++ {
		rows[i] = arrowtest.Row{
			colTs.FQN():  time.Unix(0, int64(1000000000+i)).UTC(),
			colMsg.FQN(): fmt.Sprintf("log line %d", i),
		}
	}

	return rows.Record(alloc, schema)
}

// buildSimplePlan builds a simple physical plan for testing.
// It creates a single DataObjScan node wrapped in a Parallelize node.
func buildSimplePlan(t testingT) *physical.Plan {
	var graph dag.Graph[physical.Node]

	scan := graph.Add(&physical.DataObjScan{
		Location:  "test-object",
		Section:   0,
		StreamIDs: []int64{1, 2, 3},
	})
	parallelize := graph.Add(&physical.Parallelize{})

	_ = graph.AddEdge(dag.Edge[physical.Node]{Parent: parallelize, Child: scan})

	return physical.FromGraph(graph)
}

// newTestRunner creates a new test runner for workflow testing.
func newTestRunner() *testRunner {
	return &testRunner{
		streams: make(map[ulid.ULID]*testRunnerStream),
		tasks:   make(map[ulid.ULID]*testRunnerTask),
	}
}

// newTestWorkflow creates a workflow with a test runner for testing.
func newTestWorkflow(t testingT, physicalPlan *physical.Plan) (*workflow.Workflow, *testRunner) {
	runner := newTestRunner()
	wf, err := workflow.New(workflow.Options{}, log.NewNopLogger(), "test-tenant", runner, physicalPlan)
	require.NoError(t, err)
	return wf, runner
}

// ============================================================================
// TEST INGESTER HELPERS
// ============================================================================

/*
setupTestIngesterWithData creates a TestIngester with sample data for testing.

This helper simplifies test setup by:
1. Creating an in-memory test ingester
2. Pushing the provided log data
3. Flushing to create DataObj files
4. Returning the ingester with its catalog

Usage:

	ingester := setupTestIngesterWithData(t, ctx, "tenant-1", map[string][]string{
	    `{app="test", env="prod"}`: {
	        "error: connection failed",
	        "error: timeout occurred",
	    },
	    `{app="test", env="dev"}`: {
	        "info: request completed",
	    },
	})
	defer ingester.Close()

	catalog := ingester.Catalog()
	// Use catalog in physical planner...
*/
func setupTestIngesterWithData(t testingT, ctx context.Context, tenant string, data map[string][]string) *TestIngester {
	ingester, err := InMemoryIngester()
	require.NoError(t, err)

	for labels, lines := range data {
		err := ingester.PushSimple(ctx, tenant, labels, lines)
		require.NoError(t, err)
	}

	_, err = ingester.Flush(ctx)
	require.NoError(t, err)

	return ingester
}

/*
setupTestIngesterWithTimestamps creates a TestIngester with timestamped data.

This helper allows specifying exact timestamps for each log entry, useful for
testing time-based queries and aggregations.

Usage:

	entries := []LogEntry{
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
	}
	ingester := setupTestIngesterWithTimestamps(t, ctx, "tenant-1", entries)
	defer ingester.Close()
*/
func setupTestIngesterWithTimestamps(t testingT, ctx context.Context, tenant string, entries []LogEntry) *TestIngester {
	ingester, err := InMemoryIngester()
	require.NoError(t, err)

	err = ingester.Push(ctx, tenant, entries)
	require.NoError(t, err)

	_, err = ingester.Flush(ctx)
	require.NoError(t, err)

	return ingester
}

/*
ctxWithTenant creates a context with the tenant ID injected.
This is needed for executor operations that require tenant context.

Usage:

	execCtx := ctxWithTenant(ctx, "tenant-1")
	pipeline := executor.Run(execCtx, cfg, plan, logger)
*/
func ctxWithTenant(ctx context.Context, tenant string) context.Context {
	return user.InjectOrgID(ctx, tenant)
}

/*
setupTestIngesterMultiStream creates a TestIngester with multiple streams.

This helper creates data across multiple label combinations, useful for testing
stream grouping, aggregations, and filtering.

Usage:

	streams := []struct {
	    labels string
	    lines  []string
	}{
	    {`{app="test", level="error"}`, []string{"error 1", "error 2"}},
	    {`{app="test", level="info"}`, []string{"info 1", "info 2", "info 3"}},
	    {`{app="other", level="error"}`, []string{"other error"}},
	}
	ingester := setupTestIngesterMultiStream(t, ctx, "tenant-1", streams)
	defer ingester.Close()
*/
func setupTestIngesterMultiStream(t testingT, ctx context.Context, tenant string, streams []struct {
	labels string
	lines  []string
}) *TestIngester {
	ingester, err := InMemoryIngester()
	require.NoError(t, err)

	for _, stream := range streams {
		err := ingester.PushSimple(ctx, tenant, stream.labels, stream.lines)
		require.NoError(t, err)
	}

	_, err = ingester.Flush(ctx)
	require.NoError(t, err)

	return ingester
}
