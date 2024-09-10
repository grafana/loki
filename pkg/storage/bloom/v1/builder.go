package v1

import (
	"bytes"
	"hash"
	"io"

	"github.com/pkg/errors"

	"github.com/grafana/loki/v3/pkg/chunkenc"
	iter "github.com/grafana/loki/v3/pkg/iter/v2"
	"github.com/grafana/loki/v3/pkg/util/encoding"
)

// Options for the block which are not encoded into it iself.
type UnencodedBlockOptions struct {
	MaxBloomSizeBytes uint64
}

type BlockOptions struct {
	// Schema determines the Schema of the block and cannot be changed
	// without recreating the block from underlying data
	Schema Schema

	// The following options can be changed on the fly.
	// For instance, adding another page to a block with
	// a different target page size is supported, although
	// the block will store the original sizes it was created with

	// target size in bytes (decompressed)
	// of each page type
	SeriesPageSize, BloomPageSize, BlockSize uint64

	// UnencodedBlockOptions are not encoded into the block's binary format,
	// but are a helpful way to pass additional options to the block builder.
	// Thus, they're used during construction but not on reads.
	UnencodedBlockOptions UnencodedBlockOptions
}

func (b BlockOptions) Len() int {
	return 3*8 + b.Schema.Len()
}

func (b *BlockOptions) DecodeFrom(r io.ReadSeeker) error {
	buf := make([]byte, b.Len())
	_, err := io.ReadFull(r, buf)
	if err != nil {
		return errors.Wrap(err, "reading block options")
	}

	dec := encoding.DecWith(buf)

	if err := b.Schema.Decode(&dec); err != nil {
		return errors.Wrap(err, "decoding schema")
	}
	b.SeriesPageSize = dec.Be64()
	b.BloomPageSize = dec.Be64()
	b.BlockSize = dec.Be64()
	return nil
}

func (b BlockOptions) Encode(enc *encoding.Encbuf) {
	b.Schema.Encode(enc)
	enc.PutBE64(b.SeriesPageSize)
	enc.PutBE64(b.BloomPageSize)
	enc.PutBE64(b.BlockSize)
}

func NewBlockOptions(enc chunkenc.Encoding, nGramLength, nGramSkip, maxBlockSizeBytes, maxBloomSizeBytes uint64) BlockOptions {
	opts := NewBlockOptionsFromSchema(Schema{
		version:     CurrentSchemaVersion,
		encoding:    enc,
		nGramLength: nGramLength,
		nGramSkip:   nGramSkip,
	})
	opts.BlockSize = maxBlockSizeBytes
	opts.UnencodedBlockOptions.MaxBloomSizeBytes = maxBloomSizeBytes
	return opts
}

func NewBlockOptionsFromSchema(s Schema) BlockOptions {
	return BlockOptions{
		Schema: s,
		// TODO(owen-d): benchmark and find good defaults
		SeriesPageSize: 4 << 10,   // 4KB, typical page size
		BloomPageSize:  256 << 10, // 256KB, no idea what to make this
	}
}

type PageWriter struct {
	enc        *encoding.Encbuf
	targetSize int
	n          int // number of encoded blooms
}

func NewPageWriter(targetSize int) PageWriter {
	return PageWriter{
		enc:        &encoding.Encbuf{},
		targetSize: targetSize,
	}
}

func (w *PageWriter) Count() int {
	return w.n
}

func (w *PageWriter) Reset() {
	w.enc.Reset()
	w.n = 0
}

func (w *PageWriter) SpaceFor(numBytes int) bool {
	// if a single bloom exceeds the target size, still accept it
	// otherwise only accept it if adding it would not exceed the target size
	return w.n == 0 || w.enc.Len()+numBytes <= w.targetSize
}

func (w *PageWriter) Add(item []byte) (offset int) {
	offset = w.enc.Len()
	w.enc.PutBytes(item)
	w.n++
	return offset
}

