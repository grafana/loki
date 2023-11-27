package bloomcompactor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/logproto"
	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/bloom/v1/filter"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/stores/shipper/bloomshipper"
	tsdbindex "github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

func makeChunkRefs(chksMetas []tsdbindex.ChunkMeta, tenant string, fp model.Fingerprint) []chunk.Chunk {
	chunkRefs := make([]chunk.Chunk, 0, len(chksMetas))
	for _, chk := range chksMetas {
		chunkRefs = append(chunkRefs, chunk.Chunk{
			ChunkRef: logproto.ChunkRef{
				Fingerprint: uint64(fp),
				UserID:      tenant,
				From:        chk.From(),
				Through:     chk.Through(),
				Checksum:    chk.Checksum,
			},
		})
	}

	return chunkRefs
}

// TODO Revisit this step once v1/bloom lib updated to combine blooms in the same series
func buildBloomBlock(
	ctx context.Context,
	logger log.Logger,
	options v1.BlockOptions,
	blooms []v1.SeriesWithBloom,
	job Job,
	workingDir string,
) (bloomshipper.Block, error) {
	// Ensure the context has not been canceled (ie. compactor shutdown has been triggered).
	if err := ctx.Err(); err != nil {
		return bloomshipper.Block{}, err
	}

	localDst := createLocalDirName(workingDir, job)

	// write bloom to a local dir
	builder, err := v1.NewBlockBuilder(options, v1.NewDirectoryBlockWriter(localDst))
	if err != nil {
		level.Error(logger).Log("creating builder", err)
		return bloomshipper.Block{}, err
	}

	checksum, err := builder.BuildFrom(v1.NewSliceIter(blooms))
	if err != nil {
		level.Error(logger).Log("writing bloom", err)
		return bloomshipper.Block{}, err
	}

	blockFile, err := os.Open(filepath.Join(localDst, v1.BloomFileName))
	if err != nil {
		level.Error(logger).Log("reading bloomBlock", err)
	}

	block := bloomshipper.Block{
		BlockRef: bloomshipper.BlockRef{
			Ref: bloomshipper.Ref{
				TenantID:       job.Tenant(),
				TableName:      job.TableName(),
				MinFingerprint: uint64(job.minFp),
				MaxFingerprint: uint64(job.maxFp),
				StartTimestamp: job.From().Unix(),
				EndTimestamp:   job.Through().Unix(),
				Checksum:       checksum,
			},
			IndexPath: job.IndexPath(),
		},
		Data: blockFile,
	}

	return block, nil
}

func createLocalDirName(workingDir string, job Job) string {
	dir := fmt.Sprintf("bloomBlock-%s-%s-%s-%s-%s-%s", job.TableName(), job.Tenant(), job.minFp, job.maxFp, job.From(), job.Through())
	return filepath.Join(workingDir, dir)
}

// Compacts given list of chunks, uploads them to storage and returns a list of bloomBlocks
func CompactNewChunks(
	ctx context.Context,
	logger log.Logger,
	job Job,
	chunks []chunk.Chunk,
	bt *v1.BloomTokenizer,
	fpRate float64,
	bloomShipperClient bloomshipper.Client,
	dst string,
) ([]bloomshipper.Block, error) {
	// Ensure the context has not been canceled (ie. compactor shutdown has been triggered).
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var blooms []v1.SeriesWithBloom

	for _, seriesMeta := range job.seriesMetas {
		// Create a bloom for this series
		bloomForChks := v1.SeriesWithBloom{
			Series: &v1.Series{
				Fingerprint: seriesMeta.Fingerprint(),
			},
			Bloom: &v1.Bloom{
				ScalableBloomFilter: *filter.NewDefaultScalableBloomFilter(fpRate),
			},
		}

		// Tokenize data into n-grams
		bt.PopulateSeriesWithBloom(&bloomForChks, chunks)

		blooms = append(blooms, bloomForChks)
	}

	// Build and upload bloomBlock to storage
	blockOptions := v1.NewBlockOptions(bt.GetNGramLength(), bt.GetNGramSkip())
	block, err := buildBloomBlock(ctx, logger, blockOptions, blooms, job, dst)
	if err != nil {
		level.Error(logger).Log("building bloomBlocks", err)
		return nil, err
	}
	storedBlocks, err := bloomShipperClient.PutBlocks(ctx, []bloomshipper.Block{block})
	if err != nil {
		level.Error(logger).Log("putting blocks to storage", err)
		return nil, err
	}
	return storedBlocks, nil
}
