package v1

import (
	"io"

	"github.com/pkg/errors"

	"github.com/grafana/loki/v3/pkg/util/encoding"
)

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
