package bloomcompactor

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"sort"
	"sync"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/chunkenc"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb"
)

type SimpleBloomController struct {
	tsdbStore   TSDBStore
	bloomStore  bloomshipper.Store
	chunkLoader ChunkLoader
	metrics     *Metrics
	limits      Limits

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

/*
Compaction works as follows, split across many functions for clarity:
 1. Fetch all meta.jsons for the given tenant and table which overlap the ownership range of this compactor.
 2. Load current TSDBs for this tenant/table.
 3. For each live TSDB (there should be only 1, but this works with multiple), find any gaps
    (fingerprint ranges) which are not up date, determined by checking other meta.jsons and comparing
    the tsdbs they were generated from + their ownership ranges.
 4. Build new bloom blocks for each gap, using the series and chunks from the TSDBs and any existing
    blocks which overlap the gaps to accelerate bloom generation.
 5. Write the new blocks and metas to the store.
 6. Determine if any meta.jsons overlap the ownership range but are outdated, and remove them and
    their associated blocks if so.
*/
func (s *SimpleBloomController) compactTenant(
	ctx context.Context,
	table config.DayTable,
	tenant string,
	ownershipRange v1.FingerprintBounds,
	tracker *compactionTracker,
) error {
	logger := log.With(s.logger, "org_id", tenant, "table", table.Addr(), "ownership", ownershipRange.String())

	client, err := s.bloomStore.Client(table.ModelTime())
	if err != nil {
		level.Error(logger).Log("msg", "failed to get client", "err", err)
		return errors.Wrap(err, "failed to get client")
	}

	// Fetch source metas to be used in both compaction and cleanup of out-of-date metas+blooms
	metas, err := s.bloomStore.FetchMetas(
		ctx,
		bloomshipper.MetaSearchParams{
			TenantID: tenant,
			Interval: bloomshipper.NewInterval(table.Bounds()),
			Keyspace: ownershipRange,
		},
	)
	if err != nil {
		level.Error(logger).Log("msg", "failed to get metas", "err", err)
		return errors.Wrap(err, "failed to get metas")
	}

	level.Debug(logger).Log("msg", "found relevant metas", "metas", len(metas))

	// fetch all metas overlapping our ownership range so we can safely
	// check which metas can be deleted even if they only partially overlap out ownership range
	superset, err := s.fetchSuperSet(ctx, tenant, table, ownershipRange, metas, logger)
	if err != nil {
		return errors.Wrap(err, "failed to fetch superset")
	}

	// build compaction plans
	work, err := s.findOutdatedGaps(ctx, tenant, table, ownershipRange, metas, logger)
	if err != nil {
		return errors.Wrap(err, "failed to find outdated gaps")
	}

	// build new blocks
	built, err := s.buildGaps(ctx, tenant, table, ownershipRange, client, work, tracker, logger)
	if err != nil {
		return errors.Wrap(err, "failed to build gaps")
	}

	// combine built and superset metas
	// in preparation for removing outdated ones
	combined := append(superset, built...)

	outdated, err := outdatedMetas(combined)
	if err != nil {
		return errors.Wrap(err, "failed to find outdated metas")
	}
	level.Debug(logger).Log("msg", "found outdated metas", "outdated", len(outdated))

	var (
		deletedMetas  int
		deletedBlocks int
	)
	defer func() {
		s.metrics.metasDeleted.Add(float64(deletedMetas))
		s.metrics.blocksDeleted.Add(float64(deletedBlocks))
	}()

	for _, meta := range outdated {
		for _, block := range meta.Blocks {
			err := client.DeleteBlocks(ctx, []bloomshipper.BlockRef{block})
			if err != nil {
				if client.IsObjectNotFoundErr(err) {
					level.Debug(logger).Log("msg", "block not found while attempting delete, continuing", "block", block.String())
				} else {
					level.Error(logger).Log("msg", "failed to delete block", "err", err, "block", block.String())
					return errors.Wrap(err, "failed to delete block")
				}
			}
			deletedBlocks++
			level.Debug(logger).Log("msg", "removed outdated block", "block", block.String())
		}

		err = client.DeleteMetas(ctx, []bloomshipper.MetaRef{meta.MetaRef})
		if err != nil {
			if client.IsObjectNotFoundErr(err) {
				level.Debug(logger).Log("msg", "meta not found while attempting delete, continuing", "meta", meta.MetaRef.String())
			} else {
				level.Error(logger).Log("msg", "failed to delete meta", "err", err, "meta", meta.MetaRef.String())
				return errors.Wrap(err, "failed to delete meta")
			}
		}
		deletedMetas++
		level.Debug(logger).Log("msg", "removed outdated meta", "meta", meta.MetaRef.String())
	}

	level.Debug(logger).Log("msg", "finished compaction")
	return nil
}

// fetchSuperSet fetches all metas which overlap the ownership range of the first set of metas we've resolved
func (s *SimpleBloomController) fetchSuperSet(
	ctx context.Context,
	tenant string,
	table config.DayTable,
	ownershipRange v1.FingerprintBounds,
	metas []bloomshipper.Meta,
	logger log.Logger,
) ([]bloomshipper.Meta, error) {
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
			return nil, errors.New("meta bounds union is not a single range")
		}
		superset = union[0]
	}

	within := superset.Within(ownershipRange)
	level.Debug(logger).Log(
		"msg", "looking for superset metas",
		"superset", superset.String(),
		"superset_within", within,
	)

	if within {
		// we don't need to fetch any more metas
		// NB(owen-d): here we copy metas into the output. This is slightly inefficient, but
		// helps prevent mutability bugs by returning the same slice as the input.
		results := make([]bloomshipper.Meta, len(metas))
		copy(results, metas)
		return results, nil
	}

	supersetMetas, err := s.bloomStore.FetchMetas(
		ctx,
		bloomshipper.MetaSearchParams{
			TenantID: tenant,
			Interval: bloomshipper.NewInterval(table.Bounds()),
			Keyspace: superset,
		},
	)

	if err != nil {
		level.Error(logger).Log("msg", "failed to get meta superset range", "err", err, "superset", superset)
		return nil, errors.Wrap(err, "failed to get meta supseret range")
	}

	level.Debug(logger).Log(
		"msg", "found superset metas",
		"metas", len(metas),
		"fresh_metas", len(supersetMetas),
		"delta", len(supersetMetas)-len(metas),
	)

	return supersetMetas, nil
}

