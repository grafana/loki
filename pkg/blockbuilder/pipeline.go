package blockbuilder

import (
	"context"

	"github.com/grafana/dskit/multierror"
	"golang.org/x/sync/errgroup"
)

type stage struct {
	parallelism int
	grp         *errgroup.Group
	ctx         context.Context
	fn          func(context.Context) error
	cleanup     func() error // optional; will be called once the underlying group returns
}

// pipeline is a sequence of n different stages.
type pipeline struct {
	ctx    context.Context // base context
	stages []stage
}

func newPipeline(ctx context.Context) *pipeline {
	return &pipeline{
		ctx: ctx,
	}
}

func (p *pipeline) AddStageWithCleanup(
	parallelism int,
	fn func(context.Context) error,
	cleanup func() error,
) {
	grp, ctx := errgroup.WithContext(p.ctx)
	p.stages = append(p.stages, stage{
		parallelism: parallelism,
		fn:          fn,
		cleanup:     cleanup,
		ctx:         ctx,
		grp:         grp,
	})
}

func (p *pipeline) AddStage(
	parallelism int,
	fn func(context.Context) error,
) {
	p.AddStageWithCleanup(parallelism, fn, nil)
}

func (p *pipeline) Run() error {
	var errs multierror.MultiError

	// begin all stages
	for _, s := range p.stages {
		for i := 0; i < s.parallelism; i++ {
			s.grp.Go(func() error {
				return s.fn(s.ctx)
			})
		}
	}

	// finish all stages
	for _, s := range p.stages {
		if err := s.grp.Wait(); err != nil {
			errs.Add(err)
		}
		if s.cleanup != nil {
			errs.Add(s.cleanup())
		}
	}

	return errs.Err()
}
