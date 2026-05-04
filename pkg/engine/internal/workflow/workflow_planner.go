package workflow

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/oklog/ulid/v2"

	"github.com/grafana/loki/v3/pkg/engine/internal/executor"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
)

// planner is responsible for constructing the Task graph held by a [Workflow].
type planner struct {
	tenantID  string
	batchSize int // batch size to wrap each task fragment with; 0 means no wrapping
	graph     dag.Graph[*Task]
	physical  *physical.Plan

	streamWriters map[*Stream]*Task // Lookup of stream to which task writes to it
}

// cacheParams bundles cache-related configuration for workflow planning.
type cacheParams struct {
	enabled                     bool
	taskCacheMaxSizeBytes       uint64
	dataObjScanMaxSizeBytes     uint64
	compression                 string
	registry                    executor.TaskCacheRegistry
	pruneEmptyCachedTasks       bool
	nonEmptyCachedTasksMaxBytes uint64        // 0 = disabled
	pruneFetchTimeout           time.Duration // 0 = no timeout
}

// planWorkflow partitions a physical plan into a graph of tasks.
//
// planWorkflow returns an error if the provided physical plan does not
// have exactly one root node, or if the physical plan cannot be partitioned.
func planWorkflow(tenantID string, plan *physical.Plan, cacheOpts cacheParams, logger log.Logger) (dag.Graph[*Task], error) {
	root, err := plan.Root()
	if err != nil {
		return dag.Graph[*Task]{}, err
	}

	var batchSize int
	if b, ok := root.(*physical.Batching); ok {
		batchSize = int(b.BatchSize)
		// The Batching node is consumed here to extract the batch size. Advance
		// root to its child so the Batching wrapper itself is never turned into a
		// standalone task fragment.
		//
		// The planner.Process will add the batch node on top of task in the workflow.
		if children := plan.Children(b); len(children) == 1 {
			root = children[0]
		}
	}

	planner := &planner{
		tenantID:      tenantID,
		batchSize:     batchSize,
		physical:      plan,
		streamWriters: make(map[*Stream]*Task),
	}
	if err := planner.Process(root); err != nil {
		return dag.Graph[*Task]{}, err
	}

	for _, root := range planner.graph.Roots() {
		if err := planner.graph.Walk(root, func(t *Task) error {
			optimize(t)
			return nil
		}, dag.PostOrderWalk); err != nil {
			return dag.Graph[*Task]{}, err
		}
	}

	if cacheOpts.enabled {
		if err := injectTaskCaching(tenantID, planner.graph, cacheOpts.taskCacheMaxSizeBytes, cacheOpts.compression); err != nil {
			return dag.Graph[*Task]{}, fmt.Errorf("injecting task caching: %w", err)
		}
		if err := injectDataObjScanCaching(tenantID, planner.graph, cacheOpts.dataObjScanMaxSizeBytes, cacheOpts.compression); err != nil {
			return dag.Graph[*Task]{}, fmt.Errorf("injecting DataObjScan caching: %w", err)
		}
		if err := pruneCachedTasks(planner, cacheOpts, logger); err != nil {
			return dag.Graph[*Task]{}, fmt.Errorf("pruning cached tasks: %w", err)
		}
	}

	return planner.graph, nil
}

// injectDataObjScanCaching wraps each DataObjScan node in every task fragment
// with a Cache node (using TaskCacheDataObjScanResult), placing the cache directly
// above the scan regardless of what operators sit on top.
func injectDataObjScanCaching(tenantID string, graph dag.Graph[*Task], maxSizeBytes uint64, compression string) error {
	for _, root := range graph.Roots() {
		if err := graph.Walk(root, func(task *Task) error {
			return physical.WrapDataObjScansWithCache(context.Background(), tenantID, task.Fragment, maxSizeBytes, compression)
		}, dag.PreOrderWalk); err != nil {
			return err
		}
	}
	return nil
}