func (s *SimpleBloomController) findOutdatedGaps(
	ctx context.Context,
	tenant string,
	table config.DayTable,
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
	table config.DayTable,
	tenant string,
	id tsdb.Identifier,
	gap gapWithBlocks,
) (v1.Iterator[*v1.Series], v1.CloseableResettableIterator[*v1.SeriesWithBlooms], error) {
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

	// NB(owen-d): we filter out nil blocks here to avoid panics in the bloom generator since the fetcher
	// input->output length and indexing in its contract
	// NB(chaudum): Do we want to fetch in strict mode and fail instead?
	f := FetchFunc[bloomshipper.BlockRef, *bloomshipper.CloseableBlockQuerier](func(ctx context.Context, refs []bloomshipper.BlockRef) ([]*bloomshipper.CloseableBlockQuerier, error) {
		blks, err := fetcher.FetchBlocks(ctx, refs, bloomshipper.WithFetchAsync(false), bloomshipper.WithIgnoreNotFound(true))
		if err != nil {
			return nil, err
		}
		exists := make([]*bloomshipper.CloseableBlockQuerier, 0, len(blks))
		for _, blk := range blks {
			if blk != nil {
				exists = append(exists, blk)
			}
		}
		return exists, nil
	})
	blocksIter := newBlockLoadingIter(ctx, gap.blocks, f, 10)

	return seriesItr, blocksIter, nil
}

