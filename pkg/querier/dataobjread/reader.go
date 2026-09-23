package dataobjread

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime/debug"
	"slices"
	"sync"
	"time"

	"github.com/go-kit/log/level"
	"github.com/prometheus/prometheus/model/labels"
	"go.uber.org/atomic"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	util_server "github.com/grafana/loki/v3/pkg/util/server"
	"github.com/grafana/loki/v3/pkg/xcap"
)

const (
	// DefaultMaxConcurrency is how many logs sections the reader scans at once.
	DefaultMaxConcurrency = 16

	// DefaultReadBatchSize is how many records one section read decodes and forwards at a time.
	DefaultReadBatchSize = 1024
)

// LogRecord is one decoded log line with the identity the sample layer needs.
type LogRecord struct {
	streamHash   uint64
	streamLabels labels.Labels

	// timestamp is nanoseconds since the Unix epoch.
	timestamp int64

	line     []byte
	metadata labels.Labels
}

// LogReader runs a read plan and yields decoded log lines in batches.
//
// One row reader scans each planned section, matching all of its streams together and reading
// only the planned columns. The planner rejects a section it was given twice, so no section is
// scanned twice.
//
// Up to maxConcurrency sections are scanned at once, to hide object-storage latency.
//
// Records are forwarded one batch at a time, so a read costs one channel send per batch rather
// than one per record.
//
// The output carries no order. Batches from different sections interleave as their reads
// complete, so a consumer must not depend on the order. The sample iterator does not, and the
// data-object tier is never deduplicated against another source.
//
// The records in flight do not grow with the sample count. The plan does grow with the stream
// count: every queued task references its object's decoded streams, one label set each.
//
// Next and At are for the single consumer goroutine only. Err and Close are safe to call from
// another one.
type LogReader struct {
	objects *OpenObjects
	metrics *Metrics

	// tasks are the section reads to run.
	tasks *TaskIterator

	// capture and statsCtx carry the dataset reader's byte accounting. capture is nil when the
	// caller installed none.
	capture  *xcap.Capture
	statsCtx *stats.Context

	batches chan []LogRecord

	// stopped closes once the scan goroutine has fully exited.
	stopped chan struct{}

	cancel context.CancelFunc

	errClosingMu sync.Mutex
	err          error
	closing      bool // Close cancelled the scans, rather than the caller

	// parentCtx is the caller's context, as opposed to the cancellable one the scans run under.
	parentCtx context.Context

	currBatch []LogRecord
	currPos   int
}

// NewLogReader starts scanning the tasks the iterator streams. It owns a cancellable
// context for its scans and cancels it on Close, and it owns the iterator's lifetime.
func NewLogReader(ctx context.Context, objects *OpenObjects, tasks *TaskIterator, maxConcurrency, batchSize int, metrics *Metrics) *LogReader {
	maxConcurrency = max(maxConcurrency, 1)
	batchSize = max(batchSize, 1)

	parentCtx := ctx
	ctx, cancel := context.WithCancel(ctx)
	r := &LogReader{
		objects:   objects,
		metrics:   metrics,
		tasks:     tasks,
		parentCtx: parentCtx,
		capture:   xcap.CaptureFromContext(ctx),
		statsCtx:  stats.FromContext(ctx),
		batches:   make(chan []LogRecord, maxConcurrency),
		stopped:   make(chan struct{}),
		cancel:    cancel,
	}

	go r.runTasks(ctx, tasks, maxConcurrency, batchSize)
	return r
}

