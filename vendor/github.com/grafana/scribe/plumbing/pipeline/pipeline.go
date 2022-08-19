package pipeline

import "github.com/grafana/scribe/plumbing/pipeline/dag"

// A Pipeline is really similar to a Step, except that it contains a graph of steps rather than
// a single action. Just like a Step, it has dependencies, a name, an ID, etc.
type Pipeline struct {
	ID           int64
	Name         string
	Graph        *dag.Graph[StepList]
	Events       []Event
	Type         PipelineType
	Dependencies []Pipeline
}

// New creates a new Step that represents a pipeline.
func New(name string, id int64) Pipeline {
	graph := dag.New[StepList]()
	graph.AddNode(0, StepList{})
	return Pipeline{
		Name:  name,
		ID:    id,
		Graph: graph,
	}
}

func PipelineNames(s []Pipeline) []string {
	v := make([]string, len(s))
	for i := range s {
		v[i] = s[i].Name
	}

	return v
}
