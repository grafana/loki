package bloomcompactor

import (
	"context"
	"fmt"
	"sort"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"

	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/tsdb"
)

type SimpleBloomController struct {
	ownershipRange v1.FingerprintBounds // ownership range of this controller
	tsdbStore      TSDBStore
	metaStore      MetaStore
	blockStore     BlockStore
	chunkLoader    ChunkLoader
	rwFn           func() (v1.BlockWriter, v1.BlockReader)
	metrics        *Metrics

	// TODO(owen-d): add metrics
	logger log.Logger
}

func NewSimpleBloomController(
	ownershipRange v1.FingerprintBounds,
	tsdbStore TSDBStore,
	metaStore MetaStore,
	blockStore BlockStore,
	chunkLoader ChunkLoader,
	rwFn func() (v1.BlockWriter, v1.BlockReader),
	metrics *Metrics,
	logger log.Logger,
) *SimpleBloomController {
	return &SimpleBloomController{
		ownershipRange: ownershipRange,
		tsdbStore:      tsdbStore,
		metaStore:      metaStore,
		blockStore:     blockStore,
		chunkLoader:    chunkLoader,
		rwFn:           rwFn,
		metrics:        metrics,
		logger:         log.With(logger, "ownership", ownershipRange),
	}
}

func (s *SimpleBloomController) do(ctx context.Context) error {
	// 1. Resolve TSDBs
	tsdbs, err := s.tsdbStore.ResolveTSDBs()
	if err != nil {
		level.Error(s.logger).Log("msg", "failed to resolve tsdbs", "err", err)
		return errors.Wrap(err, "failed to resolve tsdbs")
	}

	// 2. Resolve Metas
	metaRefs, err := s.metaStore.ResolveMetas(s.ownershipRange)
	if err != nil {
		level.Error(s.logger).Log("msg", "failed to resolve metas", "err", err)
		return errors.Wrap(err, "failed to resolve metas")
	}

	// 3. Fetch metas
	metas, err := s.metaStore.GetMetas(metaRefs)
	if err != nil {
		level.Error(s.logger).Log("msg", "failed to get metas", "err", err)
		return errors.Wrap(err, "failed to get metas")
	}

	ids := make([]tsdb.Identifier, 0, len(tsdbs))
	for _, id := range tsdbs {
		ids = append(ids, id)
	}

	// 4. Determine which TSDBs have gaps in the ownership range and need to
	// be processed.
	tsdbsWithGaps, err := gapsBetweenTSDBsAndMetas(s.ownershipRange, ids, metas)
	if err != nil {
		level.Error(s.logger).Log("msg", "failed to find gaps", "err", err)
		return errors.Wrap(err, "failed to find gaps")
	}

	if len(tsdbsWithGaps) == 0 {
		level.Debug(s.logger).Log("msg", "blooms exist for all tsdbs")
		return nil
	}

	work, err := blockPlansForGaps(tsdbsWithGaps, metas)
	if err != nil {
		level.Error(s.logger).Log("msg", "failed to create plan", "err", err)
		return errors.Wrap(err, "failed to create plan")
	}

	// 5. Generate Blooms
	// Now that we have the gaps, we will generate a bloom block for each gap.
	// We can accelerate this by using existing blocks which may already contain
	// needed chunks in their blooms, for instance after a new TSDB version is generated
	// but contains many of the same chunk references from the previous version.
	// To do this, we'll need to take the metas we've already resolved and find blocks
	// overlapping the ownership ranges we've identified as needing updates.
	// With these in hand, we can download the old blocks and use them to
	// accelerate bloom generation for the new blocks.

	var (
		blockCt int
		tsdbCt  = len(work)
	)

	for _, plan := range work {

		for _, gap := range plan.gaps {
			// Fetch blocks that aren't up to date but are in the desired fingerprint range
			// to try and accelerate bloom creation
			seriesItr, preExistingBlocks, err := s.loadWorkForGap(plan.tsdb, gap)
			if err != nil {
				level.Error(s.logger).Log("msg", "failed to get series and blocks", "err", err)
				return errors.Wrap(err, "failed to get series and blocks")
			}

			gen := NewSimpleBloomGenerator(
				v1.DefaultBlockOptions,
				seriesItr,
				s.chunkLoader,
				preExistingBlocks,
				s.rwFn,
				s.metrics,
				log.With(s.logger, "tsdb", plan.tsdb.Name(), "ownership", gap, "blocks", len(preExistingBlocks)),
			)

			_, newBlocks, err := gen.Generate(ctx)
			if err != nil {
				// TODO(owen-d): metrics
				level.Error(s.logger).Log("msg", "failed to generate bloom", "err", err)
				return errors.Wrap(err, "failed to generate bloom")
			}

			// TODO(owen-d): dispatch this to a queue for writing, handling retries/backpressure, etc?
			for newBlocks.Next() {
				blockCt++
				blk := newBlocks.At()
				if err := s.blockStore.PutBlock(blk); err != nil {
					level.Error(s.logger).Log("msg", "failed to write block", "err", err)
					return errors.Wrap(err, "failed to write block")
				}
			}

			if err := newBlocks.Err(); err != nil {
				// TODO(owen-d): metrics
				level.Error(s.logger).Log("msg", "failed to generate bloom", "err", err)
				return errors.Wrap(err, "failed to generate bloom")
			}

		}
	}

	level.Debug(s.logger).Log("msg", "finished bloom generation", "blocks", blockCt, "tsdbs", tsdbCt)
	return nil

}

