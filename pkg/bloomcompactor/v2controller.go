package bloomcompactor

import (
	"context"
	"fmt"

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

	// TODO(owen-d): add metrics
	logger log.Logger
}

func NewSimpleBloomController(
	ownershipRange v1.FingerprintBounds,
	tsdbStore TSDBStore,
	metaStore MetaStore,
	blockStore BlockStore,
	logger log.Logger,
) *SimpleBloomController {
	return &SimpleBloomController{
		ownershipRange: ownershipRange,
		tsdbStore:      tsdbStore,
		metaStore:      metaStore,
		blockStore:     blockStore,
		logger:         log.With(logger, "ownership", ownershipRange),
	}
}

func (s *SimpleBloomController) do(_ context.Context) error {
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

	metas, err := s.metaStore.GetMetas(metaRefs)
	if err != nil {
		level.Error(s.logger).Log("msg", "failed to get metas", "err", err)
		return errors.Wrap(err, "failed to get metas")
	}

	ids := make([]tsdb.Identifier, 0, len(tsdbs))
	for _, idx := range tsdbs {
		ids = append(ids, idx.Identifier)
	}

	// Determine which TSDBs have gaps in the ownership range and need to
	// be processed.
	work, err := gapsBetweenTSDBsAndMetas(s.ownershipRange, ids, metas)
	if err != nil {
		level.Error(s.logger).Log("msg", "failed to find gaps", "err", err)
		return errors.Wrap(err, "failed to find gaps")
	}

	if len(work) == 0 {
		level.Debug(s.logger).Log("msg", "blooms exist for all tsdbs")
		return nil
	}

	// TODO(owen-d): finish
	panic("not implemented")

	// Now that we have the gaps, we will generate a bloom block for each gap.
	// We can accelerate this by using existing blocks which may already contain
	// needed chunks in their blooms, for instance after a new TSDB version is generated
	// but contains many of the same chunk references from the previous version.
	// To do this, we'll need to take the metas we've already resolved and find blocks
	// overlapping the ownership ranges we've identified as needing updates.
	// With these in hand, we can download the old blocks and use them to
	// accelerate bloom generation for the new blocks.
}

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

		clippedMeta := meta.Clip(ownershipRange)
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
