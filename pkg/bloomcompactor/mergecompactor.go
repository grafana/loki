package bloomcompactor

import (
	"context"

	"github.com/grafana/dskit/concurrency"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/storage/chunk"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"

	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/stores/shipper/bloomshipper"
)

func makeSeriesIterFromSeriesMeta(job Job) *v1.SliceIter[*v1.Series] {
	// Satisfy types for series
	seriesFromSeriesMeta := make([]*v1.Series, len(job.seriesMetas))

	for i, s := range job.seriesMetas {
		crefs := make([]v1.ChunkRef, len(s.chunkRefs))
		for j, chk := range s.chunkRefs {
			crefs[j] = v1.ChunkRef{
				Start:    chk.From(),
				End:      chk.Through(),
				Checksum: chk.Checksum,
			}
		}
		seriesFromSeriesMeta[i] = &v1.Series{
			Fingerprint: s.seriesFP,
			Chunks:      crefs,
		}
	}
	return v1.NewSliceIter(seriesFromSeriesMeta)
}

func makeBlockIterFromBlocks(ctx context.Context, logger log.Logger,
	bloomShipperClient bloomshipper.Client, blocksToUpdate []bloomshipper.BlockRef,
	workingDir string) ([]v1.PeekingIterator[*v1.SeriesWithBloom], []string, error) {

	// Download existing blocks that needs compaction
	blockIters := make([]v1.PeekingIterator[*v1.SeriesWithBloom], len(blocksToUpdate))
	blockPaths := make([]string, len(blocksToUpdate))

	err := concurrency.ForEachJob(ctx, len(blocksToUpdate), len(blocksToUpdate), func(ctx context.Context, i int) error {
		b := blocksToUpdate[i]

		lazyBlock, err := bloomShipperClient.GetBlock(ctx, b)
		if err != nil {
			level.Error(logger).Log("msg", "failed downloading block", "err", err)
			return err
		}

		blockPath, err := bloomshipper.UncompressBloomBlock(&lazyBlock, workingDir, logger)
		if err != nil {
			level.Error(logger).Log("msg", "failed extracting block", "err", err)
			return err
		}
		blockPaths[i] = blockPath

		reader := v1.NewDirectoryBlockReader(blockPath)
		block := v1.NewBlock(reader)
		blockQuerier := v1.NewBlockQuerier(block)

		blockIters[i] = v1.NewPeekingIter[*v1.SeriesWithBloom](blockQuerier)
		return nil
	})

	if err != nil {
		return nil, nil, err
	}
	return blockIters, blockPaths, nil
}

func createPopulateFunc(ctx context.Context, logger log.Logger, job Job, storeClient storeClient, bt *v1.BloomTokenizer) func(series *v1.Series, bloom *v1.Bloom) error {
	return func(series *v1.Series, bloom *v1.Bloom) error {
		bloomForChks := v1.SeriesWithBloom{
			Series: series,
			Bloom:  bloom,
		}

		// Satisfy types for chunks
		chunkRefs := make([]chunk.Chunk, len(series.Chunks))
		for i, chk := range series.Chunks {
			chunkRefs[i] = chunk.Chunk{
				ChunkRef: logproto.ChunkRef{
					Fingerprint: uint64(series.Fingerprint),
					UserID:      job.tenantID,
					From:        chk.Start,
					Through:     chk.End,
					Checksum:    chk.Checksum,
				},
			}
		}

		chks, err := storeClient.chunk.GetChunks(ctx, chunkRefs)
		if err != nil {
			level.Error(logger).Log("msg", "failed downloading chunks", "err", err)
			return err
		}
		err = bt.PopulateSeriesWithBloom(&bloomForChks, chks)
		if err != nil {
			return err
		}
		return nil
	}
}

func mergeCompactChunks(logger log.Logger,
	populate func(*v1.Series, *v1.Bloom) error,
	mergeBlockBuilder *PersistentBlockBuilder,
	blockIters []v1.PeekingIterator[*v1.SeriesWithBloom], seriesIter *v1.SliceIter[*v1.Series],
	job Job) (bloomshipper.Block, error) {

	mergeBuilder := v1.NewMergeBuilder(
		blockIters,
		seriesIter,
		populate)

	checksum, err := mergeBlockBuilder.mergeBuild(mergeBuilder)
	if err != nil {
		level.Error(logger).Log("msg", "failed merging the blooms", "err", err)
		return bloomshipper.Block{}, err
	}
	data, err := mergeBlockBuilder.Data()
	if err != nil {
		level.Error(logger).Log("msg", "failed reading bloom data", "err", err)
		return bloomshipper.Block{}, err
	}

	mergedBlock := bloomshipper.Block{
		BlockRef: bloomshipper.BlockRef{
			Ref: bloomshipper.Ref{
				TenantID:       job.tenantID,
				TableName:      job.tableName,
				MinFingerprint: uint64(job.minFp),
				MaxFingerprint: uint64(job.maxFp),
				StartTimestamp: int64(job.from),
				EndTimestamp:   int64(job.through),
				Checksum:       checksum,
			},
			IndexPath: job.indexPath,
		},
		Data: data,
	}
	return mergedBlock, nil
}
