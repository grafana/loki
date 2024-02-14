package bloomcompactor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sort"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/multierror"
	"github.com/pkg/errors"

	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/stores/shipper/bloomshipper"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/tsdb"
)

type SimpleBloomController struct {
	tsdbStore   TSDBStore
	bloomStore  bloomshipper.Store
	chunkLoader ChunkLoader
	metrics     *Metrics
	limits      Limits

	// TODO(owen-d): add metrics
	logger log.Logger
}

func NewSimpleBloomController(
	tsdbStore TSDBStore,
	blockStore bloomshipper.Store,
	chunkLoader ChunkLoader,
	limits Limits,
	metrics *Metrics,
	logger log.Logger,
) *SimpleBloomController {
	return &SimpleBloomController{
		tsdbStore:   tsdbStore,
		bloomStore:  blockStore,
		chunkLoader: chunkLoader,
		metrics:     metrics,
		limits:      limits,
		logger:      logger,
	}
}

// TODO(owen-d): pool, evaluate if memory-only is the best choice
func (s *SimpleBloomController) rwFn() (v1.BlockWriter, v1.BlockReader) {
	indexBuf := bytes.NewBuffer(nil)
	bloomsBuf := bytes.NewBuffer(nil)
	return v1.NewMemoryBlockWriter(indexBuf, bloomsBuf), v1.NewByteReader(indexBuf, bloomsBuf)
}

func (s *SimpleBloomController) compactTenant(
	ctx context.Context,
	table DayTable,
	tenant string,
	ownershipRange v1.FingerprintBounds,
) error {
	logger := log.With(s.logger, "ownership", ownershipRange, "org_id", tenant, "table", table)

	client, err := s.bloomStore.Client(table.ModelTime())
	if err != nil {
		level.Error(logger).Log("msg", "failed to get client", "err", err, "table", table.String())
		return errors.Wrap(err, "failed to get client")
	}

	// Fetch source metas to be used in both compaction and cleanup of out-of-date metas+blooms
	bounds := table.Bounds()
	metas, err := s.bloomStore.FetchMetas(
		ctx,
		bloomshipper.MetaSearchParams{
			TenantID: tenant,
			Interval: bloomshipper.Interval{
				Start: bounds.Start,
				End:   bounds.End,
			},
			Keyspace: ownershipRange,
		},
	)
	if err != nil {
		level.Error(logger).Log("msg", "failed to get metas", "err", err)
		return errors.Wrap(err, "failed to get metas")
	}

	// build compaction plans
	work, err := s.findOutdatedGaps(ctx, tenant, table, ownershipRange, metas, logger)
	if err != nil {
		return errors.Wrap(err, "failed to find outdated gaps")
	}

	// build new blocks
	built, err := s.buildGaps(ctx, tenant, table, client, work, logger)
	if err != nil {
		return errors.Wrap(err, "failed to build gaps")
	}

	// in order to delete outdates metas which only partially fall within the ownership range,
	// we need to fetcha all metas in the entire bound range of the first set of metas we've resolved
	/*
		For instance, we have the following ownership range and we resolve `meta1` in our first Fetch call
		because it overlaps the ownership range, we'll need to fetch newer metas that may overlap it in order
		to check if it safely can be deleted. This falls partially outside our specific ownership range, but
		we can safely run multiple deletes by treating their removal as idempotent.
		     |-------------ownership range-----------------|
		                                      |-------meta1-------|

		we fetch this before possibly deleting meta1       |------|
	*/
	superset := ownershipRange
	for _, meta := range metas {
		union := superset.Union(meta.Bounds)
		if len(union) > 1 {
			level.Error(logger).Log("msg", "meta bounds union is not a single range", "union", union)
			return errors.New("meta bounds union is not a single range")
		}
		superset = union[0]
	}

	metas, err = s.bloomStore.FetchMetas(
		ctx,
		bloomshipper.MetaSearchParams{
			TenantID: tenant,
			Interval: bloomshipper.Interval{
				Start: bounds.Start,
				End:   bounds.End,
			},
			Keyspace: superset,
		},
	)
	if err != nil {
		level.Error(logger).Log("msg", "failed to get meta superset range", "err", err, "superset", superset)
		return errors.Wrap(err, "failed to get meta supseret range")
	}

	// combine built and pre-existing metas
	// in preparation for removing outdated metas
	metas = append(metas, built...)

	outdated := outdatedMetas(metas)
	for _, meta := range outdated {
		for _, block := range meta.Blocks {
			if err := client.DeleteBlocks(ctx, []bloomshipper.BlockRef{block}); err != nil {
				if client.IsObjectNotFoundErr(err) {
					level.Debug(logger).Log("msg", "block not found while attempting delete, continuing", "block", block)
					continue
				}

				level.Error(logger).Log("msg", "failed to delete blocks", "err", err)
				return errors.Wrap(err, "failed to delete blocks")
			}
		}

		if err := client.DeleteMetas(ctx, []bloomshipper.MetaRef{meta.MetaRef}); err != nil {
			if client.IsObjectNotFoundErr(err) {
				level.Debug(logger).Log("msg", "meta not found while attempting delete, continuing", "meta", meta.MetaRef)
			} else {
				level.Error(logger).Log("msg", "failed to delete metas", "err", err)
				return errors.Wrap(err, "failed to delete metas")
			}
		}
	}

	level.Debug(logger).Log("msg", "finished compaction")
	return nil

}

