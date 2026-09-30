package logsobj

import (
	"context"
)

// A SizedBuilderPool implements a fixed-size pool of builders.
type SizedBuilderPool struct {
	builders chan *Builder
}

// NewSizedBuilderPool returns a new SizedBuilderPool.
func NewSizedBuilderPool(builders []*Builder) *SizedBuilderPool {
	p := &SizedBuilderPool{builders: make(chan *Builder, len(builders))}
	for _, b := range builders {
		p.builders <- b
	}
	return p
}

// Get returns the next builder in the pool. If there are no builders available,
// because the pool is currently empty, it returns nil instead.
func (p *SizedBuilderPool) Get() *Builder {
	select {
	case res := <-p.builders:
		return res
	default:
		return nil
	}
}

// Wait returns the next builder in the pool. If there are no builders available,
// because the pool is currently empty, it blocks until either a builder becomes
// available or the context is canceled.
func (p *SizedBuilderPool) Wait(ctx context.Context) (*Builder, error) {
	select {
	case res := <-p.builders:
		return res, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Put returns the builder to the pool.
func (p *SizedBuilderPool) Put(b *Builder) {
	p.builders <- b
}
