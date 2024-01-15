package bloomcompactor

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/google/uuid"

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
	PopulateSeriesWithBloom(bloom *v1.SeriesWithBloom, chunkBatchesIterator v1.Iterator[[]chunk.Chunk]) error
}

type chunkClient interface {
	// TODO: Consider using lazyChunks to avoid downloading all requested chunks.
	GetChunks(ctx context.Context, chunks []chunk.Chunk) ([]chunk.Chunk, error)
}

type blockBuilder interface {
	// BuildFrom build a block from the given iterator.
	// If the data is too large to fit in a single block, the iterator won't be consumed completely. In this case,
	// call BuildFrom again with the same iterator to build the next block.
	BuildFrom(itr v1.Iterator[v1.SeriesWithBloom]) (string, uint32, v1.SeriesBounds, error)
	// Data returns a reader for the last block built by BuildFrom.
	Data() (io.ReadSeekCloser, error)
}

type PersistentBlockBuilder struct {
	blockOptions     v1.BlockOptions
	lastBlockIdx     uint64
	baseLocalDst     string
	currentBlockPath string
}

func NewPersistentBlockBuilder(localDst string, blockOptions v1.BlockOptions) (*PersistentBlockBuilder, error) {
	builder := PersistentBlockBuilder{
		blockOptions: blockOptions,
		baseLocalDst: localDst,
	}

	return &builder, nil
}

func (p *PersistentBlockBuilder) getNextBuilder() (*v1.BlockBuilder, error) {
	// write bloom to a local dir
	blockPath := filepath.Join(p.baseLocalDst, fmt.Sprintf("%d", p.lastBlockIdx))
	builder, err := v1.NewBlockBuilder(p.blockOptions, v1.NewDirectoryBlockWriter(blockPath))
	if err != nil {
		return nil, err
	}
	p.currentBlockPath = blockPath
	p.lastBlockIdx++
	return builder, nil
}

func (p *PersistentBlockBuilder) BuildFrom(itr v1.Iterator[v1.SeriesWithBloom]) (string, uint32, v1.SeriesBounds, error) {

	b, err := p.getNextBuilder()
	if err != nil {
		return "", 0, v1.SeriesBounds{}, err
	}

	checksum, bounds, err := b.BuildFrom(itr)
	if err != nil {
		return "", 0, v1.SeriesBounds{}, err
	}

	return p.currentBlockPath, checksum, bounds, nil
}

func (p *PersistentBlockBuilder) mergeBuild(builder *v1.MergeBuilder) (string, uint32, v1.SeriesBounds, error) {
	b, err := p.getNextBuilder()
	if err != nil {
		return "", 0, v1.SeriesBounds{}, err
	}

	checksum, bounds, err := builder.Build(b)
	if err != nil {
		return "", 0, v1.SeriesBounds{}, err
	}

	return p.currentBlockPath, checksum, bounds, nil
}

