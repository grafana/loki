package hintprovider

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/logline"
	"github.com/grafana/loki/v3/pkg/logline/format"
	"github.com/grafana/loki/v3/pkg/logline/store"
)

type termJob struct {
	term     string
	readerID string
}

type readerResult struct {
	reader logline.Reader
	meta   store.Meta

	result               format.Bitmap
	done                 bool
	reason               format.QueryMultipleTerminationReason
	termBatchesProcessed int
}

type queryExecutionState struct {
	mu          sync.Mutex
	readersByID map[string]*readerResult
}

func newQueryExecutionState(readersByID map[string]*readerResult) *queryExecutionState {
	return &queryExecutionState{
		readersByID: readersByID,
	}
}

func (s *queryExecutionState) shouldEnqueue(readerID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, ok := s.readersByID[readerID]
	return ok && !result.done
}

func (s *queryExecutionState) beginJob(readerID string) (logline.Reader, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, ok := s.readersByID[readerID]
	if !ok {
		return nil, false, fmt.Errorf("reader for index %s not found", readerID)
	}
	if result.done {
		return nil, false, nil
	}
	if result.termBatchesProcessed == 0 {
		// Jobs are scheduled independently; record one logical processing pass
		// for the reader when the first job starts.
		result.termBatchesProcessed = 1
	}
	return result.reader, true, nil
}

func (s *queryExecutionState) afterFindTerm(readerID string, termIndex int) (logline.Reader, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, ok := s.readersByID[readerID]
	if !ok {
		return nil, false, fmt.Errorf("reader for index %s not found", readerID)
	}
	if result.done {
		return nil, false, nil
	}
	if termIndex < 0 {
		result.done = true
		result.reason = format.QueryMultipleReasonTermMiss
		return nil, false, nil
	}

	return result.reader, true, nil
}

func (s *queryExecutionState) applyBitmap(readerID string, readerRes format.Bitmap) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	res, ok := s.readersByID[readerID]
	if !ok {
		return fmt.Errorf("reader for index %s not found", readerID)
	}
	if res.done {
		return nil
	}

	res.result = res.result.And(readerRes)
	if res.result.IsEmpty() {
		res.done = true
		res.reason = format.QueryMultipleReasonEmptyAnd
	}
	return nil
}

