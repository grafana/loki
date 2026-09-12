package builder

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"go.uber.org/atomic"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/logline"
	"github.com/grafana/loki/v3/pkg/logline/format"
	"github.com/grafana/loki/v3/pkg/logline/shard"
	"github.com/grafana/loki/v3/pkg/logline/store"
	"github.com/grafana/loki/v3/pkg/logproto"
)

// Flat-buffer architecture. Instead of routing each (ngram, docID) pair into a
// per-(date,shard) accumulator at ingest, the builder buffers all pairs in one
// flat SoA buffer keyed by an epoch-tick docID, radix-sorts + dedupes on
// fill (incremental fill: see postings_buffer.go), and spills sorted runs to
// scratch. All (date, shard) bucketing is deferred to a k-way merge at flush
// time (prepareIndexes), which streams the runs directly into per-(date,shard)
// .lidx files. This makes ingest allocation- and memory-bounded independent of
// shard count (one buffer, not N accumulators) and far cheaper — the old
// accumulator/partial-flush path is gone.
//
// With extract_threads > 1 (catchup mode) the per-record path becomes a
// pipeline: decoded streams are enqueued into one bounded queue consumed by N
// competing workers, each owning its own streamIngester (own postings buffer,
// own incremental-fill/spill path). Routing is correctness-free because docIDs
// are epoch ticks: whichever worker a stream lands on, it appends the same
// (ngram, docID) pairs, and the flush-time k-way merge over the union of all
// workers' runs dedupes identical pairs across runs exactly as it already does
// across one buffer's runs. See extract_pipeline.go.

// fileInfo represents a .lidx file written to disk and pending upload to object
// storage. The read handle and path are owned by the producing indexBuilder;
// callers pass file to the uploader but must not close it — builder.clear
// closes the handle and removes the file (and the scratch run files).
type fileInfo struct {
	file            *os.File
	date            string
	storageID       string // unique per file, generated once before upload retries
	minEnqueuedTime time.Time
	maxEnqueuedTime time.Time
	minLogTs        time.Time
	maxLogTs        time.Time
	shardValue      int
}

// dateRange tracks the log-timestamp and enqueue-time span observed for a date,
// used to stamp each (date, shard) file's metadata. Scoped per-date (a
// conservative superset for that date's shards) rather than per-(date,shard),
// since shard membership is a per-ngram property resolved only at merge.
type dateRange struct {
	minLogTs        time.Time
	maxLogTs        time.Time
	minEnqueuedTime time.Time
	maxEnqueuedTime time.Time
}

// streamIngester is the per-entry ingest state: one postings buffer plus every
// piece of scratch the hot loop touches. The serial builder owns exactly one;
// in pipeline mode each extract worker owns its own, so workers never share
// mutable state on the critical path.
type streamIngester struct {
	// minDate is the earliest date partition (YYYY-MM-DD) to index. Entries
	// with timestamps before this date are silently dropped.
	minDate string

	ngramLength int
	extractFn   logline.ExtractFunc

	postings *postingsBuffer

	// Per-date observed time spans, for file metadata. Unioned across workers
	// at prepare in pipeline mode.
	dateRanges map[string]*dateRange
	// dateCount mirrors len(dateRanges) atomically so bucketCount can report a
	// figure while workers are mid-ingest (logging only; maps must not be read
	// concurrently with worker writes). Bumped on the new-date path of
	// observeDate — once per date per cycle, never per entry.
	dateCount atomic.Int64

	// Reusable per-stream scratch — reset via [:0] to avoid allocs.
	scratchNgrams      [][8]byte
	scratchLabelValues []string

	// Per-entry date cache to avoid time.Format allocations.
	cachedYear  int
	cachedMonth time.Month
	cachedDay   int
	cachedDate  string

	metrics *Metrics
}

