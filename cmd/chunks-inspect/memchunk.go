package main

import (
	"bytes"
	"encoding/binary"
	"io"

	"github.com/pkg/errors"

	"github.com/grafana/loki/v3/pkg/chunkenc"
)

type block struct {
	rawData          []byte // This is compressed bytes.
	originalData     []byte
	offset           int // The offset of the block in the chunk.
	uncompressedSize int // Total uncompressed size in bytes when the chunk is cut.
}

var ErrInvalidSize = errors.New("invalid size")

const chunkMetasSectionIdx = 1

// decbuf provides safe methods to extract data from a byte slice. It does all
// necessary bounds checking and advancing of the byte slice.
// Several datums can be extracted without checking for errors. However, before using
// any datum, the err() method must be checked.
type decbuf struct {
	b []byte
	e error
}

func (d *decbuf) varint64() int64 {
	if d.e != nil {
		return 0
	}
	x, n := binary.Varint(d.b)
	if n < 1 {
		d.e = ErrInvalidSize
		return 0
	}
	d.b = d.b[n:]
	return x
}

func (d *decbuf) uvarint64() uint64 {
	if d.e != nil {
		return 0
	}
	x, n := binary.Uvarint(d.b)
	if n < 1 {
		d.e = ErrInvalidSize
		return 0
	}
	d.b = d.b[n:]
	return x
}

func (d *decbuf) uvarint() int { return int(d.uvarint64()) }

func (d *decbuf) be32() uint32 {
	if d.e != nil {
		return 0
	}
	if len(d.b) < 4 {
		d.e = ErrInvalidSize
		return 0
	}
	x := binary.BigEndian.Uint32(d.b)
	d.b = d.b[4:]
	return x
}

func (d *decbuf) byte() byte {
	if d.e != nil {
		return 0
	}
	if len(d.b) < 1 {
		d.e = ErrInvalidSize
		return 0
	}
	x := d.b[0]
	d.b = d.b[1:]
	return x
}

// extracted from pkg/chunkenc/memchunk.go newByteChunk(...)
func parseBlocks(b []byte, encoding chunkenc.Encoding, version byte) ([]block, error) {

	decompressorPool := chunkenc.GetReaderPool(encoding)

	// readSectionLenAndOffset reads len and offset for different sections within the chunk.
	// Starting from chunk version 4, we have started writing offset and length of various sections within the chunk.
	// These len and offset pairs would be stored together at the end of the chunk.
	// Considering N stored length and offset pairs, they can be referenced by index starting from [1-N]
	// where 1 would be referring to last entry, 2 would be referring to last 2nd entry and so on.
	readSectionLenAndOffset := func(idx int) (uint64, uint64) {
		lenAndOffsetPos := len(b) - (idx * 16)
		lenAndOffset := b[lenAndOffsetPos : lenAndOffsetPos+16]
		return binary.BigEndian.Uint64(lenAndOffset[:8]), binary.BigEndian.Uint64(lenAndOffset[8:])
	}

	metasOffset := uint64(0)
	metasLen := uint64(0)
	if version >= chunkenc.ChunkFormatV4 {
		// version >= 4 starts writing length of sections after their offsets
		metasLen, metasOffset = readSectionLenAndOffset(chunkMetasSectionIdx)
	} else {
		// version <= 3 does not store length of metas. metas are followed by metasOffset + hash and then the chunk ends
		metasOffset = binary.BigEndian.Uint64(b[len(b)-8:])
		metasLen = uint64(len(b)-(8+4)) - metasOffset
	}
	mb := b[metasOffset : metasOffset+metasLen]
	db := decbuf{b: mb}

	// CRC already checked

	// Read the number of blocks.
	num := db.uvarint()
	blocks := make([]block, 0, num)
	for i := 0; i < num; i++ {
		var blk block
		// Read #entries.
		_ = db.uvarint()

		// Read minT, maxT.
		_ = db.varint64()
		_ = db.varint64()

		// Read offset and length.
		blk.offset = db.uvarint()
		if version >= chunkenc.ChunkFormatV3 {
			blk.uncompressedSize = db.uvarint()
		}
		l := db.uvarint()
		blk.rawData = b[blk.offset : blk.offset+l]

		r, err := decompressorPool.GetReader(bytes.NewBuffer(blk.rawData))
		if err != nil {
			return nil, err
		}
		blk.originalData, err = io.ReadAll(r)
		if err != nil {
			return nil, err
		}

		blocks = append(blocks, blk)
	}
	return blocks, nil
}