func (p *LoglineHintProvider) executeQuery(
	ctx context.Context,
	filters []string,
	overlapping []store.Meta,
	stats *QueryStats,
	ngramLength, maxParallel int,
	windowStart, windowEnd time.Time,
) (map[shardKey][]HintTimeRange, error) {
	jobs, metasByID, err := buildTermJobs(filters, overlapping, ngramLength)
	if err != nil {
		return nil, err
	}
	byShard := make(map[shardKey][]HintTimeRange)
	if len(jobs) == 0 {
		return byShard, nil
	}

	readersByID, err := p.openReadersForMetas(ctx, metasByID, stats, maxParallel)
	if err != nil {
		return nil, err
	}
	defer closeReaders(readersByID)

	state := newQueryExecutionState(readersByID)
	g, gCtx := errgroup.WithContext(ctx)
	g.SetLimit(maxParallel)

	for _, job := range jobs {
		if !state.shouldEnqueue(job.readerID) {
			continue
		}

		g.Go(func() error {
			select {
			case <-gCtx.Done():
				return gCtx.Err()
			default:
			}

			reader, shouldRun, err := state.beginJob(job.readerID)
			if err != nil || !shouldRun {
				return err
			}

			termIndex, err := reader.FindTerm(job.term)
			if err != nil {
				return err
			}

			reader, shouldRun, err = state.afterFindTerm(job.readerID, termIndex)
			if err != nil || !shouldRun {
				return err
			}

			select {
			case <-gCtx.Done():
				return gCtx.Err()
			default:
			}

			res, err := reader.GetBitmap(termIndex)
			if err != nil {
				return err
			}

			return state.applyBitmap(job.readerID, res)
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}

	for _, res := range readersByID {
		if !res.done {
			res.done = true
			res.reason = format.QueryMultipleReasonComplete
		}

		stats.ObserveQueryMultiple(res.reason, res.termBatchesProcessed)
		if p.queryMultipleObserver != nil {
			p.queryMultipleObserver(queryMultipleReasonLabel(res.reason), res.termBatchesProcessed)
		}

		key := shardKeyOf(res.meta)
		var ranges []HintTimeRange
		switch res.reason {
		case format.QueryMultipleReasonComplete:
			if res.result.MatchesAll {
				ranges = []HintTimeRange{hintTimeRangeForMeta(res.meta)}
			} else if !res.result.IsEmpty() {
				ranges = rangesForDocIDs(res.meta, res.result.Roaring.ToArray(), res.reader.Documents(), windowStart, windowEnd)
			}
			if len(ranges) == 0 {
				continue
			}
		case format.QueryMultipleReasonEmptyAnd, format.QueryMultipleReasonTermMiss:
			// Unsharded empties are a no-op; only register empty keys when sharded so they annihilate.
			if !key.isSharded() {
				continue
			}

			ranges = []HintTimeRange{}
		}

		byShard[key] = append(byShard[key], ranges...)
	}

	return byShard, nil
}

func buildTermJobs(
	filters []string,
	overlapping []store.Meta,
	ngramLength int,
) ([]termJob, map[string]store.Meta, error) {
	jobs := make([]termJob, 0, len(filters)*len(overlapping))
	metasByID := make(map[string]store.Meta, len(overlapping))

	// Filters are ANDed, so a filter that yields no terms under a version
	// constrains nothing there and drops out. For example, v4 emits no term for
	// a number shorter than 9 digits. hasTerms records, per index version,
	// whether any filter is left to narrow it.
	hasTerms := make(map[string]bool)

	for _, filter := range filters {
		// Per-version cache: each unique index version is extracted at most once
		// per filter. The common case (all blocks share the current index version)
		// hits this cache on every block after the first and does exactly one
		// extraction per filter.
		ngramsByVersion := make(map[string][]string)

		for _, meta := range overlapping {
			orderedNgrams, seen := ngramsByVersion[meta.Version]
			if !seen {
				ngrams, err := ExtractQueryNgrams(filter, ngramLength, meta.Version)
				if err != nil {
					return nil, nil, fmt.Errorf("block %s: %w", meta.ID(), err)
				}
				hasTerms[meta.Version] = hasTerms[meta.Version] || len(ngrams) > 0
				orderedNgrams = orderUncorrelated(ngrams)
				ngramsByVersion[meta.Version] = orderedNgrams
			}

			jobTerms := filterNgramsForShard(orderedNgrams, meta)
			if len(jobTerms) == 0 {
				continue
			}
			readerID := meta.ID()
			metasByID[readerID] = meta
			for _, term := range jobTerms {
				jobs = append(jobs, termJob{
					term:     term,
					readerID: readerID,
				})
			}
		}
	}

	// A version that no filter can narrow leaves its blocks unconstrained, so
	// the whole query passes through to a full Loki scan.
	for _, ok := range hasTerms {
		if !ok {
			return nil, nil, ErrUnconstrained
		}
	}
	return jobs, metasByID, nil
}

func (p *LoglineHintProvider) openReadersForMetas(
	ctx context.Context,
	metasByID map[string]store.Meta,
	stats *QueryStats,
	maxParallel int,
) (map[string]*readerResult, error) {
	readersByID := make(map[string]*readerResult, len(metasByID))
	if len(metasByID) == 0 {
		return readersByID, nil
	}

	var mu sync.Mutex
	g, gCtx := errgroup.WithContext(ctx)
	g.SetLimit(maxParallel)
	for readerID, meta := range metasByID {
		g.Go(func() error {
			if gCtx.Err() != nil {
				return gCtx.Err()
			}
			if meta.SizeBytes <= 0 {
				return fmt.Errorf("index %s has unknown size, cannot query via range reads", meta.ID())
			}
			// Use the parent ctx (not gCtx) so the bucketReaderAt inside
			// the reader keeps a live context after the errgroup finishes.
			indexReader, err := p.openIndexReader(ctx, meta, stats)
			if err != nil {
				return fmt.Errorf("open index %s: %w", meta.ID(), err)
			}
			mu.Lock()
			readersByID[readerID] = &readerResult{
				reader: indexReader,
				meta:   meta,
				result: format.Bitmap{MatchesAll: true},
			}
			mu.Unlock()
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		closeReaders(readersByID)
		return nil, err
	}
	return readersByID, nil
}

func closeReaders(readersByID map[string]*readerResult) {
	for _, result := range readersByID {
		_ = result.reader.Close()
	}
}

func queryMultipleReasonLabel(reason format.QueryMultipleTerminationReason) string {
	switch reason {
	case format.QueryMultipleReasonTermMiss:
		return "term_miss"
	case format.QueryMultipleReasonEmptyAnd:
		return "empty_and"
	case format.QueryMultipleReasonComplete:
		return "positive"
	default:
		return "unknown"
	}
}

// hintTimeRangeForMeta converts inclusive observed index bounds to a half-open
// hint range. One millisecond matches the cache and document timestamp
// precision and guarantees that a log at MaxLogTs remains covered.
func hintTimeRangeForMeta(meta store.Meta) HintTimeRange {
	return HintTimeRange{
		Start: meta.MinLogTs,
		End:   meta.MaxLogTs.Add(time.Millisecond),
		Source: fmt.Sprintf(
			"index=%s,matches_all,min=%s,max=%s",
			meta.ID(),
			meta.MinLogTs.Format(time.RFC3339Nano),
			meta.MaxLogTs.Format(time.RFC3339Nano),
		),
	}
}

// rangesForDocIDs turns matching document IDs into hint ranges.
//
// Document IDs increase with time (epoch ticks, or the dense ranks assigned
// from those ticks), and roaring.ToArray returns them sorted, so successive
// matches are in time order. Abutting or overlapping buckets collapse into
// one range during the scan. Emitting one range per document and merging
// afterward retained one HintTimeRange plus a source string for every 100ms
// bucket on a dense day.
//
// A zero windowStart and windowEnd keeps every match. Otherwise documents
// outside the inclusive query window [windowStart, windowEnd] are skipped.
// Bounds of a document that overlaps the window are left intact, matching
// the coverage NormalizeRanges produced from the per-document ranges.
func rangesForDocIDs(meta store.Meta, docIDs []uint32, docs []format.DocumentMetadata, windowStart, windowEnd time.Time) []HintTimeRange {
	if len(docIDs) == 0 || len(docs) == 0 {
		return nil
	}

	ordered := docs
	if !docsSortedByID(docs) {
		ordered = make([]format.DocumentMetadata, len(docs))
		copy(ordered, docs)
		sort.Slice(ordered, func(i, j int) bool {
			return ordered[i].ID < ordered[j].ID
		})
	}

	clip := !windowStart.IsZero() || !windowEnd.IsZero()
	var ranges []HintTimeRange
	var run docRun
	flush := func() {
		if run.docs == 0 {
			return
		}
		ranges = append(ranges, HintTimeRange{
			Start:  run.start,
			End:    run.end,
			Source: runSource(meta, run),
		})
		run = docRun{}
	}

	j := 0
	for _, id := range docIDs {
		for j < len(ordered) && ordered[j].ID < id {
			j++
		}
		if j >= len(ordered) || ordered[j].ID != id {
			continue
		}
		doc := ordered[j]
		minTS := time.UnixMilli(doc.MinTimeUnix).UTC()
		maxTS := time.UnixMilli(doc.MaxTimeUnix).UTC()
		if !maxTS.After(minTS) {
			continue
		}
		if clip && !docOverlapsInclusiveWindow(minTS, maxTS, windowStart, windowEnd) {
			continue
		}
		if !run.add(doc.ID, minTS, maxTS) {
			flush()
			run.add(doc.ID, minTS, maxTS)
		}
	}
	flush()
	return ranges
}

// docRun is one contiguous half-open cover built from successive documents.
type docRun struct {
	start, end time.Time
	firstDoc   uint32
	docs       int
}

// add extends the run when [minTS, maxTS) overlaps or abuts it. The same
// condition as NormalizeRanges: a.End == b.Start is one cover.
func (r *docRun) add(id uint32, minTS, maxTS time.Time) bool {
	if r.docs == 0 {
		*r = docRun{start: minTS, end: maxTS, firstDoc: id, docs: 1}
		return true
	}
	if minTS.After(r.end) || maxTS.Before(r.start) {
		return false
	}
	if minTS.Before(r.start) {
		r.start = minTS
	}
	if maxTS.After(r.end) {
		r.end = maxTS
	}
	r.docs++
	return true
}

func runSource(meta store.Meta, run docRun) string {
	if run.docs == 1 {
		return fmt.Sprintf(
			"index=%s,doc=%d,min=%s,max=%s",
			meta.ID(),
			run.firstDoc,
			run.start.Format(time.RFC3339Nano),
			run.end.Format(time.RFC3339Nano),
		)
	}
	return fmt.Sprintf(
		"index=%s,docs=%d,first=%d,min=%s,max=%s",
		meta.ID(),
		run.docs,
		run.firstDoc,
		run.start.Format(time.RFC3339Nano),
		run.end.Format(time.RFC3339Nano),
	)
}

func docsSortedByID(docs []format.DocumentMetadata) bool {
	for i := 1; i < len(docs); i++ {
		if docs[i].ID < docs[i-1].ID {
			return false
		}
	}
	return true
}

// docOverlapsInclusiveWindow reports whether half-open [minTS, maxTS) contains
// any instant in the inclusive query window [windowStart, windowEnd].
func docOverlapsInclusiveWindow(minTS, maxTS, windowStart, windowEnd time.Time) bool {
	if minTS.After(windowEnd) {
		return false
	}
	return maxTS.After(windowStart)
}
