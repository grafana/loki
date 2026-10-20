package hintprovider

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/logline"
	"github.com/grafana/loki/v3/pkg/logline/format"
	"github.com/grafana/loki/v3/pkg/logline/needle"
	"github.com/grafana/loki/v3/pkg/logline/store"
)

// termJob looks up one term on one reader. It serves every conjunction that
// needs the term on that reader, so a term shared by several conjunctions is
// read once.
type termJob struct {
	term     string
	readerID string
	// conjs are the indexes of the conjunctions that need term on readerID.
	conjs []int
}

// conjResult is the lookup state of one conjunction on one reader.
type conjResult struct {
	result               format.Bitmap
	done                 bool
	reason               format.QueryMultipleTerminationReason
	termBatchesProcessed int
}

type readerResult struct {
	reader logline.Reader
	meta   store.Meta

	// conjs holds the conjunctions that have at least one term on this
	// reader. A conjunction with no term here takes no part for this
	// reader's shard key.
	conjs map[int]*conjResult
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

// live returns the conjunctions of job that are not done yet on its reader.
// The caller must hold s.mu.
func (s *queryExecutionState) live(job termJob) (*readerResult, []*conjResult, error) {
	res, ok := s.readersByID[job.readerID]
	if !ok {
		return nil, nil, fmt.Errorf("reader for index %s not found", job.readerID)
	}
	var live []*conjResult
	for _, c := range job.conjs {
		if cr := res.conjs[c]; !cr.done {
			live = append(live, cr)
		}
	}
	return res, live, nil
}

func (s *queryExecutionState) shouldEnqueue(job termJob) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, live, err := s.live(job)
	return err == nil && len(live) > 0
}

func (s *queryExecutionState) beginJob(job termJob) (logline.Reader, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	res, live, err := s.live(job)
	if err != nil || len(live) == 0 {
		return nil, false, err
	}
	for _, cr := range live {
		if cr.termBatchesProcessed == 0 {
			// Jobs are scheduled independently; record one logical
			// processing pass for the conjunction when its first job starts.
			cr.termBatchesProcessed = 1
		}
	}
	return res.reader, true, nil
}

func (s *queryExecutionState) afterFindTerm(job termJob, termIndex int) (logline.Reader, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	res, live, err := s.live(job)
	if err != nil || len(live) == 0 {
		return nil, false, err
	}
	if termIndex < 0 {
		for _, cr := range live {
			cr.done = true
			cr.reason = format.QueryMultipleReasonTermMiss
		}
		return nil, false, nil
	}
	return res.reader, true, nil
}

func (s *queryExecutionState) applyBitmap(job termJob, bitmap format.Bitmap) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, live, err := s.live(job)
	if err != nil {
		return err
	}
	for i, cr := range live {
		b := bitmap
		if i > 0 {
			// And may return its argument, and later Ands change a result in
			// place, so each conjunction needs its own copy.
			b = cloneBitmap(bitmap)
		}
		cr.result = cr.result.And(b)
		if cr.result.IsEmpty() {
			cr.done = true
			cr.reason = format.QueryMultipleReasonEmptyAnd
		}
	}
	return nil
}

func cloneBitmap(b format.Bitmap) format.Bitmap {
	if b.Roaring != nil {
		b.Roaring = b.Roaring.Clone()
	}
	return b
}