func (s *SimpleBloomController) buildGaps(
	ctx context.Context,
	tenant string,
	table config.DayTable,
	ownershipRange v1.FingerprintBounds,
	client bloomshipper.Client,
	work []blockPlan,
	tracker *compactionTracker,
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

	blockEnc, err := chunkenc.ParseEncoding(s.limits.BloomBlockEncoding(tenant))
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse block encoding")
	}

	var (
		blockCt      int
		tsdbCt       = len(work)
		nGramSize    = uint64(s.limits.BloomNGramLength(tenant))
		nGramSkip    = uint64(s.limits.BloomNGramSkip(tenant))
		maxBlockSize = uint64(s.limits.BloomCompactorMaxBlockSize(tenant))
		maxBloomSize = uint64(s.limits.BloomCompactorMaxBloomSize(tenant))
		blockOpts    = v1.NewBlockOptions(blockEnc, nGramSize, nGramSkip, maxBlockSize, maxBloomSize)
		created      []bloomshipper.Meta
		totalSeries  int
		bytesAdded   int
	)

	for i, plan := range work {

		reporter := biasedReporter(func(fp model.Fingerprint) {
			tracker.update(tenant, table.DayTime, ownershipRange, fp)
		}, ownershipRange, i, len(work))

		for i := range plan.gaps {
			gap := plan.gaps[i]
			logger := log.With(logger, "gap", gap.bounds.String(), "tsdb", plan.tsdb.Name())

			meta := bloomshipper.Meta{
				MetaRef: bloomshipper.MetaRef{
					Ref: bloomshipper.Ref{
						TenantID:  tenant,
						TableName: table.Addr(),
						Bounds:    gap.bounds,
					},
				},
				Sources: []tsdb.SingleTenantTSDBIdentifier{plan.tsdb},
			}

			// Fetch blocks that aren't up to date but are in the desired fingerprint range
			// to try and accelerate bloom creation
			level.Debug(logger).Log("msg", "loading series and blocks for gap", "blocks", len(gap.blocks))
			seriesItr, blocksIter, err := s.loadWorkForGap(ctx, table, tenant, plan.tsdb, gap)
			if err != nil {
				level.Error(logger).Log("msg", "failed to get series and blocks", "err", err)
				return nil, errors.Wrap(err, "failed to get series and blocks")
			}

			// TODO(owen-d): more elegant error handling than sync.OnceFunc
			closeBlocksIter := sync.OnceFunc(func() {
				if err := blocksIter.Close(); err != nil {
					level.Error(logger).Log("msg", "failed to close blocks iterator", "err", err)
				}
			})
			defer closeBlocksIter()

			// Blocks are built consuming the series iterator. For observability, we wrap the series iterator
			// with a counter iterator to count the number of times Next() is called on it.
			// This is used to observe the number of series that are being processed.
			seriesItrWithCounter := v1.NewCounterIter[*v1.Series](seriesItr)

			gen := NewSimpleBloomGenerator(
				tenant,
				blockOpts,
				seriesItrWithCounter,
				s.chunkLoader,
				blocksIter,
				s.rwFn,
				reporter,
				s.metrics,
				logger,
			)

			level.Debug(logger).Log("msg", "generating blocks", "overlapping_blocks", len(gap.blocks))

			newBlocks := gen.Generate(ctx)
			if err != nil {
				level.Error(logger).Log("msg", "failed to generate bloom", "err", err)
				return nil, errors.Wrap(err, "failed to generate bloom")
			}

			for newBlocks.Next() && newBlocks.Err() == nil {
				blockCt++
				blk := newBlocks.At()

				built, err := bloomshipper.BlockFrom(tenant, table.Addr(), blk)
				if err != nil {
					level.Error(logger).Log("msg", "failed to build block", "err", err)
					return nil, errors.Wrap(err, "failed to build block")
				}

				if err := client.PutBlock(
					ctx,
					built,
				); err != nil {
					level.Error(logger).Log("msg", "failed to write block", "err", err)
					return nil, errors.Wrap(err, "failed to write block")
				}
				s.metrics.blocksCreated.Inc()

				totalGapKeyspace := (gap.bounds.Max - gap.bounds.Min)
				progress := (built.Bounds.Max - gap.bounds.Min)
				pct := float64(progress) / float64(totalGapKeyspace) * 100
				level.Debug(logger).Log(
					"msg", "uploaded block",
					"block", built.BlockRef.String(),
					"progress_pct", fmt.Sprintf("%.2f", pct),
				)

				meta.Blocks = append(meta.Blocks, built.BlockRef)
			}

			if err := newBlocks.Err(); err != nil {
				level.Error(logger).Log("msg", "failed to generate bloom", "err", err)
				return nil, errors.Wrap(err, "failed to generate bloom")
			}

			closeBlocksIter()
			bytesAdded += newBlocks.Bytes()
			totalSeries += seriesItrWithCounter.Count()
			s.metrics.blocksReused.Add(float64(len(gap.blocks)))

			// Write the new meta
			// TODO(owen-d): put total size in log, total time in metrics+log
			ref, err := bloomshipper.MetaRefFrom(tenant, table.Addr(), gap.bounds, meta.Sources, meta.Blocks)
			if err != nil {
				level.Error(logger).Log("msg", "failed to checksum meta", "err", err)
				return nil, errors.Wrap(err, "failed to checksum meta")
			}
			meta.MetaRef = ref

			if err := client.PutMeta(ctx, meta); err != nil {
				level.Error(logger).Log("msg", "failed to write meta", "err", err)
				return nil, errors.Wrap(err, "failed to write meta")
			}

			s.metrics.metasCreated.Inc()
			level.Debug(logger).Log("msg", "uploaded meta", "meta", meta.MetaRef.String())
			created = append(created, meta)
		}
	}

	s.metrics.seriesPerCompaction.Observe(float64(totalSeries))
	s.metrics.bytesPerCompaction.Observe(float64(bytesAdded))
	level.Debug(logger).Log("msg", "finished bloom generation", "blocks", blockCt, "tsdbs", tsdbCt, "series", totalSeries, "bytes_added", bytesAdded)
	return created, nil
}

