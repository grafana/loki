package deletion

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/thanos-io/objstore"
	"golang.org/x/sync/errgroup"
)

// maxConcurrentDeletes limits the deletes that one sweep runs at the same
// time. The limit is per sweep, so a worker that runs several sweeps runs
// more deletes in total.
const maxConcurrentDeletes = 64

// maxLoggedDeleteFailures limits the failed deletes that one sweep logs. A
// failure of the bucket can fail every delete, and one line for each would
// flood the logs.
const maxLoggedDeleteFailures = 5

// The values of the kind label, and of the kind key in log lines.
const (
	kindLog   = "log"
	kindIndex = "index"
)

// SweepMetrics counts the deletes of the sweeps that delete their deletion
// candidates.
//
// SweepMetrics is safe for concurrent use. Create it once for each registry,
// and share it between the sweepers.
type SweepMetrics struct {
	deletedObjects *prometheus.CounterVec
	failedDeletes  *prometheus.CounterVec
}

// NewSweepMetrics creates the metrics of the sweeps and registers them with
// reg. If reg is nil, it does not register them. It panics if reg rejects a
// metric, for example one that reg already holds.
func NewSweepMetrics(reg prometheus.Registerer) *SweepMetrics {
	factory := promauto.With(reg)
	metrics := &SweepMetrics{
		deletedObjects: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "loki_dataobj_compaction_deletion_sweep_deleted_objects_total",
			Help: "Deletion candidates that deletion sweeps deleted, by kind of object. A candidate that was already gone counts as deleted.",
		}, []string{"kind"}),
		failedDeletes: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "loki_dataobj_compaction_deletion_sweep_failed_deletes_total",
			Help: "Deletion candidates that deletion sweeps could not delete, by kind of object. A sweep at least 23 hours later tries them again.",
		}, []string{"kind"}),
	}

	// Initialize each series to 0, otherwise neither the rate nor increase
	// PromQL functions detect increases from 0 to 1.
	for _, kind := range []string{kindLog, kindIndex} {
		metrics.deletedObjects.WithLabelValues(kind)
		metrics.failedDeletes.WithLabelValues(kind)
	}
	return metrics
}

// deleter deletes the deletion candidates of a sweep.
type deleter struct {
	bucket  objstore.Bucket
	metrics *SweepMetrics
}

// handle deletes every candidate, with at most maxConcurrentDeletes deletes
// at the same time. It counts a candidate that is already gone as deleted.
// It logs a summary, and the first maxLoggedDeleteFailures failed deletes.
//
// A failed delete does not stop the other deletes, and does not fail the
// sweep. The new list holds every candidate. So a sweep at least
// MinSweepInterval later tries a failed candidate again.
//
// handle returns the context error if ctx ends before all deletes finish.
func (d deleter) handle(ctx context.Context, logger log.Logger, list FileList, cands candidates) error {
	tally := &deleteTally{metrics: d.metrics, deleted: map[string]int{}, failed: map[string]int{}}
	var g errgroup.Group
	g.SetLimit(maxConcurrentDeletes)
	startDeletes := func(kind string, paths []string) {
		for _, p := range paths {
			if ctx.Err() != nil {
				return
			}
			g.Go(func() error {
				err := d.bucket.Delete(ctx, p)
				// A delete that fails because ctx ended says nothing about the
				// candidate, so handle does not count it.
				if err != nil && ctx.Err() != nil {
					return nil
				}
				if d.bucket.IsObjNotFoundErr(err) {
					err = nil
				}
				tally.record(kind, p, err)
				return nil
			})
		}
	}
	startDeletes(kindIndex, cands.indexObjects)
	startDeletes(kindLog, cands.logObjects)
	// The deletes return no error. They record their failures in tally.
	_ = g.Wait()

	tally.log(logger, list, cands)
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("deleting candidates: %w", err)
	}
	return nil
}

// deleteFailure is a candidate that a sweep could not delete.
type deleteFailure struct {
	kind string
	path string
	err  error
}

// deleteTally counts the deletes of one sweep for each kind of object. It
// keeps the first maxLoggedDeleteFailures failures to log them.
//
// deleteTally is safe for concurrent use.
type deleteTally struct {
	metrics *SweepMetrics

	resultsMu sync.Mutex
	deleted   map[string]int
	failed    map[string]int
	failures  []deleteFailure
}

// record counts the delete of path, of the given kind, that returned err.
func (t *deleteTally) record(kind, path string, err error) {
	if err == nil {
		t.metrics.deletedObjects.WithLabelValues(kind).Inc()
	} else {
		t.metrics.failedDeletes.WithLabelValues(kind).Inc()
	}

	t.resultsMu.Lock()
	defer t.resultsMu.Unlock()
	if err == nil {
		t.deleted[kind]++
		return
	}
	t.failed[kind]++
	if len(t.failures) < maxLoggedDeleteFailures {
		t.failures = append(t.failures, deleteFailure{kind: kind, path: path, err: err})
	}
}

// log logs a summary of the deletes, and one line for each kept failure.
func (t *deleteTally) log(logger log.Logger, list FileList, cands candidates) {
	t.resultsMu.Lock()
	defer t.resultsMu.Unlock()
	// Log the summary at warn level if a delete failed, so that an operator
	// finds it among the failure lines.
	logAt := level.Info
	if len(t.failures) > 0 {
		logAt = level.Warn
	}
	logAt(logger).Log(
		"msg", "deletion sweep deleted candidates",
		"previous_list_created_at", cands.previousCreatedAt.Format(time.RFC3339),
		"list_created_at", list.CreatedAt.Format(time.RFC3339),
		"candidates", cands.count(),
		"deleted_log_objects", t.deleted[kindLog],
		"deleted_index_objects", t.deleted[kindIndex],
		"failed_log_objects", t.failed[kindLog],
		"failed_index_objects", t.failed[kindIndex],
	)
	for _, failure := range t.failures {
		level.Warn(logger).Log("msg", "deletion sweep cannot delete candidate", "kind", failure.kind, "path", failure.path, "err", failure.err)
	}
}
