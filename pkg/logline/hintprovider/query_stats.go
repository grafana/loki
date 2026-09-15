package hintprovider

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/logline"
	"github.com/grafana/loki/v3/pkg/logline/format"
)

type queryStatsContextKey struct{}

// NewQueryStatsContext returns a context carrying a fresh QueryStats accumulator.
func NewQueryStatsContext(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, queryStatsContextKey{}, NewQueryStats())
}

// QueryStatsFromContext returns query stats from ctx when present.
func QueryStatsFromContext(ctx context.Context) *QueryStats {
	if ctx == nil {
		return nil
	}
	stats, _ := ctx.Value(queryStatsContextKey{}).(*QueryStats)
	return stats
}

// QueryStats accumulates per-query debug information for hint lookups.
type QueryStats struct {
	headerReads   atomic.Int64
	metadataReads atomic.Int64
	termDictReads atomic.Int64
	bitmapReads   atomic.Int64

	headerCacheMisses   atomic.Int64
	metadataCacheMisses atomic.Int64

	totalIOWaitNanos atomic.Int64
	totalIOBytes     atomic.Int64

	activeWorkers   atomic.Int32
	peakConcurrency atomic.Int32
	totalWorkNanos  atomic.Int64
	wallTimeNanos   atomic.Int64

	prefetchCalls    atomic.Int32
	prefetchTimeouts atomic.Int32

	indexQueriesTotal         atomic.Int64
	indexQueriesTermMiss      atomic.Int64
	indexQueriesEmptyAnd      atomic.Int64
	indexQueriesPositive      atomic.Int64
	totalTermBatchesProcessed atomic.Int64

	hintCacheResult      atomic.Value // string: "hit", "miss", "skip", ""
	hintCacheDaysFetched atomic.Int64
	hintCacheDaysHit     atomic.Int64
}

func NewQueryStats() *QueryStats {
	return &QueryStats{}
}

func (s *QueryStats) ObserveHeaderCacheMiss() {
	if s == nil {
		return
	}
	s.headerCacheMisses.Add(1)
}

func (s *QueryStats) ObserveMetadataCacheMiss() {
	if s == nil {
		return
	}
	s.metadataCacheMisses.Add(1)
}

// ObserveHintCache records the per-query hint cache outcome.
func (s *QueryStats) ObserveHintCache(result string, daysFetched, daysHit int) {
	if s == nil {
		return
	}
	s.hintCacheResult.Store(result)
	s.hintCacheDaysFetched.Add(int64(daysFetched))
	s.hintCacheDaysHit.Add(int64(daysHit))
}

// ObservePrefetchCall records one filter-layer wait on the prefetch result.
func (s *QueryStats) ObservePrefetchCall(timedOut bool) {
	if s == nil {
		return
	}
	s.prefetchCalls.Add(1)
	if timedOut {
		s.prefetchTimeouts.Add(1)
	}
}

// ObserveQueryMultiple records one QueryMultiple outcome and term-batch depth.
func (s *QueryStats) ObserveQueryMultiple(reason format.QueryMultipleTerminationReason, termBatchesProcessed int) {
	if s == nil {
		return
	}
	s.indexQueriesTotal.Add(1)
	if termBatchesProcessed > 0 {
		s.totalTermBatchesProcessed.Add(int64(termBatchesProcessed))
	}

	switch reason {
	case format.QueryMultipleReasonTermMiss:
		s.indexQueriesTermMiss.Add(1)
	case format.QueryMultipleReasonEmptyAnd:
		s.indexQueriesEmptyAnd.Add(1)
	case format.QueryMultipleReasonComplete:
		s.indexQueriesPositive.Add(1)
	}
}

// WorkerStarted increments active worker count and updates the peak.
func (s *QueryStats) WorkerStarted() {
	if s == nil {
		return
	}
	active := s.activeWorkers.Add(1)
	for {
		peak := s.peakConcurrency.Load()
		if active <= peak {
			return
		}
		if s.peakConcurrency.CompareAndSwap(peak, active) {
			return
		}
	}
}

// WorkerFinished records worker execution time and decrements active count.
func (s *QueryStats) WorkerFinished(startedAt time.Time) {
	if s == nil {
		return
	}
	if !startedAt.IsZero() {
		s.totalWorkNanos.Add(time.Since(startedAt).Nanoseconds())
	}
	s.activeWorkers.Add(-1)
}

// SetWallTime stores total wall time for the hint lookup.
func (s *QueryStats) SetWallTime(d time.Duration) {
	if s == nil {
		return
	}
	s.wallTimeNanos.Store(d.Nanoseconds())
}

