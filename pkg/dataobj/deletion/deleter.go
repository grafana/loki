package deletion

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/thanos-io/objstore"
	"golang.org/x/sync/errgroup"
)

// maxConcurrentDeletes limits the deletes that one DeleteObjects call runs at
// the same time. The limit is per call, so a worker that runs several sweeps
// runs more deletes in total.
const maxConcurrentDeletes = 64

// maxLoggedDeleteFailures limits the failed deletes that one DeleteObjects
// call logs. A failure of the bucket can fail every delete, and one line for
// each would flood the logs.
const maxLoggedDeleteFailures = 5

// The values of the kind label, and of the kind key in log lines.
const (
	kindLog   = "log"
	kindIndex = "index"
)

// SweepMetrics counts the deletes of DeleteObjects.
//
// SweepMetrics is safe for concurrent use. Create it once for each registry,
// and share it between the DeleteObjects calls.
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

// DeleteObjects deletes paths from bucket, with at most maxConcurrentDeletes
// deletes at the same time. paths are relative to the bucket root, for
// example the SweepResult.Candidates of a sweep. DeleteObjects counts the
// deletes in metrics by kind of object.
//
// DeleteObjects counts a path that is already gone as deleted. It logs a
// summary, and the first maxLoggedDeleteFailures failed deletes. It logs
// nothing if paths is empty, because most sweeps skip and return no paths.
// It adds no tenant to the log lines, so logger must carry it.
//
// A failed delete does not stop the other deletes, and does not make
// DeleteObjects fail. The stored file list holds every candidate of a sweep.
// So a sweep at least MinSweepInterval later returns a failed candidate
// again.
//
// DeleteObjects returns an error, and deletes nothing, if metrics is nil. It
// returns the context error if ctx ends before all deletes finish. It does
// not count a delete that fails because ctx ended.
func DeleteObjects(ctx context.Context, bucket objstore.Bucket, paths []string, metrics *SweepMetrics, logger log.Logger) error {
	if metrics == nil {
		return errors.New("deleting objects: metrics are required")
	}
	if len(paths) == 0 {
		return nil
	}
	tally := &deleteTally{metrics: metrics, deleted: map[string]int{}, failed: map[string]int{}}
	var g errgroup.Group
	g.SetLimit(maxConcurrentDeletes)
	for _, p := range paths {
		if ctx.Err() != nil {
			break
		}
		g.Go(func() error {
			err := bucket.Delete(ctx, p)
			// A delete that fails because ctx ended says nothing about the
			// object, so DeleteObjects does not count it.
			if err != nil && ctx.Err() != nil {
				return nil
			}
			if bucket.IsObjNotFoundErr(err) {
				err = nil
			}
			tally.record(p, err)
			return nil
		})
	}
	// The deletes return no error. They record their failures in tally.
	_ = g.Wait()

	tally.log(logger, len(paths))
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("deleting objects: %w", err)
	}
	return nil
}

// deleteFailure is an object that DeleteObjects could not delete.
type deleteFailure struct {
	kind string
	path string
	err  error
}

// deleteTally counts the deletes of one DeleteObjects call for each kind of
// object. It keeps the first maxLoggedDeleteFailures failures to log them.
//
// deleteTally is safe for concurrent use.
type deleteTally struct {
	metrics *SweepMetrics

	resultsMu sync.Mutex
	deleted   map[string]int
	failed    map[string]int
	failures  []deleteFailure
}

// record counts the delete of path that returned err.
func (t *deleteTally) record(path string, err error) {
	kind := kindLog
	if isIndexObject(path) {
		kind = kindIndex
	}
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

// log logs a summary of the deletes of the given number of paths, and one
// line for each kept failure.
func (t *deleteTally) log(logger log.Logger, paths int) {
	t.resultsMu.Lock()
	defer t.resultsMu.Unlock()
	// Log the summary at warn level if a delete failed, so that an operator
	// finds it among the failure lines.
	logAt := level.Info
	if len(t.failures) > 0 {
		logAt = level.Warn
	}
	logAt(logger).Log(
		"msg", fmt.Sprintf("deletion sweep deleted candidates (logs max %d failures)", maxLoggedDeleteFailures),
		"candidates", paths,
		"deleted_log_objects", t.deleted[kindLog],
		"deleted_index_objects", t.deleted[kindIndex],
		"failed_log_objects", t.failed[kindLog],
		"failed_index_objects", t.failed[kindIndex],
	)
	for _, failure := range t.failures {
		level.Warn(logger).Log("msg", "deletion sweep cannot delete candidate", "kind", failure.kind, "path", failure.path, "err", failure.err)
	}
}