func optimize(t *Task) {
	for _, root := range t.Fragment.Roots() {
		optimizer := physical.NewOptimizer(t.Fragment, physical.WorkflowOptimizations(t.Fragment))
		optimizer.Optimize(root)
	}
}

// pruneCachedTasks eliminates tasks from the workflow graph whose cached result is
// known at plan time. A single batch-fetch pass classifies each cache hit as
// either empty (zero records) or non-empty.
//
//   - Empty hits (only when pruneEmpty=true): the task is eliminated. Its parent
//     is eliminated recursively if it has no remaining sources.
//   - Non-empty hits (only when pruneNonEmpty=true): the task is eliminated and
//     its parent tasks are wired with a [CachedSource] so they read directly from
//     the cache instead of waiting for a network stream.
//
// A non-leaf task is only eligible for elimination if all of its children are
// also being eliminated, preventing orphaned tasks with dangling Sink streams.
//
// Cache fetch errors are non-fatal: the batch is skipped and an error is logged.
func pruneCachedTasks(p *planner, cacheOpts cacheParams, logger log.Logger) error {
	if !cacheOpts.pruneEmptyCachedTasks && cacheOpts.nonEmptyCachedTasksMaxBytes == 0 {
		return nil
	}

	start := time.Now()

	keyToTask, backends, taskCount := collectCacheKeys(p, cacheOpts.registry, logger)

	fetchStart := time.Now()
	fetchCtx := context.Background()
	if cacheOpts.pruneFetchTimeout > 0 {
		var cancel context.CancelFunc
		fetchCtx, cancel = context.WithTimeout(fetchCtx, cacheOpts.pruneFetchTimeout)
		defer cancel()
	}
	emptyResults, nonEmptyResults := classifyTasksByCacheHit(fetchCtx, keyToTask, backends, logger)
	fetchDuration := time.Since(fetchStart)

	// NOTE: tasks cannot be eliminated inside the walk or fetch loops since
	// dag.Graph.Eliminate uses slices.DeleteFunc which zeroes the tail of the
	// underlying slice, corrupting any live range slice.
	pruningStart := time.Now()
	var (
		tasksRemoved int
		tasksSkipped int
		skippedBytes uint64
		cachedBytes  uint64
	)

	// Compute the non-empty tasks that fit within the size budget up front so
	// that wireCachedSources can run before the empty-elimination pass.
	// This is necessary to prevent cascade elimination from incorrectly removing
	// tasks that have non-empty cache hits: the cascade check guards on
	// len(task.CachedSources) > 0, which is only true after wireCachedSources runs.
	var nonEmptyUpToMaxSize []nonEmptyCachedTask
	if cacheOpts.nonEmptyCachedTasksMaxBytes > 0 {
		for _, r := range nonEmptyResults {
			size := uint64(len(r.buf))
			if cachedBytes+size <= cacheOpts.nonEmptyCachedTasksMaxBytes {
				nonEmptyUpToMaxSize = append(nonEmptyUpToMaxSize, r)
				cachedBytes += size
			} else {
				tasksSkipped++
				skippedBytes += size
			}
		}
		if tasksSkipped > 0 {
			level.Debug(logger).Log(
				"msg", "non-empty cached tasks skipped due to size budget",
				"skipped", tasksSkipped,
				"budget", humanize.Bytes(cacheOpts.nonEmptyCachedTasksMaxBytes),
			)
		}
		// Wire CachedSources into parent tasks before the empty-elimination pass
		// so that the cascade guard (len(task.CachedSources) > 0) correctly
		// protects non-empty tasks whose children are all empty hits.
		wireCachedSources(p, nonEmptyUpToMaxSize)
	}

	if cacheOpts.pruneEmptyCachedTasks && len(emptyResults) > 0 {
		removed := eliminateTasks(p, emptyResults)
		eliminatedCachedTasksTotal.WithLabelValues(eliminationReasonEmpty).Add(float64(removed))
		tasksRemoved += removed
	}
	if len(nonEmptyUpToMaxSize) > 0 {
		nonEmptyTasks := make([]*Task, len(nonEmptyUpToMaxSize))
		for i, r := range nonEmptyUpToMaxSize {
			nonEmptyTasks[i] = r.task
		}
		removed := eliminateTasks(p, nonEmptyTasks)
		eliminatedCachedTasksTotal.WithLabelValues(eliminationReasonNonEmpty).Add(float64(removed))
		tasksRemoved += removed
	}
	pruningDuration := time.Since(pruningStart)

	// Log the number of tasks removed. Note that if removed_tasks is bigger than to_eliminate
	// then, (removed_tasks-to_eliminate) parents were removed because all their children were removed.
	if tasksRemoved > 0 {
		level.Info(logger).Log(
			"msg", "pruned cached tasks from workflow",
			"total_tasks", taskCount,
			"removed_tasks", tasksRemoved,
			"skipped_tasks", tasksSkipped,
			"skipped_bytes", humanize.Bytes(skippedBytes),
			"empty_hits", len(emptyResults),
			"non_empty_hits", len(nonEmptyResults),
			"non_empty_bytes", humanize.Bytes(cachedBytes),
			"elapsed", time.Since(start),
			"fetch_duration", fetchDuration,
			"pruning_duration", pruningDuration,
		)
	}

	return nil
}