func (w *PageWriter) writePage(writer io.Writer, pool chunkenc.WriterPool, crc32Hash hash.Hash32) (int, int, error) {
	// write the number of blooms in this page, must not be varint
	// so we can calculate it's position+len during decoding
	w.enc.PutBE64(uint64(w.n))
	decompressedLen := w.enc.Len()

	buf := &bytes.Buffer{}
	compressor := pool.GetWriter(buf)
	defer pool.PutWriter(compressor)

	if _, err := compressor.Write(w.enc.Get()); err != nil {
		return 0, 0, errors.Wrap(err, "compressing page")
	}

	if err := compressor.Close(); err != nil {
		return 0, 0, errors.Wrap(err, "closing compressor")
	}

	// replace the encoded series page with the compressed one
	w.enc.B = buf.Bytes()
	w.enc.PutHash(crc32Hash)

	// write the page
	if _, err := writer.Write(w.enc.Get()); err != nil {
		return 0, 0, errors.Wrap(err, "writing page")
	}
	return decompressedLen, w.enc.Len(), nil
}

// indexingInfo is a datastructure that holds information about the indexing operation.
type indexingInfo struct {
	sourceBytes   int
	indexedFields Set[Field]
}

func newIndexingInfo() indexingInfo {
	return indexingInfo{
		sourceBytes:   0,
		indexedFields: NewSet[Field](16),
	}
}

func (s indexingInfo) merge(other indexingInfo) indexingInfo {
	s.sourceBytes += other.sourceBytes
	s.indexedFields.Union(other.indexedFields)
	return s
}

type BloomCreation struct {
	Bloom *Bloom
	Info  indexingInfo
	Err   error
}

// Simplistic implementation of a merge builder that builds a single block
// from a list of blocks and a store of series.
type MergeBuilder struct {
	// existing blocks
	blocks iter.Iterator[*SeriesWithBlooms]
	// store
	store iter.Iterator[*Series]
	// Add chunks to a bloom
	populate func(s *Series, srcBlooms iter.SizedIterator[*Bloom], toAdd ChunkRefs, ch chan *BloomCreation)
	metrics  *Metrics
}

type BloomPopulatorFunc = func(s *Series, srcBlooms iter.SizedIterator[*Bloom], toAdd ChunkRefs, ch chan *BloomCreation)

// NewMergeBuilder is a specific builder which does the following:
//  1. merges multiple blocks into a single ordered querier,
//     i) When two blocks have the same series, it will prefer the one with the most chunks already indexed
//  2. iterates through the store, adding chunks to the relevant blooms via the `populate` argument
func NewMergeBuilder(
	blocks iter.Iterator[*SeriesWithBlooms],
	store iter.Iterator[*Series],
	populate BloomPopulatorFunc,
	metrics *Metrics,
) *MergeBuilder {
	// combinedSeriesIter handles series with fingerprint collisions:
	// because blooms dont contain the label-set (only the fingerprint),
	// in the case of a fingerprint collision we simply treat it as one
	// series with multiple chunks.
	combinedSeriesIter := iter.NewDedupingIter[*Series, *Series](
		// eq
		func(s1, s2 *Series) bool {
			return s1.Fingerprint == s2.Fingerprint
		},
		// from
		iter.Identity[*Series],
		// merge
		func(s1, s2 *Series) *Series {
			return &Series{
				Fingerprint: s1.Fingerprint,
				Chunks:      s1.Chunks.Union(s2.Chunks),
			}
		},
		iter.NewPeekIter[*Series](store),
	)

	return &MergeBuilder{
		blocks:   blocks,
		store:    combinedSeriesIter,
		populate: populate,
		metrics:  metrics,
	}
}

