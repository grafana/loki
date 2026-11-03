package dataobjread

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime/debug"
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

	// line is nil when nothing reads the line after the read, and for an empty line.
	//
	// The line of a record that [LogReader.At] returns may use memory the reader reuses. It is
	// valid until the next call to [LogReader.Next]. A caller that keeps a line longer must copy
	// it.
	line     []byte
	metadata labels.Labels
}

// LogReader runs a read plan and yields decoded log records.
//
// One reader scans each planned section, matching all of its streams together and returning only
// the planned columns. The planner rejects a section it was given twice, so no section is scanned
// twice.
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

	// pool holds the buffers that batches travel in.
	pool    *bufferPool
	batches chan *scanBuffer

	// stopped closes once the scan goroutine has fully exited.
	stopped chan struct{}

	cancel context.CancelFunc

	errClosingMu sync.Mutex
	err          error
	closing      bool // Close cancelled the scans, rather than the caller

	// parentCtx is the caller's context, as opposed to the cancellable one the scans run under.
	parentCtx context.Context

	// curr is the batch the consumer reads, or nil before the first batch and after the last.
	// currRow is the row At returns, and currRun is the index of the run that holds it.
	curr    *scanBuffer
	currRow int
	currRun int

	// metadataBuilder builds the metadata labels of the row At returns. Only the consumer uses it.
	metadataBuilder labels.ScratchBuilder
}

// NewLogReader starts scanning the tasks the iterator streams. It owns a cancellable
// context for its scans and cancels it on Close, and it owns the iterator's lifetime.
func NewLogReader(ctx context.Context, objects *OpenObjects, tasks *TaskIterator, maxConcurrency, batchSize int, metrics *Metrics) *LogReader {
	maxConcurrency = max(maxConcurrency, 1)
	return newLogReader(ctx, objects, tasks, maxConcurrency, batchSize, scanBufferCount(maxConcurrency), metrics)
}

// scanBufferCount returns how many buffers keep every scan busy. Each scan holds one while it
// reads or sends, the batch channel holds up to maxConcurrency, and the consumer holds one.
func scanBufferCount(maxConcurrency int) int { return 2*maxConcurrency + 1 }

// newLogReader is [NewLogReader] with the number of buffers as a parameter, so a test can starve
// the pool.
func newLogReader(ctx context.Context, objects *OpenObjects, tasks *TaskIterator, maxConcurrency, batchSize, bufferCount int, metrics *Metrics) *LogReader {
	maxConcurrency = max(maxConcurrency, 1)
	batchSize = max(batchSize, 1)

	parentCtx := ctx
	ctx, cancel := context.WithCancel(ctx)

	// Each scan sends a batch in a buffer from the pool. The consumer returns the buffer when it
	// moves past the batch, so the pool bounds the number of batches in flight.
	r := &LogReader{
		objects:         objects,
		metrics:         metrics,
		tasks:           tasks,
		parentCtx:       parentCtx,
		capture:         xcap.CaptureFromContext(ctx),
		statsCtx:        stats.FromContext(ctx),
		pool:            newBufferPool(bufferCount),
		batches:         make(chan *scanBuffer, maxConcurrency),
		stopped:         make(chan struct{}),
		cancel:          cancel,
		metadataBuilder: labels.NewScratchBuilder(8),
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

	if columnarRead.Load() {
		return r.runBatchTask(ctx, task, section, batchSize)
	}

	reader := logs.NewRowReader(section)
	defer func() {
		// Report a close failure only when nothing else failed, so the first error wins.
		if closeErr := reader.Close(); returnErr == nil {
			returnErr = closeErr
		}
	}()

	if err := task.openScan(ctx, reader); err != nil {
		return err
	}

	rows := make([]logs.Record, batchSize)
	for {
		n, readErr := reader.Read(ctx, rows)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return readErr
		}
		records, err := task.records(rows[:n])
		if err != nil {
			return err
		}
		if len(records) > 0 {
			if err := r.sendRecords(ctx, records); err != nil {
				return err
			}
		}
		if n == 0 && errors.Is(readErr, io.EOF) {
			return nil
		}
	}
}

// runBatchTask scans one logs section with the columnar reader. It reads the same rows as the
// row reader of runTask and forwards them in the same batches, as arrays.
func (r *LogReader) runBatchTask(ctx context.Context, task ReadTask, section *logs.Section, batchSize int) (returnErr error) {
	reader := logs.NewBatchReader(section)
	defer func() {
		// Report a close failure only when nothing else failed, so the first error wins.
		if closeErr := reader.Close(); returnErr == nil {
			returnErr = closeErr
		}
	}()

	if err := task.openScan(ctx, reader); err != nil {
		return err
	}

	for {
		buf, err := r.pool.get(ctx)
		if err != nil {
			return err
		}

		batch, err := reader.Read(ctx, &buf.alloc, batchSize)
		if errors.Is(err, io.EOF) {
			r.pool.put(buf)
			return nil
		} else if err != nil {
			r.pool.put(buf)
			return err
		}

		runs, err := task.resolveRuns(batch, buf.runs[:0])
		if err != nil {
			r.pool.put(buf)
			return err
		}

		buf.setBatch(batch, runs, task.needReturningLine)
		if err := r.send(ctx, buf); err != nil {
			return err
		}
	}
}