// indexBuilder processes decoded log streams into sorted (ngram, docID) runs
// and merges them into partial indexes at flush.
type indexBuilder struct {
	cfg Config

	// Exactly one ingest path is active for the builder's lifetime:
	// ing (serial, extract_threads=1 — the default; no queue, no goroutines)
	// or pipeline (extract_threads>=2 — bounded queue + N competing workers).
	// Flush-side / test code that needs the postings buffer goes through
	// ingesters() (or ing directly in serial-only tests).
	ing      *streamIngester
	pipeline *extractPipeline

	// runDir is this builder's private scratch subdirectory holding its run
	// files and produced .lidx files; removed wholesale by clear.
	runDir string

	// Produced .lidx handles + paths, cleaned up by clear.
	openFiles []*os.File
	lidxPaths []string

	// Global flush trigger timestamps (wall-clock, not log timestamps).
	// Updated on the main path (processStream / enqueue) in both modes so the
	// age/idle flush triggers keep wall-clock semantics regardless of how far
	// behind the workers are.
	firstAppend time.Time
	lastAppend  time.Time

	logger  log.Logger
	metrics *Metrics
}

// newIndexBuilder creates a new indexBuilder. Scratch directory creation and
// cleanup are handled by Service.starting().
func newIndexBuilder(cfg Config, minDate string, logger log.Logger, metrics *Metrics) (*indexBuilder, error) {
	extractFn, err := logline.ExtractorForVersion(cfg.Index.Version)
	if err != nil {
		return nil, fmt.Errorf("resolve extractor for index version %q: %w", cfg.Index.Version, err)
	}

	shardFn := shard.Noop
	if cfg.Index.ShardCount > 1 {
		fn, err := shard.New(cfg.Index.ShardAlgorithm)
		if err != nil {
			return nil, fmt.Errorf("create shard func %q: %w", cfg.Index.ShardAlgorithm, err)
		}
		shardFn = fn
	}

	intervalNanos := cfg.Index.DocumentInterval.Nanoseconds()
	if intervalNanos <= 0 {
		return nil, fmt.Errorf("document interval must be positive, got %s", cfg.Index.DocumentInterval)
	}

	// A min_date at or after the docID epoch is what makes the PRE-epoch panic
	// in processStream unreachable: the minDate drop runs before tick(), so
	// every pre-epoch entry is dropped (and counted) before it can reach the
	// window check. A pre-epoch min_date would instead let pre-epoch entries
	// through to a guaranteed panic — the data cannot be indexed either way, so
	// reject the config up front (CLAUDE.md invariant #7).
	if minDate != "" && minDate < docIDEpoch.Format("2006-01-02") {
		return nil, fmt.Errorf("min_date %s predates the docID epoch %s; pre-epoch data cannot be indexed (see docid_window.go)", minDate, docIDEpoch.Format("2006-01-02"))
	}

	// Config.Validate defaults these; fail loudly on a config that bypassed it
	// (a zero-capacity buffer would otherwise panic deep inside the first fill).
	if cfg.PostingsBufferPairs <= 0 {
		return nil, fmt.Errorf("postings buffer pairs must be positive, got %d (Config.Validate applies the default)", cfg.PostingsBufferPairs)
	}
	if cfg.PostingsSpillWatermark <= 0 || cfg.PostingsSpillWatermark > 0.95 {
		return nil, fmt.Errorf("postings spill watermark must be > 0 and <= 0.95, got %v (Config.Validate applies the default)", cfg.PostingsSpillWatermark)
	}

	// Each builder gets its own scratch subdirectory so that a swapped-out
	// builder being flushed and the fresh active builder never collide on run
	// or .lidx filenames while both live under the shared scratch dir. The
	// directory is created lazily on first spill so an empty cycle leaves no
	// scratch behind.
	sid, err := store.NewStorageID()
	if err != nil {
		return nil, fmt.Errorf("generate builder scratch id: %w", err)
	}
	runDir := filepath.Join(cfg.ScratchDir, "flat_"+sid)

	// The docID base bucket comes from the FIXED docIDEpoch (2026-01-01, the
	// earliest Adaptive Logs archive date — see docid_window.go). Timestamps
	// outside the uint32 window panic at ingest. In pipeline mode this config
	// is computed ONCE and shared by every worker's buffer — all buffers of
	// one cycle must agree on baseBucket or the merged docIDs would disagree.
	pbCfg := postingsBufferConfig{
		bufferPairs:    cfg.PostingsBufferPairs,
		spillWatermark: cfg.PostingsSpillWatermark,
		intervalNanos:  intervalNanos,
		ticksPerDay:    uint64((24 * time.Hour) / cfg.Index.DocumentInterval),
		baseBucket:     epochBucket(docIDEpoch, intervalNanos),
		shardCount:     cfg.Index.ShardCount,
		shardFn:        shardFn,
		scratchDir:     runDir,
	}

	newIngester := func(runPrefix string) *streamIngester {
		c := pbCfg
		c.runPrefix = runPrefix
		return &streamIngester{
			minDate:            minDate,
			ngramLength:        cfg.Index.NgramLength,
			extractFn:          extractFn,
			postings:           newPostingsBuffer(c),
			dateRanges:         make(map[string]*dateRange),
			scratchNgrams:      make([][8]byte, 0, 128),
			scratchLabelValues: make([]string, 0, 8),
			metrics:            metrics,
		}
	}

	b := &indexBuilder{
		cfg:     cfg,
		runDir:  runDir,
		logger:  logger,
		metrics: metrics,
	}

	// extract_threads <= 1 is the serial production path: no queue, no
	// goroutines, no pipeline machinery constructed at all. (cfg.Validate
	// defaults 0 to 1; the guard also covers direct construction in tests and
	// benchmarks that skip Validate.)
	if cfg.ExtractThreads <= 1 {
		b.ing = newIngester("run_")
		return b, nil
	}
	b.pipeline = newExtractPipeline(cfg.ExtractThreads, newIngester)
	return b, nil
}

