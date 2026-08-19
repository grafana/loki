package querier

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"sync"

	"github.com/prometheus/prometheus/model/labels"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/dataobjmetrics"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/xcap"
)

const (
	// defaultMaxConcurrency is how many logs sections the reader scans at once. Reads are
	// object-storage I/O bound, so a high fan-out hides latency.
	defaultMaxConcurrency = 16

	// defaultReadBatchSize is how many records each section read decodes and forwards as one batch.
	defaultReadBatchSize = 1024
)

// dataObjLogRecord is one decoded log line with the identity the sample layer needs.
type dataObjLogRecord struct {
	fingerprint  uint64
	streamLabels labels.Labels
	timestamp    int64
	line         []byte
	metadata     labels.Labels
}

// dataObjLogReader executes a read plan and yields decoded log lines in batches. Each section is
// scanned exactly once by one RowReader (all its shard-filtered streams matched together, projected
// columns only), and up to maxConcurrency sections are scanned concurrently to hide object-storage
// latency.
//
// Records are forwarded one batch at a time — one batch per section read — so the per-line hand-off
// cost stays negligible at millions of lines per second. The consumer (the sample iterator and the
// stream-first range-vector evaluator) is order-independent: the evaluator folds each sample into a
// per-(series, step) reduction regardless of arrival order, so batches from different sections may
// interleave freely. Within a batch the rows keep the section's stream-clustered order, so the
// evaluator's same-series fast path still hits. Memory is bounded by the batch channel plus the
// in-flight readers' page batches — independent of stream and sample counts.
type dataObjLogReader struct {
	cache *dataObjCache

	// capture and statsCtx bridge the dataset reader's byte accounting into the query stats.
	// capture is nil when the caller did not install an xcap capture. metrics records the per-component
	// fetched/processed byte counters; it is nil-safe.
	capture  *xcap.Capture
	statsCtx *stats.Context
	metrics  *dataobjmetrics.Metrics

	nextBatches chan []dataObjLogRecord

	// stopped is closed when the background scan goroutine has fully exited. Close waits on it so the
	// object cache is released only after every scan has stopped using it.
	stopped chan struct{}

	cancel context.CancelFunc

	errMu sync.Mutex
	err   error

	currBatch []dataObjLogRecord
	currPos   int
}

// newDataObjLogReader starts scanning the tasks the iterator streams. It owns a cancellable context for
// its scans and cancels it on Close. It is a pure consumer of the iterator: stopping the background
// planner that feeds it is the caller's job (see dataObjAbortReader).
func newDataObjLogReader(ctx context.Context, cache *dataObjCache, tasks *dataObjTaskIterator, maxConcurrency, batchSize int, metrics *dataobjmetrics.Metrics) *dataObjLogReader {
	if maxConcurrency < 1 {
		maxConcurrency = 1
	}
	if batchSize < 1 {
		batchSize = 1
	}

	ctx, cancel := context.WithCancel(ctx)
	r := &dataObjLogReader{
		cache:       cache,
		capture:     xcap.CaptureFromContext(ctx),
		statsCtx:    stats.FromContext(ctx),
		metrics:     metrics,
		nextBatches: make(chan []dataObjLogRecord, maxConcurrency),
		stopped:     make(chan struct{}),
		cancel:      cancel,
	}

	go r.runTasks(ctx, tasks, maxConcurrency, batchSize)
	return r
}

func (r *dataObjLogReader) runTasks(ctx context.Context, tasks *dataObjTaskIterator, maxConcurrency, batchSize int) {
	defer close(r.stopped)
	defer close(r.nextBatches)

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrency)
	for tasks.Next() {
		if ctx.Err() != nil {
			break // a scan failed or Close cancelled the scans; stop pulling tasks the planner may buffer
		}
		task := tasks.At()
		g.Go(func() error {
			err := r.runTask(ctx, task, batchSize)
			if err != nil {
				// Record the error the moment a scan fails, so Next can stop without draining the batches
				// queued before it. The errgroup then cancels the sibling scans.
				r.setErr(err)
			}
			return err
		})
	}
	_ = g.Wait() // Errors are recorded above; wait only so the channel closes after every scan stops.

	// A resolution failure (metastore lookup or object streams read) is surfaced the same way as a scan
	// error, so the query fails rather than silently returning the tasks planned before it.
	if err := tasks.Err(); err != nil {
		r.setErr(err)
	}
}