// collectCacheKeys walks the task graph and builds:
//
//   - keyToTask: root-level cache nodes for every task, keyed by (cache name → hashed key → task).
//     Only root-level cache nodes are collected because a non-root empty hit is always also
//     reported as a hit at the root level.
//   - backends: resolved cache.Cache per cache name (nil = unavailable).
func collectCacheKeys(p *planner, caches executor.TaskCacheRegistry, logger log.Logger) (
	keyToTask map[physical.TaskCacheName]map[string]*Task,
	backends map[physical.TaskCacheName]cache.Cache,
	taskCount int,
) {
	keyToTask = make(map[physical.TaskCacheName]map[string]*Task)
	backends = make(map[physical.TaskCacheName]cache.Cache)

	for _, root := range p.graph.Roots() {
		_ = p.graph.Walk(root, func(task *Task) error {
			taskCount++

			taskRoot, _ := task.Fragment.Root()
			cacheNode, ok := taskRoot.(*physical.Cache)
			if !ok {
				return nil
			}

			if _, resolved := backends[cacheNode.CacheName]; !resolved {
				c, _, err := caches.GetForType(cacheNode.CacheName)
				if err != nil {
					level.Error(logger).Log(
						"msg", "failed to resolve cache",
						"cache_name", cacheNode.CacheName,
						"err", err,
					)
				}
				backends[cacheNode.CacheName] = c // store nil on failure to avoid repeated lookups
			}

			hashedKey := cache.HashKey(cacheNode.Key)
			if keyToTask[cacheNode.CacheName] == nil {
				keyToTask[cacheNode.CacheName] = make(map[string]*Task)
			}
			keyToTask[cacheNode.CacheName][hashedKey] = task

			return nil
		}, dag.PreOrderWalk)
	}

	return keyToTask, backends, taskCount
}

// nonEmptyCachedTask holds a task whose cached result is a non-empty hit along
// with the pre-fetched encoded buffer to serve to parent tasks.
type nonEmptyCachedTask struct {
	task *Task
	buf  []byte // pre-fetched encoded cache entry
}

