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
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/logql/model/stats"
	"github.com/grafana/loki/v3/pkg/memory"
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

// LogRecord is one decoded log entry with the identity the sample layer needs.
type LogRecord struct {
	streamHash   uint64
	streamLabels labels.Labels

	// timestamp is nanoseconds since the Unix epoch.
	timestamp int64

	// line is nil when nothing reads it after the read, or when it is empty. Otherwise it
	// points into memory that the record does not own. The line of a record from
	// [LogReader.At] is valid only until the next call to [LogReader.Next].
	line     []byte
	metadata labels.Labels
}

// LogReader runs a read plan and yields decoded log records in batches.
//
// One row reader scans each planned section, matching all of its streams together and returning
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
// count: every queued task references the decoded streams of its object, one label set each.
//
// A record's line is valid only until the next call to Next, because the scans recycle the
// memory of the lines. This avoids one allocation per line.
//
// Next and At are for the single consumer goroutine only. Err and Close are safe to call from
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

	batches   chan *readBatch
	batchPool *readBatchPool

	// stopped closes once the scan goroutine has fully exited.
	stopped chan struct{}

	cancel context.CancelFunc

	errClosingMu sync.Mutex
	err          error
	closing      bool // Close cancelled the scans, rather than the caller

	// parentCtx is the caller's context, as opposed to the cancellable one the scans run under.
	parentCtx context.Context

	currBatch *readBatch
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
		batches:   make(chan *readBatch, maxConcurrency),
		// At most 2*maxConcurrency+1 batches exist: at most one per scan, maxConcurrency queued,
		// and one at the consumer. The pool holds them all, so put never overflows.
		batchPool: newReadBatchPool(2*maxConcurrency+1, batchSize),
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

	// A panic in sendBatches drops the batch that it held. The garbage collector frees that
	// batch, and the outer recover reports the panic itself.
	batch, err := r.sendBatches(ctx, reader, task, batchSize)
	if batch != nil {
		r.batchPool.put(batch)
	}
	return err
}

// sendBatches reads up to batchSize rows of reader at a time, and sends one batch of records per
// Read call that returns rows. A send passes the batch to the consumer, which owns it from then on.
//
// When sendBatches fails after it took a batch from the pool and did not send it, it returns
// that batch, and the caller must return it to the pool. It never returns a batch that it sent,
// because the consumer could then read lines that a scan copies into the same memory.
func (r *LogReader) sendBatches(ctx context.Context, reader *logs.RowReader, task ReadTask, batchSize int) (*readBatch, error) {
	rows := make([]logs.Record, batchSize)
	for {
		n, readErr := reader.Read(ctx, rows)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return nil, readErr
		}
		if n > 0 {
			batch := r.batchPool.get()
			if err := batch.fill(task, rows[:n]); err != nil {
				return batch, err
			}
			select {
			case r.batches <- batch:
			case <-ctx.Done():
				return batch, ctx.Err()
			}
		}
		if n == 0 && errors.Is(readErr, io.EOF) {
			return nil, nil
		}
	}
}

func (r *LogReader) Next() bool {
	if r.currBatch != nil {
		if r.currPos+1 < len(r.currBatch.records) {
			r.currPos++
			return true
		}
		// The lines of the batch are valid only until this call, so a scan may now reuse its
		// line memory.
		r.batchPool.put(r.currBatch)
		r.currBatch = nil
	}

	// A failed scan is terminal, so stop instead of returning the batches queued before it.
	// Checked once per batch, not per record, to keep the per-sample path cheap.
	if err := r.Err(); err != nil {
		// Stop the planner too: it must not keep planning objects for a query that has
		// already failed.
		r.tasks.Abort(err)

		// The consumer reads no further batch, so free the pooled ones. Scans that are still
		// running can refill the pool, and Close drains it again.
		r.batchPool.drain()
		return false
	}
	// A scan sends only batches with records, so the new batch has a record at position 0.
	batch, ok := <-r.batches
	if !ok {
		// The channel closes only after every scan has exited. A scan returns an unsent batch to
		// the pool, or drops it when it panics, so no batch is in use. Free the pooled batches
		// now, because the closed reader can stay reachable until the query ends.
		r.batchPool.drain()
		return false
	}
	r.currBatch = batch
	r.currPos = 0
	return true
}