func (s *SimpleBloomController) loadWorkForGap(id tsdb.Identifier, gap gapWithBlocks) (v1.CloseableIterator[*v1.Series], []*v1.Block, error) {
	// load a series iterator for the gap
	seriesItr, err := s.tsdbStore.LoadTSDB(id, gap.bounds)
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to load tsdb")
	}

	blocks, err := s.blockStore.GetBlocks(gap.blocks)
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to get blocks")
	}

	return seriesItr, blocks, nil
}

type gapWithBlocks struct {
	bounds v1.FingerprintBounds
	blocks []BlockRef
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
	tsdb tsdb.Identifier
	gaps []gapWithBlocks
}

// blockPlansForGaps groups tsdb gaps we wish to fill with overlapping but out of date blocks.
// This allows us to expedite bloom generation by using existing blocks to fill in the gaps
// since many will contain the same chunks.
func blockPlansForGaps(tsdbs []tsdbGaps, metas []Meta) ([]blockPlan, error) {
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

				if meta.OwnershipRange.Intersection(gap) == nil {
					// this meta doesn't overlap the gap, skip
					continue
				}

				for _, block := range meta.Blocks {
					if block.OwnershipRange.Intersection(gap) == nil {
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
				return planGap.blocks[i].OwnershipRange.Less(planGap.blocks[j].OwnershipRange)
			})

			peekingBlocks := v1.NewPeekingIter[BlockRef](
				v1.NewSliceIter[BlockRef](
					planGap.blocks,
				),
			)
			// dedupe blocks which could be in multiple metas
			itr := v1.NewDedupingIter[BlockRef, BlockRef](
				func(a, b BlockRef) bool {
					return a == b
				},
				v1.Identity[BlockRef],
				func(a, _ BlockRef) BlockRef {
					return a
				},
				peekingBlocks,
			)

			deduped, err := v1.Collect[BlockRef](itr)
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
	tsdb tsdb.Identifier
	gaps []v1.FingerprintBounds
}

// tsdbsUpToDate returns if the metas are up to date with the tsdbs. This is determined by asserting
// that for each TSDB, there are metas covering the entire ownership range which were generated from that specific TSDB.
func gapsBetweenTSDBsAndMetas(
	ownershipRange v1.FingerprintBounds,
	tsdbs []tsdb.Identifier,
	metas []Meta,
) (res []tsdbGaps, err error) {
	for _, db := range tsdbs {
		id := db.Name()

		relevantMetas := make([]v1.FingerprintBounds, 0, len(metas))
		for _, meta := range metas {
			for _, s := range meta.Sources {
				if s.Name() == id {
					relevantMetas = append(relevantMetas, meta.OwnershipRange)
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
