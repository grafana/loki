package distributor

import (
	"context"
)

// Tee implementations can duplicate the log streams to another endpoint.
type Tee interface {
	Duplicate(ctx context.Context, tenant string, streams []KeyedStream, pushTracker *PushTracker)

	// Register is a prehook to allow Tee's to register its pending streams, allowing distributors to wait for them before concluding a push request.
	// If pending streams are registered, one should make sure `pushTracker.doneWithResult` is invoked for the same number of streams added.
	Register(ctx context.Context, tenant string, streams []KeyedStream, pushTracker *PushTracker)
}

// WrapTee wraps a new Tee around an existing Tee.
func WrapTee(existing, newTee Tee) Tee {
	if existing == nil {
		return newTee
	}
	if multi, ok := existing.(*multiTee); ok {
		return &multiTee{append(multi.tees, newTee)}
	}
	return &multiTee{tees: []Tee{existing, newTee}}
}

type multiTee struct {
	tees []Tee
}

func (m *multiTee) Duplicate(ctx context.Context, tenant string, streams []KeyedStream, pushTracker *PushTracker) {
	for _, tee := range m.tees {
		tee.Duplicate(ctx, tenant, streams, pushTracker)
	}
}

func (m *multiTee) Register(ctx context.Context, tenant string, streams []KeyedStream, pushTracker *PushTracker) {
	for _, tee := range m.tees {
		tee.Register(ctx, tenant, streams, pushTracker)
	}
}
