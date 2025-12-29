package engine_lab

/*
================================================================================
STAGE 5: DISTRIBUTED EXECUTION - Scheduler/Worker Architecture
================================================================================

This file covers Stage 5 of query processing: Distributed Execution.
Distributed execution enables running query tasks across multiple workers
for improved parallelism and scalability.

================================================================================
STAGE OVERVIEW
================================================================================

Input:  workflow.Workflow (Task Graph)
Output: Query results via task execution across workers

Distributed execution involves coordinating multiple components:

1. SCHEDULER
   - Central coordinator that receives task manifests
   - Maintains queue of pending tasks
   - Assigns tasks to available workers
   - Coordinates stream bindings between tasks
   - Handles task status updates (pending, running, completed, failed)

2. WORKERS
   - Execute task fragments (portions of physical plans)
   - Connect to one or more schedulers
   - Request tasks when ready (pull model)
   - Send results via streams
   - Report completion/failure status

3. STREAMS
   - Data channels connecting tasks
   - Carry Arrow RecordBatches between tasks
   - Support backpressure via blocking writes
   - Have exactly one sender and one receiver

================================================================================
ARCHITECTURE DIAGRAM
================================================================================

                    ┌─────────────────────────────────────────┐
                    │              SCHEDULER                   │
                    │  ┌────────────────────────────────────┐ │
                    │  │           Task Queue                │ │
                    │  │  [Task1] [Task2] [Task3] [...]     │ │
                    │  └────────────────────────────────────┘ │
                    │                    │                    │
                    │          ┌─────────┼─────────┐          │
                    │          ▼         ▼         ▼          │
                    │  ┌─────────┐ ┌─────────┐ ┌─────────┐   │
                    │  │Worker 1│ │Worker 2│ │Worker 3│   │
                    │  └─────────┘ └─────────┘ └─────────┘   │
                    └─────────────────────────────────────────┘

Each worker:
  1. Connects to scheduler
  2. Sends "ready" message
  3. Receives task assignment
  4. Executes task fragment
  5. Sends results via streams
  6. Reports completion status

================================================================================
COMMUNICATION PROTOCOL
================================================================================

The scheduler and workers communicate using a binary wire protocol:

MESSAGE TYPES:
--------------
1. WorkerHelloMessage
   - Sent: Worker → Scheduler
   - Purpose: Worker registration
   - Contains: Worker ID, capabilities

2. TaskAssignMessage
   - Sent: Scheduler → Worker
   - Purpose: Assign task to worker
   - Contains: Task ID, Fragment (physical plan portion), Stream bindings

3. TaskStatusMessage
   - Sent: Worker → Scheduler
   - Purpose: Report task completion/failure
   - Contains: Task ID, Status (completed/failed), Error (if any)

4. StreamBindMessage
   - Sent: Both directions
   - Purpose: Bind streams between tasks
   - Contains: Stream ID, Sender task, Receiver task

5. StreamDataMessage
   - Sent: Worker → Worker (or Worker → Scheduler for final results)
   - Purpose: Carry Arrow RecordBatches
   - Contains: Stream ID, Record batch data

TRANSPORT LAYERS:
-----------------
- Local: In-process channels (wire.Local)
  Used for: Single-process deployments, testing

- Remote: HTTP/2 connections (wire.HTTP2Listener/Dialer)
  Used for: Multi-node distributed deployments

================================================================================
TASK LIFECYCLE
================================================================================

Task State Machine:

    ┌─────────┐
    │ Created │
    └────┬────┘
         │ (registered with scheduler)
         ▼
    ┌─────────┐
    │ Pending │
    └────┬────┘
         │ (assigned to worker)
         ▼
    ┌─────────┐
    │ Running │
    └────┬────┘
         │
    ┌────┴────┐
    │         │
    ▼         ▼
┌─────────┐ ┌─────────┐ ┌───────────┐
│Completed│ │ Failed  │ │ Cancelled │
└─────────┘ └─────────┘ └───────────┘

State Descriptions:
  - Created: Task object created but not yet registered
  - Pending: Registered with scheduler, waiting for worker
  - Running: Assigned to worker, executing
  - Completed: Finished successfully, results sent
  - Failed: Encountered error during execution
  - Cancelled: Explicitly cancelled (e.g., query timeout)

================================================================================
STREAM LIFECYCLE
================================================================================

Stream State Machine:

    ┌──────┐
    │ Idle │ (created, not bound)
    └───┬──┘
        │ (sender and receiver bind)
        ▼
    ┌──────┐
    │ Open │ (ready for data)
    └───┬──┘
        │
    ┌───┴───┐
    │       │
    ▼       ▼
┌─────────┐ ┌─────────┐
│ Blocked │ │ Closed  │
└────┬────┘ └─────────┘
     │ (data available/consumed)
     ▼
   ┌──────┐
   │ Open │
   └──────┘

Stream Properties:
  - Each stream has a unique ULID identifier
  - Tenant ID for isolation (multi-tenancy)
  - Exactly one sender task
  - Exactly one receiver (task or local listener)
  - Backpressure: writers block when buffer full

================================================================================
RUNNER INTERFACE
================================================================================

The Runner interface bridges workflows and the scheduler:

    type Runner interface {
        // RegisterManifest registers tasks and streams with scheduler
        RegisterManifest(ctx context.Context, manifest *Manifest) error

        // UnregisterManifest removes tasks and streams
        UnregisterManifest(ctx context.Context, manifest *Manifest) error

        // Listen creates a local listener for a stream
        Listen(ctx context.Context, writer RecordWriter, stream *Stream) error

        // Start starts tasks (transitions to Pending)
        Start(ctx context.Context, tasks ...*Task) error

        // Cancel cancels running tasks
        Cancel(ctx context.Context, tasks ...*Task) error
    }

The Manifest contains:
  - Tasks: List of tasks to register
  - Streams: List of streams for data flow
  - TaskEventHandler: Callback for task status changes
  - StreamEventHandler: Callback for stream events

================================================================================
*/

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/logical"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
	"github.com/grafana/loki/v3/pkg/logproto"
)