// ingesters returns every ingest unit of this builder: the single serial one,
// or the pipeline's workers. Flush-side code (prepare/clear/accounting) must
// iterate this instead of assuming one buffer.
func (s *indexBuilder) ingesters() []*streamIngester {
	if s.pipeline != nil {
		return s.pipeline.workers
	}
	return []*streamIngester{s.ing}
}

// formatDate returns a "YYYY-MM-DD" date string, reusing a cached value when
// the date hasn't changed — avoids the per-entry time.Format allocation.
func (w *streamIngester) formatDate(t time.Time) string {
	y, m, d := t.Date()
	if y == w.cachedYear && m == w.cachedMonth && d == w.cachedDay {
		return w.cachedDate
	}
	var buf [10]byte
	buf[0] = byte('0' + y/1000)
	buf[1] = byte('0' + (y/100)%10)
	buf[2] = byte('0' + (y/10)%10)
	buf[3] = byte('0' + y%10)
	buf[4] = '-'
	buf[5] = byte('0' + int(m)/10)
	buf[6] = byte('0' + int(m)%10)
	buf[7] = '-'
	buf[8] = byte('0' + d/10)
	buf[9] = byte('0' + d%10)
	w.cachedYear, w.cachedMonth, w.cachedDay = y, m, d
	w.cachedDate = string(buf[:])
	return w.cachedDate
}

// processStream stamps the wall-clock flush-trigger timestamps and routes the
// decoded stream to the ingest path: inline in serial mode, enqueued to the
// worker pipeline in catchup mode. Returns an error only if a run spill fails
// (non-recoverable I/O; bubbles up so running() fails and the pod restarts
// from the last committed offset) — in pipeline mode the first worker's spill
// error surfaces here on the next call, and every later call fails fast.
//
// ref identifies the Kafka record the stream was decoded from; it is read only
// on the out-of-window panic path (zero per-line cost) so the crash names the
// poison record — in pipeline mode it rides the queue item to the worker.
// Callers without Kafka context (tests, benches) pass the zero recordRef.
func (s *indexBuilder) processStream(stream *logproto.Stream, parsedLabels *labels.Labels, enqueuedAt time.Time, ref recordRef) error {
	if len(stream.Entries) == 0 {
		return nil
	}

	now := time.Now()
	if s.firstAppend.IsZero() {
		s.firstAppend = now
	}
	s.lastAppend = now

	if s.pipeline != nil {
		return s.pipeline.enqueue(stream, parsedLabels, enqueuedAt, ref)
	}
	return s.ing.ingest(stream, parsedLabels, enqueuedAt, ref)
}