func (r *LogReader) runTasks(ctx context.Context, tasks *TaskIterator, maxConcurrency, batchSize int) {
	defer close(r.stopped)
	defer close(r.batches)

	// Fail the query rather than the process. Every goroutine this package starts recovers:
	// none of them is covered by the server's request-level recovery, so one panic would take
	// the querier down and abort every other in-flight query. The deferred closes above still
	// run, so the consumer sees the failure instead of blocking.
	defer func() {
		if panicked := recover(); panicked != nil {
			util_server.RecordPanic()

			// Log the stack: reducing the panic to an error is what keeps the process up, and it
			// is also what throws away the only trace of where it came from.
			level.Error(util_log.Logger).Log(
				"msg", "panic reading data object logs",
				"panic", panicked,
				"stack", string(debug.Stack()),
			)
			r.setErr(fmt.Errorf("reading data object logs: %v", panicked))
		}
	}()

	// Scans run concurrently, so their durations sum through an atomic rather than a lock.
	var scannedNanos atomic.Int64

	group, ctx := errgroup.WithContext(ctx)
	group.SetLimit(maxConcurrency)

	// Stop and wait for the scans however this frame ends, including a panic, which would
	// otherwise unwind past the Wait below. Close promises that nothing reads an object after it
	// is released, and a scan still blocked on the batch channel would find it closed. Cancel
	// first, or Wait blocks on a send nobody will drain.
	defer func() {
		r.cancel()
		_ = group.Wait()
	}()

	for tasks.Next() {
		if err := ctx.Err(); err != nil {
			// Stop pulling tasks the planner may still be queueing. Record the cause: without
			// it a cancelled query looks like a clean end of results and returns a truncated
			// count with no error. A scan error recorded earlier wins, and setErr drops the
			// cancellation Close itself caused.
			r.setErr(context.Cause(ctx))
			break
		}
		task := tasks.At()
		group.Go(func() (err error) {
			// A scan runs on its own goroutine, so recover here too and report the panic as the
			// scan's error, which stops the siblings the same way a read failure does.
			defer func() {
				if panicked := recover(); panicked != nil {
					util_server.RecordPanic()

					level.Error(util_log.Logger).Log(
						"msg", "panic scanning a data object logs section",
						"object", task.objectPath,
						"section", task.sectionIdx,
						"panic", panicked,
						"stack", string(debug.Stack()),
					)
					err = fmt.Errorf("scanning data object %q logs section %d: %v", task.objectPath, task.sectionIdx, panicked)
					r.setErr(err)
				}
			}()

			start := time.Now()
			err = r.runTask(ctx, task, batchSize)
			scannedNanos.Add(int64(time.Since(start)))
			if err != nil {
				// Record the error as soon as a scan fails, so Next stops without draining the
				// batches queued before it. The group then cancels the sibling scans.
				r.setErr(err)
			}
			return err
		})
	}
	// Errors are recorded above. Wait only so the batch channel closes after every scan stops.
	_ = group.Wait()

	// A planning failure surfaces the same way a scan error does, so the query fails instead of
	// quietly returning the tasks planned before it.
	if err := tasks.Err(); err != nil {
		r.setErr(err)
	}

	r.metrics.taskWaitSeconds.Add(tasks.Waited().Seconds())
	r.metrics.taskScanSeconds.Add(time.Duration(scannedNanos.Load()).Seconds())
}

