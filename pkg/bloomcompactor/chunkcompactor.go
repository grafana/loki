package bloomcompactor

import (
	"context"
	"fmt"
	"io"
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

type compactorTokenizer interface {
	PopulateSeriesWithBloom(bloom *v1.SeriesWithBloom, chunks []chunk.Chunk) error
}

type chunkClient interface {
	// TODO: Consider using lazyChunks to avoid downloading all requested chunks.
	GetChunks(ctx context.Context, chunks []chunk.Chunk) ([]chunk.Chunk, error)
}

type blockBuilder interface {
	BuildFrom(itr v1.Iterator[v1.SeriesWithBloom]) (uint32, error)
	Data() (io.ReadCloser, error)
}

type PersistentBlockBuilder struct {
	builder  *v1.BlockBuilder
	localDst string
}

func NewPersistentBlockBuilder(localDst string, blockOptions v1.BlockOptions) (*PersistentBlockBuilder, error) {
	// write bloom to a local dir
	b, err := v1.NewBlockBuilder(blockOptions, v1.NewDirectoryBlockWriter(localDst))
	if err != nil {
		return nil, err
	}
	builder := PersistentBlockBuilder{
		builder:  b,
		localDst: localDst,
	}
	return &builder, nil
}

func (p *PersistentBlockBuilder) BuildFrom(itr v1.Iterator[v1.SeriesWithBloom]) (uint32, error) {
	return p.builder.BuildFrom(itr)
}

func (p *PersistentBlockBuilder) Data() (io.ReadCloser, error) {
	blockFile, err := os.Open(filepath.Join(p.localDst, v1.BloomFileName))
	if err != nil {
		return nil, err
	}
	return blockFile, nil
}

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

func buildBloomFromSeries(seriesMeta seriesMeta, fpRate float64, tokenizer compactorTokenizer, chunks []chunk.Chunk) v1.SeriesWithBloom {
	// Create a bloom for this series
	bloomForChks := v1.SeriesWithBloom{
		Series: &v1.Series{
			Fingerprint: seriesMeta.seriesFP,
		},
		Bloom: &v1.Bloom{
			ScalableBloomFilter: *filter.NewDefaultScalableBloomFilter(fpRate),
		},
	}

	// Tokenize data into n-grams
	tokenizer.PopulateSeriesWithBloom(&bloomForChks, chunks)
	return bloomForChks
}

// TODO Test this when bloom block size check is implemented
func buildBlockFromBlooms(
	ctx context.Context,
	logger log.Logger,
	builder blockBuilder,
	blooms []v1.SeriesWithBloom,
	job Job,
) (bloomshipper.Block, error) {
	// Ensure the context has not been canceled (ie. compactor shutdown has been triggered).
	if err := ctx.Err(); err != nil {
		return bloomshipper.Block{}, err
	}

	checksum, err := builder.BuildFrom(v1.NewSliceIter(blooms))
	if err != nil {
		level.Error(logger).Log("msg", "failed writing to bloom", "err", err)
		return bloomshipper.Block{}, err
	}

	data, err := builder.Data()
	if err != nil {
		level.Error(logger).Log("msg", "failed reading bloom data", "err", err)
		return bloomshipper.Block{}, err
	}

	block := bloomshipper.Block{
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

	return block, nil
}

func createLocalDirName(workingDir string, job Job) string {
	dir := fmt.Sprintf("bloomBlock-%s-%s-%s-%s-%s-%s", job.tableName, job.tenantID, job.minFp, job.maxFp, job.from, job.through)
	return filepath.Join(workingDir, dir)
}

// Compacts given list of chunks, uploads them to storage and returns a list of bloomBlocks
func compactNewChunks(
	ctx context.Context,
	logger log.Logger,
	job Job,
	fpRate float64,
	bt compactorTokenizer,
	storeClient chunkClient,
	builder blockBuilder,
) (bloomshipper.Block, error) {
	// Ensure the context has not been canceled (ie. compactor shutdown has been triggered).
	if err := ctx.Err(); err != nil {
		return bloomshipper.Block{}, err
	}

	blooms := make([]v1.SeriesWithBloom, len(job.seriesMetas))

	for _, seriesMeta := range job.seriesMetas {
		// Get chunks data from list of chunkRefs
		chks, err := storeClient.GetChunks(ctx, makeChunkRefs(seriesMeta.chunkRefs, job.tenantID, seriesMeta.seriesFP))
		if err != nil {
			return bloomshipper.Block{}, err
		}

		bloom := buildBloomFromSeries(seriesMeta, fpRate, bt, chks)
		blooms = append(blooms, bloom)
	}

	// Build and upload bloomBlock to storage
	block, err := buildBlockFromBlooms(ctx, logger, builder, blooms, job)
	if err != nil {
		level.Error(logger).Log("msg", "building bloomBlocks", "err", err)
		return bloomshipper.Block{}, err
	}

	return block, nil
}
