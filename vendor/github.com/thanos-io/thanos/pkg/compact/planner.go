// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package compact

import (
	"context"
	"fmt"
	"math"
	"path/filepath"

	"github.com/go-kit/kit/log"
	"github.com/oklog/ulid"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/thanos/pkg/block"
	"github.com/thanos-io/thanos/pkg/block/metadata"
	"github.com/thanos-io/thanos/pkg/objstore"
)

type tsdbBasedPlanner struct {
	logger log.Logger

	ranges []int64

	noCompBlocksFunc func() map[ulid.ULID]*metadata.NoCompactMark
}

var _ Planner = &tsdbBasedPlanner{}

// NewTSDBBasedPlanner is planner with the same functionality as Prometheus' TSDB.
// TODO(bwplotka): Consider upstreaming this to Prometheus.
// It's the same functionality just without accessing filesystem.
func NewTSDBBasedPlanner(logger log.Logger, ranges []int64) *tsdbBasedPlanner {
	return &tsdbBasedPlanner{
		logger: logger,
		ranges: ranges,
		noCompBlocksFunc: func() map[ulid.ULID]*metadata.NoCompactMark {
			return make(map[ulid.ULID]*metadata.NoCompactMark)
		},
	}
}

// NewPlanner is a default Thanos planner with the same functionality as Prometheus' TSDB plus special handling of excluded blocks.
// It's the same functionality just without accessing filesystem, and special handling of excluded blocks.
func NewPlanner(logger log.Logger, ranges []int64, noCompBlocks *GatherNoCompactionMarkFilter) *tsdbBasedPlanner {
	return &tsdbBasedPlanner{logger: logger, ranges: ranges, noCompBlocksFunc: noCompBlocks.NoCompactMarkedBlocks}
}

// TODO(bwplotka): Consider smarter algorithm, this prefers smaller iterative compactions vs big single one: https://github.com/thanos-io/thanos/issues/3405
func (p *tsdbBasedPlanner) Plan(_ context.Context, metasByMinTime []*metadata.Meta) ([]*metadata.Meta, error) {
	return p.plan(p.noCompBlocksFunc(), metasByMinTime)
}

func (p *tsdbBasedPlanner) plan(noCompactMarked map[ulid.ULID]*metadata.NoCompactMark, metasByMinTime []*metadata.Meta) ([]*metadata.Meta, error) {
	notExcludedMetasByMinTime := make([]*metadata.Meta, 0, len(metasByMinTime))
	for _, meta := range metasByMinTime {
		if _, excluded := noCompactMarked[meta.ULID]; excluded {
			continue
		}
		notExcludedMetasByMinTime = append(notExcludedMetasByMinTime, meta)
	}

	res := selectOverlappingMetas(notExcludedMetasByMinTime)
	if len(res) > 0 {
		return res, nil
	}
	// No overlapping blocks, do compaction the usual way.

	// We do not include a recently producted block with max(minTime), so the block which was just uploaded to bucket.
	// This gives users a window of a full block size maintenance if needed.
	if _, excluded := noCompactMarked[metasByMinTime[len(metasByMinTime)-1].ULID]; !excluded {
		notExcludedMetasByMinTime = notExcludedMetasByMinTime[:len(notExcludedMetasByMinTime)-1]
	}
	metasByMinTime = metasByMinTime[:len(metasByMinTime)-1]
	res = append(res, selectMetas(p.ranges, noCompactMarked, metasByMinTime)...)
	if len(res) > 0 {
		return res, nil
	}

	// Compact any blocks with big enough time range that have >5% tombstones.
	for i := len(notExcludedMetasByMinTime) - 1; i >= 0; i-- {
		meta := notExcludedMetasByMinTime[i]
		if meta.MaxTime-meta.MinTime < p.ranges[len(p.ranges)/2] {
			break
		}
		if float64(meta.Stats.NumTombstones)/float64(meta.Stats.NumSeries+1) > 0.05 {
			return []*metadata.Meta{notExcludedMetasByMinTime[i]}, nil
		}
	}

	return nil, nil
}

