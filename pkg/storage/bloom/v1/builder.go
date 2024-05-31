package v1

import (
	"bytes"
	"fmt"
	"hash"
	"io"

	"github.com/pkg/errors"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/chunkenc"
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
		version:     DefaultSchemaVersion,
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

type BloomBlockBuilder struct {
	opts   BlockOptions
	writer io.WriteCloser

	offset        int // track the offset of the file
	writtenSchema bool
	pages         []BloomPageHeader
	page          PageWriter
	scratch       *encoding.Encbuf
}

func NewBloomBlockBuilder(opts BlockOptions, writer io.WriteCloser) *BloomBlockBuilder {
	return &BloomBlockBuilder{
		opts:    opts,
		writer:  writer,
		page:    NewPageWriter(int(opts.BloomPageSize)),
		scratch: &encoding.Encbuf{},
	}
}

func (b *BloomBlockBuilder) WriteSchema() error {
	b.scratch.Reset()
	b.opts.Schema.Encode(b.scratch)
	if _, err := b.writer.Write(b.scratch.Get()); err != nil {
		return errors.Wrap(err, "writing schema")
	}
	b.writtenSchema = true
	b.offset += b.scratch.Len()
	return nil
}

func (b *BloomBlockBuilder) Append(bloom *Bloom) (BloomOffset, error) {
	if !b.writtenSchema {
		if err := b.WriteSchema(); err != nil {
			return BloomOffset{}, errors.Wrap(err, "writing schema")
		}
	}

	b.scratch.Reset()
	if err := bloom.Encode(b.scratch); err != nil {
		return BloomOffset{}, errors.Wrap(err, "encoding bloom")
	}

	if !b.page.SpaceFor(b.scratch.Len()) {
		if err := b.flushPage(); err != nil {
			return BloomOffset{}, errors.Wrap(err, "flushing bloom page")
		}
	}

	return BloomOffset{
		Page:       len(b.pages),
		ByteOffset: b.page.Add(b.scratch.Get()),
	}, nil
}

func (b *BloomBlockBuilder) Close() (uint32, error) {
	if b.page.Count() > 0 {
		if err := b.flushPage(); err != nil {
			return 0, errors.Wrap(err, "flushing final bloom page")
		}
	}

	b.scratch.Reset()
	b.scratch.PutUvarint(len(b.pages))
	for _, h := range b.pages {
		h.Encode(b.scratch)
	}
	// put offset to beginning of header section
	// cannot be varint encoded because it's offset will be calculated as
	// the 8 bytes prior to the checksum
	b.scratch.PutBE64(uint64(b.offset))

	crc32Hash := Crc32HashPool.Get()
	defer Crc32HashPool.Put(crc32Hash)
	// wrap with final checksum
	b.scratch.PutHash(crc32Hash)
	_, err := b.writer.Write(b.scratch.Get())
	if err != nil {
		return 0, errors.Wrap(err, "writing bloom page headers")
	}
	return crc32Hash.Sum32(), errors.Wrap(b.writer.Close(), "closing bloom writer")
}

func (b *BloomBlockBuilder) flushPage() error {
	crc32Hash := Crc32HashPool.Get()
	defer Crc32HashPool.Put(crc32Hash)

	decompressedLen, compressedLen, err := b.page.writePage(
		b.writer,
		b.opts.Schema.CompressorPool(),
		crc32Hash,
	)
	if err != nil {
		return errors.Wrap(err, "writing bloom page")
	}
	header := BloomPageHeader{
		N:               b.page.Count(),
		Offset:          b.offset,
		Len:             compressedLen,
		DecompressedLen: decompressedLen,
	}
	b.pages = append(b.pages, header)
	b.offset += compressedLen
	b.page.Reset()
	return nil
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

type IndexBuilder struct {
	opts   BlockOptions
	writer io.WriteCloser

	offset        int // track the offset of the file
	writtenSchema bool
	pages         []SeriesPageHeaderWithOffset
	page          PageWriter
	scratch       *encoding.Encbuf

	previousFp        model.Fingerprint
	previousOffset    BloomOffset
	fromFp            model.Fingerprint
	fromTs, throughTs model.Time
}

func NewIndexBuilder(opts BlockOptions, writer io.WriteCloser) *IndexBuilder {
	return &IndexBuilder{
		opts:    opts,
		writer:  writer,
		page:    NewPageWriter(int(opts.SeriesPageSize)),
		scratch: &encoding.Encbuf{},
	}
}

func (b *IndexBuilder) WriteOpts() error {
	b.scratch.Reset()
	b.opts.Encode(b.scratch)
	if _, err := b.writer.Write(b.scratch.Get()); err != nil {
		return errors.Wrap(err, "writing opts+schema")
	}
	b.writtenSchema = true
	b.offset += b.scratch.Len()
	return nil
}

func (b *IndexBuilder) Append(series SeriesWithOffsets) error {
	if !b.writtenSchema {
		if err := b.WriteOpts(); err != nil {
			return errors.Wrap(err, "appending series")
		}
	}

	b.scratch.Reset()
	// we don't want to update the previous pointers yet in case
	// we need to flush the page first which would
	// be passed the incorrect final fp/offset
	lastOffset := series.Encode(b.scratch, b.previousFp, b.previousOffset)

	if !b.page.SpaceFor(b.scratch.Len()) && b.page.Count() > 0 {
		if err := b.flushPage(); err != nil {
			return errors.Wrap(err, "flushing series page")
		}

		// re-encode now that a new page has been cut and we use delta-encoding
		b.scratch.Reset()
		lastOffset = series.Encode(b.scratch, b.previousFp, b.previousOffset)
	}

	switch {
	case b.page.Count() == 0:
		// Special case: this is the first series in a page
		if len(series.Chunks) < 1 {
			return fmt.Errorf("series with zero chunks for fingerprint %v", series.Fingerprint)
		}
		b.fromFp = series.Fingerprint
		b.fromTs, b.throughTs = chkBounds(series.Chunks)
	case b.previousFp > series.Fingerprint:
		return fmt.Errorf("out of order series fingerprint for series %v", series.Fingerprint)
	default:
		from, through := chkBounds(series.Chunks)
		if b.fromTs.After(from) {
			b.fromTs = from
		}
		if b.throughTs.Before(through) {
			b.throughTs = through
		}
	}

	_ = b.page.Add(b.scratch.Get())
	b.previousFp = series.Fingerprint
	b.previousOffset = lastOffset
	return nil
}

// must be > 1
func chkBounds(chks []ChunkRef) (from, through model.Time) {
	from, through = chks[0].From, chks[0].Through
	for _, chk := range chks[1:] {
		if chk.From.Before(from) {
			from = chk.From
		}

		if chk.Through.After(through) {
			through = chk.Through
		}
	}
	return
}

func (b *IndexBuilder) flushPage() error {
	crc32Hash := Crc32HashPool.Get()
	defer Crc32HashPool.Put(crc32Hash)

	decompressedLen, compressedLen, err := b.page.writePage(
		b.writer,
		b.opts.Schema.CompressorPool(),
		crc32Hash,
	)
	if err != nil {
		return errors.Wrap(err, "writing series page")
	}

	header := SeriesPageHeaderWithOffset{
		Offset:          b.offset,
		Len:             compressedLen,
		DecompressedLen: decompressedLen,
		SeriesHeader: SeriesHeader{
			NumSeries: b.page.Count(),
			Bounds:    NewBounds(b.fromFp, b.previousFp),
			FromTs:    b.fromTs,
			ThroughTs: b.throughTs,
		},
	}

	b.pages = append(b.pages, header)
	b.offset += compressedLen

	b.fromFp = 0
	b.fromTs = 0
	b.throughTs = 0
	b.previousFp = 0
	b.previousOffset = BloomOffset{}
	b.page.Reset()

	return nil
}

func (b *IndexBuilder) Close() (uint32, error) {
	if b.page.Count() > 0 {
		if err := b.flushPage(); err != nil {
			return 0, errors.Wrap(err, "flushing final series page")
		}
	}

	b.scratch.Reset()
	b.scratch.PutUvarint(len(b.pages))
	for _, h := range b.pages {
		h.Encode(b.scratch)
	}

	// put offset to beginning of header section
	// cannot be varint encoded because it's offset will be calculated as
	// the 8 bytes prior to the checksum
	b.scratch.PutBE64(uint64(b.offset))
	crc32Hash := Crc32HashPool.Get()
	defer Crc32HashPool.Put(crc32Hash)
	// wrap with final checksum
	b.scratch.PutHash(crc32Hash)
	_, err := b.writer.Write(b.scratch.Get())
	if err != nil {
		return 0, errors.Wrap(err, "writing series page headers")
	}
	return crc32Hash.Sum32(), errors.Wrap(b.writer.Close(), "closing series writer")
}

type BloomCreation struct {
	Bloom            *Bloom
	SourceBytesAdded int
	Err              error
}

// Simplistic implementation of a merge builder that builds a single block
// from a list of blocks and a store of series.
type MergeBuilder struct {
	// existing blocks
	blocks Iterator[*SeriesWithBlooms]
	// store
	store Iterator[*Series]
	// Add chunks to a bloom
	populate func(s *Series, srcBlooms SizedIterator[*Bloom], toAdd ChunkRefs, ch chan *BloomCreation)
	metrics  *Metrics
}

type BloomPopulatorFunc = func(s *Series, srcBlooms SizedIterator[*Bloom], toAdd ChunkRefs, ch chan *BloomCreation)

// NewMergeBuilder is a specific builder which does the following:
//  1. merges multiple blocks into a single ordered querier,
//     i) When two blocks have the same series, it will prefer the one with the most chunks already indexed
//  2. iterates through the store, adding chunks to the relevant blooms via the `populate` argument
func NewMergeBuilder(
	blocks Iterator[*SeriesWithBlooms],
	store Iterator[*Series],
	populate BloomPopulatorFunc,
	metrics *Metrics,
) *MergeBuilder {
	// combinedSeriesIter handles series with fingerprint collisions:
	// because blooms dont contain the label-set (only the fingerprint),
	// in the case of a fingerprint collision we simply treat it as one
	// series with multiple chunks.
	combinedSeriesIter := NewDedupingIter[*Series, *Series](
		// eq
		func(s1, s2 *Series) bool {
			return s1.Fingerprint == s2.Fingerprint
		},
		// from
		Identity[*Series],
		// merge
		func(s1, s2 *Series) *Series {
			return &Series{
				Fingerprint: s1.Fingerprint,
				Chunks:      s1.Chunks.Union(s2.Chunks),
			}
		},
		NewPeekingIter[*Series](store),
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
	bool, // blocksFinished update
	bool, // done building block
	error, // error
) {
	var blockSeriesIterated, chunksIndexed, chunksCopied, bytesAdded int
	defer func() {
		mb.metrics.blockSeriesIterated.Add(float64(blockSeriesIterated))
		mb.metrics.chunksIndexed.WithLabelValues(chunkIndexedTypeIterated).Add(float64(chunksIndexed))
		mb.metrics.chunksIndexed.WithLabelValues(chunkIndexedTypeCopied).Add(float64(chunksCopied))
		mb.metrics.chunksPerSeries.Observe(float64(chunksIndexed + chunksCopied))
	}()

	if !mb.store.Next() {
		return nil, 0, false, true, nil
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
			return nil, 0, false, false, errors.Wrap(err, "iterating blocks")
		}
		blockSeriesIterated++
		nextInBlocks = mb.blocks.At()
	}

	var (
		offsets           []BloomOffset
		chunksToAdd                             = nextInStore.Chunks
		preExistingBlooms SizedIterator[*Bloom] = NewEmptyIter[*Bloom]()
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

	for bloom := range ch {
		if bloom.Err != nil {
			return nil, bytesAdded, false, false, errors.Wrap(bloom.Err, "populating bloom")
		}
		offset, err := builder.AddBloom(bloom.Bloom)
		if err != nil {
			return nil, bytesAdded, false, false, errors.Wrapf(
				err, "adding bloom to block for fp (%s)", nextInStore.Fingerprint,
			)
		}
		offsets = append(offsets, offset)
		bytesAdded += bloom.SourceBytesAdded
	}

	done, err := builder.AddSeries(*nextInStore, offsets)
	if err != nil {
		return nil, bytesAdded, false, false, errors.Wrap(err, "committing series")
	}

	return nextInBlocks, bytesAdded, blocksFinished, done, nil
}

func (mb *MergeBuilder) Build(builder *BlockBuilder) (checksum uint32, totalBytes int, err error) {
	var (
		nextInBlocks   *SeriesWithBlooms
		blocksFinished bool // whether any previous blocks have been exhausted while building new block
		done           bool
	)
	for {
		var bytesAdded int
		nextInBlocks, bytesAdded, blocksFinished, done, err = mb.processNextSeries(builder, nextInBlocks, blocksFinished)
		totalBytes += bytesAdded
		if err != nil {
			return 0, totalBytes, errors.Wrap(err, "processing next series")
		}
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
	mb.metrics.blockFlushReason.WithLabelValues(flushedFor).Inc()

	checksum, err = builder.Close()
	if err != nil {
		return 0, totalBytes, errors.Wrap(err, "closing block")
	}
	return checksum, totalBytes, nil
}
