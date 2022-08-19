package pipeline

import (
	"context"
	"errors"
	"fmt"

	"github.com/grafana/scribe/plumbing/pipeline/dag"
)

var (
	ErrorNoSteps = errors.New("no steps were provided")
)

// AddStep adds the steps as a single node in the pipeline.
func (p *Pipeline) AddSteps(id int64, steps StepList) error {
	if err := p.Graph.AddNode(id, steps); err != nil {
		return err
	}

	return nil
}

func nodeID(steps []Step) int64 {
	return steps[len(steps)-1].ID
}

// WalkFunc is implemented by the pipeline 'Clients'. This function is executed for each step.
// If multiple steps are provided in the argument, then they were provided in "Parallel".
// If one step in the list of steps is of type "Background", then they all should be.
type StepWalkFunc func(context.Context, ...Step) error

// PipelineWalkFunc is implemented by the pipeline 'Clients'. This function is executed for each pipeline.
// This function follows the same rules for pipelines as the StepWalker func does for pipelines. If multiple pipelines are provided in the steps argument,
// then those pipelines are intended to be executed in parallel.
type PipelineWalkFunc func(context.Context, ...Pipeline) error

// Walker is an interface that collections of steps should satisfy.
type Walker interface {
	WalkSteps(context.Context, int64, StepWalkFunc) error
	WalkPipelines(context.Context, PipelineWalkFunc) error
}

func StepIDs(steps []Step) []int64 {
	ids := make([]int64, len(steps))
	for i, v := range steps {
		ids[i] = v.ID
	}

	return ids
}

// Collection defines a directed acyclic Graph that stores a collection of Steps.
type Collection struct {
	Graph *dag.Graph[Pipeline]
}

func withoutBackgroundSteps(steps []Step) []Step {
	s := []Step{}

	for i, v := range steps {
		if v.Type != StepTypeBackground {
			s = append(s, steps[i])
		}
	}

	return s
}

// NodeListToSteps converts a list of Nodes to a list of Steps
func NodesToSteps(nodes []dag.Node[Step]) []Step {
	steps := make([]Step, len(nodes))

	for i, v := range nodes {
		steps[i] = v.Value
	}

	return steps
}

// AdjNodesToPipelines converts a list of Nodes (with type Pipeline) to a list of Pipelines
func AdjNodesToPipelines(nodes []*dag.Node[Pipeline]) []Pipeline {
	pipelines := make([]Pipeline, len(nodes))

	for i, v := range nodes {
		pipelines[i] = v.Value
	}

	return pipelines
}

// NodesToPipelines converts a list of Nodes (with type Pipeline) to a list of Pipelines
func NodesToPipelines(nodes []dag.Node[Pipeline]) []Pipeline {
	pipelines := make([]Pipeline, len(nodes))

	for i, v := range nodes {
		pipelines[i] = v.Value
	}

	return pipelines
}

// NodeListToSteps converts a list of Nodes to a list of Steps
func NodeListToStepLists(nodes []dag.Node[StepList]) []StepList {
	steps := make([]StepList, len(nodes))

	for i, v := range nodes {
		steps[i] = v.Value
	}

	return steps
}

func pipelinesEqual(a, b []Pipeline) bool {
	if len(a) != len(b) {
		return false
	}

	for i, v := range a {
		if b[i].ID != v.ID {
			return false
		}
	}

	return true
}

// stepVisitFunc returns a dag.VisitFunc that popules the provided list of `steps` with the order that they should be ran.
func (c *Collection) stepVisitFunc(ctx context.Context, wf StepWalkFunc) dag.VisitFunc[StepList] {
	return func(n *dag.Node[StepList]) error {
		if n.ID == 0 {
			return nil
		}

		list := n.Value

		// Because every group of steps run in parallel, they all share dependencies.
		// Those dependencies however should not be the single ID that represents the group,
		// but all of the steps that are contained within the group.
		deps := []Step{}
		for _, step := range list.Dependencies {
			deps = append(deps, step.Steps...)
		}

		for i := range list.Steps {
			list.Steps[i].Dependencies = deps
		}
		return wf(ctx, list.Steps...)
	}
}

func (c *Collection) WalkSteps(ctx context.Context, pipelineID int64, wf StepWalkFunc) error {
	node, err := c.Graph.Node(pipelineID)
	if err != nil {
		return fmt.Errorf("could not find pipeline '%d'. %w", pipelineID, err)
	}

	pipeline := node.Value

	if err := pipeline.Graph.BreadthFirstSearch(0, c.stepVisitFunc(ctx, wf)); err != nil {
		return err
	}

	return nil
}

func (c *Collection) AddEvents(pipelineID int64, events ...Event) error {
	node, err := c.Graph.Node(pipelineID)
	if err != nil {
		return err
	}

	pipeline := node.Value
	pipeline.Events = append(node.Value.Events, events...)
	node.Value = pipeline
	return nil
}

// stepVisitFunc returns a dag.VisitFunc that popules the provided list of `steps` with the order that they should be ran.
func (c *Collection) pipelineVisitFunc(ctx context.Context, wf PipelineWalkFunc) dag.VisitFunc[Pipeline] {
	var (
		adj  = []Pipeline{}
		next = []Pipeline{}
	)

	return func(n *dag.Node[Pipeline]) error {
		if n.ID == 0 {
			adj = AdjNodesToPipelines(c.Graph.Adj(0))
			return nil
		}

		next = append(next, n.Value)

		if pipelinesEqual(adj, next) {
			if err := wf(ctx, next...); err != nil {
				return err
			}

			adj = AdjNodesToPipelines(c.Graph.Adj(n.ID))
			next = []Pipeline{}
		}

		return nil
	}
}