func (s *SimpleBloomController) findOutdatedGaps(
	ctx context.Context,
	tenant string,
	table DayTable,
	ownershipRange v1.FingerprintBounds,
	metas []bloomshipper.Meta,
	logger log.Logger,
) ([]blockPlan, error) {
	// Resolve TSDBs
	tsdbs, err := s.tsdbStore.ResolveTSDBs(ctx, table, tenant)
	if err != nil {
		level.Error(logger).Log("msg", "failed to resolve tsdbs", "err", err)
		return nil, errors.Wrap(err, "failed to resolve tsdbs")
	}

	if len(tsdbs) == 0 {
		return nil, nil
	}

	// Determine which TSDBs have gaps in the ownership range and need to
	// be processed.
	tsdbsWithGaps, err := gapsBetweenTSDBsAndMetas(ownershipRange, tsdbs, metas)
	if err != nil {
		level.Error(logger).Log("msg", "failed to find gaps", "err", err)
		return nil, errors.Wrap(err, "failed to find gaps")
	}

	if len(tsdbsWithGaps) == 0 {
		level.Debug(logger).Log("msg", "blooms exist for all tsdbs")
		return nil, nil
	}

	work, err := blockPlansForGaps(tsdbsWithGaps, metas)
	if err != nil {
		level.Error(logger).Log("msg", "failed to create plan", "err", err)
		return nil, errors.Wrap(err, "failed to create plan")
	}

	return work, nil
}

func (s *SimpleBloomController) loadWorkForGap(
	ctx context.Context,
	table DayTable,
	tenant string,
	id tsdb.Identifier,
	gap gapWithBlocks,
) (v1.CloseableIterator[*v1.Series], v1.CloseableIterator[*bloomshipper.CloseableBlockQuerier], error) {
	// load a series iterator for the gap
	seriesItr, err := s.tsdbStore.LoadTSDB(ctx, table, tenant, id, gap.bounds)
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to load tsdb")
	}

	// load a blocks iterator for the gap
	fetcher, err := s.bloomStore.Fetcher(table.ModelTime())
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to get fetcher")
	}

	blocksIter, err := newBatchedBlockLoader(ctx, fetcher, gap.blocks)
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to load blocks")
	}

	return seriesItr, blocksIter, nil
}