// ingest extracts n-grams from a decoded stream and appends them to this
// ingester's postings buffer as (ngram, epoch-tick) pairs. This is the
// critical path — see the performance rules in CLAUDE.md.
func (w *streamIngester) ingest(stream *logproto.Stream, parsedLabels *labels.Labels, enqueuedAt time.Time, ref recordRef) error {
	w.scratchLabelValues = w.scratchLabelValues[:0]
	if parsedLabels != nil {
		parsedLabels.Range(func(l labels.Label) {
			if l.Value != "" {
				w.scratchLabelValues = append(w.scratchLabelValues, l.Value)
			}
		})
	}

	postings := w.postings
	ngramLength := w.ngramLength
	for i := range stream.Entries {
		entry := &stream.Entries[i]
		entryTime := entry.Timestamp.UTC()
		date := w.formatDate(entryTime)

		if w.minDate != "" && date < w.minDate {
			w.metrics.droppedLinesPreMinDate.Inc()
			continue
		}

		absBucket := uint64(entryTime.UnixNano()) / uint64(postings.intervalNanos)
		docID, ok := postings.tick(absBucket)
		if !ok {
			// DELIBERATE never-panic override (see CLAUDE.md invariant #7): a
			// timestamp outside the fixed-epoch docID window means replayed
			// Adaptive Logs archive data (or a mis-set epoch) would be
			// silently unindexed while the zero-file flush path still commits
			// Kafka offsets — a permanent, silent data skip. Fail loudly
			// instead; wrapping would index the line under a wrong date.
			//
			// The PRE-epoch side of this panic is unreachable when minDate >=
			// docIDEpoch (enforced by newIndexBuilder): the minDate drop above
			// runs first, so pre-epoch entries never get here. That branch now
			// defends only against future bugs that bypass the minDate filter
			// (e.g. an empty minDate, or reordering the checks).
			panicOutOfWindow(entryTime, time.Duration(postings.intervalNanos), ref)
		}
		w.observeDate(date, entryTime, enqueuedAt)

		w.scratchNgrams = w.extractFn(ngramLength, entry.Line, entry.StructuredMetadata, w.scratchLabelValues, w.scratchNgrams[:0])
		for _, gram := range w.scratchNgrams {
			// appendPair and bufferFull inline, so the per-ngram cost stays two
			// slice appends and a length check.
			postings.appendPair(gram, docID)
			if postings.bufferFull() {
				// The spill counters are observed as before/after deltas here
				// (and after finish in prepareIndexes) so postingsBuffer needs
				// no metrics dependency. This branch runs once per buffer fill
				// (~postings_buffer_pairs pairs), never per pair, and onFull spills only
				// when the deduped head crosses the high-water mark — hence the
				// runSeq comparison before touching prometheus.
				prevRuns, prevBytes := postings.runSeq, postings.runBytes.Load()
				if err := postings.onFull(); err != nil {
					return err
				}
				if postings.runSeq != prevRuns {
					w.metrics.runsSpilledTotal.Add(float64(postings.runSeq - prevRuns))
					w.metrics.runSpillBytesTotal.Add(float64(postings.runBytes.Load() - prevBytes))
				}
			}
		}
		w.metrics.linesPerBucket.WithLabelValues(date).Inc()
	}
	return nil
}