func (c *Collection) WalkPipelines(ctx context.Context, wf PipelineWalkFunc) error {
	if err := c.Graph.BreadthFirstSearch(0, c.pipelineVisitFunc(ctx, wf)); err != nil {
		return err
	}
	return nil
}

// Add adds a new list of Steps which are siblings to a pipeline.
// Because they are siblings, they must all depend on the same step(s).
func (c *Collection) AddSteps(pipelineID int64, steps StepList) error {
	// Find the pipeline in our Graph of pipelines
	v, err := c.Graph.Node(pipelineID)
	if err != nil {
		return fmt.Errorf("error getting pipeline graph: %w", err)
	}
	pipeline := v.Value

	if err := pipeline.AddSteps(steps.ID, steps); err != nil {
		return fmt.Errorf("error adding steps to pipeline graph: %w", err)
	}

	// Background steps should only have an edge from the root node. This is automatically added as Background Steps do not have dependencies.
	// Because Backgorund steps are intended to persist until the pipeline terminates, they can't have child steps.
	if len(steps.Dependencies) == 0 {
		pipeline.Graph.AddEdge(0, steps.ID)
	}

	if steps.Type == StepTypeBackground {
		return nil
	}

	for _, parent := range steps.Dependencies {
		if err := pipeline.Graph.AddEdge(parent.ID, steps.ID); err != nil {
			return fmt.Errorf("error adding edges to pipeline graph: %w", err)
		}
	}

	return nil
}

func (c *Collection) addPipeline(p Pipeline) error {
	if err := c.Graph.AddNode(p.ID, p); err != nil {
		return fmt.Errorf("error adding new pipeline to graph: %w", err)
	}

	if len(p.Dependencies) == 0 {
		if err := c.Graph.AddEdge(0, p.ID); err != nil {
			return err
		}
	}

	for _, v := range p.Dependencies {
		if err := c.Graph.AddEdge(v.ID, p.ID); err != nil {
			return err
		}
	}

	return nil
}

// AppendPipeline adds a populated sub-pipeline of Steps to the Graph.
func (c *Collection) AddPipelines(p ...Pipeline) error {
	for _, v := range p {
		if err := c.addPipeline(v); err != nil {
			return err
		}
	}
	return nil
}

// ByID should return the Step that corresponds with a specific ID
func (c *Collection) ByID(ctx context.Context, id int64) ([]Step, error) {
	steps := []Step{}

	// Search every pipeline and step for the listed IDs
	if err := c.WalkPipelines(ctx, func(ctx context.Context, pipelines ...Pipeline) error {
		for _, pipeline := range pipelines {
			return c.WalkSteps(ctx, pipeline.ID, func(ctx context.Context, s ...Step) error {
				for i, step := range s {
					if step.ID == id {
						steps = []Step{s[i]}
						return dag.ErrorBreak
					}
				}
				return nil
			})
		}

		return nil
	}); err != nil {
		return nil, err
	}

	if len(steps) == 0 {
		return nil, errors.New("no step found")
	}

	return steps, nil
}

// ByName should return the Step that corresponds with a specific Name
func (c *Collection) ByName(ctx context.Context, name string) ([]Step, error) {
	steps := []Step{}

	// Search every pipeline and step for the listed IDs
	if err := c.WalkPipelines(ctx, func(ctx context.Context, pipelines ...Pipeline) error {
		for _, pipeline := range pipelines {
			if pipeline.Name == name {
				// uhhh.. todo
				continue
			}

			return c.WalkSteps(ctx, pipeline.ID, func(ctx context.Context, s ...Step) error {
				for i, step := range s {
					if step.Name == name {
						steps = []Step{s[i]}
						return dag.ErrorBreak
					}
				}
				return nil
			})
		}

		return nil
	}); err != nil {
		return nil, err
	}

	return steps, nil
}

// PipelinesByName should return the Pipelines that corresponds with a specified names
func (c *Collection) PipelinesByName(ctx context.Context, names []string) ([]Pipeline, error) {
	var (
		retP  = make([]Pipeline, len(names))
		found = false
	)

	// Search every pipeline for the listed names
	if err := c.WalkPipelines(ctx, func(ctx context.Context, pipelines ...Pipeline) error {
		for i, argPipeline := range names {
			fmt.Println("looking names: ", argPipeline)
			// fmt.Println("overall pipelines: ", )
			for _, pipeline := range pipelines {
				fmt.Println("current pipeline: ", pipeline)
				if pipeline.Name == argPipeline {
					pipeline.Dependencies = []Pipeline{}
					retP[i] = pipeline
					fmt.Println("adding pipeline: ", pipeline)
					found = true
					break
				}
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}

	if !found {
		return nil, errors.New("no matching pipelines found")
	}
	return retP, nil
}

// NewCollectinoWithSteps creates a new Collection with a single pipeline from a list of Steps.
func NewCollectionWithSteps(pipelineName string, steps ...StepList) (*Collection, error) {
	var (
		col       = NewCollection()
		id  int64 = 1
	)
	if err := col.AddPipelines(New(pipelineName, id)); err != nil {
		return nil, err
	}

	for i := range steps {
		if err := col.AddSteps(id, steps[i]); err != nil {
			return nil, err
		}
	}

	return col, nil
}

func NewCollection() *Collection {
	graph := dag.New[Pipeline]()
	graph.AddNode(0, New("default", 0))
	return &Collection{
		Graph: graph,
	}
}