func (s *SimpleBloomController) buildGaps(
	ctx context.Context,
	tenant string,
	table DayTable,
	client bloomshipper.Client,
	work []blockPlan,
	logger log.Logger,
) ([]bloomshipper.Meta, error) {
	// Generate Blooms
	// Now that we have the gaps, we will generate a bloom block for each gap.
	// We can accelerate this by using existing blocks which may already contain
	// needed chunks in their blooms, for instance after a new TSDB version is generated
	// but contains many of the same chunk references from the previous version.
	// To do this, we'll need to take the metas we've already resolved and find blocks
	// overlapping the ownership ranges we've identified as needing updates.
	// With these in hand, we can download the old blocks and use them to
	// accelerate bloom generation for the new blocks.

	var (
		blockCt      int
		tsdbCt       = len(work)
		nGramSize    = uint64(s.limits.BloomNGramLength(tenant))
		nGramSkip    = uint64(s.limits.BloomNGramSkip(tenant))
		maxBlockSize = uint64(s.limits.BloomCompactorMaxBlockSize(tenant))
		blockOpts    = v1.NewBlockOptions(nGramSize, nGramSkip, maxBlockSize)
		created      []bloomshipper.Meta
	)

	for _, plan := range work {

		for i := range plan.gaps {
			gap := plan.gaps[i]

			meta := bloomshipper.Meta{
				MetaRef: bloomshipper.MetaRef{
					Ref: bloomshipper.Ref{
						TenantID:  tenant,
						TableName: table.String(),
						Bounds:    gap.bounds,
					},
				},
				Sources: []tsdb.SingleTenantTSDBIdentifier{plan.tsdb},
			}

			// Fetch blocks that aren't up to date but are in the desired fingerprint range
			// to try and accelerate bloom creation
			seriesItr, blocksIter, err := s.loadWorkForGap(ctx, table, tenant, plan.tsdb, gap)
			if err != nil {
				level.Error(logger).Log("msg", "failed to get series and blocks", "err", err)
				return nil, errors.Wrap(err, "failed to get series and blocks")
			}

			gen := NewSimpleBloomGenerator(
				tenant,
				blockOpts,
				seriesItr,
				s.chunkLoader,
				blocksIter,
				s.rwFn,
				s.metrics,
				log.With(logger, "tsdb", plan.tsdb.Name(), "ownership", gap),
			)

			_, loaded, newBlocks, err := gen.Generate(ctx)

			if err != nil {
				// TODO(owen-d): metrics
				level.Error(logger).Log("msg", "failed to generate bloom", "err", err)
				s.closeLoadedBlocks(loaded, blocksIter)
				return nil, errors.Wrap(err, "failed to generate bloom")
			}

			for newBlocks.Next() && newBlocks.Err() == nil {
				blockCt++
				blk := newBlocks.At()

				built, err := bloomshipper.BlockFrom(tenant, table.String(), blk)
				if err != nil {
					level.Error(logger).Log("msg", "failed to build block", "err", err)
					return nil, errors.Wrap(err, "failed to build block")
				}

				if err := client.PutBlock(
					ctx,
					built,
				); err != nil {
					level.Error(logger).Log("msg", "failed to write block", "err", err)
					s.closeLoadedBlocks(loaded, blocksIter)
					return nil, errors.Wrap(err, "failed to write block")
				}

				meta.Blocks = append(meta.Blocks, built.BlockRef)
			}

			if err := newBlocks.Err(); err != nil {
				// TODO(owen-d): metrics
				level.Error(logger).Log("msg", "failed to generate bloom", "err", err)
				s.closeLoadedBlocks(loaded, blocksIter)
				return nil, errors.Wrap(err, "failed to generate bloom")
			}

			// Close pre-existing blocks
			s.closeLoadedBlocks(loaded, blocksIter)

			// Write the new meta
			ref, err := bloomshipper.MetaRefFrom(tenant, table.String(), gap.bounds, meta.Sources, meta.Blocks)
			if err != nil {
				level.Error(logger).Log("msg", "failed to checksum meta", "err", err)
				return nil, errors.Wrap(err, "failed to checksum meta")
			}
			meta.MetaRef = ref

			if err := client.PutMeta(ctx, meta); err != nil {
				level.Error(logger).Log("msg", "failed to write meta", "err", err)
				return nil, errors.Wrap(err, "failed to write meta")
			}
			created = append(created, meta)
		}
	}

	level.Debug(logger).Log("msg", "finished bloom generation", "blocks", blockCt, "tsdbs", tsdbCt)
	return created, nil
}

// outdatedMetas returns metas that are outdated and need to be removed,
// determined by if their entire ownership range is covered by other metas with newer
// TSDBs
func outdatedMetas(metas []bloomshipper.Meta) (outdated []bloomshipper.Meta) {
	// first, ensure data is sorted so we can take advantage of that
	sort.Slice(metas, func(i, j int) bool {
		return metas[i].Bounds.Less(metas[j].Bounds)
	})

	// NB(owen-d): time complexity shouldn't be a problem
	// given the number of metas should be low (famous last words, i know).
	for i := range metas {
		a := metas[i]

		var overlaps []v1.FingerprintBounds

		for j := range metas {
			if j == i {
				continue
			}

			b := metas[j]
			intersection := a.Bounds.Intersection(b.Bounds)
			if intersection == nil {
				if a.Bounds.Cmp(b.Bounds.Min) == v1.After {
					// All subsequent metas will be newer, so we can break
					break
				}
				// otherwise, just check the next meta
				continue
			}

			// we can only remove older data, not data which may be newer
			if !tsdbsStrictlyNewer(b.Sources, a.Sources) {
				continue
			}

			// because we've sorted the metas, we only have to test overlaps against the last
			// overlap we found (if any)
			if len(overlaps) == 0 {
				overlaps = append(overlaps, *intersection)
				continue
			}

			// best effort at merging overlaps first pass
			last := overlaps[len(overlaps)-1]
			overlaps = append(overlaps[:len(overlaps)-1], last.Union(*intersection)...)

		}

		if coversFullRange(a.Bounds, overlaps) {
			outdated = append(outdated, a)
		}
	}
	return
}