func (mb *MergeBuilder) processNextSeries(
	builder *BlockBuilder,
	nextInBlocks *SeriesWithBlooms,
	blocksFinished bool,
) (
	*SeriesWithBlooms, // nextInBlocks pointer update
	int, // bytes added
	int, // chunks added
	bool, // blocksFinished update
	bool, // done building block
	error, // error
) {
	var blockSeriesIterated, chunksIndexed, chunksCopied int

	defer func() {
		mb.metrics.blockSeriesIterated.Add(float64(blockSeriesIterated))
		mb.metrics.chunksIndexed.WithLabelValues(chunkIndexedTypeIterated).Add(float64(chunksIndexed))
		mb.metrics.chunksIndexed.WithLabelValues(chunkIndexedTypeCopied).Add(float64(chunksCopied))
		mb.metrics.chunksPerSeries.Observe(float64(chunksIndexed + chunksCopied))
	}()

	if !mb.store.Next() {
		return nil, 0, 0, false, true, nil
	}

	nextInStore := mb.store.At()

	// advance the merged blocks iterator until we find a series that is
	// greater than or equal to the next series in the store.
	// TODO(owen-d): expensive, but Seek is not implemented for this itr.
	// It's also more efficient to build an iterator over the Series file in the index
	// without the blooms until we find a bloom we actually need to unpack from the blooms file.
	for !blocksFinished && (nextInBlocks == nil || nextInBlocks.Series.Fingerprint < mb.store.At().Fingerprint) {
		if !mb.blocks.Next() {
			// we've exhausted all the blocks
			blocksFinished = true
			nextInBlocks = nil
			break
		}

		if err := mb.blocks.Err(); err != nil {
			return nil, 0, 0, false, false, errors.Wrap(err, "iterating blocks")
		}
		blockSeriesIterated++
		nextInBlocks = mb.blocks.At()
	}

	var (
		offsets           []BloomOffset
		chunksToAdd                                  = nextInStore.Chunks
		preExistingBlooms iter.SizedIterator[*Bloom] = iter.NewEmptyIter[*Bloom]()
		info                                         = newIndexingInfo()
	)

	if nextInBlocks != nil && nextInBlocks.Series.Fingerprint == nextInStore.Fingerprint {
		// if the series already exists in the block, we only need to add the new chunks
		chunksToAdd = nextInStore.Chunks.Unless(nextInBlocks.Series.Chunks)
		chunksCopied += len(nextInStore.Chunks) - len(chunksToAdd)
		preExistingBlooms = nextInBlocks.Blooms
	}

	chunksIndexed += len(chunksToAdd)

	// populate bloom
	ch := make(chan *BloomCreation)
	go mb.populate(nextInStore, preExistingBlooms, chunksToAdd, ch)

	for creation := range ch {
		if creation.Err != nil {
			return nil, info.sourceBytes, 0, false, false, errors.Wrap(creation.Err, "populating bloom")
		}
		offset, err := builder.AddBloom(creation.Bloom)
		if err != nil {
			return nil, info.sourceBytes, 0, false, false, errors.Wrapf(
				err, "adding bloom to block for fp (%s)", nextInStore.Fingerprint,
			)
		}
		offsets = append(offsets, offset)
		info.merge(creation.Info)
	}

	done, err := builder.AddSeries(*nextInStore, offsets, info.indexedFields)
	if err != nil {
		return nil, info.sourceBytes, 0, false, false, errors.Wrap(err, "committing series")
	}

	return nextInBlocks, info.sourceBytes, chunksIndexed + chunksCopied, blocksFinished, done, nil
}

func (mb *MergeBuilder) Build(builder *BlockBuilder) (checksum uint32, totalBytes int, err error) {
	var (
		nextInBlocks     *SeriesWithBlooms
		blocksFinished   bool // whether any previous blocks have been exhausted while building new block
		done             bool
		totalSeriesAdded = 0
		totalChunksAdded int
	)
	for {
		var bytesAdded, chunksAdded int
		nextInBlocks, bytesAdded, chunksAdded, blocksFinished, done, err = mb.processNextSeries(builder, nextInBlocks, blocksFinished)
		totalBytes += bytesAdded
		totalChunksAdded += chunksAdded
		if err != nil {
			return 0, totalBytes, errors.Wrap(err, "processing next series")
		}
		totalSeriesAdded++
		if done {
			break
		}
	}

	if err := mb.store.Err(); err != nil {
		return 0, totalBytes, errors.Wrap(err, "iterating store")
	}

	flushedFor := blockFlushReasonFinished
	full, sz, _ := builder.writer.Full(builder.opts.BlockSize)
	if full {
		flushedFor = blockFlushReasonFull
	}
	mb.metrics.blockSize.Observe(float64(sz))
	mb.metrics.seriesPerBlock.Observe(float64(totalSeriesAdded))
	mb.metrics.chunksPerBlock.Observe(float64(totalChunksAdded))
	mb.metrics.blockFlushReason.WithLabelValues(flushedFor).Inc()

	checksum, err = builder.Close()
	if err != nil {
		return 0, totalBytes, errors.Wrap(err, "closing block")
	}
	return checksum, totalBytes, nil
}
