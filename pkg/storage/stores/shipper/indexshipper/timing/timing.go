// Package timing carries shard-planning histogram observers through index calls.
package timing

import (
	"context"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Phase identifies a lower-level shard-planning operation.
type Phase string

const (
	// ReadyWait measures waiting for index initialization, excluding mutex acquisition.
	ReadyWait Phase = "ready_wait"
	// IndexScan measures one file's postings lookup and series/chunk traversal.
	IndexScan Phase = "index_scan"
	// LockWait measures index-set read-lock acquisition after initialization.
	LockWait Phase = "lock_wait"
	// DispatchWait measures bounded worker submission, overlapping running workers.
	DispatchWait Phase = "dispatch_wait"
	// Table measures a table visit through completion of its workers, including waits.
	Table Phase = "table"
	// Merge measures cross-file deduplication and sorting when multiple results exist.
	Merge Phase = "merge"
)

// Stats summarizes work completed by a lookup, including partial work on errors.
// Files counts scan invocations, not distinct filenames. Tables counts table visits.
// Refs counts file-scan results before cross-file deduplication.
type Stats struct {
	Tables, Files, Refs int64
	ScanTotal, ScanMax  time.Duration
}

type contextKey struct{}
type observers struct {
	phases *prometheus.HistogramVec
	mu     sync.Mutex
	stats  Stats
}

// WithObservers enables lower-level measurements and concurrent lookup accounting.
func WithObservers(ctx context.Context, phases *prometheus.HistogramVec) context.Context {
	return context.WithValue(ctx, contextKey{}, &observers{phases: phases})
}

// Snapshot returns completed lookup work. Call after all lookup workers return.
func Snapshot(ctx context.Context) Stats {
	obs, ok := ctx.Value(contextKey{}).(*observers)
	if !ok {
		return Stats{}
	}
	obs.mu.Lock()
	defer obs.mu.Unlock()
	return obs.stats
}

// AddRefs records file-scan results, before any cross-file merging.
func AddRefs(ctx context.Context, n int) {
	if obs, ok := ctx.Value(contextKey{}).(*observers); ok {
		obs.mu.Lock()
		obs.stats.Refs += int64(n)
		obs.mu.Unlock()
	}
}

// Track returns a completion function to call once around an operation.
// Calls outside shard planning do not read the clock or update these metrics.
func Track(ctx context.Context, phase Phase) func() {
	obs, ok := ctx.Value(contextKey{}).(*observers)
	if !ok {
		return func() {}
	}
	start := time.Now()
	return func() {
		elapsed := time.Since(start)
		obs.phases.WithLabelValues(string(phase)).Observe(elapsed.Seconds())
		if phase == IndexScan || phase == Table {
			obs.mu.Lock()
			if phase == IndexScan {
				obs.stats.Files++
				obs.stats.ScanTotal += elapsed
				obs.stats.ScanMax = max(obs.stats.ScanMax, elapsed)
			} else {
				obs.stats.Tables++
			}
			obs.mu.Unlock()
		}
	}
}