func (p *PersistentBlockBuilder) Data() (io.ReadSeekCloser, error) {
	blockFile, err := os.Open(filepath.Join(p.currentBlockPath, v1.BloomFileName))
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

func buildBloomFromSeries(seriesMeta seriesMeta, fpRate float64, tokenizer compactorTokenizer, chunks v1.Iterator[[]chunk.Chunk]) (v1.SeriesWithBloom, error) {
	chunkRefs := makeChunkRefsFromChunkMetas(seriesMeta.chunkRefs)

	// Create a bloom for this series
	bloomForChks := v1.SeriesWithBloom{
		Series: &v1.Series{
			Fingerprint: seriesMeta.seriesFP,
			Chunks:      chunkRefs,
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
func buildBlocksFromBlooms(
	ctx context.Context,
	logger log.Logger,
	builder blockBuilder,
	blooms v1.PeekingIterator[v1.SeriesWithBloom],
	job Job,
) ([]bloomshipper.Block, error) {
	// Ensure the context has not been canceled (ie. compactor shutdown has been triggered).
	if err := ctx.Err(); err != nil {
		return []bloomshipper.Block{}, err
	}

	// TODO(salvacorts): Reuse buffer
	blocks := make([]bloomshipper.Block, 0)

	for {
		// Create blocks until the iterator is empty
		if _, hasNext := blooms.Peek(); !hasNext {
			break
		}

		if err := ctx.Err(); err != nil {
			return []bloomshipper.Block{}, err
		}

		blockPath, checksum, bounds, err := builder.BuildFrom(blooms)
		if err != nil {
			level.Error(logger).Log("msg", "failed writing to bloom", "err", err)
			return []bloomshipper.Block{}, err
		}

		data, err := builder.Data()
		if err != nil {
			level.Error(logger).Log("msg", "failed reading bloom data", "err", err)
			return []bloomshipper.Block{}, err
		}

		blocks = append(blocks, bloomshipper.Block{
			BlockRef: bloomshipper.BlockRef{
				Ref: bloomshipper.Ref{
					TenantID:       job.tenantID,
					TableName:      job.tableName,
					MinFingerprint: uint64(bounds.FromFp),
					MaxFingerprint: uint64(bounds.ThroughFp),
					StartTimestamp: bounds.FromTs,
					EndTimestamp:   bounds.ThroughTs,
					Checksum:       checksum,
				},
				IndexPath: job.indexPath,
				BlockPath: blockPath,
			},
			Data: data,
		})
	}

	return blocks, nil
}

func createLocalDirName(workingDir string, job Job) string {
	dir := fmt.Sprintf("bloomBlock-%s-%s-%s-%s-%d-%d-%s", job.tableName, job.tenantID, job.minFp, job.maxFp, job.from, job.through, uuid.New().String())
	return filepath.Join(workingDir, dir)
}

// Compacts given list of chunks, uploads them to storage and returns a list of bloomBlocks
func compactNewChunks(ctx context.Context,
	logger log.Logger,
	job Job,
	bt compactorTokenizer,
	storeClient chunkClient,
	builder blockBuilder,
	limits Limits,
) ([]bloomshipper.Block, error) {
	// Ensure the context has not been canceled (ie. compactor shutdown has been triggered).
	if err := ctx.Err(); err != nil {
		return []bloomshipper.Block{}, err
	}

	bloomIter := v1.NewPeekingIter[v1.SeriesWithBloom](newLazyBloomBuilder(ctx, job, storeClient, bt, logger, limits))

	// Build and upload bloomBlock to storage
	blocks, err := buildBlocksFromBlooms(ctx, logger, builder, bloomIter, job)
	if err != nil {
		level.Error(logger).Log("msg", "failed building bloomBlocks", "err", err)
		return []bloomshipper.Block{}, err
	}

	return blocks, nil
}

type lazyBloomBuilder struct {
	ctx             context.Context
	metas           v1.Iterator[seriesMeta]
	tenant          string
	client          chunkClient
	bt              compactorTokenizer
	fpRate          float64
	logger          log.Logger
	chunksBatchSize int

	cur v1.SeriesWithBloom // retured by At()
	err error              // returned by Err()
}

// newLazyBloomBuilder returns an iterator that yields v1.SeriesWithBloom
// which are used by the blockBuilder to write a bloom block.
// We use an interator to avoid loading all blooms into memory first, before
// building the block.
func newLazyBloomBuilder(ctx context.Context, job Job, client chunkClient, bt compactorTokenizer, logger log.Logger, limits Limits) *lazyBloomBuilder {
	return &lazyBloomBuilder{
		ctx:             ctx,
		metas:           v1.NewSliceIter(job.seriesMetas),
		client:          client,
		tenant:          job.tenantID,
		bt:              bt,
		fpRate:          limits.BloomFalsePositiveRate(job.tenantID),
		logger:          logger,
		chunksBatchSize: limits.BloomCompactorChunksBatchSize(job.tenantID),
	}
}

func (it *lazyBloomBuilder) Next() bool {
	if !it.metas.Next() {
		it.cur = v1.SeriesWithBloom{}
		level.Debug(it.logger).Log("msg", "No seriesMeta")
		return false
	}
	meta := it.metas.At()

	batchesIterator, err := newChunkBatchesIterator(it.ctx, it.client, makeChunkRefs(meta.chunkRefs, it.tenant, meta.seriesFP), it.chunksBatchSize)
	if err != nil {
		it.err = err
		it.cur = v1.SeriesWithBloom{}
		level.Debug(it.logger).Log("msg", "err creating chunks batches iterator", "err", err)
		return false
	}
	it.cur, err = buildBloomFromSeries(meta, it.fpRate, it.bt, batchesIterator)
	if err != nil {
		it.err = err
		it.cur = v1.SeriesWithBloom{}
		level.Debug(it.logger).Log("msg", "err in buildBloomFromSeries", "err", err)
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
