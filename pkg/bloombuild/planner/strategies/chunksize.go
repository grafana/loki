package strategies

import (
	"context"
	"fmt"
	"github.com/dustin/go-humanize"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/loki/v3/pkg/bloombuild/protos"
	iter "github.com/grafana/loki/v3/pkg/iter/v2"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"math"
)

type ChunkSizeStrategyLimits interface {
	BloomTaskTargetChunkSizeBytes(tenantID string) uint64
}

type ChunkSizeStrategy struct {
	limits ChunkSizeStrategyLimits
	logger log.Logger
}

func NewChunkSizeStrategy(
	limits ChunkSizeStrategyLimits,
	logger log.Logger,
) (*ChunkSizeStrategy, error) {
	return &ChunkSizeStrategy{
		limits: limits,
		logger: logger,
	}, nil
}

func (s *ChunkSizeStrategy) Plan(
	ctx context.Context,
	table config.DayTable,
	tenant string,
	tsdbs TSDBSet,
	metas []bloomshipper.Meta,
) ([]*protos.Task, error) {
	targetTaskSize := s.limits.BloomTaskTargetChunkSizeBytes(tenant)

	logger := log.With(s.logger, "table", table.Addr(), "tenant", tenant)
	level.Debug(s.logger).Log("msg", "loading work for tenant", "target task size", humanize.Bytes(targetTaskSize))

	// Determine which TSDBs have gaps and need to be processed.
	tsdbsWithGaps, err := gapsBetweenTSDBsAndMetas(v1.NewBounds(0, math.MaxUint64), tsdbs, metas)
	if err != nil {
		level.Error(logger).Log("msg", "failed to find gaps", "err", err)
		return nil, fmt.Errorf("failed to find gaps: %w", err)
	}

	if len(tsdbsWithGaps) == 0 {
		level.Debug(logger).Log("msg", "blooms exist for all tsdbs")
		return nil, nil
	}

	tasks, err := s.computeTasks(ctx, tenant, table, metas, tsdbsWithGaps, targetTaskSize)
	if err != nil {
		return nil, fmt.Errorf("failed to compute block plan: %w", err)
	}

	return tasks, nil
}

func (s *ChunkSizeStrategy) computeTasks(
	ctx context.Context,
	tenant string,
	table config.DayTable,
	metas []bloomshipper.Meta,
	tsdbsWithGaps []tsdbGaps,
	targetTaskSizeBytes uint64,
) ([]*protos.Task, error) {
	sizedIter, iterSize, err := s.sizedSeriesIter(ctx, tenant, tsdbsWithGaps, targetTaskSizeBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to create sized series iterator: %w", err)
	}

	tasks := make([]*protos.Task, 0, iterSize)
	for sizedIter.Next() {
		series := sizedIter.At()
		if len(series) == 0 {
			// This should never happen, but just in case.
			level.Error(s.logger).Log("msg", "got empty series batch")
			continue
		}

		bounds := series.Bounds()

		relevantBlocks, err := getBlocksMatchingBounds(metas, bounds)
		if err != nil {
			return nil, fmt.Errorf("failed to get blocks matching bounds: %w", err)
		}

		planGap := protos.Gap{
			Bounds: bounds,
			Series: series.V1Series(),
			Blocks: relevantBlocks,
		}

		tasks = append(tasks, protos.NewTask(table, tenant, bounds, series[0].TSDB, []protos.Gap{planGap}))
	}
	if err := sizedIter.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate over sized series: %w", err)
	}

	return tasks, nil
}

func getBlocksMatchingBounds(metas []bloomshipper.Meta, bounds v1.FingerprintBounds) ([]bloomshipper.BlockRef, error) {
	blocks := make([]bloomshipper.BlockRef, 0, 10)

	for _, meta := range metas {
		if meta.Bounds.Intersection(bounds) == nil {
			// this meta doesn't overlap the gap, skip
			continue
		}

		for _, block := range meta.Blocks {
			if block.Bounds.Intersection(bounds) == nil {
				// this block doesn't overlap the gap, skip
				continue
			}
			// this block overlaps the gap, add it to the plan
			// for this gap
			blocks = append(blocks, block)
		}
	}

	return blocks, nil
}

type seriesWithChunks struct {
	TSDB   tsdb.SingleTenantTSDBIdentifier
	FP     model.Fingerprint
	Chunks []index.ChunkMeta
}

type seriesBatch []seriesWithChunks

func (sb seriesBatch) Bounds() v1.FingerprintBounds {
	if len(sb) == 0 {
		return v1.NewBounds(0, 0)
	}

	// We assume that the series are sorted by fingerprint.
	return v1.NewBounds(sb[0].FP, sb[len(sb)-1].FP)
}

func (sb seriesBatch) V1Series() []*v1.Series {
	series := make([]*v1.Series, 0, len(sb))
	for _, s := range sb {
		res := &v1.Series{
			Fingerprint: s.FP,
			Chunks:      make(v1.ChunkRefs, 0, len(s.Chunks)),
		}
		for _, chk := range s.Chunks {
			res.Chunks = append(res.Chunks, v1.ChunkRef{
				From:     model.Time(chk.MinTime),
				Through:  model.Time(chk.MaxTime),
				Checksum: chk.Checksum,
			})
		}

		series = append(series, res)
	}

	return series
}

func (s *ChunkSizeStrategy) sizedSeriesIter(
	ctx context.Context,
	tenant string,
	tsdbsWithGaps []tsdbGaps,
	targetTaskSizeBytes uint64,
) (iter.Iterator[seriesBatch], int, error) {
	batches := make([]seriesBatch, 0, 100)
	var currentBatch seriesBatch
	var currentBatchSizeBytes uint64

	for _, idx := range tsdbsWithGaps {
		for _, gap := range idx.gaps {
			if err := idx.tsdb.ForSeries(
				ctx,
				tenant,
				gap,
				0, math.MaxInt64,
				func(_ labels.Labels, fp model.Fingerprint, chks []index.ChunkMeta) (stop bool) {
					select {
					case <-ctx.Done():
						return true
					default:
						var seriesSize uint64
						for _, chk := range chks {
							seriesSize += uint64(chk.KB * 1024)
						}

						// Cut a new batch IF:
						// - The current batch is not empty --> So we add at least one series to the batch.
						// AND either:
						// - Adding this series to the batch would exceed the target task size.
						// - OR this series is from a different TSDB than the current batch.
						if len(currentBatch) > 0 && (currentBatchSizeBytes+seriesSize >= targetTaskSizeBytes || currentBatch[len(currentBatch)-1].TSDB != idx.tsdbIdentifier) {
							batches = append(batches, currentBatch)
							currentBatch = make([]seriesWithChunks, 0, 100)
							currentBatchSizeBytes = 0
						}

						currentBatchSizeBytes += seriesSize
						currentBatch = append(currentBatch, seriesWithChunks{
							TSDB:   idx.tsdbIdentifier,
							FP:     fp,
							Chunks: chks,
						})
						return false
					}
				},
				labels.MustNewMatcher(labels.MatchEqual, "", ""),
			); err != nil {
				return nil, 0, err
			}
		}
	}

	select {
	case <-ctx.Done():
		return iter.NewEmptyIter[seriesBatch](), 0, ctx.Err()
	default:
		return iter.NewCancelableIter[seriesBatch](ctx, iter.NewSliceIter[seriesBatch](batches)), len(batches), nil
	}
}