// observeDate widens the per-date time spans used for file metadata.
func (w *streamIngester) observeDate(date string, logTs, enqueuedAt time.Time) {
	dr := w.dateRanges[date]
	if dr == nil {
		w.dateRanges[date] = &dateRange{
			minLogTs: logTs, maxLogTs: logTs,
			minEnqueuedTime: enqueuedAt, maxEnqueuedTime: enqueuedAt,
		}
		w.dateCount.Add(1)
		return
	}
	if logTs.Before(dr.minLogTs) {
		dr.minLogTs = logTs
	}
	if logTs.After(dr.maxLogTs) {
		dr.maxLogTs = logTs
	}
	if enqueuedAt.Before(dr.minEnqueuedTime) {
		dr.minEnqueuedTime = enqueuedAt
	}
	if enqueuedAt.After(dr.maxEnqueuedTime) {
		dr.maxEnqueuedTime = enqueuedAt
	}
}

// unionDateRanges returns the per-date time spans for file metadata. Serial
// mode returns the single ingester's map; pipeline mode builds a fresh min/max
// union of every worker's map on each call — callers must only use it after
// the drain barrier (workers stopped), and rebuilding per prepare attempt
// keeps retries idempotent without mutating worker state.
func (s *indexBuilder) unionDateRanges() map[string]*dateRange {
	if s.pipeline == nil {
		return s.ing.dateRanges
	}
	out := make(map[string]*dateRange)
	for _, w := range s.pipeline.workers {
		for date, dr := range w.dateRanges {
			u := out[date]
			if u == nil {
				cp := *dr
				out[date] = &cp
				continue
			}
			if dr.minLogTs.Before(u.minLogTs) {
				u.minLogTs = dr.minLogTs
			}
			if dr.maxLogTs.After(u.maxLogTs) {
				u.maxLogTs = dr.maxLogTs
			}
			if dr.minEnqueuedTime.Before(u.minEnqueuedTime) {
				u.minEnqueuedTime = dr.minEnqueuedTime
			}
			if dr.maxEnqueuedTime.After(u.maxEnqueuedTime) {
				u.maxEnqueuedTime = dr.maxEnqueuedTime
			}
		}
	}
	return out
}

// unionRefTicksInto ORs every other worker's referenced-tick bitsets into the
// merge host's map so writerForDay sees the (shard, day) union across all
// worker buffers. Bitset lengths always match: every buffer of a cycle shares
// ticksPerDay. Idempotent by construction — OR-ing the same bits again is a
// no-op — so a retried prepareIndexes can call it repeatedly.
func unionRefTicksInto(host *postingsBuffer, workers []*streamIngester) {
	for _, w := range workers {
		if w.postings == host {
			continue
		}
		for k, bs := range w.postings.refTicks {
			dst := host.refTicks[k]
			if dst == nil {
				dst = make([]uint64, len(bs))
				host.refTicks[k] = dst
				host.refTicksBytes.Add(uint64(len(dst)) * 8)
			}
			for i := range bs {
				dst[i] |= bs[i]
			}
		}
	}
}