// Simple way to ensure increasing progress reporting
// and likely unused in practice because we don't expect to see more than 1 tsdb to compact.
// We assume each TSDB accounts for the same amount of work and only move progress forward
// depending on the current TSDB's index. For example, if we have 2 TSDBs and a fingerprint
// range from 0-100 (valid for both TSDBs), we'll limit reported progress for each TSDB to 50%.
func biasedReporter(
	f func(model.Fingerprint),
	ownershipRange v1.FingerprintBounds,
	i,
	total int,
) func(model.Fingerprint) {
	return func(fp model.Fingerprint) {
		clipped := min(max(fp, ownershipRange.Min), ownershipRange.Max)
		delta := (clipped - ownershipRange.Min) / model.Fingerprint(total)
		step := model.Fingerprint(ownershipRange.Range() / uint64(total))
		res := ownershipRange.Min + (step * model.Fingerprint(i)) + delta
		f(res)
	}
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
		// We do the max to prevent the max bound to overflow from MaxUInt64 to 0
		leftBound = min(
			max(clippedMeta.Max+1, clippedMeta.Max),
			max(ownershipRange.Max+1, ownershipRange.Max),
		)

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

	// If the leftBound is less than the ownership range max, and it's smaller than MaxUInt64,
	// There is a gap between the last meta and the end of the ownership range.
	// Note: we check `leftBound < math.MaxUint64` since in the loop above we clamp the
	// leftBound to MaxUint64 to prevent an overflow to 0: `max(clippedMeta.Max+1, clippedMeta.Max)`
	if leftBound < math.MaxUint64 && leftBound <= ownershipRange.Max {
		gaps = append(gaps, v1.NewBounds(leftBound, ownershipRange.Max))
	}

	return gaps, nil
}