// classifyTasksByCacheHit fetches cache keys per cache name and splits
// the results into emptyResults (zero-record hits) and nonEmptyResults (non-zero
// hits). keyToTask must only contain root-level cache nodes so that parents
// receive task-level output rather than raw within-task data.
func classifyTasksByCacheHit(
	ctx context.Context,
	keyToTask map[physical.TaskCacheName]map[string]*Task,
	backends map[physical.TaskCacheName]cache.Cache,
	logger log.Logger,
) (
	emptyResults []*Task,
	nonEmptyResults []nonEmptyCachedTask,
) {
	for cacheName, keyMap := range keyToTask {
		logger := log.With(logger, "cache_name", cacheName)
		c := backends[cacheName]
		if c == nil {
			level.Warn(logger).Log("msg", "cache backend not available")
			continue
		}

		keys := make([]string, 0, len(keyMap))
		for k := range keyMap {
			keys = append(keys, k)
		}

		found, bufs, _, err := c.Fetch(ctx, keys)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				level.Debug(logger).Log(
					"msg", "cache fetch timed out during task pruning, using partial results",
					"found", len(found),
					"err", err)
			} else {
				level.Error(logger).Log("msg", "cache fetch failed during task pruning", "err", err)
				continue
			}
		}
		for i, key := range found {
			task := keyMap[key]
			dec, err := executor.NewCacheEntryDecoder(bufs[i])
			if err != nil {
				level.Error(logger).Log("msg", "cache entry decoding failed during task pruning", "err", err)
				continue
			}

			if dec.Len() == 0 {
				emptyResults = append(emptyResults, task)
			} else {
				nonEmptyResults = append(nonEmptyResults, nonEmptyCachedTask{
					task: task,
					buf:  bufs[i],
				})
			}
		}
	}

	return emptyResults, nonEmptyResults
}

// wireCachedSources moves Sink streams of non-empty cached tasks from their
// parent tasks' Sources into CachedSources, so that parent tasks decode the
// pre-fetched buffers directly instead of waiting for a network stream from the
// child task.
func wireCachedSources(p *planner, nonEmptyResults []nonEmptyCachedTask) {
	if len(nonEmptyResults) == 0 {
		return
	}

	// Build stream ULID → pre-fetched buffer for fast lookup.
	streamToBuf := make(map[ulid.ULID][]byte, len(nonEmptyResults))
	for _, info := range nonEmptyResults {
		for _, streams := range info.task.Sinks {
			for _, s := range streams {
				streamToBuf[s.ULID] = info.buf
			}
		}
	}

	for _, root := range p.graph.Roots() {
		_ = p.graph.Walk(root, func(task *Task) error {
			for node, streams := range task.Sources {
				kept := streams[:0]
				var bufs [][]byte
				for _, s := range streams {
					if buf, ok := streamToBuf[s.ULID]; ok {
						bufs = append(bufs, buf)
					} else {
						kept = append(kept, s)
					}
				}
				if len(bufs) > 0 {
					if task.CachedSources == nil {
						task.CachedSources = make(map[physical.Node]CachedSources)
					}
					task.CachedSources[node] = append(task.CachedSources[node], bufs...)
				}
				if len(kept) == 0 {
					delete(task.Sources, node)
				} else {
					task.Sources[node] = kept
				}
			}
			return nil
		}, dag.PreOrderWalk)
	}
}

