package builder

import (
	"context"

	"github.com/grafana/dskit/multierror"
	"golang.org/x/sync/errgroup"
)

type stage struct {
	name        string
	parallelism int
	grp         *errgroup.Group
	ctx         context.Context
	fn          func(context.Context) error
	cleanup     func(context.Context) error // optional; will be called once the underlying group returns
}

// pipeline is a sequence of n different stages.
type pipeline struct {
	ctx context.Context // base context
	// we use a separate errgroup for stage dispatch/collection
	// and inherit stage-specific groups from this ctx to
	// propagate cancellation
	grp    *errgroup.Group
	stages []stage
}

func newPipeline(ctx context.Context) *pipeline {
	stagesGrp, ctx := errgroup.WithContext(ctx)
	return &pipeline{
		ctx: ctx,
		grp: stagesGrp,
	}
}

func (p *pipeline) AddStageWithCleanup(
	name string,
	parallelism int,
	fn func(context.Context) error,
	cleanup func(context.Context) error,
) {
	grp, ctx := errgroup.WithContext(p.ctx)
	p.stages = append(p.stages, stage{
		name:        name,
		parallelism: parallelism,
		fn:          fn,
		cleanup:     cleanup,
		ctx:         ctx,
		grp:         grp,
	})
}

func (p *pipeline) AddStage(
	name string,
	parallelism int,
	fn func(context.Context) error,
) {
	p.AddStageWithCleanup(name, parallelism, fn, nil)
}

func (p *pipeline) Run() error {

	for i := range p.stages {
		// we're using this in subsequent async closures;
		// assign it directly in-loop
		s := p.stages[i]

		// spin up n workers for each stage using that stage's
		// error group.
		for j := 0; j < s.parallelism; j++ {
			s.grp.Go(func() error {
				return s.fn(s.ctx)
			})
		}

		// Using the pipeline's err group, await the stage finish,
		// calling any necessary cleanup fn
		// NB: by using the pipeline's errgroup here, we propagate
		// failures to downstream stage contexts, so once a single stage
		// fails, the others will be notified.
		p.grp.Go(func() error {
			var errs multierror.MultiError
			errs.Add(s.grp.Wait())
			if s.cleanup != nil {
				// NB: we use the pipeline's context for the cleanup call b/c
				// the stage's context is cancelled once `Wait` returns.
				// That's ok. cleanup is always called for a relevant stage
				// and just needs to know if _other_ stages failed at this point
				errs.Add(s.cleanup(p.ctx))
			}

			return errs.Err()
		})
	}

	// finish all stages
	return p.grp.Wait()
}
