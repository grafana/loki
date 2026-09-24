// Package timing carries shard-planning histogram observers through index calls.
package timing

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Phase identifies the two lower-level request phases measured by this package.
type Phase int

const (
	// ReadyWait measures waiting for an index set to finish initialization.
	ReadyWait Phase = iota
	// IndexScan measures a file's postings lookup and series/chunk traversal.
	IndexScan
)

type contextKey struct{}
type observers [2]prometheus.Observer

// WithObservers enables lower-level measurements for a shard-planning request.
func WithObservers(ctx context.Context, readyWait, indexScan prometheus.Observer) context.Context {
	return context.WithValue(ctx, contextKey{}, observers{readyWait, indexScan})
}

// Track returns a completion function to defer around a phase invocation.
// Calls outside shard planning do not read the clock or update these metrics.
func Track(ctx context.Context, phase Phase) func() {
	obs, ok := ctx.Value(contextKey{}).(observers)
	if !ok {
		return func() {}
	}
	start := time.Now()
	return func() { obs[phase].Observe(time.Since(start).Seconds()) }
}