func (r *LogReader) runTask(ctx context.Context, task ReadTask, batchSize int) (returnErr error) {
	object, err := r.objects.get(ctx, task.objectPath)
	if err != nil {
		return err
	}
	section, err := object.logsSection(ctx, task.sectionIdx)
	if err != nil {
		return err
	}

	reader := logs.NewRowReader(section)
	defer func() {
		// Report a close failure only when nothing else failed, so the first error wins.
		if closeErr := reader.Close(); returnErr == nil {
			returnErr = closeErr
		}
	}()

	if err := reader.SetProjectedColumns(task.columns, task.metadataNames); err != nil {
		return err
	}
	if err := reader.MatchStreams(slices.Values(task.streamIDs)); err != nil {
		return err
	}
	if err := reader.SetPredicates(task.rowPredicates()); err != nil {
		return err
	}
	if err := reader.Open(ctx); err != nil {
		return err
	}

	buf := make([]logs.Record, batchSize)
	for {
		n, readErr := reader.Read(ctx, buf)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return readErr
		}
		batch, err := task.records(buf[:n])
		if err != nil {
			return err
		}
		if len(batch) > 0 {
			select {
			case r.batches <- batch:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if n == 0 && errors.Is(readErr, io.EOF) {
			return nil
		}
	}
}

func (r *LogReader) Next() bool {
	for {
		if r.currPos+1 < len(r.currBatch) {
			r.currPos++
			return true
		}
		// A failed scan is terminal, so stop instead of returning the batches queued before it.
		// Checked once per batch, not per record, to keep the per-sample path cheap.
		if err := r.Err(); err != nil {
			// Stop the planner too: it must not keep resolving objects for a query that has
			// already failed.
			r.tasks.Abort(err)
			return false
		}
		batch, ok := <-r.batches
		if !ok {
			return false
		}
		r.currBatch = batch
		r.currPos = 0
		if len(r.currBatch) > 0 {
			return true
		}
	}
}

// At returns the record the last Next fetched, so it panics unless Next returned true.
func (r *LogReader) At() LogRecord { return r.currBatch[r.currPos] }

func (r *LogReader) Err() error {
	r.errClosingMu.Lock()
	defer r.errClosingMu.Unlock()
	return r.err
}

func (r *LogReader) setErr(err error) {
	r.errClosingMu.Lock()
	defer r.errClosingMu.Unlock()

	if r.closing && errors.Is(err, context.Canceled) {
		// Close cancelled the scans, so this is the cancellation they were given rather than a
		// query failure. Close only sets closing once it has established that the caller's own
		// context is still alive, so this cannot drop a cancellation the caller caused.
		return
	}
	if r.err == nil {
		r.err = err
	}
}

// Close stops the planner and the scan workers, records the query's statistics, and releases
// the objects. It blocks until both have exited, so nothing reads an object after it is
// released.
//
// Closing before the results are drained is not a failure, so Close reports no error for the
// cancellation it causes.
func (r *LogReader) Close() error {
	// Decide, before anything is cancelled, whether a later cancellation is the caller's or
	// this reader's own. Asking the caller's context is the whole question, and it does not
	// depend on whether a scan goroutine recorded the cancellation first.
	r.errClosingMu.Lock()
	if cause := context.Cause(r.parentCtx); cause != nil {
		// The caller's context died on its own, so the query failed whatever happens next. A
		// scan may not have reported it yet, and the guard in setErr would then drop it.
		if r.err == nil {
			r.err = cause
		}
	} else {
		r.closing = true
	}
	r.errClosingMu.Unlock()

	// Stop the planner first and wait for it, so it has let go of the objects before the scans
	// are cancelled and the objects released.
	r.tasks.Abort(nil)

	r.cancel()
	<-r.stopped
	r.recordStats()
	r.objects.release()
	return r.Err()
}

// recordStats folds the data-object byte and row counts into the query stats. It does nothing
// when no capture was installed.
func (r *LogReader) recordStats() {
	if r.capture == nil {
		return
	}

	r.statsCtx.AddPrePredicateDecompressedBytes(xcap.ValueFromRegion[int64](r.capture, logs.RegionRead, dataobj.StatDatasetPrimaryRowBytes))
	r.statsCtx.AddPostPredicateDecompressedBytes(xcap.ValueFromRegion[int64](r.capture, logs.RegionRead, dataobj.StatDatasetSecondaryRowBytes))
	r.statsCtx.AddPrePredicateDecompressedRows(xcap.ValueFromRegion[int64](r.capture, logs.RegionRead, dataobj.StatDatasetPrimaryRowsRead))
	r.statsCtx.AddPostFilterRows(xcap.ValueFromRegion[int64](r.capture, logs.RegionRead, dataobj.StatDatasetSecondaryRowsRead))
	r.capture.End()

	// Clear the capture so a repeated Close does not count the same bytes twice.
	r.capture = nil
}

// recordReader is the reader the sample iterator consumes.
//
// Next and At are for the single consumer goroutine only. Err and Close are safe to call from
// another one.
type recordReader interface {
	Next() bool
	At() LogRecord
	Err() error
	Close() error
}