// selectMetas returns the dir metas that should be compacted into a single new block.
// If only a single block range is configured, the result is always nil.
// Copied and adjusted from https://github.com/prometheus/prometheus/blob/3d8826a3d42566684283a9b7f7e812e412c24407/tsdb/compact.go#L229.
func selectMetas(ranges []int64, noCompactMarked map[ulid.ULID]*metadata.NoCompactMark, metasByMinTime []*metadata.Meta) []*metadata.Meta {
	if len(ranges) < 2 || len(metasByMinTime) < 1 {
		return nil
	}
	highTime := metasByMinTime[len(metasByMinTime)-1].MinTime

	for _, iv := range ranges[1:] {
		parts := splitByRange(metasByMinTime, iv)
		if len(parts) == 0 {
			continue
		}
	Outer:
		for _, p := range parts {
			// Do not select the range if it has a block whose compaction failed.
			for _, m := range p {
				if m.Compaction.Failed {
					continue Outer
				}
			}

			if len(p) < 2 {
				continue
			}

			mint := p[0].MinTime
			maxt := p[len(p)-1].MaxTime

			// Pick the range of blocks if it spans the full range (potentially with gaps) or is before the most recent block.
			// This ensures we don't compact blocks prematurely when another one of the same size still would fits in the range
			// after upload.
			if maxt-mint != iv && maxt > highTime {
				continue
			}

			// Check if any of resulted blocks are excluded. Exclude them in a way that does not introduce gaps to the system
			// as well as preserve the ranges that would be used if they were not excluded.
			// This is meant as short-term workaround to create ability for marking some blocks to not be touched for compaction.
			lastExcluded := 0
			for i, id := range p {
				if _, excluded := noCompactMarked[id.ULID]; !excluded {
					continue
				}
				if len(p[lastExcluded:i]) > 1 {
					return p[lastExcluded:i]
				}
				lastExcluded = i + 1
			}
			if len(p[lastExcluded:]) > 1 {
				return p[lastExcluded:]
			}
		}
	}

	return nil
}

// selectOverlappingMetas returns all dirs with overlapping time ranges.
// It expects sorted input by mint and returns the overlapping dirs in the same order as received.
// Copied and adjusted from https://github.com/prometheus/prometheus/blob/3d8826a3d42566684283a9b7f7e812e412c24407/tsdb/compact.go#L268.
func selectOverlappingMetas(metasByMinTime []*metadata.Meta) []*metadata.Meta {
	if len(metasByMinTime) < 2 {
		return nil
	}
	var overlappingMetas []*metadata.Meta
	globalMaxt := metasByMinTime[0].MaxTime
	for i, m := range metasByMinTime[1:] {
		if m.MinTime < globalMaxt {
			if len(overlappingMetas) == 0 {
				// When it is the first overlap, need to add the last one as well.
				overlappingMetas = append(overlappingMetas, metasByMinTime[i])
			}
			overlappingMetas = append(overlappingMetas, m)
		} else if len(overlappingMetas) > 0 {
			break
		}

		if m.MaxTime > globalMaxt {
			globalMaxt = m.MaxTime
		}
	}
	return overlappingMetas
}

// splitByRange splits the directories by the time range. The range sequence starts at 0.
//
// For example, if we have blocks [0-10, 10-20, 50-60, 90-100] and the split range tr is 30
// it returns [0-10, 10-20], [50-60], [90-100].
// Copied and adjusted from: https://github.com/prometheus/prometheus/blob/3d8826a3d42566684283a9b7f7e812e412c24407/tsdb/compact.go#L294.
func splitByRange(metasByMinTime []*metadata.Meta, tr int64) [][]*metadata.Meta {
	var splitDirs [][]*metadata.Meta

	for i := 0; i < len(metasByMinTime); {
		var (
			group []*metadata.Meta
			t0    int64
			m     = metasByMinTime[i]
		)
		// Compute start of aligned time range of size tr closest to the current block's start.
		if m.MinTime >= 0 {
			t0 = tr * (m.MinTime / tr)
		} else {
			t0 = tr * ((m.MinTime - tr + 1) / tr)
		}

		// Skip blocks that don't fall into the range. This can happen via mis-alignment or
		// by being the multiple of the intended range.
		if m.MaxTime > t0+tr {
			i++
			continue
		}

		// Add all metas to the current group that are within [t0, t0+tr].
		for ; i < len(metasByMinTime); i++ {
			// Either the block falls into the next range or doesn't fit at all (checked above).
			if metasByMinTime[i].MaxTime > t0+tr {
				break
			}
			group = append(group, metasByMinTime[i])
		}

		if len(group) > 0 {
			splitDirs = append(splitDirs, group)
		}
	}

	return splitDirs
}