// executeQuery looks up every conjunction of needles in the overlapping
// indexes. It returns, per conjunction index, the ranges of each shard key.
// Conjunctions are aggregated separately (aggregateConjunctionRanges).
func (p *LoglineHintProvider) executeQuery(
	ctx context.Context,
	needles needle.Expr,
	overlapping []store.Meta,
	stats *QueryStats,
	ngramLength, maxParallel int,
) (map[int]map[shardKey][]HintTimeRange, error) {
	jobs, metasByID, err := buildTermJobs(needles, overlapping, ngramLength)
	if err != nil {
		return nil, err
	}
	byConj := make(map[int]map[shardKey][]HintTimeRange)
	if len(jobs) == 0 {
		return byConj, nil
	}

	readersByID, err := p.openReadersForMetas(ctx, metasByID, stats, maxParallel)
	if err != nil {
		return nil, err
	}
	defer closeReaders(readersByID)

	for _, job := range jobs {
		res := readersByID[job.readerID]
		for _, c := range job.conjs {
			if _, ok := res.conjs[c]; !ok {
				res.conjs[c] = &conjResult{result: format.Bitmap{MatchesAll: true}}
			}
		}
	}

	state := newQueryExecutionState(readersByID)
	g, gCtx := errgroup.WithContext(ctx)
	g.SetLimit(maxParallel)

	for _, job := range jobs {
		if !state.shouldEnqueue(job) {
			continue
		}

		g.Go(func() error {
			select {
			case <-gCtx.Done():
				return gCtx.Err()
			default:
			}

			reader, shouldRun, err := state.beginJob(job)
			if err != nil || !shouldRun {
				return err
			}

			termIndex, err := reader.FindTerm(job.term)
			if err != nil {
				return err
			}

			reader, shouldRun, err = state.afterFindTerm(job, termIndex)
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

			return state.applyBitmap(job, res)
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}

	for _, res := range readersByID {
		key := shardKeyOf(res.meta)
		for c, cr := range res.conjs {
			if !cr.done {
				cr.done = true
				cr.reason = format.QueryMultipleReasonComplete
			}

			stats.ObserveQueryMultiple(cr.reason, cr.termBatchesProcessed)
			if p.queryMultipleObserver != nil {
				p.queryMultipleObserver(queryMultipleReasonLabel(cr.reason), cr.termBatchesProcessed)
			}

			var ranges []HintTimeRange
			switch cr.reason {
			case format.QueryMultipleReasonComplete:
				if cr.result.MatchesAll {
					ranges = []HintTimeRange{hintTimeRangeForMeta(res.meta)}
				} else if !cr.result.IsEmpty() {
					ranges = rangesForDocIDs(res.meta, cr.result.Roaring.ToArray(), res.reader.Documents())
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

			byShard, ok := byConj[c]
			if !ok {
				byShard = make(map[shardKey][]HintTimeRange)
				byConj[c] = byShard
			}
			byShard[key] = append(byShard[key], ranges...)
		}
	}

	return byConj, nil
}

// buildTermJobs returns one job per distinct (reader, term) pair, and the
// metas of the readers that have at least one job.
//
// Each conjunction of needles is ANDed on its own. Its terms under a version
// are the union of its literals' terms. A literal with no terms under a
// version (v4 emits none for a number shorter than 9 digits) drops out of its
// conjunction there. A conjunction left with no terms under some version
// constrains nothing, and the conjunctions are ORed, so the whole query is
// unconstrained.
func buildTermJobs(
	needles needle.Expr,
	overlapping []store.Meta,
	ngramLength int,
) ([]termJob, map[string]store.Meta, error) {
	if needles.IsTrue() {
		return nil, nil, ErrUnconstrained
	}
	conjs := needles.Conjunctions()

	// termsByVersion[version][c] are the sorted terms conjunction c needs
	// under that version. Each unique version is extracted once. The common
	// case (all blocks share the current index version) does exactly one
	// extraction per literal.
	termsByVersion := make(map[string][][]string)
	unconstrained := false
	for _, meta := range overlapping {
		if _, seen := termsByVersion[meta.Version]; seen {
			continue
		}
		terms, err := conjunctionTerms(conjs, ngramLength, meta.Version)
		if err != nil {
			return nil, nil, fmt.Errorf("block %s: %w", meta.ID(), err)
		}
		termsByVersion[meta.Version] = terms
		for _, t := range terms {
			unconstrained = unconstrained || len(t) == 0
		}
	}
	// A version that some conjunction cannot narrow leaves its blocks
	// unconstrained, so the whole query passes through to a full Loki scan.
	if unconstrained {
		return nil, nil, ErrUnconstrained
	}

	jobs := make([]termJob, 0, len(overlapping))
	metasByID := make(map[string]store.Meta, len(overlapping))
	for _, meta := range overlapping {
		readerID := meta.ID()
		if _, seen := metasByID[readerID]; seen {
			continue
		}

		conjsByTerm := make(map[string][]int)
		for c, terms := range termsByVersion[meta.Version] {
			for _, term := range filterNgramsForShard(terms, meta) {
				conjsByTerm[term] = append(conjsByTerm[term], c)
			}
		}
		if len(conjsByTerm) == 0 {
			continue
		}

		metasByID[readerID] = meta
		for _, term := range orderUncorrelated(slices.Sorted(maps.Keys(conjsByTerm))) {
			jobs = append(jobs, termJob{
				term:     term,
				readerID: readerID,
				conjs:    conjsByTerm[term],
			})
		}
	}
	return jobs, metasByID, nil
}

// conjunctionTerms returns the sorted terms of each conjunction under
// version. Each literal is extracted once, even when several conjunctions
// share it.
func conjunctionTerms(conjs [][]string, ngramLength int, version string) ([][]string, error) {
	byLiteral := make(map[string][]string)
	out := make([][]string, len(conjs))
	for c, conj := range conjs {
		var terms []string
		for _, lit := range conj {
			litTerms, ok := byLiteral[lit]
			if !ok {
				var err error
				litTerms, err = ExtractQueryNgrams(lit, ngramLength, version)
				if err != nil {
					return nil, err
				}
				byLiteral[lit] = litTerms
			}
			terms = append(terms, litTerms...)
		}
		slices.Sort(terms)
		out[c] = slices.Compact(terms)
	}
	return out, nil
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
			if p.wrapReader != nil {
				indexReader = p.wrapReader(indexReader)
			}
			mu.Lock()
			readersByID[readerID] = &readerResult{
				reader: indexReader,
				meta:   meta,
				conjs:  make(map[int]*conjResult),
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

func rangesForDocIDs(meta store.Meta, docIDs []uint32, docs []format.DocumentMetadata) []HintTimeRange {
	if len(docIDs) == 0 || len(docs) == 0 {
		return nil
	}

	docByID := make(map[uint32]format.DocumentMetadata, len(docs))
	for _, doc := range docs {
		docByID[doc.ID] = doc
	}

	ranges := make([]HintTimeRange, 0, len(docIDs))
	for _, id := range docIDs {
		doc, ok := docByID[id]
		if !ok {
			continue
		}

		minTS := time.UnixMilli(doc.MinTimeUnix).UTC()
		maxTS := time.UnixMilli(doc.MaxTimeUnix).UTC()
		ranges = append(ranges, HintTimeRange{
			Start: minTS,
			End:   maxTS,
			Source: fmt.Sprintf(
				"index=%s,doc=%d,min=%s,max=%s",
				meta.ID(),
				doc.ID,
				minTS.Format(time.RFC3339Nano),
				maxTS.Format(time.RFC3339Nano),
			),
		})
	}

	return ranges
}
