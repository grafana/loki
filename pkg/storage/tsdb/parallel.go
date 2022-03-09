package tsdb

import (
	"context"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
)

type BoundedParallelism struct {
	parallelism chan struct{}
}

func NewBoundedParallelism(parallelism int) *BoundedParallelism {
	ch := make(chan struct{}, parallelism)

	// seed the channel
	for i := 0; i < parallelism; i++ {
		ch <- struct{}{}
	}

	return &BoundedParallelism{parallelism: ch}
}

func (p *BoundedParallelism) Run(ctx context.Context, fn func()) error {
	select {
	case <-ctx.Done():
	case <-p.parallelism:
		fn()
		p.parallelism <- struct{}{}
	}
	return ctx.Err()
}

// ParallelIndex wraps an index and ensures it respects a maximum parallelization factor.
type ParallelIndex struct {
	p *BoundedParallelism
	Index
}

func NewParallelIndex(p *BoundedParallelism, i Index) *ParallelIndex {
	return &ParallelIndex{
		p:     p,
		Index: i,
	}
}

func (i *ParallelIndex) GetChunkRefs(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) (res []ChunkRef, err error) {
	if err := i.p.Run(ctx, func() {
		res, err = i.Index.GetChunkRefs(ctx, userID, from, through, matchers...)
	}); err != nil {
		return nil, err
	}

	return
}

func (i *ParallelIndex) Series(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) (res []Series, err error) {
	if err := i.p.Run(ctx, func() {
		res, err = i.Index.Series(ctx, userID, from, through, matchers...)
	}); err != nil {
		return nil, err
	}

	return
}

func (i *ParallelIndex) LabelNames(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) (res []string, err error) {
	if err := i.p.Run(ctx, func() {
		res, err = i.Index.LabelNames(ctx, userID, from, through, matchers...)
	}); err != nil {
		return nil, err
	}
	return
}

func (i *ParallelIndex) LabelValues(ctx context.Context, userID string, from, through model.Time, name string, matchers ...*labels.Matcher) (res []string, err error) {
	if err := i.p.Run(ctx, func() {
		res, err = i.Index.LabelValues(ctx, userID, from, through, name, matchers...)
	}); err != nil {
		return nil, err
	}
	return
}
