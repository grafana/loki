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
	"go.uber.org/atomic"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/bucketstat"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/logql/model/stats"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	util_server "github.com/grafana/loki/v3/pkg/util/server"
	"github.com/grafana/loki/v3/pkg/xcap"
)

const (
	// DefaultMaxConcurrency is how many logs sections the reader scans at once.
	DefaultMaxConcurrency = 16

	// DefaultReadBatchSize is the most records that one section read decodes and sends at a time.
	DefaultReadBatchSize = 1024
)

// LogReader runs a read plan and yields the records it reads as batches.
//
// One [logs.RowReader] scans each planned section with ReadRecordBatch, matching all of its
// streams together and returning only the planned columns. The planner rejects a section it was
// given twice, so no section is scanned twice.
//
// Up to maxConcurrency sections are scanned at once, to hide object-storage latency.
//
// A scan reads each batch into memory that the batch owns, and sends the batch as it is. A read
// costs one channel send per batch and copies no record. The consumer releases a batch when it has
// used it, and the scans recycle its memory.
//
// The output carries no order. Batches from different sections interleave as their reads
// complete, so a consumer must not depend on the order. The sample iterator does not, and the
// data-object tier is never deduplicated against another source.
//
// The number of batches in flight is bounded by 2*maxConcurrency+1. The channel holds up to
// maxConcurrency of them. Each scan holds one while it reads. The consumer holds at most one.
//
// The queued tasks grow with the stream count: every task references the decoded streams of its
// object, one label set each.
//
// nextRowsBatch is for the single consumer goroutine only. Err and Close are safe to call from
// another one.
type LogReader struct {
	objects *OpenObjects
	metrics *Metrics

	// tasks are the section reads to run.
	tasks *TaskIterator

	// capture and statsCtx carry the statistics the query records. capture is nil when the
	// caller installed none.
	capture  *xcap.Capture
	statsCtx *stats.Context

	batches chan *rowsBatch
	pool    *rowsBatchPool

	// stopped closes once the scan goroutine has fully exited.
	stopped chan struct{}

	cancel context.CancelFunc

	errClosingMu sync.Mutex
	err          error
	closing      bool // Close cancelled the scans, rather than the caller

	// parentCtx is the caller's context, as opposed to the cancellable one the scans run under.
	parentCtx context.Context
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
		batches:   make(chan *rowsBatch, maxConcurrency),
		pool:      newRowsBatchPool(),
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
				// Record the error as soon as a scan fails, so nextRowsBatch stops without draining the
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

	return r.sendBatches(ctx, reader, task, batchSize)
}

// sendBatches reads up to batchSize records of reader at a time into a pooled batch, and sends the
// batch. A send passes the batch to the consumer, which owns it from then on.
//
// sendBatches releases the batch that it holds when it reaches the end of the section or fails.
// It never releases a batch that it sent. A panic drops the batch that it holds instead, because
// the state of the batch is unknown. The garbage collector frees that batch, and the recover of
// the scan reports the panic.
func (r *LogReader) sendBatches(ctx context.Context, reader *logs.RowReader, task ReadTask, batchSize int) error {
	for {
		batch := r.pool.get()

		records, err := reader.ReadRecordBatch(ctx, &batch.alloc, batchSize)
		if err != nil {
			batch.release()
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}

		if err := batch.init(task, records); err != nil {
			batch.release()
			return err
		}

		select {
		case r.batches <- batch:
		case <-ctx.Done():
			batch.release()
			return ctx.Err()
		}
	}
}

// nextRowsBatch returns the next batch of records. It returns nil when no batch is left, or when
// the read failed: see [LogReader.Err]. The caller owns the batch, and must release it.
func (r *LogReader) nextRowsBatch() *rowsBatch {
	// A failed scan is terminal, so stop instead of returning the batches queued before it.
	if err := r.Err(); err != nil {
		// Stop the planner too: it must not keep planning objects for a query that has
		// already failed.
		r.tasks.Abort(err)
		return nil
	}

	// The scan goroutine closes the channel after the last scan stops, so a closed channel ends the read.
	batch, ok := <-r.batches
	if !ok {
		return nil
	}
	return batch
}

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
//
// Close does not release a batch that the consumer holds. The consumer releases it.
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

	// Drain the queued batches, so the closed reader does not keep them alive until the query ends.
	// Do not release them. The scans have exited, and the garbage collector frees their memory.
	for range r.batches {
	}

	r.recordStats()
	r.objects.release()
	return r.Err()
}

