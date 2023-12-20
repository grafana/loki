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
	Data() (io.ReadSeekCloser, error)
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

func (p *PersistentBlockBuilder) mergeBuild(builder *v1.MergeBuilder) (uint32, error) {
	return builder.Build(p.builder)
}

func (p *PersistentBlockBuilder) Data() (io.ReadSeekCloser, error) {
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

func buildBloomFromSeries(seriesMeta seriesMeta, fpRate float64, tokenizer compactorTokenizer, chunks []chunk.Chunk) (v1.SeriesWithBloom, error) {
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
	err := tokenizer.PopulateSeriesWithBloom(&bloomForChks, chunks)
	if err != nil {
		return v1.SeriesWithBloom{}, err
	}
	return bloomForChks, nil
}

// TODO Test this when bloom block size check is implemented
func buildBlockFromBlooms(
	ctx context.Context,
	logger log.Logger,
	builder blockBuilder,
	blooms v1.Iterator[v1.SeriesWithBloom],
	job Job,
) (bloomshipper.Block, error) {
	// Ensure the context has not been canceled (ie. compactor shutdown has been triggered).
	if err := ctx.Err(); err != nil {
		return bloomshipper.Block{}, err
	}

	checksum, err := builder.BuildFrom(blooms)
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

	bloomIter := newLazyBloomBuilder(ctx, job, storeClient, bt, fpRate)

	// Build and upload bloomBlock to storage
	block, err := buildBlockFromBlooms(ctx, logger, builder, bloomIter, job)
	if err != nil {
		level.Error(logger).Log("msg", "failed building bloomBlocks", "err", err)
		return bloomshipper.Block{}, err
	}

	return block, nil
}

type lazyBloomBuilder struct {
	ctx    context.Context
	metas  v1.Iterator[seriesMeta]
	tenant string
	client chunkClient
	bt     compactorTokenizer
	fpRate float64

	cur v1.SeriesWithBloom // retured by At()
	err error              // returned by Err()
}

// newLazyBloomBuilder returns an iterator that yields v1.SeriesWithBloom
// which are used by the blockBuilder to write a bloom block.
// We use an interator to avoid loading all blooms into memory first, before
// building the block.
func newLazyBloomBuilder(ctx context.Context, job Job, client chunkClient, bt compactorTokenizer, fpRate float64) *lazyBloomBuilder {
	return &lazyBloomBuilder{
		ctx:    ctx,
		metas:  v1.NewSliceIter(job.seriesMetas),
		client: client,
		tenant: job.tenantID,
		bt:     bt,
		fpRate: fpRate,
	}
}

func (it *lazyBloomBuilder) Next() bool {
	if !it.metas.Next() {
		it.err = io.EOF
		it.cur = v1.SeriesWithBloom{}
		return false
	}
	meta := it.metas.At()

	// Get chunks data from list of chunkRefs
	chks, err := it.client.GetChunks(it.ctx, makeChunkRefs(meta.chunkRefs, it.tenant, meta.seriesFP))
	if err != nil {
		it.err = err
		it.cur = v1.SeriesWithBloom{}
		return false
	}

	it.cur, err = buildBloomFromSeries(meta, it.fpRate, it.bt, chks)
	if err != nil {
		it.err = err
		it.cur = v1.SeriesWithBloom{}
		return false
	}
	return true
}

func (it *lazyBloomBuilder) At() v1.SeriesWithBloom {
	return it.cur
}

func (it *lazyBloomBuilder) Err() error {
	return it.err
}
