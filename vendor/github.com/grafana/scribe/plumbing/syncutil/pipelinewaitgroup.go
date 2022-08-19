package syncutil

import (
	"context"

	"github.com/grafana/scribe/plumbing/pipeline"
)

// PipelineWaitGroup is a wrapper around a WaitGroup that runs the actions of a list of steps, handles errors, and watches for context cancellation.
type PipelineWaitGroup struct {
	wg *WaitGroup
}

// Add adds a new Action to the waitgroup. The provided function will be run in parallel with all other added functions.
func (w *PipelineWaitGroup) Add(f pipeline.Pipeline, walker pipeline.Walker, wf pipeline.StepWalkFunc) {
	w.wg.Add(func(ctx context.Context) error {
		return walker.WalkSteps(ctx, f.ID, wf)
	})
}

// Wait runs all provided functions (via Add(...)) and runs them in parallel and waits for them to finish.
// If they are not all finished before the provided timeout (via NewPipelineWaitGroup), then an error is returned.
// If any functions return an error, the first error encountered is returned.
func (w *PipelineWaitGroup) Wait(ctx context.Context) error {
	return w.wg.Wait(ctx)
}

func NewPipelineWaitGroup() *PipelineWaitGroup {
	return &PipelineWaitGroup{
		wg: NewWaitGroup(),
	}
}