// eliminateTasks removes all tasks in initial (and any cascade parents) from the planner graph.
func eliminateTasks(p *planner, tasks []*Task) int {
	allToEliminate := make(map[*Task]struct{}, len(tasks))
	streamsToRemove := make(map[*Stream]struct{})

	// These are some convenience methods to improve the legibility of the code
	markForElimination := func(task *Task) {
		allToEliminate[task] = struct{}{}
		for _, sinkStreams := range task.Sinks {
			for _, s := range sinkStreams {
				streamsToRemove[s] = struct{}{}
			}
		}
	}
	isMarkedForElimination := func(task *Task) bool {
		_, ok := allToEliminate[task]
		return ok
	}

	// Collect the initial set of tasks and their streams.
	for _, task := range tasks {
		markForElimination(task)
	}

	// Iterate all parents across all levels of the DAG (BFS), collecting
	// all that should be removed (no remaining source streams and no CachedSources)
	parentsToCheck := make(map[*Task]struct{})
	for task := range allToEliminate {
		for _, parent := range p.graph.Parents(task) {
			if !isMarkedForElimination(parent) {
				parentsToCheck[parent] = struct{}{}
			}
		}
	}
	for len(parentsToCheck) > 0 { // While there are parents
		nextLevel := make(map[*Task]struct{}, len(parentsToCheck))

		for task := range parentsToCheck {
			// Check how many sources are left.
			remainingSources := 0
			for _, streams := range task.Sources {
				for _, s := range streams {
					if _, removed := streamsToRemove[s]; !removed {
						remainingSources++
					}
				}
			}

			// Only mark if there are no remaining sources or CachedSources left.
			if remainingSources > 0 || len(task.CachedSources) > 0 {
				continue
			}
			markForElimination(task)

			// Add all parents (not marked for deletion already) to the next level of the BFS.
			for _, gp := range p.graph.Parents(task) {
				if !isMarkedForElimination(gp) {
					nextLevel[gp] = struct{}{}
				}
			}
		}

		parentsToCheck = nextLevel
	}

	// We are done marking all tasks for deletion, now for all the parents that survived,
	// clean the sources pointing to deleted tasks.
	survivingParents := make(map[*Task]struct{})
	for task := range allToEliminate {
		for _, parent := range p.graph.Parents(task) {
			if !isMarkedForElimination(parent) {
				survivingParents[parent] = struct{}{}
			}
		}
	}
	for parent := range survivingParents {
		for node, streams := range parent.Sources {
			kept := streams[:0]
			for _, s := range streams {
				if _, removed := streamsToRemove[s]; removed {
					continue
				}
				kept = append(kept, s)
			}
			if len(kept) == 0 {
				delete(parent.Sources, node)
			} else {
				parent.Sources[node] = kept
			}
		}
	}
	for s := range streamsToRemove {
		delete(p.streamWriters, s)
	}

	// Phase 4: update the DAG in a single batch operation.
	tasksToRemove := make([]*Task, 0, len(allToEliminate))
	for task := range allToEliminate {
		tasksToRemove = append(tasksToRemove, task)
	}
	p.graph.EliminateBatch(tasksToRemove)
	return len(tasksToRemove)
}

// injectTaskCaching wraps each cacheable task fragment with a Cache node.
// A fragment is cacheable when TaskCacheKey returns a non-empty string
// (requires at least one DataObjScan or PointersScan and no non-cacheable nodes).
func injectTaskCaching(tenantID string, graph dag.Graph[*Task], maxSizeBytes uint64, compression string) error {
	for _, root := range graph.Roots() {
		if err := graph.Walk(root, func(task *Task) error {
			oldRoot, err := task.Fragment.Root()
			if err != nil {
				return err
			}
			newRoot, wrapped, err := physical.WrapWithCacheIfSupported(context.Background(), tenantID, task.Fragment, maxSizeBytes, compression)
			if err != nil {
				return err
			}
			if !wrapped {
				// The task is not cacheable, so it stays as it is.
				return nil
			}

			// Migrate Sinks from the old root to the new Cache root so the
			// workflow executor and Sprint printer find streams at the right node.
			if streams, ok := task.Sinks[oldRoot]; ok {
				task.Sinks[newRoot] = streams
				delete(task.Sinks, oldRoot)
			}
			return nil
		}, dag.PreOrderWalk); err != nil {
			return err
		}
	}
	return nil
}

// Process builds a set of tasks from a root physical plan node. Built tasks are
// added to p.graph.
func (p *planner) Process(root physical.Node) error {
	_, err := p.processNode(root, true)
	return err
}