// Merge adds counters from other into s.
func (s *QueryStats) Merge(other *QueryStats) {
	if s == nil || other == nil || s == other {
		return
	}

	s.headerReads.Add(other.headerReads.Load())
	s.metadataReads.Add(other.metadataReads.Load())
	s.termDictReads.Add(other.termDictReads.Load())
	s.bitmapReads.Add(other.bitmapReads.Load())

	s.headerCacheMisses.Add(other.headerCacheMisses.Load())
	s.metadataCacheMisses.Add(other.metadataCacheMisses.Load())

	s.totalIOWaitNanos.Add(other.totalIOWaitNanos.Load())
	s.totalIOBytes.Add(other.totalIOBytes.Load())
	s.totalWorkNanos.Add(other.totalWorkNanos.Load())
	s.prefetchCalls.Add(other.prefetchCalls.Load())
	s.prefetchTimeouts.Add(other.prefetchTimeouts.Load())
	if v, ok := other.hintCacheResult.Load().(string); ok && v != "" {
		s.hintCacheResult.Store(v)
	}
	s.hintCacheDaysFetched.Add(other.hintCacheDaysFetched.Load())
	s.hintCacheDaysHit.Add(other.hintCacheDaysHit.Load())
	s.indexQueriesTotal.Add(other.indexQueriesTotal.Load())
	s.indexQueriesTermMiss.Add(other.indexQueriesTermMiss.Load())
	s.indexQueriesEmptyAnd.Add(other.indexQueriesEmptyAnd.Load())
	s.indexQueriesPositive.Add(other.indexQueriesPositive.Load())
	s.totalTermBatchesProcessed.Add(other.totalTermBatchesProcessed.Load())

	otherPeak := other.peakConcurrency.Load()
	for {
		peak := s.peakConcurrency.Load()
		if otherPeak <= peak {
			break
		}
		if s.peakConcurrency.CompareAndSwap(peak, otherPeak) {
			break
		}
	}

	otherWall := other.wallTimeNanos.Load()
	for {
		wall := s.wallTimeNanos.Load()
		if otherWall <= wall {
			break
		}
		if s.wallTimeNanos.CompareAndSwap(wall, otherWall) {
			break
		}
	}
}

func (s *QueryStats) observeRead(readType trackedReadType, bytesRead int, waited time.Duration) {
	if s == nil {
		return
	}

	switch readType {
	case trackedReadHeader:
		s.headerReads.Add(1)
	case trackedReadMetadata:
		s.metadataReads.Add(1)
	case trackedReadTermDict:
		s.termDictReads.Add(1)
	case trackedReadBitmap:
		s.bitmapReads.Add(1)
	}

	if bytesRead > 0 {
		s.totalIOBytes.Add(int64(bytesRead))
	}
	if waited > 0 {
		s.totalIOWaitNanos.Add(waited.Nanoseconds())
	}
}

type QueryStatsSnapshot struct {
	HeaderReads   int64
	MetadataReads int64
	TermDictReads int64
	BitmapReads   int64

	HeaderCacheMisses   int64
	MetadataCacheMisses int64

	ObjectStorageRequests int64
	TotalIOWait           time.Duration
	TotalIOBytes          int64

	PeakConcurrency      int32
	EffectiveConcurrency float64

	PrefetchCalls    int32
	PrefetchTimeouts int32

	IndexQueriesTotal         int64
	IndexQueriesTermMiss      int64
	IndexQueriesEmptyAnd      int64
	IndexQueriesPositive      int64
	TotalTermBatchesProcessed int64

	HintCacheResult      string
	HintCacheDaysFetched int64
	HintCacheDaysHit     int64
}

