package v1

import (
	"bytes"
	"fmt"
	"hash"
	"io"
	"sort"

	"github.com/pkg/errors"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/storage/bloom/v1/filter"
	"github.com/grafana/loki/pkg/util/encoding"
)

type BlockOptions struct {
	schema Schema

	// target size in bytes (decompressed)
	// of each page type
	SeriesPageSize, BloomPageSize, BlockSize int
}

type BlockBuilder struct {
	opts BlockOptions

	index  *IndexBuilder
	blooms *BloomBlockBuilder
}

func NewBlockOptions(NGramLength, NGramSkip uint64) BlockOptions {
	return BlockOptions{
		schema: Schema{
			version:     byte(1),
			nGramLength: NGramLength,
			nGramSkip:   NGramSkip,
		},
		SeriesPageSize: 100,
		BloomPageSize:  10 << 10, // 0.01MB
	}
}

func NewBlockBuilder(opts BlockOptions, writer BlockWriter) (*BlockBuilder, error) {
	index, err := writer.Index()
	if err != nil {
		return nil, errors.Wrap(err, "initializing index writer")
	}
	blooms, err := writer.Blooms()
	if err != nil {
		return nil, errors.Wrap(err, "initializing blooms writer")
	}

	return &BlockBuilder{
		opts:   opts,
		index:  NewIndexBuilder(opts, index),
		blooms: NewBloomBlockBuilder(opts, blooms),
	}, nil
}

type SeriesWithBloom struct {
	Series *Series
	Bloom  *Bloom
}

func (b *BlockBuilder) BuildFrom(itr Iterator[SeriesWithBloom]) (uint32, error) {
	for itr.Next() {
		if err := b.AddSeries(itr.At()); err != nil {
			return 0, err
		}

	}

	if err := itr.Err(); err != nil {
		return 0, errors.Wrap(err, "iterating series with blooms")
	}

	checksum, err := b.blooms.Close()
	if err != nil {
		return 0, errors.Wrap(err, "closing bloom file")
	}
	if err := b.index.Close(); err != nil {
		return 0, errors.Wrap(err, "closing series file")
	}
	return checksum, nil
}

func (b *BlockBuilder) AddSeries(series SeriesWithBloom) error {
	offset, err := b.blooms.Append(series)
	if err != nil {
		return errors.Wrapf(err, "writing bloom for series %v", series.Series.Fingerprint)
	}

	if err := b.index.Append(SeriesWithOffset{
		Offset: offset,
		Series: *series.Series,
	}); err != nil {
		return errors.Wrapf(err, "writing index for series %v", series.Series.Fingerprint)
	}

	return nil
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
		page:    NewPageWriter(opts.BloomPageSize),
		scratch: &encoding.Encbuf{},
	}
}

func (b *BloomBlockBuilder) WriteSchema() error {
	b.scratch.Reset()
	b.opts.schema.Encode(b.scratch)
	if _, err := b.writer.Write(b.scratch.Get()); err != nil {
		return errors.Wrap(err, "writing schema")
	}
	b.writtenSchema = true
	b.offset += b.scratch.Len()
	return nil
}

func (b *BloomBlockBuilder) Append(series SeriesWithBloom) (BloomOffset, error) {
	if !b.writtenSchema {
		if err := b.WriteSchema(); err != nil {
			return BloomOffset{}, errors.Wrap(err, "writing schema")
		}
	}

	b.scratch.Reset()
	if err := series.Bloom.Encode(b.scratch); err != nil {
		return BloomOffset{}, errors.Wrapf(err, "encoding bloom for %v", series.Series.Fingerprint)
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
		b.opts.schema.CompressorPool(),
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
		page:    NewPageWriter(opts.SeriesPageSize),
		scratch: &encoding.Encbuf{},
	}
}

func (b *IndexBuilder) WriteSchema() error {
	b.scratch.Reset()
	b.opts.schema.Encode(b.scratch)
	if _, err := b.writer.Write(b.scratch.Get()); err != nil {
		return errors.Wrap(err, "writing schema")
	}
	b.writtenSchema = true
	b.offset += b.scratch.Len()
	return nil
}

func (b *IndexBuilder) Append(series SeriesWithOffset) error {
	if !b.writtenSchema {
		if err := b.WriteSchema(); err != nil {
			return errors.Wrap(err, "writing schema")
		}
	}

	b.scratch.Reset()
	// we don't want to update the previous pointers yet in case
	// we need to flush the page first which would
	// be passed the incorrect final fp/offset
	previousFp, previousOffset := series.Encode(b.scratch, b.previousFp, b.previousOffset)

	if !b.page.SpaceFor(b.scratch.Len()) {
		if err := b.flushPage(); err != nil {
			return errors.Wrap(err, "flushing series page")
		}

		// re-encode now that a new page has been cut and we use delta-encoding
		b.scratch.Reset()
		previousFp, previousOffset = series.Encode(b.scratch, b.previousFp, b.previousOffset)
	}
	b.previousFp = previousFp
	b.previousOffset = previousOffset

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
	b.previousOffset = series.Offset
	return nil
}

// must be > 1
func chkBounds(chks []ChunkRef) (from, through model.Time) {
	from, through = chks[0].Start, chks[0].End
	for _, chk := range chks[1:] {
		if chk.Start.Before(from) {
			from = chk.Start
		}

		if chk.End.After(through) {
			through = chk.End
		}
	}
	return
}