// processNode builds a set of tasks from the given node. splitOnBreaker
// indicates whether pipeline breaker nodes should be split off into their own
// task.
//
// The resulting task is the task immediately produced by node, which callers
// can use to add edges. All tasks, including those produced in recursive calls
// to processNode, are added into p.Graph.
func (p *planner) processNode(node physical.Node, splitOnBreaker bool) (*Task, error) {
	var (
		// taskPlan is the in-progress physical plan for an individual task.
		taskPlan dag.Graph[physical.Node]

		sources = make(map[physical.Node][]*Stream)

		// childrenTasks is the slice of immediate Tasks produced by processing
		// the children of node.
		childrenTasks []*Task
	)

	// Immediately add the node to the task physical plan.
	taskPlan.Add(node)

	var (
		stack     = make(stack[physical.Node], 0, p.physical.Len())
		visited   = make(map[physical.Node]struct{}, p.physical.Len())
		nodeTasks = make(map[physical.Node][]*Task, p.physical.Len())
		timeRange physical.TimeRange
	)

	stack.Push(node)
	for stack.Len() > 0 {
		next := stack.Pop()
		if _, ok := visited[next]; ok {
			// Ignore nodes that have already been visited, which can happen
			// if a node has multiple parents.
			continue
		}
		visited[next] = struct{}{}

		for _, child := range p.physical.Children(next) {
			// NOTE(rfratto): We may have already seen child before (if it has
			// more than one parent), but we want to continue processing to
			// ensure we update sources properly and retain all relationships
			// within the same graph.

			switch {
			case splitOnBreaker && isPipelineBreaker(child):
				childTasks, found := nodeTasks[child]
				if !found {
					// Split the pipeline breaker into its own set of tasks.
					task, err := p.processNode(child, splitOnBreaker)
					if err != nil {
						return nil, err
					}
					childrenTasks = append(childrenTasks, task)
					nodeTasks[child] = append(nodeTasks[child], task)
					childTasks = nodeTasks[child]
				}

				// Create one unique stream for each child task so we can
				// receive output from them.
				for _, task := range childTasks {
					stream := &Stream{ULID: ulid.Make(), TenantID: p.tenantID}
					if err := p.addSink(task, stream); err != nil {
						return nil, err
					}
					sources[next] = append(sources[next], stream)

					// Merge in time ranges of each child
					timeRange = timeRange.Merge(task.MaxTimeRange)
				}

			case child.Type() == physical.NodeTypeParallelize:
				childTasks, found := nodeTasks[child]
				if !found {
					// Split the pipeline breaker into its own set of tasks.
					tasks, err := p.processParallelizeNode(child.(*physical.Parallelize))
					if err != nil {
						return nil, err
					}
					childrenTasks = append(childrenTasks, tasks...)
					nodeTasks[child] = append(nodeTasks[child], tasks...)
					childTasks = nodeTasks[child]
				}

				// Create one unique stream for each child task so we can
				// receive output from them.
				for _, task := range childTasks {
					stream := &Stream{ULID: ulid.Make(), TenantID: p.tenantID}
					if err := p.addSink(task, stream); err != nil {
						return nil, err
					}
					sources[next] = append(sources[next], stream)

					// Merge in time ranges of each child
					timeRange = timeRange.Merge(task.MaxTimeRange)
				}

			default:
				// Add child node into the plan (if we haven't already) and
				// retain existing edges.
				taskPlan.Add(child)
				_ = taskPlan.AddEdge(dag.Edge[physical.Node]{
					Parent: next,
					Child:  child,
				})

				stack.Push(child) // Push child for further processing.
			}
		}
	}

	fragment := physical.FromGraph(taskPlan)
	planTimeRange := fragment.CalculateMaxTimeRange()
	if !planTimeRange.IsZero() {
		timeRange = planTimeRange
	}

	// If batching is enabled, wrap every task fragment with a Batching node so all task outputs are batched.
	// Batching is enabled when the root of the original plan is a Batching node.
	if p.batchSize > 0 {
		var err error
		if fragment, err = physical.WrapWithBatching(fragment, p.batchSize); err != nil {
			return nil, fmt.Errorf("wrapping task fragment with batching: %w", err)
		}
	}

	task := &Task{
		ULID:         ulid.Make(),
		TenantID:     p.tenantID,
		Fragment:     fragment,
		Sources:      sources,
		Sinks:        make(map[physical.Node][]*Stream),
		MaxTimeRange: timeRange,
	}
	p.graph.Add(task)

	// Wire edges to children tasks and update their sinks to note sending to
	// our task.
	for _, child := range childrenTasks {
		_ = p.graph.AddEdge(dag.Edge[*Task]{
			Parent: task,
			Child:  child,
		})
	}

	return task, nil
}