// prepareIndexes finishes the buffer into a final run, k-way merges all runs
// into per-(date,shard) .lidx files, and returns them for upload.
//
// Pipeline mode adds a DRAIN BARRIER first: the work queue is closed, every
// worker finishes its in-flight items and exits, and the first worker error
// (if any) fails the prepare. Only then are the buffers finished and the UNION
// of all workers' runs merged (refTicks OR-unioned into the merge host).
//
// Retry safety: the scratch run files are NOT deleted here — they are removed
// only by clear (after every upload in the cycle succeeds). If an upload fails
// and prepareIndexes is retried, finish is a no-op (buffer already drained) and
// the merge re-reads the surviving runs. On merge failure any .lidx written
// this attempt is discarded and the runs are preserved.
func (s *indexBuilder) prepareIndexes() ([]fileInfo, error) {
	if s.pipeline != nil {
		// The barrier is once-guarded and returns the latched first worker
		// error on every retry: a worker spill failure is permanent for this
		// cycle, so the bounded prepare retries exhaust and the restart
		// re-consumes from the last committed offset.
		if err := s.pipeline.drain(); err != nil {
			return nil, fmt.Errorf("extract pipeline: %w", err)
		}
	}

	// Re-entry is idempotent by construction: discard anything a prior attempt
	// registered (open fds, .lidx paths) before rebuilding from the runs, so a
	// second call never appends duplicate handles to openFiles/lidxPaths.
	s.discardIndexes()

	// Finish EVERY buffer before the zero-run check: the zero-files ⇒
	// zero-pairs invariant (CLAUDE.md invariant #1) holds only if no buffer
	// still holds unspilled pairs when runPaths comes back empty.
	// Same spill-delta observation as ingest's onFull path; on a retried
	// prepare, finish is a no-op (buffer drained) so the delta is zero and
	// nothing is double-counted.
	var runPaths []string
	for _, w := range s.ingesters() {
		pb := w.postings
		prevRuns, prevBytes := pb.runSeq, pb.runBytes.Load()
		if err := pb.finish(); err != nil {
			return nil, fmt.Errorf("finish postings buffer: %w", err)
		}
		if pb.runSeq != prevRuns {
			s.metrics.runsSpilledTotal.Add(float64(pb.runSeq - prevRuns))
			s.metrics.runSpillBytesTotal.Add(float64(pb.runBytes.Load() - prevBytes))
		}
		runPaths = append(runPaths, pb.runPaths...)
	}
	if len(runPaths) == 0 {
		return nil, nil
	}

	// The first buffer hosts the merge; in pipeline mode it needs the union of
	// every worker's refTicks, and the other buffers' sort memory is released
	// here (the host's is released inside mergeRuns).
	host := s.ingesters()[0].postings
	if s.pipeline != nil {
		unionRefTicksInto(host, s.pipeline.workers)
		for _, w := range s.pipeline.workers[1:] {
			w.postings.releaseSortBuffers()
		}
	}
	dates := s.unionDateRanges()

	writerCfg := format.WriterConfig{
		DensityThreshold: float32(s.cfg.Index.DensityThreshold),
		DocumentInterval: s.cfg.Index.DocumentInterval,
	}
	runCount := len(runPaths)
	mergeStart := time.Now()
	merged, err := host.mergeRuns(s.runDir, s.cfg.Index.Version, writerCfg, runPaths)
	if err != nil {
		s.discardIndexes()
		return nil, fmt.Errorf("merge runs: %w", err)
	}
	// flushDuration covers the whole cycle including upload; these isolate the
	// runs→.lidx merge and its k-way fan-in. Observed only on success — a
	// failed merge is retried whole, and observing eagerly would double-count.
	s.metrics.mergeDuration.Observe(time.Since(mergeStart).Seconds())
	s.metrics.runsPerMerge.Observe(float64(runCount))

	files := make([]fileInfo, 0, len(merged))
	for _, mf := range merged {
		s.lidxPaths = append(s.lidxPaths, mf.path)
		fh, err := os.Open(mf.path)
		if err != nil {
			s.discardIndexes()
			return nil, fmt.Errorf("open merged index %s: %w", mf.path, err)
		}
		s.openFiles = append(s.openFiles, fh)

		storageID, err := store.NewStorageID()
		if err != nil {
			s.discardIndexes()
			return nil, fmt.Errorf("generate storage id for %s: %w", mf.date, err)
		}

		dr := dates[mf.date]
		if dr == nil {
			dr = &dateRange{}
		}
		files = append(files, fileInfo{
			file:            fh,
			date:            mf.date,
			storageID:       storageID,
			minEnqueuedTime: dr.minEnqueuedTime,
			maxEnqueuedTime: dr.maxEnqueuedTime,
			minLogTs:        dr.minLogTs,
			maxLogTs:        dr.maxLogTs,
			shardValue:      mf.shard,
		})
	}

	// Observe per-file metrics only after every file registered cleanly: a
	// failure mid-loop retries the whole prepare, and observing eagerly would
	// double-count the files that preceded the failure.
	for i, mf := range merged {
		s.metrics.indexFileTermsCount.Observe(float64(mf.terms))
		s.metrics.indexFileDocumentsCount.Observe(float64(mf.docs))
		if statFi, err := files[i].file.Stat(); err == nil {
			s.metrics.indexFileSizeBytes.Observe(float64(statFi.Size()))
			level.Info(s.logger).Log("msg", "index file written", "date", mf.date, "shard", mf.shard, "size_bytes", statFi.Size(), "terms", mf.terms, "documents", mf.docs)
		}
	}

	s.metrics.indexFilesWrittenTotal.Add(float64(len(files)))
	return files, nil
}