// At returns the record the last Next fetched, so it panics unless Next returned true.
func (r *LogReader) At() LogRecord { return r.currBatch.records[r.currPos] }

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

	// The scans have exited, so nothing fills a batch again. Free the queued and pooled
	// batches, because the closed reader can stay reachable until the query ends. The consumer
	// returns its current batch when Next moves past the last record of the batch.
	for range r.batches {
	}
	r.batchPool.drain()

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
// The metrics get the object-store requests and the downloaded bytes. Only the metrics count the
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
					downloadedBytes += regionInt64(region, dataobj.StatObjectBytesDownloaded)
				}
				compactedObjects += regionInt64(region, dataobj.StatObjectsOpenedCompacted)
				uncompactedObjects += regionInt64(region, dataobj.StatObjectsOpenedUncompacted)
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

// readBatch holds the records built from the rows of one Read call. A non-nil record line
// points into a region of linesAlloc, and stays valid until the next fill or reset of the batch.
// Only fill and reset use linesAlloc.
//
// A readBatch must not be copied, because it owns an Allocator.
type readBatch struct {
	records    []LogRecord
	linesAlloc memory.Allocator

	// pooled is true from a put until the next get.
	pooled bool
}

// fill replaces the records of the batch with one record per row. It copies the lines that the
// task needs into one region from b.linesAlloc, so the records do not alias rows. On error the
// batch holds no records.
//
// fill reclaims b.linesAlloc first, so the lines of the previous fill must no longer be in use.
func (b *readBatch) fill(task ReadTask, rows []logs.Record) error {
	b.linesAlloc.Reclaim()

	var lineBuf []byte
	if size := task.forwardedLineBytes(rows); size > 0 {
		lineBuf = b.linesAlloc.Allocate(size).Data()[:0:size]
	}

	records, err := task.appendRecords(b.records[:0], lineBuf, rows)
	if err != nil {
		b.records = b.records[:0]
		return err
	}
	b.records = records
	return nil
}

// reset drops the records and releases the line regions that the last fill did not use. A fill
// reuses a region that is large enough, so a batch keeps one region, sized for the largest set
// of lines that it has held.
//
// A fill that copied no line uses no region. reset then keeps the region of the batch, because
// Reset would trim every region that the last fill did not use.
//
// reset clears the whole backing array of the records, because a failed fill can leave records
// past their length.
func (b *readBatch) reset() {
	clear(b.records[:cap(b.records)])
	b.records = b.records[:0]

	if b.linesAlloc.FreeBytes() < b.linesAlloc.AllocatedBytes() {
		b.linesAlloc.Reset()
	}
}

// readBatchPool recycles read batches between the scans and the consumer, so a fill can reuse a
// region instead of allocating one.
//
// The pool holds at most capacity batches, and get allocates a new batch when the pool is empty.
// The caller sets capacity to the number of batches that can exist at once, so put never finds
// the pool full.
//
// readBatchPool is safe for concurrent use.
//
// readBatchPool does not use sync.Pool, for three reasons:
//   - A GC can drop the items of a sync.Pool during a query, which forces new allocations.
//   - A package-level sync.Pool would keep line memory alive after the query ends, until two
//     GC cycles pass.
//   - sync.Pool has no capacity, so it cannot report a full pool.
//
// A per-reader channel frees its batches with the reader, and its capacity matches the bound
// on live batches.
type readBatchPool struct {
	free      chan *readBatch
	batchSize int
}

func newReadBatchPool(capacity, batchSize int) *readBatchPool {
	return &readBatchPool{free: make(chan *readBatch, capacity), batchSize: batchSize}
}

// get returns a batch with no records.
func (p *readBatchPool) get() *readBatch {
	select {
	case batch := <-p.free:
		batch.pooled = false
		return batch
	default:
		return &readBatch{records: make([]LogRecord, 0, p.batchSize)}
	}
}

// put returns batch to the pool. The caller must not use batch or its records after put.
//
// put panics in two cases that mean the ownership rules broke:
//   - a second put of batch before the next get, and
//   - a full pool, which means that more batches exist than the caller sized the pool for.
//
// Neither check catches every batch with two owners. That rule rests on the scans, which send a
// batch or return it to the pool, never both.
func (p *readBatchPool) put(batch *readBatch) {
	if batch.pooled {
		panic("dataobjread: read batch returned to the pool twice")
	}
	batch.reset()
	batch.pooled = true

	select {
	case p.free <- batch:
	default:
		panic("dataobjread: read batch pool is full, so a batch has more than one owner")
	}
}

// drain removes every batch from the pool, so the garbage collector can free them.
func (p *readBatchPool) drain() {
	for {
		select {
		case <-p.free:
		default:
			return
		}
	}
}

// recordReader is the reader the sample iterator consumes.
//
// The line of the record from At is valid only until the next call to Next.
//
// Next and At are for the single consumer goroutine only. Err and Close are safe to call from
// another one.
type recordReader interface {
	Next() bool
	At() LogRecord
	Err() error
	Close() error
}