// addSink adds the sink stream to the root node of the provided task. addSink
// returns an error if t has more than one root node.
func (p *planner) addSink(t *Task, sink *Stream) error {
	root, err := t.Fragment.Root()
	if err != nil {
		return err
	}

	if _, exist := p.streamWriters[sink]; exist {
		return fmt.Errorf("writer for stream %s already exists", sink.ULID)
	}
	p.streamWriters[sink] = t

	t.Sinks[root] = append(t.Sinks[root], sink)
	return nil
}

// removeSink removes the sink stream from the root node of the provided task.
// removeSink is a no-op if sink doesn't have a writer.
func (p *planner) removeSink(sink *Stream) error {
	t, exist := p.streamWriters[sink]
	if !exist {
		return nil
	}

	root, err := t.Fragment.Root()
	if err != nil {
		return err
	}

	t.Sinks[root] = slices.DeleteFunc(t.Sinks[root], func(s *Stream) bool { return s == sink })
	delete(p.streamWriters, sink)
	return nil
}

// isPipelineBreaker returns true if the node is a pipeline breaker.
func isPipelineBreaker(node physical.Node) bool {
	// TODO(rfratto): Should this information be exposed by the node itself? A
	// decision on this should wait until we're able to serialize the node over
	// the network, since that might impact how we're able to define this at the
	// node level.
	switch node.Type() {
	case physical.NodeTypeTopK, physical.NodeTypeRangeAggregation, physical.NodeTypeVectorAggregation:
		return true
	}

	return false
}