// recordStats adds the figures of the capture to the query stats and to the metrics. It does nothing
// when no capture was installed.
//
// The query stats get:
//   - the decompressed row and byte counts
//   - the section resolution time
//   - the counts of logs objects and of index objects opened
//   - the downloaded bytes of logs objects and of index objects
//
// The metrics get the object-store requests, the downloaded bytes and the metadata reads. Only the metrics count the
// table-of-contents read.
func (r *LogReader) recordStats() {
	if r.capture == nil {
		return
	}

	r.statsCtx.AddPrePredicateDecompressedBytes(xcap.ValueFromRegion[int64](r.capture, logs.RegionRead, dataobj.StatDatasetPrimaryRowBytes))
	r.statsCtx.AddPostPredicateDecompressedBytes(xcap.ValueFromRegion[int64](r.capture, logs.RegionRead, dataobj.StatDatasetSecondaryRowBytes))
	r.statsCtx.AddPrePredicateDecompressedRows(xcap.ValueFromRegion[int64](r.capture, logs.RegionRead, dataobj.StatDatasetPrimaryRowsRead))
	r.statsCtx.AddPostFilterRows(xcap.ValueFromRegion[int64](r.capture, logs.RegionRead, dataobj.StatDatasetSecondaryRowsRead))
	r.statsCtx.RecordDataobjSectionsResolutionTime(time.Duration(xcap.ValueFromRegion[int64](r.capture, metastore.RegionSections, metastore.StatMetastoreSectionsDuration)))

	// End first: it freezes the set of regions the code below reads. Every goroutine that recorded
	// into them has stopped, so the counts are final.
	r.capture.End()

	r.metrics.Record(r.capture)

	// statsFrom sums the downloaded bytes and the opened objects of the regions whose root region
	// belongs to one of the wanted components.
	statsFrom := func(wanted ...string) (downloadedBytes, compactedObjects, uncompactedObjects int64) {
		for root, tree := range r.capture.RootRegions() {
			if !slices.Contains(wanted, componentForRootRegion(root.Name())) {
				continue
			}
			for _, region := range tree {
				if region.Name() != metastore.RegionGetIndexes {
					downloadedBytes += region.Int64Value(bucketstat.StatObjectBytesDownloaded)
				}
				compactedObjects += region.Int64Value(dataobj.StatObjectsOpenedCompacted)
				uncompactedObjects += region.Int64Value(dataobj.StatObjectsOpenedUncompacted)
			}
		}
		return downloadedBytes, compactedObjects, uncompactedObjects
	}

	logsBytes, logsCompacted, logsUncompacted := statsFrom(componentStreamsReader, componentLogsReader)
	indexBytes, indexCompacted, indexUncompacted := statsFrom(componentMetastore)
	r.statsCtx.AddDataobjLogsCompactedObjectsCount(logsCompacted)
	r.statsCtx.AddDataobjLogsUncompactedObjectsCount(logsUncompacted)
	r.statsCtx.AddDataobjIndexCompactedObjectsCount(indexCompacted)
	r.statsCtx.AddDataobjIndexUncompactedObjectsCount(indexUncompacted)
	r.statsCtx.AddDataobjLogsDownloadedBytes(logsBytes)
	r.statsCtx.AddDataobjIndexDownloadedBytes(indexBytes)

	// Clear the capture so a repeated Close does not count the same figures twice.
	r.capture = nil
}

// rowsBatchReader is the reader the sample iterator consumes.
//
// nextRowsBatch returns nil at the end of the read and after a failure. Err reports the failure.
// The consumer owns each batch that nextRowsBatch returns, and must release it.
//
// nextRowsBatch is for the single consumer goroutine only. Err and Close are safe to call from
// another one.
type rowsBatchReader interface {
	nextRowsBatch() *rowsBatch
	Err() error
	Close() error
}