type largeTotalIndexSizeFilter struct {
	*tsdbBasedPlanner

	bkt                    objstore.Bucket
	markedForNoCompact     prometheus.Counter
	totalMaxIndexSizeBytes int64
}

var _ Planner = &largeTotalIndexSizeFilter{}

// WithLargeTotalIndexSizeFilter wraps Planner with largeTotalIndexSizeFilter that checks the given plans and estimates total index size.
// When found, it marks block for no compaction by placing no-compact-mark.json and updating cache.
// NOTE: The estimation is very rough as it assumes extreme cases of indexes sharing no bytes, thus summing all source index sizes.
// Adjust limit accordingly reducing to some % of actual limit you want to give.
// TODO(bwplotka): This is short term fix for https://github.com/thanos-io/thanos/issues/1424, replace with vertical block sharding https://github.com/thanos-io/thanos/pull/3390.
func WithLargeTotalIndexSizeFilter(with *tsdbBasedPlanner, bkt objstore.Bucket, totalMaxIndexSizeBytes int64, markedForNoCompact prometheus.Counter) *largeTotalIndexSizeFilter {
	return &largeTotalIndexSizeFilter{tsdbBasedPlanner: with, bkt: bkt, totalMaxIndexSizeBytes: totalMaxIndexSizeBytes, markedForNoCompact: markedForNoCompact}
}

func (t *largeTotalIndexSizeFilter) Plan(ctx context.Context, metasByMinTime []*metadata.Meta) ([]*metadata.Meta, error) {
	noCompactMarked := t.noCompBlocksFunc()
	copiedNoCompactMarked := make(map[ulid.ULID]*metadata.NoCompactMark, len(noCompactMarked))
	for k, v := range noCompactMarked {
		copiedNoCompactMarked[k] = v
	}

PlanLoop:
	for {
		plan, err := t.plan(copiedNoCompactMarked, metasByMinTime)
		if err != nil {
			return nil, err
		}
		var totalIndexBytes, maxIndexSize int64 = 0, math.MinInt64
		var biggestIndex int
		for i, p := range plan {
			indexSize := int64(-1)
			for _, f := range p.Thanos.Files {
				if f.RelPath == block.IndexFilename {
					indexSize = f.SizeBytes
				}
			}
			if indexSize <= 0 {
				// Get size from bkt instead.
				attr, err := t.bkt.Attributes(ctx, filepath.Join(p.ULID.String(), block.IndexFilename))
				if err != nil {
					return nil, errors.Wrapf(err, "get attr of %v", filepath.Join(p.ULID.String(), block.IndexFilename))
				}
				indexSize = attr.Size
			}

			if maxIndexSize < indexSize {
				maxIndexSize = indexSize
				biggestIndex = i
			}
			totalIndexBytes += indexSize
			// Leave 15% headroom for index compaction bloat.
			if totalIndexBytes >= int64(float64(t.totalMaxIndexSizeBytes)*0.85) {
				// Marking blocks for no compact to limit size.
				// TODO(bwplotka): Make sure to reset cache once this is done: https://github.com/thanos-io/thanos/issues/3408
				if err := block.MarkForNoCompact(
					ctx,
					t.logger,
					t.bkt,
					plan[biggestIndex].ULID,
					metadata.IndexSizeExceedingNoCompactReason,
					fmt.Sprintf("largeTotalIndexSizeFilter: Total compacted block's index size could exceed: %v with this block. See https://github.com/thanos-io/thanos/issues/1424", t.totalMaxIndexSizeBytes),
					t.markedForNoCompact,
				); err != nil {
					return nil, errors.Wrapf(err, "mark %v for no compaction", plan[biggestIndex].ULID.String())
				}
				// Make sure wrapped planner exclude this block.
				copiedNoCompactMarked[plan[biggestIndex].ULID] = &metadata.NoCompactMark{ID: plan[biggestIndex].ULID, Version: metadata.NoCompactMarkVersion1}
				continue PlanLoop
			}
		}
		// Planned blocks should not exceed limit.
		return plan, nil
	}
}