func (s *QueryStats) Snapshot() QueryStatsSnapshot {
	if s == nil {
		return QueryStatsSnapshot{}
	}

	headerReads := s.headerReads.Load()
	metadataReads := s.metadataReads.Load()
	termDictReads := s.termDictReads.Load()
	bitmapReads := s.bitmapReads.Load()
	objectStorageRequests := headerReads + metadataReads + termDictReads + bitmapReads
	indexQueriesTotal := s.indexQueriesTotal.Load()
	indexQueriesTermMiss := s.indexQueriesTermMiss.Load()
	indexQueriesEmptyAnd := s.indexQueriesEmptyAnd.Load()
	indexQueriesPositive := s.indexQueriesPositive.Load()
	totalTermBatchesProcessed := s.totalTermBatchesProcessed.Load()

	totalWorkNanos := s.totalWorkNanos.Load()
	wallTimeNanos := s.wallTimeNanos.Load()
	effective := 0.0
	if wallTimeNanos > 0 {
		effective = float64(totalWorkNanos) / float64(wallTimeNanos)
	}

	hintCacheResult, _ := s.hintCacheResult.Load().(string)

	return QueryStatsSnapshot{
		HeaderReads:               headerReads,
		MetadataReads:             metadataReads,
		TermDictReads:             termDictReads,
		BitmapReads:               bitmapReads,
		HeaderCacheMisses:         s.headerCacheMisses.Load(),
		MetadataCacheMisses:       s.metadataCacheMisses.Load(),
		ObjectStorageRequests:     objectStorageRequests,
		TotalIOWait:               time.Duration(s.totalIOWaitNanos.Load()),
		TotalIOBytes:              s.totalIOBytes.Load(),
		PeakConcurrency:           s.peakConcurrency.Load(),
		EffectiveConcurrency:      effective,
		PrefetchCalls:             s.prefetchCalls.Load(),
		PrefetchTimeouts:          s.prefetchTimeouts.Load(),
		IndexQueriesTotal:         indexQueriesTotal,
		IndexQueriesTermMiss:      indexQueriesTermMiss,
		IndexQueriesEmptyAnd:      indexQueriesEmptyAnd,
		IndexQueriesPositive:      indexQueriesPositive,
		TotalTermBatchesProcessed: totalTermBatchesProcessed,
		HintCacheResult:           hintCacheResult,
		HintCacheDaysFetched:      s.hintCacheDaysFetched.Load(),
		HintCacheDaysHit:          s.hintCacheDaysHit.Load(),
	}
}

func (s *QueryStats) String() string {
	snap := s.Snapshot()
	return fmt.Sprintf(
		"requests=%d header=%d metadata=%d term_dict=%d bitmap=%d header_cache_misses=%d metadata_cache_misses=%d io_wait=%s io_bytes=%d peak=%d effective=%.2f prefetch_calls=%d prefetch_timeouts=%d index_queries_total=%d index_queries_term_miss=%d index_queries_empty_and=%d index_queries_positive=%d term_batches_processed_total=%d",
		snap.ObjectStorageRequests,
		snap.HeaderReads,
		snap.MetadataReads,
		snap.TermDictReads,
		snap.BitmapReads,
		snap.HeaderCacheMisses,
		snap.MetadataCacheMisses,
		snap.TotalIOWait,
		snap.TotalIOBytes,
		snap.PeakConcurrency,
		snap.EffectiveConcurrency,
		snap.PrefetchCalls,
		snap.PrefetchTimeouts,
		snap.IndexQueriesTotal,
		snap.IndexQueriesTermMiss,
		snap.IndexQueriesEmptyAnd,
		snap.IndexQueriesPositive,
		snap.TotalTermBatchesProcessed,
	)
}

type trackedReadType uint8

const (
	trackedReadUnknown trackedReadType = iota
	trackedReadHeader
	trackedReadMetadata
	trackedReadTermDict
	trackedReadBitmap
)

// trackingReaderAt wraps io.ReaderAt and records per-read debug stats.
// Call SetClassifier after the index reader is opened so that subsequent
// reads are classified by the reader's own layout knowledge. Reads that
// occur during open (before SetClassifier is called) are recorded as unknown.
type trackingReaderAt struct {
	reader io.ReaderAt
	stats  *QueryStats

	mu         sync.RWMutex
	classifier logline.Reader // set after reader is opened; nil until then
}

func newTrackingReaderAt(reader io.ReaderAt, stats *QueryStats) *trackingReaderAt {
	return &trackingReaderAt{
		reader: reader,
		stats:  stats,
	}
}

// SetClassifier installs the opened reader as the classifier for subsequent reads.
func (r *trackingReaderAt) SetClassifier(reader logline.Reader) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.classifier = reader
}

func (r *trackingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	r.stats.WorkerStarted()
	start := time.Now()
	n, err := r.reader.ReadAt(p, off)
	waited := time.Since(start)
	r.stats.WorkerFinished(start)

	readType := r.classify(off, int64(len(p)))
	r.stats.observeRead(readType, n, waited)

	return n, err
}

func (r *trackingReaderAt) classify(off, length int64) trackedReadType {
	r.mu.RLock()
	classifier := r.classifier
	r.mu.RUnlock()

	if classifier == nil {
		return trackedReadUnknown
	}

	switch classifier.ClassifyRead(off, length) {
	case format.ReadSectionHeader:
		return trackedReadHeader
	case format.ReadSectionPostings:
		return trackedReadBitmap
	case format.ReadSectionTermDict:
		return trackedReadTermDict
	case format.ReadSectionMetadata:
		return trackedReadMetadata
	default:
		return trackedReadUnknown
	}
}