func (r *dataObjLogReader) runTask(ctx context.Context, task dataObjReadTask, batchSize int) error {
	obj, err := r.cache.get(ctx, task.object)
	if err != nil {
		return err
	}
	section, err := obj.logsSection(ctx, task.section)
	if err != nil {
		return err
	}
	if section == nil {
		return fmt.Errorf("logs section %d not found in data object %q", task.section, task.object)
	}

	reader := logs.NewRowReader(section)
	defer reader.Close()
	if err := reader.SetColumns(task.projectedColumns, task.projectedMetadata); err != nil {
		return err
	}
	if err := reader.MatchStreams(slices.Values(streamIDsToInt64(task.streamIDs))); err != nil {
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
		n, err := reader.Read(ctx, buf)
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		batch, batchErr := task.recordBatch(buf[:n])
		if batchErr != nil {
			return batchErr
		}
		if len(batch) > 0 {
			select {
			case r.nextBatches <- batch:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if n == 0 && errors.Is(err, io.EOF) {
			return nil
		}
	}
}

func (r *dataObjLogReader) Next() bool {
	for {
		if r.currPos+1 < len(r.currBatch) {
			r.currPos++
			return true
		}
		// A failed scan is terminal, so stop rather than return batches queued before the failure.
		// Checked once per batch, not per record, to keep the per-sample path cheap.
		if r.Err() != nil {
			return false
		}
		batch, ok := <-r.nextBatches
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

func (r *dataObjLogReader) At() dataObjLogRecord { return r.currBatch[r.currPos] }

func (r *dataObjLogReader) Err() error {
	r.errMu.Lock()
	defer r.errMu.Unlock()
	return r.err
}

func (r *dataObjLogReader) setErr(err error) {
	r.errMu.Lock()
	defer r.errMu.Unlock()
	if r.err == nil {
		r.err = err
	}
}

// Close stops the scan workers and releases the object cache. It blocks until the workers have exited.
func (r *dataObjLogReader) Close() error {
	r.cancel()
	<-r.stopped
	r.recordBytesStat()
	r.cache.Close()
	return r.Err()
}

// recordBytesStat folds the query's data-object byte counts into the per-component querier metrics
// and the query stats. It does nothing when no xcap capture was installed.
func (r *dataObjLogReader) recordBytesStat() {
	if r.capture == nil {
		return
	}

	// Track metrics.
	r.metrics.Record(r.capture)

	// Track query stats.
	primary := xcap.ValueFromRegion[int64](r.capture, logs.RegionRead, dataobj.StatDatasetPrimaryRowBytes)
	secondary := xcap.ValueFromRegion[int64](r.capture, logs.RegionRead, dataobj.StatDatasetSecondaryRowBytes)
	r.statsCtx.AddPrePredicateDecompressedBytes(primary)
	r.statsCtx.AddPostPredicateDecompressedBytes(secondary)
	r.capture.End()

	// Clear the capture after recording so a repeated Close does not add the bytes twice.
	r.capture = nil
}

// dataObjRecordReader yields decoded log records. Both dataObjLogReader and dataObjAbortReader implement
// it, so the sample iterator can consume either.
type dataObjRecordReader interface {
	Next() bool
	At() dataObjLogRecord
	Err() error
	Close() error
}

// dataObjAbortReader wraps a dataObjLogReader and stops the background planner (through the task
// iterator's Abort) once reading finishes — on a terminal error or on Close — so the planner never
// outlives the read it feeds. The wrapped reader stays a pure consumer with no knowledge of the planner.
type dataObjAbortReader struct {
	*dataObjLogReader
	tasks *dataObjTaskIterator
}

func newDataObjAbortReader(reader *dataObjLogReader, tasks *dataObjTaskIterator) *dataObjAbortReader {
	return &dataObjAbortReader{dataObjLogReader: reader, tasks: tasks}
}

// Next forwards to the wrapped reader. When the reader stops on an error, it aborts the planner so it
// does not keep resolving objects for a query that has already failed.
func (r *dataObjAbortReader) Next() bool {
	ok := r.dataObjLogReader.Next()
	if !ok {
		if err := r.dataObjLogReader.Err(); err != nil {
			r.tasks.Abort(err)
		}
	}
	return ok
}

// Close aborts the planner first — Abort waits for it to stop using the cache — then closes the wrapped
// reader, which waits for the scans and releases the cache. So the cache is released only after both let
// go.
func (r *dataObjAbortReader) Close() error {
	r.tasks.Abort(nil)
	return r.dataObjLogReader.Close()
}