/*
TestDistributedExecution demonstrates the scheduler/worker architecture.

The distributed execution system enables parallel query processing across
multiple workers, coordinated by a central scheduler.
*/
func TestDistributedExecution(t *testing.T) {
	t.Run("scheduler_accepts_task_manifests", func(t *testing.T) {
		/*
		   ============================================================================
		   TEST: Scheduler Accepts Task Manifests
		   ============================================================================

		   This test demonstrates how workflows register their tasks with the scheduler.

		   WORKFLOW:
		   ---------
		   1. Workflow creates a Manifest containing:
		      - List of Tasks (units of work)
		      - List of Streams (data channels)
		      - Event handlers for status updates

		   2. Workflow calls runner.RegisterManifest(manifest)

		   3. Scheduler validates the manifest:
		      - No duplicate task IDs
		      - No duplicate stream IDs
		      - All stream bindings reference valid tasks

		   4. Tasks are added to scheduler's queue

		   5. Workflow calls runner.Start(tasks...) to begin execution

		   MANIFEST STRUCTURE:
		   -------------------
		   type Manifest struct {
		       Tasks              []*Task              // Tasks to register
		       Streams            []*Stream            // Streams for data flow
		       TaskEventHandler   TaskEventHandler     // Task status callback
		       StreamEventHandler StreamEventHandler   // Stream event callback
		   }

		   TASK STRUCTURE:
		   ---------------
		   type Task struct {
		       ULID         ulid.ULID          // Unique identifier
		       TenantID     string             // Tenant for isolation
		       Fragment     *physical.Plan     // Plan portion to execute
		       Sources      StreamsByChild     // Input streams
		       Sinks        StreamsByChild     // Output streams
		       MaxTimeRange physical.TimeRange // Time bounds
		   }

		   ============================================================================
		*/
		ctx := context.Background()

		// Create test ingester with real data
		ingester := setupTestIngesterWithData(t, ctx, "test-tenant", map[string][]string{
			`{app="test"}`: {"log 1", "log 2", "log 3"},
		})
		defer ingester.Close()

		catalog := ingester.Catalog()

		now := time.Now()
		q := &mockQuery{
			statement: `{app="test"}`,
			start:     now.Add(-10 * time.Minute).Unix(),
			end:       now.Add(10 * time.Minute).Unix(),
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

		// Create a mock runner that simulates scheduler behavior
		runner := newTestRunner()

		// Create workflow - this internally creates a Manifest and registers with runner
		wf, err := workflow.New(workflow.Options{}, log.NewNopLogger(), "test-tenant", runner, optimizedPlan)
		require.NoError(t, err)

		// Run workflow - this starts tasks
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		pipeline, err := wf.Run(ctx)
		require.NoError(t, err)
		defer pipeline.Close()

		// Verify tasks were registered with the mock scheduler
		runner.mu.RLock()
		defer runner.mu.RUnlock()

		require.NotEmpty(t, runner.tasks, "scheduler should have received tasks")

		// Log registered tasks
		for id, task := range runner.tasks {
			t.Logf("Task %s registered with runner", id)
			require.NotNil(t, task.handler, "task should have status handler")
		}
	})

	t.Run("task_state_transitions", func(t *testing.T) {
		/*
		   ============================================================================
		   TEST: Task State Transitions
		   ============================================================================

		   This test demonstrates the task state machine.

		   STATE TRANSITIONS:
		   ------------------
		   Created → Pending:  When registered with scheduler via Start()
		   Pending → Running:  When assigned to a worker
		   Running → Completed: When execution finishes successfully
		   Running → Failed:    When execution encounters an error
		   Any → Cancelled:     When explicitly cancelled

		   STATE HANDLING:
		   ---------------
		   The TaskEventHandler callback is invoked on each state change:

		       type TaskEventHandler func(ctx context.Context, task *Task, status TaskStatus)

		       type TaskStatus struct {
		           State  TaskState  // New state
		           Err    error      // Error if Failed state
		       }

		   The workflow uses this to:
		   - Track which tasks are still running
		   - Handle failures (cancel other tasks, propagate error)
		   - Know when all tasks are complete

		   ============================================================================
		*/
		runner := newTestRunner()

		var graph dag.Graph[physical.Node]
		scan := graph.Add(&physical.DataObjScan{Location: "obj1"})
		agg := graph.Add(&physical.RangeAggregation{Operation: types.RangeAggregationTypeCount})
		_ = graph.AddEdge(dag.Edge[physical.Node]{Parent: agg, Child: scan})

		physicalPlan := physical.FromGraph(graph)

		wf, err := workflow.New(workflow.Options{}, log.NewNopLogger(), "test-tenant", runner, physicalPlan)
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		pipeline, err := wf.Run(ctx)
		require.NoError(t, err)
		defer pipeline.Close()

		// Simulate task completion (in real execution, worker would do this)
		runner.mu.RLock()
		for _, rt := range runner.tasks {
			// Notify task completion via the event handler
			rt.handler(ctx, rt.task, workflow.TaskStatus{State: workflow.TaskStateCompleted})
		}
		runner.mu.RUnlock()

		// In real execution:
		// 1. Worker executes task fragment
		// 2. Worker sends TaskStatusMessage with state=Completed
		// 3. Scheduler calls TaskEventHandler with new status
		// 4. Workflow updates its tracking
	})

	t.Run("task_cancellation", func(t *testing.T) {
		/*
		   ============================================================================
		   TEST: Task Cancellation
		   ============================================================================

		   Tasks can be cancelled for various reasons:
		   - Query timeout
		   - User cancellation
		   - Failure in a dependent task
		   - Resource exhaustion

		   CANCELLATION FLOW:
		   ------------------
		   1. Caller invokes runner.Cancel(tasks...)
		   2. Scheduler sends cancel signal to workers running those tasks
		   3. Workers abort execution and clean up
		   4. TaskEventHandler called with state=Cancelled
		   5. Any dependent tasks are also cancelled

		   GRACEFUL SHUTDOWN:
		   ------------------
		   When a workflow is cancelled:
		   - All pending tasks are cancelled
		   - Running tasks receive cancellation signal
		   - Streams are closed
		   - Resources are released

		   ============================================================================
		*/
		runner := newTestRunner()

		var graph dag.Graph[physical.Node]
		scan := graph.Add(&physical.DataObjScan{Location: "obj1"})
		parallelize := graph.Add(&physical.Parallelize{})
		_ = graph.AddEdge(dag.Edge[physical.Node]{Parent: parallelize, Child: scan})

		physicalPlan := physical.FromGraph(graph)

		wf, err := workflow.New(workflow.Options{}, log.NewNopLogger(), "test-tenant", runner, physicalPlan)
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		pipeline, err := wf.Run(ctx)
		require.NoError(t, err)
		defer pipeline.Close()

		// Simulate cancellation
		runner.mu.RLock()
		for _, rt := range runner.tasks {
			rt.handler(ctx, rt.task, workflow.TaskStatus{State: workflow.TaskStateCancelled})
		}
		runner.mu.RUnlock()

		t.Log("Tasks cancelled successfully")
	})
}

/*
TestStreamBindings demonstrates how tasks communicate via streams.

Streams are the data channels that connect tasks in a workflow. They carry
Arrow RecordBatches from producer tasks to consumer tasks.
*/
func TestStreamBindingsDistributed(t *testing.T) {
	t.Run("streams_connect_tasks", func(t *testing.T) {
		/*
		   ============================================================================
		   TEST: Streams Connect Tasks
		   ============================================================================

		   This test demonstrates stream bindings between tasks.

		   STREAM BINDING MODEL:
		   ---------------------
		   Each stream has:
		   - ULID: Unique identifier
		   - TenantID: For multi-tenant isolation
		   - Sender: Exactly one task that produces data
		   - Receiver: Exactly one task (or local listener) that consumes data

		   STREAM CREATION EXAMPLE:
		   ------------------------
		   Given this physical plan:

		       RangeAggregation
		         └── Parallelize
		               └── ScanSet
		                     ├── DataObjScan[obj1]
		                     └── DataObjScan[obj2]

		   The workflow planner creates:
		   - Task 1: RangeAggregation (receives streams from scan tasks)
		   - Task 2: DataObjScan[obj1] (sends to stream S1)
		   - Task 3: DataObjScan[obj2] (sends to stream S2)

		   Streams:
		   - S1: sender=Task2, receiver=Task1
		   - S2: sender=Task3, receiver=Task1

		   DATA FLOW:
		   ----------
		   1. Task 2 executes, produces RecordBatch
		   2. Task 2 writes batch to stream S1
		   3. Task 1 reads from S1, receives batch
		   4. Task 3 executes, produces RecordBatch
		   5. Task 3 writes batch to stream S2
		   6. Task 1 reads from S2, receives batch
		   7. Task 1 merges batches, produces aggregated result

		   ============================================================================
		*/
		ctx := context.Background()

		// Create test ingester with data in multiple streams
		ingester := setupTestIngesterWithData(t, ctx, "test-tenant", map[string][]string{
			`{app="test", stream="1"}`: {"stream 1 log 1", "stream 1 log 2"},
			`{app="test", stream="2"}`: {"stream 2 log 1", "stream 2 log 2"},
		})
		defer ingester.Close()

		catalog := ingester.Catalog()

		now := time.Now()
		q := &mockQuery{
			statement: `{app="test"}`,
			start:     now.Add(-10 * time.Minute).Unix(),
			end:       now.Add(10 * time.Minute).Unix(),
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

		wf, err := workflow.New(workflow.Options{}, log.NewNopLogger(), "test-tenant", runner, optimizedPlan)
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		pipeline, err := wf.Run(ctx)
		require.NoError(t, err)
		defer pipeline.Close()

		// Verify streams were created
		runner.mu.RLock()
		defer runner.mu.RUnlock()

		require.NotEmpty(t, runner.streams, "workflow should create streams")

		// Log stream bindings
		for id, stream := range runner.streams {
			t.Logf("Stream %s: sender=%v, receiver=%v",
				id, stream.Sender, stream.TaskReceiver)
		}
	})

	t.Run("local_stream_listener", func(t *testing.T) {
		/*
		   ============================================================================
		   TEST: Local Stream Listener
		   ============================================================================

		   The final stream in a workflow doesn't go to another task - it goes
		   to a local listener that collects the results.

		   LOCAL LISTENER:
		   ---------------
		   type RecordWriter interface {
		       WriteRecordBatch(arrow.RecordBatch) error
		   }

		   The workflow's result pipeline implements RecordWriter and is registered
		   as the listener for the root task's output stream.

		   FLOW:
		   -----
		   1. Root task produces final RecordBatch
		   2. Root task writes to output stream
		   3. Local listener receives batch
		   4. Batch available via Pipeline.Read()

		   This is how results flow from distributed tasks back to the caller.

		   ============================================================================
		*/
		runner := newTestRunner()

		var graph dag.Graph[physical.Node]
		scan := graph.Add(&physical.DataObjScan{
			Location:  "obj1",
			StreamIDs: []int64{1},
		})
		agg := graph.Add(&physical.RangeAggregation{Operation: types.RangeAggregationTypeCount})
		_ = graph.AddEdge(dag.Edge[physical.Node]{Parent: agg, Child: scan})

		physicalPlan := physical.FromGraph(graph)

		wf, err := workflow.New(workflow.Options{}, log.NewNopLogger(), "test-tenant", runner, physicalPlan)
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		pipeline, err := wf.Run(ctx)
		require.NoError(t, err)
		defer pipeline.Close()

		// Check for streams with local listeners (Listener != nil, TaskReceiver == Zero)
		runner.mu.RLock()
		defer runner.mu.RUnlock()

		var hasLocalListener bool
		for id, stream := range runner.streams {
			if stream.Listener != nil {
				hasLocalListener = true
				t.Logf("Stream %s has local listener (final output stream)", id)
			}
		}

		// Note: The workflow may not create streams if there's only one task
		// In that case, the result is returned directly without streaming
		t.Logf("Has local listener: %v", hasLocalListener)
	})

	t.Run("stream_backpressure", func(t *testing.T) {
		/*
		   ============================================================================
		   TEST: Stream Backpressure
		   ============================================================================

		   Streams implement backpressure to prevent memory exhaustion.

		   BACKPRESSURE MECHANISM:
		   -----------------------
		   1. Each stream has a bounded buffer for RecordBatches
		   2. When buffer is full, writes block
		   3. When consumer reads, space is freed
		   4. Writers unblock and can continue

		   WHY BACKPRESSURE MATTERS:
		   -------------------------
		   Without backpressure:
		   - Fast producer could overwhelm slow consumer
		   - Memory usage grows unbounded
		   - System crashes

		   With backpressure:
		   - Producer slows down to match consumer speed
		   - Memory usage stays bounded
		   - System remains stable

		   IMPLEMENTATION:
		   ---------------
		   In the wire protocol, backpressure is implemented via:
		   - Bounded channel for local streams
		   - Flow control for HTTP/2 remote streams

		   ============================================================================
		*/
		// This test documents the concept - actual backpressure testing
		// requires more complex setup with real data flowing

		t.Log("Stream backpressure concept documented")
		t.Log("- Bounded buffers prevent memory exhaustion")
		t.Log("- Writers block when buffer full")
		t.Log("- Readers free space for writers")
	})
}

/*
TestDistributedDeploymentModes demonstrates different deployment topologies.

The distributed execution system supports multiple deployment modes, from
single-process to fully distributed.
*/
func TestDistributedDeploymentModes(t *testing.T) {
	t.Run("local_mode", func(t *testing.T) {
		/*
		   ============================================================================
		   TEST: Local Mode (Single Process)
		   ============================================================================

		   In local mode, all components run in the same process:
		   - Scheduler runs in-process
		   - Workers are goroutines
		   - Communication via channels (wire.Local)

		   USE CASES:
		   ----------
		   - Development and testing
		   - Small deployments
		   - Debugging (easier to trace)

		   BENEFITS:
		   ---------
		   - No network latency
		   - Easier debugging
		   - Lower operational complexity

		   LIMITATIONS:
		   -----------
		   - Limited to single machine resources
		   - No fault tolerance (process crash = total failure)

		   ============================================================================
		*/
		t.Log("Local mode: All components in single process")
		t.Log("- Scheduler: In-process")
		t.Log("- Workers: Goroutines")
		t.Log("- Communication: wire.Local (channels)")
	})

	t.Run("distributed_mode", func(t *testing.T) {
		/*
		   ============================================================================
		   TEST: Distributed Mode (Multiple Nodes)
		   ============================================================================

		   In distributed mode, components run on separate machines:
		   - Scheduler runs on dedicated node(s)
		   - Workers run on multiple nodes
		   - Communication via HTTP/2 (wire.HTTP2Listener/Dialer)

		   USE CASES:
		   ----------
		   - Production deployments
		   - Large-scale queries
		   - High availability requirements

		   DEPLOYMENT TOPOLOGY:
		   --------------------
		              ┌────────────────┐
		              │   Scheduler    │
		              │    (Node 1)    │
		              └───────┬────────┘
		                      │ HTTP/2
		          ┌───────────┼───────────┐
		          │           │           │
		   ┌──────▼─────┐ ┌──▼──────┐ ┌──▼──────┐
		   │  Worker 1  │ │Worker 2 │ │Worker 3 │
		   │  (Node 2)  │ │(Node 3) │ │(Node 4) │
		   └────────────┘ └─────────┘ └─────────┘

		   BENEFITS:
		   ---------
		   - Horizontal scalability
		   - Better resource utilization
		   - Fault tolerance (worker failure doesn't kill query)

		   CHALLENGES:
		   -----------
		   - Network latency
		   - Serialization overhead
		   - More complex operations

		   ============================================================================
		*/
		t.Log("Distributed mode: Components on separate machines")
		t.Log("- Scheduler: Dedicated node(s)")
		t.Log("- Workers: Multiple nodes")
		t.Log("- Communication: wire.HTTP2 (HTTP/2 connections)")
	})
}

/*
TestAdmissionControl demonstrates workflow admission control.

Admission control prevents resource exhaustion by limiting concurrent tasks.
*/
func TestAdmissionControl(t *testing.T) {
	t.Run("task_limits", func(t *testing.T) {
		/*
		   ============================================================================
		   TEST: Task Admission Control
		   ============================================================================

		   The workflow system uses semaphore-based admission control:

		   CONFIGURATION:
		   --------------
		   type Options struct {
		       MaxRunningScanTasks  int  // Limit concurrent scan tasks
		       MaxRunningOtherTasks int  // Limit concurrent non-scan tasks (0 = unlimited)
		   }

		   WHY SEPARATE LIMITS:
		   --------------------
		   Scan tasks are I/O-bound (reading from storage), while other tasks
		   (aggregation, filtering) are CPU-bound. Different limits allow
		   tuning for optimal resource utilization.

		   Example with 1000 scan targets:
		   - MaxRunningScanTasks = 10
		   - Only 10 scans run concurrently
		   - Prevents overwhelming storage
		   - Prevents memory exhaustion from too many concurrent reads

		   ADMISSION FLOW:
		   ---------------
		   1. Task ready to start
		   2. Acquire semaphore (blocks if limit reached)
		   3. Execute task
		   4. Release semaphore
		   5. Next waiting task can proceed

		   ============================================================================
		*/
		// Create workflow with admission control limits
		runner := newTestRunner()

		var graph dag.Graph[physical.Node]
		scan := graph.Add(&physical.DataObjScan{Location: "obj1"})
		parallelize := graph.Add(&physical.Parallelize{})
		_ = graph.AddEdge(dag.Edge[physical.Node]{Parent: parallelize, Child: scan})

		physicalPlan := physical.FromGraph(graph)

		// Configure admission control
		options := workflow.Options{
			MaxRunningScanTasks:  10, // Limit concurrent scans
			MaxRunningOtherTasks: 0,  // Unlimited non-scan tasks
		}

		wf, err := workflow.New(options, log.NewNopLogger(), "test-tenant", runner, physicalPlan)
		require.NoError(t, err)

		t.Logf("Workflow created with admission limits: scan=%d, other=%d",
			options.MaxRunningScanTasks, options.MaxRunningOtherTasks)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		pipeline, err := wf.Run(ctx)
		require.NoError(t, err)
		defer pipeline.Close()
	})
}

/*
TestErrorHandling demonstrates error handling in distributed execution.

Proper error handling is critical for a distributed system - failures can
occur at any component.
*/
func TestErrorHandling(t *testing.T) {
	t.Run("task_failure_propagation", func(t *testing.T) {
		/*
		   ============================================================================
		   TEST: Task Failure Propagation
		   ============================================================================

		   When a task fails, the error must propagate correctly:

		   FAILURE HANDLING:
		   -----------------
		   1. Worker detects error during execution
		   2. Worker sends TaskStatusMessage with state=Failed, error=<err>
		   3. Scheduler calls TaskEventHandler with error
		   4. Workflow cancels dependent tasks
		   5. Error propagated to caller via Pipeline

		   ERROR TYPES:
		   ------------
		   - Storage errors: File not found, permission denied
		   - Processing errors: Invalid data, computation error
		   - Resource errors: Out of memory, timeout
		   - Network errors: Connection lost, timeout

		   CASCADING FAILURES:
		   -------------------
		   If Task A depends on Task B, and Task B fails:
		   1. Task B transitions to Failed
		   2. Workflow detects dependency failure
		   3. Task A is cancelled (can't proceed without input)
		   4. All downstream tasks cancelled
		   5. Error returned to caller

		   ============================================================================
		*/
		runner := newTestRunner()

		var graph dag.Graph[physical.Node]
		scan := graph.Add(&physical.DataObjScan{Location: "obj1"})
		agg := graph.Add(&physical.RangeAggregation{Operation: types.RangeAggregationTypeCount})
		_ = graph.AddEdge(dag.Edge[physical.Node]{Parent: agg, Child: scan})

		physicalPlan := physical.FromGraph(graph)

		wf, err := workflow.New(workflow.Options{}, log.NewNopLogger(), "test-tenant", runner, physicalPlan)
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		pipeline, err := wf.Run(ctx)
		require.NoError(t, err)
		defer pipeline.Close()

		// Simulate task failure
		runner.mu.RLock()
		for _, rt := range runner.tasks {
			rt.handler(ctx, rt.task, workflow.TaskStatus{
				State: workflow.TaskStateFailed,
				// In real code, would include error
			})
		}
		runner.mu.RUnlock()

		t.Log("Task failure propagation documented")
	})

	t.Run("timeout_handling", func(t *testing.T) {
		/*
		   ============================================================================
		   TEST: Timeout Handling
		   ============================================================================

		   Queries have configurable timeouts to prevent resource exhaustion:

		   TIMEOUT SOURCES:
		   ----------------
		   1. Query-level timeout (context deadline)
		   2. Task-level timeout (execution limit per task)
		   3. Stream-level timeout (data transfer limit)

		   TIMEOUT HANDLING:
		   -----------------
		   When timeout occurs:
		   1. Context cancelled (ctx.Done() returns)
		   2. All running tasks receive cancellation
		   3. Streams are closed
		   4. Resources are cleaned up
		   5. Context.DeadlineExceeded returned to caller

		   CONFIGURATION:
		   --------------
		   Timeouts are typically set at the query level:

		       ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		       defer cancel()
		       result, err := engine.Execute(ctx, params)

		   ============================================================================
		*/
		runner := newTestRunner()

		var graph dag.Graph[physical.Node]
		scan := graph.Add(&physical.DataObjScan{Location: "obj1"})
		parallelize := graph.Add(&physical.Parallelize{})
		_ = graph.AddEdge(dag.Edge[physical.Node]{Parent: parallelize, Child: scan})

		physicalPlan := physical.FromGraph(graph)

		wf, err := workflow.New(workflow.Options{}, log.NewNopLogger(), "test-tenant", runner, physicalPlan)
		require.NoError(t, err)

		// Create context with very short timeout
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
		defer cancel()

		// The workflow might fail due to timeout
		_, err = wf.Run(ctx)
		if err != nil {
			t.Logf("Workflow returned error (expected with short timeout): %v", err)
		}

		t.Log("Timeout handling documented")
	})
}