// sendRecords forwards the records of one row read.
func (r *LogReader) sendRecords(ctx context.Context, records []LogRecord) error {
	buf, err := r.pool.get(ctx)
	if err != nil {
		return err
	}
	buf.setRecords(records)
	return r.send(ctx, buf)
}

// send hands buf to the consumer. When ctx ends first, send gives the buffer back to the pool and
// returns the error of ctx.
func (r *LogReader) send(ctx context.Context, buf *scanBuffer) error {
	select {
	case r.batches <- buf:
		return nil
	case <-ctx.Done():
		r.pool.put(buf)
		return ctx.Err()
	}
}

func (r *LogReader) Next() bool {
	for {
		if r.curr != nil && r.currRow+1 < r.curr.rows {
			r.currRow++
			r.advanceRun()
			return true
		}

		// The consumer is done with the batch. Give the buffer back before waiting for the next
		// batch, because a scan may be waiting for exactly this buffer.
		if r.curr != nil {
			r.pool.put(r.curr)
			r.curr = nil
		}

		// A failed scan is terminal, so stop instead of returning the batches queued before it.
		// Checked once per batch, not per record, to keep the per-sample path cheap.
		if err := r.Err(); err != nil {
			// Stop the planner too: it must not keep planning objects for a query that has
			// already failed.
			r.tasks.Abort(err)
			return false
		}
		buf, ok := <-r.batches
		if !ok {
			return false
		}

		// The first row of the batch comes from the check at the top of the loop.
		r.curr, r.currRow, r.currRun = buf, -1, 0
	}
}

// advanceRun moves currRun to the run that holds currRow. The runs cover every row, so the loop
// stays inside them.
func (r *LogReader) advanceRun() {
	if r.curr.batch == nil {
		return
	}
	for r.currRow >= r.curr.runs[r.currRun].end {
		r.currRun++
	}
}

// At returns the record the last Next fetched. It panics unless Next returned true.
//
// The line of the record is valid until the next call to Next. A caller that keeps it longer must
// copy it.
func (r *LogReader) At() LogRecord {
	buf := r.curr
	if buf.batch == nil {
		return buf.records[r.currRow]
	}

	// Build the record from the arrays of the batch. The line aliases the batch memory.
	stream := &buf.runs[r.currRun].stream
	var line []byte
	if buf.needLine && buf.batch.Message != nil {
		// The capacity cap stops an append to the line from overwriting the next one.
		if value := buf.batch.Message.Get(r.currRow); len(value) > 0 {
			line = value[:len(value):len(value)]
		}
	}

	return LogRecord{
		streamHash:   stream.streamHash,
		streamLabels: stream.streamLabels,
		timestamp:    buf.batch.Timestamp.Get(r.currRow),
		line:         line,
		metadata:     buf.batch.MetadataLabelsWith(&r.metadataBuilder, r.currRow),
	}
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

	// No scan runs any more, so no allocator is in use.
	r.pool.release()

	r.recordStats()
	r.objects.release()
	return r.Err()
}

// recordStats folds the data-object byte and row counts and the section resolution time into the
// query stats. It also folds the object-store requests and bytes into the metrics. It does nothing
// when no capture was installed.
func (r *LogReader) recordStats() {
	if r.capture == nil {
		return
	}

	r.statsCtx.AddPrePredicateDecompressedBytes(xcap.ValueFromRegion[int64](r.capture, logs.RegionRead, dataobj.StatDatasetPrimaryRowBytes))
	r.statsCtx.AddPostPredicateDecompressedBytes(xcap.ValueFromRegion[int64](r.capture, logs.RegionRead, dataobj.StatDatasetSecondaryRowBytes))
	r.statsCtx.AddPrePredicateDecompressedRows(xcap.ValueFromRegion[int64](r.capture, logs.RegionRead, dataobj.StatDatasetPrimaryRowsRead))
	r.statsCtx.AddPostFilterRows(xcap.ValueFromRegion[int64](r.capture, logs.RegionRead, dataobj.StatDatasetSecondaryRowsRead))
	r.statsCtx.RecordDataobjSectionsResolutionTime(time.Duration(xcap.ValueFromRegion[int64](r.capture, metastore.RegionSections, metastore.StatMetastoreSectionsDuration)))

	// End first: it freezes the set of regions the metrics fold reads. Every goroutine that recorded
	// into them has stopped, so the counts are final.
	r.capture.End()
	r.metrics.Record(r.capture)

	// Clear the capture so a repeated Close does not count the same bytes twice.
	r.capture = nil
}

// recordReader is the reader the sample iterator consumes.
//
// Next and At are for the single consumer goroutine only. Err and Close are safe to call from
// another one.
//
// The line of the record At returns is valid until the next call to Next.
type recordReader interface {
	Next() bool
	At() LogRecord
	Err() error
	Close() error
}