func (b *IndexBuilder) flushPage() error {
	crc32Hash := Crc32HashPool.Get()
	defer Crc32HashPool.Put(crc32Hash)

	decompressedLen, compressedLen, err := b.page.writePage(
		b.writer,
		b.opts.schema.CompressorPool(),
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
			FromFp:    b.fromFp,
			ThroughFp: b.previousFp,
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

func (b *IndexBuilder) Close() error {
	if b.page.Count() > 0 {
		if err := b.flushPage(); err != nil {
			return errors.Wrap(err, "flushing final series page")
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
		return errors.Wrap(err, "writing series page headers")
	}
	return errors.Wrap(b.writer.Close(), "closing series writer")
}

// SortBlocksIntoOverlappingGroups sorts a list of blocks into a sorted list of lists,
// where each list contains blocks that overlap with each other.
// TODO(owen-d): implement as an iterator so we don't have to load all blocks at once
// NB: unused now, but likely useful when we want to optimize compaction. I wrote this expecting to need it now
// but it feels unsavory to remove it
func SortBlocksIntoOverlappingGroups(xs []*Block) (groups [][]*Block) {
	sort.Slice(xs, func(i, j int) bool {
		a, b := xs[i].index, xs[j].index
		return a.pageHeaders[0].FromFp <= b.pageHeaders[0].FromFp
	})

	var curGroup []*Block
	for _, x := range xs {
		switch {
		case len(curGroup) == 0:
			curGroup = append(curGroup, x)
		case curGroup[len(curGroup)-1].dataRange.OverlapFingerprintRange(x.dataRange):
			curGroup = append(curGroup, x)
		default:
			groups = append(groups, curGroup)
			curGroup = []*Block{x}
		}
	}

	if len(curGroup) > 0 {
		groups = append(groups, curGroup)
	}
	return groups
}

// Simplistic implementation of a merge builder that builds a single block
// from a list of blocks and a store of series.
type MergeBuilder struct {
	// existing blocks
	blocks []PeekingIterator[*SeriesWithBloom]
	// store
	store Iterator[*Series]
	// Add chunks to a bloom
	populate func(*Series, *Bloom) error
}

// NewMergeBuilder is a specific builder which does the following:
//  1. merges multiple blocks into a single ordered querier,
//     i) When two blocks have the same series, it will prefer the one with the most chunks already indexed
//  2. iterates through the store, adding chunks to the relevant blooms via the `populate` argument
func NewMergeBuilder(blocks []PeekingIterator[*SeriesWithBloom], store Iterator[*Series], populate func(*Series, *Bloom) error) *MergeBuilder {
	return &MergeBuilder{
		blocks:   blocks,
		store:    store,
		populate: populate,
	}
}

// NB: this will build one block. Ideally we would build multiple blocks once a target size threshold is met
// but this gives us a good starting point.
func (mb *MergeBuilder) Build(builder *BlockBuilder) (uint32, error) {
	var (
		nextInBlocks *SeriesWithBloom
	)

	// Turn the list of blocks into a single iterator that returns the next series
	mergedBlocks := NewPeekingIter[*SeriesWithBloom](NewHeapIterForSeriesWithBloom(mb.blocks...))
	// two overlapping blocks can conceivably have the same series, so we need to dedupe,
	// preferring the one with the most chunks already indexed since we'll have
	// to add fewer chunks to the bloom
	deduped := NewDedupingIter[*SeriesWithBloom](
		func(a, b *SeriesWithBloom) bool {
			return a.Series.Fingerprint == b.Series.Fingerprint
		},
		id[*SeriesWithBloom],
		func(a, b *SeriesWithBloom) *SeriesWithBloom {
			if len(a.Series.Chunks) > len(b.Series.Chunks) {
				return a
			}
			return b
		},
		mergedBlocks,
	)

	for mb.store.Next() {
		nextInStore := mb.store.At()

		// advance the merged blocks iterator until we find a series that is
		// greater than or equal to the next series in the store.
		// TODO(owen-d): expensive, but Seek is not implemented for this itr.
		// It's also more efficient to build an iterator over the Series file in the index
		// without the blooms until we find a bloom we actually need to unpack from the blooms file.
		for nextInBlocks == nil || nextInBlocks.Series.Fingerprint < mb.store.At().Fingerprint {
			if !deduped.Next() {
				// we've exhausted all the blocks
				nextInBlocks = nil
				break
			}
			nextInBlocks = deduped.At()
		}

		cur := nextInBlocks
		chunksToAdd := nextInStore.Chunks
		// The next series from the store doesn't exist in the blocks, so we add it
		// in its entirety
		if nextInBlocks == nil || nextInBlocks.Series.Fingerprint > nextInStore.Fingerprint {
			cur = &SeriesWithBloom{
				Series: nextInStore,
				Bloom: &Bloom{
					// TODO parameterise SBF options. fp_rate
					ScalableBloomFilter: *filter.NewScalableBloomFilter(1024, 0.01, 0.8),
				},
			}
		} else {
			// if the series already exists in the block, we only need to add the new chunks
			chunksToAdd = nextInStore.Chunks.Unless(nextInBlocks.Series.Chunks)
		}

		if len(chunksToAdd) > 0 {
			if err := mb.populate(
				&Series{
					Fingerprint: nextInStore.Fingerprint,
					Chunks:      chunksToAdd,
				},
				cur.Bloom,
			); err != nil {
				return 0, errors.Wrapf(err, "populating bloom for series with fingerprint: %v", nextInStore.Fingerprint)
			}
		}

		if err := builder.AddSeries(*cur); err != nil {
			return 0, errors.Wrap(err, "adding series to block")
		}
	}

	checksum, err := builder.blooms.Close()
	if err != nil {
		return 0, errors.Wrap(err, "closing bloom file")
	}
	if err := builder.index.Close(); err != nil {
		return 0, errors.Wrap(err, "closing series file")
	}
	return checksum, nil
}