// processParallelizeNode builds a set of tasks for a Parallelize node.
func (p *planner) processParallelizeNode(node *physical.Parallelize) ([]*Task, error) {
	// Parallelize nodes are used as a marker task for splitting a branch of the
	// query plan into many distributed tasks.
	//
	// For example
	//
	//   Parallelize
	//     TopK limit=1000
	//       ScanSet
	//           @target type=ScanTypeDataObject location=object-a section_id=1
	//           @target type=ScanTypeDataObject location=object-b section_id=1
	//
	// becomes two tasks:
	//
	//    TopK limit=1000 -> DataObjScan location=object-a
	//    TopK limit=1000 -> DataObjScan location=object-b
	//
	// We handle this in a few phases:
	//
	//   1. Create a "template task" starting from the child of the Parallelize (TopK)
	//   2. Find the target node in the template task that can be split into smaller shard nodes (ScanSet)
	//   3. Create shard nodes from the target node.
	//   4. For each shard node, clone the template task and replace the target node with the shard node.
	//   5. Finally, remove the template task.
	//
	// Not all nodes can be used as a target for splitting into shards. See
	// [findShardableNode] for the full list of rules.
	roots := p.physical.Children(node)
	if len(roots) != 1 {
		return nil, errors.New("parallelize node must have exactly one child")
	}
	root := roots[0]

	// Since parallelize nodes can fan out to many smaller tasks, we don't split
	// on pipeline breakers, which would otherwise create far too many tasks
	// that do too *little* work.
	templateTask, err := p.processNode(root, false)
	if err != nil {
		return nil, err
	}

	shardableNode, err := findShardableNode(templateTask.Fragment.Graph(), root)
	if err != nil {
		return nil, err
	} else if shardableNode == nil {
		// In the case we have no node to shard (a physical planner bug?), we
		// can skip the rest of the process and treat the template task as a
		// single shard.
		return []*Task{templateTask}, nil
	}

	// Safety check:
	//
	// Our template task should currently be a root node, as we called
	// processNode ourselves and haven't wired up parents yet. This means
	// that it should have no parents, and it shouldn't have any sink
	// streams yet.
	//
	// We double-check here to ensure that the invariant holds. If this
	// invariant breaks, the loop below will be incorrect.
	switch {
	case len(p.graph.Parents(templateTask)) != 0:
		return nil, errors.New("unexpected template task with parents")
	case len(templateTask.Sinks) > 0:
		return nil, errors.New("unexpected template task with sinks")
	}

	var partitions []*Task

	for shard := range shardableNode.Shards() {
		// Create a new task for the shard.
		shardedPlan := templateTask.Fragment.Graph().Clone()
		shardedPlan.Inject(shardableNode, shard)
		shardedPlan.Eliminate(shardableNode)

		// The sources of the template task need to be replaced with new unique
		// streams.
		shardSources := make(map[physical.Node][]*Stream, len(templateTask.Sources))
		for node, templateStreams := range templateTask.Sources {
			shardStreams := make([]*Stream, 0, len(templateStreams))

			for _, templateStream := range templateStreams {
				shardStream := &Stream{ULID: ulid.Make()}
				shardStreams = append(shardStreams, shardStream)

				// Find the writer of the template stream and tell it about the
				// new stream.
				writer, ok := p.streamWriters[templateStream]
				if !ok {
					return nil, fmt.Errorf("unconnected stream %s", templateStream.ULID)
				} else if err := p.addSink(writer, shardStream); err != nil {
					return nil, err
				}
			}

			shardSources[node] = shardStreams
		}

		fragment := physical.FromGraph(*shardedPlan)
		partition := &Task{
			ULID:     ulid.Make(),
			TenantID: p.tenantID,

			Fragment: fragment,
			Sources:  shardSources,
			Sinks:    make(map[physical.Node][]*Stream),
			// Recalculate MaxTimeRange because the new injected `shard` node can cover another time range.
			MaxTimeRange: fragment.CalculateMaxTimeRange(),
		}
		p.graph.Add(partition)

		// Copy the downstream edges from the template task into our partitioned
		// task.
		for _, child := range p.graph.Children(templateTask) {
			_ = p.graph.AddEdge(dag.Edge[*Task]{
				Parent: partition,
				Child:  child,
			})
		}

		partitions = append(partitions, partition)
	}

	// Before we remove our template task, we need to unlink its streams from
	// whichever task is writing to it.
	//
	// This keeps stream alive in templateTask.Sources, but since templateTask
	// is removed before the function returns, this is safe.
	for _, streams := range templateTask.Sources {
		for _, stream := range streams {
			_ = p.removeSink(stream)
		}
	}

	p.graph.Eliminate(templateTask)
	return partitions, nil
}

// findShardableNode finds the first node in the graph that can be split into
// smaller shards. Only leaf nodes are examined.
//
// If there is no shardable leaf node reachable from root, findShardableNode
// returns nil. findShardableNode returns an error if there are two shardable
// leaf nodes.
func findShardableNode(graph *dag.Graph[physical.Node], root physical.Node) (physical.ShardableNode, error) {
	var found physical.ShardableNode

	err := graph.Walk(root, func(n physical.Node) error {
		isLeaf := len(graph.Children(n)) == 0
		if !isLeaf {
			return nil
		}

		shardable, ok := n.(physical.ShardableNode)
		if !ok {
			return nil
		} else if found != nil {
			return errors.New("multiple shardable leaf nodes found")
		}

		found = shardable
		return nil
	}, dag.PreOrderWalk)

	return found, err
}

// stack is a slice with Push and Pop operations.
type stack[E any] []E

func (s stack[E]) Len() int { return len(s) }

func (s *stack[E]) Push(e E) { *s = append(*s, e) }

func (s *stack[E]) Pop() E {
	if len(*s) == 0 {
		panic("stack is empty")
	}
	last := len(*s) - 1
	e := (*s)[last]
	*s = (*s)[:last]
	return e
}
