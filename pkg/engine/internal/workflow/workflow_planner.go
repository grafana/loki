package workflow

import (
	"errors"
	"fmt"
	"slices"

	"github.com/oklog/ulid/v2"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
)

// planner is responsible for constructing the Task graph held by a [Workflow].
type planner struct {
	tenantID string
	graph    dag.Graph[*Task]
	physical *physical.Plan

	streamWriters map[*Stream]*Task // Lookup of stream to which task writes to it
}

// planWorkflow partitions a physical plan into a graph of tasks.
//
// planWorkflow returns an error if the provided physical plan does not
// have exactly one root node, or if the physical plan cannot be partitioned.
func planWorkflow(tenantID string, plan *physical.Plan) (dag.Graph[*Task], error) {
	root, err := plan.Root()
	if err != nil {
		return dag.Graph[*Task]{}, err
	}

	planner := &planner{
		tenantID: tenantID,
		physical: plan,

		streamWriters: make(map[*Stream]*Task),
	}
	if err := planner.Process(root); err != nil {
		return dag.Graph[*Task]{}, err
	}

	return planner.graph, nil
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