func coversFullRange(bounds v1.FingerprintBounds, overlaps []v1.FingerprintBounds) bool {
	// if there are no overlaps, the range is not covered
	if len(overlaps) == 0 {
		return false
	}

	// keep track of bounds which need to be filled in order
	// for the overlaps to cover the full range
	missing := []v1.FingerprintBounds{bounds}
	ignores := make(map[int]bool)
	for _, overlap := range overlaps {
		var i int
		for {
			if i >= len(missing) {
				break
			}

			if ignores[i] {
				i++
				continue
			}

			remaining := missing[i].Unless(overlap)
			switch len(remaining) {
			case 0:
				// this range is covered, ignore it
				ignores[i] = true
			case 1:
				// this range is partially covered, updated it
				missing[i] = remaining[0]
			case 2:
				// this range has been partially covered in the middle,
				// split it into two ranges and append
				ignores[i] = true
				missing = append(missing, remaining...)
			}
			i++
		}

	}

	return len(ignores) == len(missing)
}

// tsdbStrictlyNewer returns if all of the tsdbs in a are newer than all of the tsdbs in b
func tsdbsStrictlyNewer(as, bs []tsdb.SingleTenantTSDBIdentifier) bool {
	for _, a := range as {
		for _, b := range bs {
			if a.TS.Before(b.TS) {
				return false
			}
		}
	}
	return true
}

func (s *SimpleBloomController) closeLoadedBlocks(toClose []io.Closer, it v1.CloseableIterator[*bloomshipper.CloseableBlockQuerier]) {
	// close loaded blocks
	var err multierror.MultiError
	for _, closer := range toClose {
		err.Add(closer.Close())
	}

	switch itr := it.(type) {
	case *batchedBlockLoader:
		// close remaining loaded blocks from batch
		err.Add(itr.CloseBatch())
	default:
		// close remaining loaded blocks
		for itr.Next() && itr.Err() == nil {
			err.Add(itr.At().Close())
		}
	}

	// log error
	if err.Err() != nil {
		level.Error(s.logger).Log("msg", "failed to close blocks", "err", err)
	}
}

type gapWithBlocks struct {
	bounds v1.FingerprintBounds
	blocks []bloomshipper.BlockRef
}

// blockPlan is a plan for all the work needed to build a meta.json
// It includes:
//   - the tsdb (source of truth) which contains all the series+chunks
//     we need to ensure are indexed in bloom blocks
//   - a list of gaps that are out of date and need to be checked+built
//   - within each gap, a list of block refs which overlap the gap are included
//     so we can use them to accelerate bloom generation. They likely contain many
//     of the same chunks we need to ensure are indexed, just from previous tsdb iterations.
//     This is a performance optimization to avoid expensive re-reindexing
type blockPlan struct {
	tsdb tsdb.SingleTenantTSDBIdentifier
	gaps []gapWithBlocks
}

// blockPlansForGaps groups tsdb gaps we wish to fill with overlapping but out of date blocks.
// This allows us to expedite bloom generation by using existing blocks to fill in the gaps
// since many will contain the same chunks.
func blockPlansForGaps(tsdbs []tsdbGaps, metas []bloomshipper.Meta) ([]blockPlan, error) {
	plans := make([]blockPlan, 0, len(tsdbs))

	for _, idx := range tsdbs {
		plan := blockPlan{
			tsdb: idx.tsdb,
			gaps: make([]gapWithBlocks, 0, len(idx.gaps)),
		}

		for _, gap := range idx.gaps {
			planGap := gapWithBlocks{
				bounds: gap,
			}

			for _, meta := range metas {

				if meta.Bounds.Intersection(gap) == nil {
					// this meta doesn't overlap the gap, skip
					continue
				}

				for _, block := range meta.Blocks {
					if block.Bounds.Intersection(gap) == nil {
						// this block doesn't overlap the gap, skip
						continue
					}
					// this block overlaps the gap, add it to the plan
					// for this gap
					planGap.blocks = append(planGap.blocks, block)
				}
			}

			// ensure we sort blocks so deduping iterator works as expected
			sort.Slice(planGap.blocks, func(i, j int) bool {
				return planGap.blocks[i].Bounds.Less(planGap.blocks[j].Bounds)
			})

			peekingBlocks := v1.NewPeekingIter[bloomshipper.BlockRef](
				v1.NewSliceIter[bloomshipper.BlockRef](
					planGap.blocks,
				),
			)
			// dedupe blocks which could be in multiple metas
			itr := v1.NewDedupingIter[bloomshipper.BlockRef, bloomshipper.BlockRef](
				func(a, b bloomshipper.BlockRef) bool {
					return a == b
				},
				v1.Identity[bloomshipper.BlockRef],
				func(a, _ bloomshipper.BlockRef) bloomshipper.BlockRef {
					return a
				},
				peekingBlocks,
			)

			deduped, err := v1.Collect[bloomshipper.BlockRef](itr)
			if err != nil {
				return nil, errors.Wrap(err, "failed to dedupe blocks")
			}
			planGap.blocks = deduped

			plan.gaps = append(plan.gaps, planGap)
		}

		plans = append(plans, plan)
	}

	return plans, nil
}