// discardIndexes closes and removes any .lidx produced this prepare attempt,
// preserving the scratch runs so a retried prepareIndexes can rebuild.
func (s *indexBuilder) discardIndexes() {
	for _, fh := range s.openFiles {
		fh.Close()
	}
	for _, p := range s.lidxPaths {
		os.Remove(p)
	}
	s.openFiles = nil
	s.lidxPaths = nil
}

// clear removes every on-disk file produced by this cycle (.lidx + scratch
// runs), closes open handles, and fully resets builder state — including the
// postings buffers' run counters and referenced-tick bitsets. Builders are
// nonetheless single-cycle (swapBuilder always constructs a fresh one); the
// full reset exists so no stale state lies in wait for a future caller that
// assumes clear means clear. Called from Service.executeFlush at cycle end.
func (s *indexBuilder) clear() {
	if s.pipeline != nil {
		// Normally the drain barrier already ran inside prepareIndexes; this
		// covers flushes abandoned before/without a successful prepare (ctx
		// canceled) so no worker goroutine outlives its builder. Once-guarded
		// and idempotent; any worker error was already surfaced upstream.
		_ = s.pipeline.drain()
	}
	for _, fh := range s.openFiles {
		fh.Close()
	}
	// Everything this builder wrote (runs + .lidx) lives under runDir.
	os.RemoveAll(s.runDir)
	s.openFiles = nil
	s.lidxPaths = nil
	for _, w := range s.ingesters() {
		w.postings.clear()
		w.dateRanges = make(map[string]*dateRange)
		w.dateCount.Store(0)
	}
	s.firstAppend = time.Time{}
	s.lastAppend = time.Time{}
}

// runDiskBytes returns the scratch bytes spilled by this builder's runs —
// the flush trigger bounding scratch-disk footprint (invariant: scoped to this
// builder only; a swapped-out builder's runs live on its retired buffers).
// Summed across worker buffers in pipeline mode.
func (s *indexBuilder) runDiskBytes() uint64 {
	var total int64
	for _, w := range s.ingesters() {
		total += w.postings.runBytes.Load()
	}
	return uint64(total)
}

// estimatedMemoryBytes returns the resident in-memory working set the
// GOMEMLIMIT-fraction flush trigger reads: the full capacity of the ingest sort
// buffers (~24 B per postings_buffer_pairs pair — ~460 MiB per buffer at the
// default, live pairs plus radix scratch) plus the refTicks bitsets, summed
// across worker buffers in pipeline mode. The buffer term is a fixed floor
// (× extract_threads); refTicks grows with shardCount × active days, which is
// what can push the total past the trigger.
func (s *indexBuilder) estimatedMemoryBytes() uint64 {
	var total uint64
	for _, w := range s.ingesters() {
		total += w.postings.residentBytes()
	}
	return total
}

// bucketCount reports the maximum number of (date, shard) partial indexes this
// cycle can produce, for logging. In pipeline mode, while workers are still
// running it returns the busiest worker's date count (reading the maps would
// race); after the drain barrier it is the exact union.
func (s *indexBuilder) bucketCount() int {
	shards := max(s.cfg.Index.ShardCount, 1)
	if s.pipeline != nil {
		if s.pipeline.drained() {
			return len(s.unionDateRanges()) * shards
		}
		var maxDates int64
		for _, w := range s.pipeline.workers {
			if n := w.dateCount.Load(); n > maxDates {
				maxDates = n
			}
		}
		return int(maxDates) * shards
	}
	return len(s.ing.dateRanges) * shards
}
