package builder

import (
	"fmt"
	"sync"
	"time"

	"go.uber.org/atomic"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/logproto"
)

// extractQueueCapacity bounds the decoded-stream queue between the poll
// goroutine and the extract workers. Sizing: the longest single-worker stall
// is a run spill (~170 MB of sorted pairs s2-encoded to scratch at the
// production batch — a few hundred ms on NVMe, up to ~2 s on slow volumes).
// While one worker spills, the others keep consuming, so the queue only has to
// absorb the stalled worker's share of the feed: at observed production record
// rates (~4 KB decoded per record, roughly 5k records/s per worker), 8192
// items cover ~1.5-2 s of one worker's intake while pinning at most a few tens
// of MB of decoded entries. A full queue blocks enqueue — deliberate
// backpressure into PollFetches, exactly like a slow serial builder.
const extractQueueCapacity = 8192

// extractItem is one decoded stream handed from the poll goroutine to a
// worker. entries is a private shallow copy (see enqueue); labels are the
// decoder's cached parsed labels, safe for concurrent read-only use. ref
// carries the source Kafka record's identity to the worker so an
// out-of-window panic in ingest still names the poison record.
type extractItem struct {
	entries    []logproto.Entry
	labels     labels.Labels
	hasLabels  bool
	enqueuedAt time.Time
	ref        recordRef
}

// extractPipeline fans decoded streams out to N competing extract workers over
// ONE bounded queue. Competing consumers — not per-worker queues with modulo
// assignment — so one worker's spill stall never head-of-line blocks streams
// that another idle worker could take. Routing is correctness-free: docIDs are
// epoch ticks, so the same (ngram, docID) pairs are produced whichever
// worker handles a stream, and the flush merge dedupes identical pairs across
// all workers' runs (invariant #4 machinery, unchanged).
//
// Lifecycle: workers start at builder construction and exit either when drain
// closes the queue (flush-time barrier) or when their own ingest fails. The
// first worker error is latched and: (a) every subsequent enqueue fails fast
// with it — surfacing through processStream exactly like a serial spill error
// (running() fails, pod restarts); (b) drain returns it, so a flush racing the
// error cannot commit offsets for pairs the failed worker dropped.
type extractPipeline struct {
	workers []*streamIngester

	workC chan extractItem
	wg    sync.WaitGroup

	closeOnce sync.Once
	drainDone atomic.Bool

	errOnce  sync.Once
	firstErr atomic.Pointer[error]
	errC     chan struct{} // closed after firstErr is set; unblocks a full-queue enqueue
}

// newExtractPipeline builds N workers (each with its own streamIngester and
// postings buffer, spilling run_w<i>_<seq>.frun into the shared runDir) and
// starts their goroutines.
func newExtractPipeline(workers int, newIngester func(runPrefix string) *streamIngester) *extractPipeline {
	p := &extractPipeline{
		workers: make([]*streamIngester, workers),
		workC:   make(chan extractItem, extractQueueCapacity),
		errC:    make(chan struct{}),
	}
	for i := range p.workers {
		p.workers[i] = newIngester(fmt.Sprintf("run_w%d_", i))
	}
	p.wg.Add(workers)
	for _, w := range p.workers {
		go p.run(w)
	}
	return p
}

func (p *extractPipeline) run(w *streamIngester) {
	defer p.wg.Done()
	for it := range p.workC {
		stream := logproto.Stream{Entries: it.entries}
		var lbls *labels.Labels
		if it.hasLabels {
			lbls = &it.labels
		}
		if err := w.ingest(&stream, lbls, it.enqueuedAt, it.ref); err != nil {
			// Spill failure: latch with this item's Kafka identity (surfaces on a later call) and exit.
			p.setErr(fmt.Errorf("run spill failed (%s): %w", it.ref, err))
			return
		}
	}
}

func (p *extractPipeline) setErr(err error) {
	p.errOnce.Do(func() {
		p.firstErr.Store(&err)
		close(p.errC)
	})
}

func (p *extractPipeline) err() error {
	if e := p.firstErr.Load(); e != nil {
		return *e
	}
	return nil
}

// enqueue hands one decoded stream to the workers, blocking when the queue is
// full (backpressure into the poll loop). Fails fast once any worker has
// failed. Called with builderMtx held (processRecordBatch), and never
// concurrently with drain: a builder is swapped out under builderMtx before
// its flush (and thus its drain) starts, so no enqueue can race the close.
func (p *extractPipeline) enqueue(stream *logproto.Stream, parsedLabels *labels.Labels, enqueuedAt time.Time, ref recordRef) error {
	if err := p.err(); err != nil {
		return err
	}

	// The kafka.Decoder reuses its stream's Entries backing array across
	// Decode calls, so the slice must be copied before crossing the goroutine
	// boundary. A SHALLOW copy of the Entry structs suffices: each decode
	// appends zero-valued Entries and fills them with freshly allocated
	// strings and metadata slices, so the backing data a previous copy points
	// at is never rewritten.
	entries := make([]logproto.Entry, len(stream.Entries))
	copy(entries, stream.Entries)

	it := extractItem{entries: entries, enqueuedAt: enqueuedAt, ref: ref}
	if parsedLabels != nil {
		it.labels = *parsedLabels
		it.hasLabels = true
	}

	select {
	case p.workC <- it:
		return nil
	case <-p.errC:
		// Queue full and a worker just died — without this arm a fully wedged
		// pipeline (all workers exited) would block the poll loop forever
		// instead of surfacing the error.
		return p.err()
	}
}

// drain is the flush-time barrier: stop accepting work, wait for every worker
// to finish its in-flight items and exit, and report the first worker error.
// Idempotent (close is once-guarded; the error stays latched), so retried
// prepareIndexes calls and the clear() backstop can all invoke it.
func (p *extractPipeline) drain() error {
	p.closeOnce.Do(func() { close(p.workC) })
	p.wg.Wait()
	p.drainDone.Store(true)
	return p.err()
}

// drained reports whether the barrier has completed — after which worker state
// (dateRanges, buffers) is safe to read from the flush goroutine.
func (p *extractPipeline) drained() bool {
	return p.drainDone.Load()
}