// Used to signal the gaps that need to be populated for a tsdb
type tsdbGaps struct {
	tsdb tsdb.SingleTenantTSDBIdentifier
	gaps []v1.FingerprintBounds
}

// tsdbsUpToDate returns if the metas are up to date with the tsdbs. This is determined by asserting
// that for each TSDB, there are metas covering the entire ownership range which were generated from that specific TSDB.
func gapsBetweenTSDBsAndMetas(
	ownershipRange v1.FingerprintBounds,
	tsdbs []tsdb.SingleTenantTSDBIdentifier,
	metas []bloomshipper.Meta,
) (res []tsdbGaps, err error) {
	for _, db := range tsdbs {
		id := db.Name()

		relevantMetas := make([]v1.FingerprintBounds, 0, len(metas))
		for _, meta := range metas {
			for _, s := range meta.Sources {
				if s.Name() == id {
					relevantMetas = append(relevantMetas, meta.Bounds)
				}
			}
		}

		gaps, err := findGaps(ownershipRange, relevantMetas)
		if err != nil {
			return nil, err
		}

		if len(gaps) > 0 {
			res = append(res, tsdbGaps{
				tsdb: db,
				gaps: gaps,
			})
		}
	}

	return res, err
}

func findGaps(ownershipRange v1.FingerprintBounds, metas []v1.FingerprintBounds) (gaps []v1.FingerprintBounds, err error) {
	if len(metas) == 0 {
		return []v1.FingerprintBounds{ownershipRange}, nil
	}

	// turn the available metas into a list of non-overlapping metas
	// for easier processing
	var nonOverlapping []v1.FingerprintBounds
	// First, we reduce the metas into a smaller set by combining overlaps. They must be sorted.
	var cur *v1.FingerprintBounds
	for i := 0; i < len(metas); i++ {
		j := i + 1

		// first iteration (i == 0), set the current meta
		if cur == nil {
			cur = &metas[i]
		}

		if j >= len(metas) {
			// We've reached the end of the list. Add the last meta to the non-overlapping set.
			nonOverlapping = append(nonOverlapping, *cur)
			break
		}

		combined := cur.Union(metas[j])
		if len(combined) == 1 {
			// There was an overlap between the two tested ranges. Combine them and keep going.
			cur = &combined[0]
			continue
		}

		// There was no overlap between the two tested ranges. Add the first to the non-overlapping set.
		// and keep the second for the next iteration.
		nonOverlapping = append(nonOverlapping, combined[0])
		cur = &combined[1]
	}

	// Now, detect gaps between the non-overlapping metas and the ownership range.
	// The left bound of the ownership range will be adjusted as we go.
	leftBound := ownershipRange.Min
	for _, meta := range nonOverlapping {

		clippedMeta := meta.Intersection(ownershipRange)
		// should never happen as long as we are only combining metas
		// that intersect with the ownership range
		if clippedMeta == nil {
			return nil, fmt.Errorf("meta is not within ownership range: %v", meta)
		}

		searchRange := ownershipRange.Slice(leftBound, clippedMeta.Max)
		// update the left bound for the next iteration
		leftBound = min(clippedMeta.Max+1, ownershipRange.Max+1)

		// since we've already ensured that the meta is within the ownership range,
		// we know the xor will be of length zero (when the meta is equal to the ownership range)
		// or 1 (when the meta is a subset of the ownership range)
		xors := searchRange.Unless(*clippedMeta)
		if len(xors) == 0 {
			// meta is equal to the ownership range. This means the meta
			// covers this entire section of the ownership range.
			continue
		}

		gaps = append(gaps, xors[0])
	}

	if leftBound <= ownershipRange.Max {
		// There is a gap between the last meta and the end of the ownership range.
		gaps = append(gaps, v1.NewBounds(leftBound, ownershipRange.Max))
	}

	return gaps, nil
}
