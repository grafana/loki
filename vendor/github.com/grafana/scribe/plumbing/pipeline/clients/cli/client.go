package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/grafana/scribe/plumbing/pipeline"
	"github.com/grafana/scribe/plumbing/syncutil"
	"github.com/grafana/scribe/plumbing/wrappers"
	"github.com/sirupsen/logrus"
)

var (
	ErrorCLIStepHasImage = errors.New("step has a docker image specified. This may cause unexpected results if ran in CLI mode. The `-mode=docker` flag is likely more suitable")
)

// PipelineWalkFunc walks through the pipelines that the collection provides. Each pipeline is a pipeline of steps, so each will walk through the list of steps using the StepWalkFunc.
func (c *Client) PipelineWalkFunc(w pipeline.Walker, wf pipeline.StepWalkFunc) func(context.Context, ...pipeline.Pipeline) error {
	return func(ctx context.Context, pipelines ...pipeline.Pipeline) error {
		log := c.Log
		log.Debugln("Running pipeline(s)", pipeline.PipelineNames(pipelines))

		var (
			wg = syncutil.NewPipelineWaitGroup()
		)

		// These pipelines run in parallel, but must all complete before continuing on to the next set.
		for i, v := range pipelines {
			// If this is a sub-pipeline, then run these steps without waiting on other pipeliens to complete.
			// However, if a sub-pipeline returns an error, then we shoud(?) stop.
			// TODO: This is probably not true... if a sub-pipeline stops then users will probably want to take note of it, but let the rest of the pipeline continue.
			// and see a report of the failure towards the end of the execution.
			if v.Type == pipeline.PipelineTypeSub {
				go func(ctx context.Context, p pipeline.Pipeline) {
					if err := c.runPipeline(ctx, w, wf, p); err != nil {
						c.Log.WithError(err).Errorln("sub-pipeline failed")
					}
				}(ctx, pipelines[i])
				continue
			}

			// Otherwise, add this pipeline to the set that needs to complete before moving on to the next set of pipelines.
			wg.Add(pipelines[i], w, wf)
		}

		if err := wg.Wait(ctx); err != nil {
			return err
		}

		return nil
	}
}

// StepWalkFunc walks through the steps that the collection provides.
func (c *Client) StepWalkFunc(ctx context.Context, steps ...pipeline.Step) error {
	if err := c.runSteps(ctx, steps); err != nil {
		return err
	}

	return nil
}

// The Client is used when interacting with a scribe pipeline using the scribe CLI.
// In order to emulate what happens in a remote environment, the steps are put into a queue before being ran.
type Client struct {
	Opts pipeline.CommonOpts
	Log  *logrus.Logger
}

func (c *Client) Validate(step pipeline.Step) error {
	if step.Image != "" {
		c.Log.Debugln(fmt.Sprintf("[%s]", step.Name), ErrorCLIStepHasImage.Error())
	}

	return nil
}

func (c *Client) HandleEvents(events []pipeline.Event) error {
	return nil
}

func (c *Client) Done(ctx context.Context, w pipeline.Walker) error {
	// if err := c.HandleEvents(events); err != nil {
	// 	return err
	// }

	logWrapper := &wrappers.LogWrapper{
		Opts: c.Opts,
		Log:  c.Log,
	}

	traceWrapper := &wrappers.TraceWrapper{
		Opts:   c.Opts,
		Tracer: c.Opts.Tracer,
	}

	// Because these wrappers wrap the actions of each step, the first wrapper typically runs first.
	stepWalkFunc := traceWrapper.Wrap(c.StepWalkFunc)
	stepWalkFunc = logWrapper.Wrap(stepWalkFunc)

	pipelineWalkFunc := c.PipelineWalkFunc(w, stepWalkFunc)

	return w.WalkPipelines(ctx, pipelineWalkFunc)
}

func (c *Client) runPipeline(ctx context.Context, w pipeline.Walker, wf pipeline.StepWalkFunc, p pipeline.Pipeline) error {
	return w.WalkSteps(ctx, p.ID, wf)
}

func (c *Client) prepopulateState(state *pipeline.State) error {
	log := c.Log
	for k, v := range KnownValues {
		exists, err := state.Exists(k)
		if err != nil {
			// Even if we encounter an error, we still want to attempt to set the state.
			// One error that could happen here is if the state is empty.
			log.WithError(err).Debugln("Failed to read state")
		}

		if !exists {
			log.Debugln("State not found for", k.Key, "preopulating value")
			if err := v(state); err != nil {
				log.WithError(err).Debugln("Failed to pre-populate state for argument", k.Key)
			}
		}
	}

	return nil
}

func (c *Client) runSteps(ctx context.Context, steps []pipeline.Step) error {
	c.Log.Debugln("Running steps in parallel:", len(steps))

	var (
		wg = syncutil.NewStepWaitGroup()
	)
	state := c.Opts.State

	if err := c.prepopulateState(state); err != nil {
		c.Log.WithError(err).Debugln("Error encountered when pre-populating state...")
	}

	for _, v := range steps {
		log := c.Log.WithField("step", v.Name)
		wg.Add(v, pipeline.ActionOpts{
			Path:    c.Opts.Args.Path,
			State:   c.Opts.State,
			Tracer:  c.Opts.Tracer,
			Version: c.Opts.Version,
			Logger:  log,
		})
	}

	// If we wanted to allow users to configure a timeout, here would be the place. To configure a time out for the list of steps, use context.WithTimeout.
	if err := wg.Wait(ctx); err != nil {
		return fmt.Errorf("Error waiting for steps (%s) to complete: %w", pipeline.StepNames(steps), err)
	}

	return nil
}
