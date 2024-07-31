package splitkeyspace

import (
	"context"
	"fmt"
	"sort"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"

	"github.com/grafana/loki/v3/pkg/bloombuild/common"
	"github.com/grafana/loki/v3/pkg/bloombuild/protos"
	iter "github.com/grafana/loki/v3/pkg/iter/v2"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb"
)

type Limits interface {
	BloomSplitSeriesKeyspaceBy(tenantID string) int
}

type Strategy struct {
	limits Limits

	logger log.Logger
}

func NewSplitKeyspaceStrategy(
	limits Limits,
	logger log.Logger,
) (*Strategy, error) {
	return &Strategy{
		limits: limits,
		logger: logger,
	}, nil
}

func (s *Strategy) Plan(
	ctx context.Context,
	table config.DayTable,
	tenant string,
	tsdbs map[tsdb.SingleTenantTSDBIdentifier]common.ClosableForSeries,
	metas []bloomshipper.Meta,
) ([]*protos.Task, error) {
	splitFactor := s.limits.BloomSplitSeriesKeyspaceBy(tenant)
	ownershipRanges := SplitFingerprintKeyspaceByFactor(splitFactor)

	logger := log.With(s.logger, "table", table.Addr(), "tenant", tenant)
	level.Debug(s.logger).Log("msg", "loading work for tenant", "splitFactor", splitFactor)

	var tasks []*protos.Task
	for _, ownershipRange := range ownershipRanges {
		logger := log.With(logger, "ownership", ownershipRange.String())

		// Filter only the metas that overlap in the ownership range
		metasInBounds := bloomshipper.FilterMetasOverlappingBounds(metas, ownershipRange)

		// Find gaps in the TSDBs for this tenant/table
		gaps, err := s.findOutdatedGaps(ctx, tenant, tsdbs, ownershipRange, metasInBounds, logger)
		if err != nil {
			level.Error(logger).Log("msg", "failed to find outdated gaps", "err", err)
			continue
		}

		for _, gap := range gaps {
			tasks = append(tasks, protos.NewTask(table, tenant, ownershipRange, gap.tsdb, gap.gaps))
		}
	}

	return tasks, nil
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
	gaps []protos.Gap
}

func (s *Strategy) findOutdatedGaps(
	ctx context.Context,
	tenant string,
	tsdbs map[tsdb.SingleTenantTSDBIdentifier]common.ClosableForSeries,
	ownershipRange v1.FingerprintBounds,
	metas []bloomshipper.Meta,
	logger log.Logger,
) ([]blockPlan, error) {
	// Determine which TSDBs have gaps in the ownership range and need to
	// be processed.
	tsdbsWithGaps, err := gapsBetweenTSDBsAndMetas(ownershipRange, tsdbs, metas)
	if err != nil {
		level.Error(logger).Log("msg", "failed to find gaps", "err", err)
		return nil, fmt.Errorf("failed to find gaps: %w", err)
	}

	if len(tsdbsWithGaps) == 0 {
		level.Debug(logger).Log("msg", "blooms exist for all tsdbs")
		return nil, nil
	}

	work, err := blockPlansForGaps(ctx, tenant, tsdbsWithGaps, metas)
	if err != nil {
		level.Error(logger).Log("msg", "failed to create plan", "err", err)
		return nil, fmt.Errorf("failed to create plan: %w", err)
	}

	return work, nil
}

// Used to signal the gaps that need to be populated for a tsdb
type tsdbGaps struct {
	tsdbIdentifier tsdb.SingleTenantTSDBIdentifier
	tsdb           common.ClosableForSeries
	gaps           []v1.FingerprintBounds
}

// gapsBetweenTSDBsAndMetas returns if the metas are up-to-date with the TSDBs. This is determined by asserting
// that for each TSDB, there are metas covering the entire ownership range which were generated from that specific TSDB.
func gapsBetweenTSDBsAndMetas(
	ownershipRange v1.FingerprintBounds,
	tsdbs map[tsdb.SingleTenantTSDBIdentifier]common.ClosableForSeries,
	metas []bloomshipper.Meta,
) (res []tsdbGaps, err error) {
	for db, tsdb := range tsdbs {
		id := db.Name()

		relevantMetas := make([]v1.FingerprintBounds, 0, len(metas))
		for _, meta := range metas {
			for _, s := range meta.Sources {
				if s.Name() == id {
					relevantMetas = append(relevantMetas, meta.Bounds)
				}
			}
		}

		gaps, err := FindGapsInFingerprintBounds(ownershipRange, relevantMetas)
		if err != nil {
			return nil, err
		}

		if len(gaps) > 0 {
			res = append(res, tsdbGaps{
				tsdbIdentifier: db,
				tsdb:           tsdb,
				gaps:           gaps,
			})
		}
	}

	return res, err
}

// blockPlansForGaps groups tsdb gaps we wish to fill with overlapping but out of date blocks.
// This allows us to expedite bloom generation by using existing blocks to fill in the gaps
// since many will contain the same chunks.
func blockPlansForGaps(
	ctx context.Context,
	tenant string,
	tsdbs []tsdbGaps,
	metas []bloomshipper.Meta,
) ([]blockPlan, error) {
	plans := make([]blockPlan, 0, len(tsdbs))

	for _, idx := range tsdbs {
		plan := blockPlan{
			tsdb: idx.tsdbIdentifier,
			gaps: make([]protos.Gap, 0, len(idx.gaps)),
		}

		for _, gap := range idx.gaps {
			planGap := protos.Gap{
				Bounds: gap,
			}

			seriesItr, err := common.NewTSDBSeriesIter(ctx, tenant, idx.tsdb, gap)
			if err != nil {
				return nil, fmt.Errorf("failed to load series from TSDB for gap (%s): %w", gap.String(), err)
			}
			planGap.Series, err = iter.Collect(seriesItr)
			if err != nil {
				return nil, fmt.Errorf("failed to collect series: %w", err)
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
					planGap.Blocks = append(planGap.Blocks, block)
				}
			}

			// ensure we sort blocks so deduping iterator works as expected
			sort.Slice(planGap.Blocks, func(i, j int) bool {
				return planGap.Blocks[i].Bounds.Less(planGap.Blocks[j].Bounds)
			})

			peekingBlocks := iter.NewPeekIter[bloomshipper.BlockRef](
				iter.NewSliceIter[bloomshipper.BlockRef](
					planGap.Blocks,
				),
			)
			// dedupe blocks which could be in multiple metas
			itr := iter.NewDedupingIter[bloomshipper.BlockRef, bloomshipper.BlockRef](
				func(a, b bloomshipper.BlockRef) bool {
					return a == b
				},
				iter.Identity[bloomshipper.BlockRef],
				func(a, _ bloomshipper.BlockRef) bloomshipper.BlockRef {
					return a
				},
				peekingBlocks,
			)

			deduped, err := iter.Collect[bloomshipper.BlockRef](itr)
			if err != nil {
				return nil, fmt.Errorf("failed to dedupe blocks: %w", err)
			}
			planGap.Blocks = deduped

			plan.gaps = append(plan.gaps, planGap)
		}

		plans = append(plans, plan)
	}

	return plans, nil
}
