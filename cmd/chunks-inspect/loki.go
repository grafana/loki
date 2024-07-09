package main

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/grafana/loki/v3/pkg/chunkenc"
)

type Block struct {
	chunkenc.Block
	block
}

// LokiChunk from pkg/storage/chunk/chunk.go Chunk
type LokiChunk struct {
	version          byte
	encoding         chunkenc.Encoding
	compressedSize   int
	uncompressedSize int
	blocks           []Block
}

func parseLokiChunk(chunkHeader *ChunkHeader, r io.Reader) (*LokiChunk, error) {

	/* Loki Chunk Format

	4B magic number
	1B version
	1B encoding
	Block 1 <------------------------------------B
	Block 1 Checksum
	...
	Uvarint # blocks <-------------------------- A
	Block1 Uvarint # entries
	Block1 Varint64 mint
	Block1 Varint64 maxt
	Block1 Varint64 offset --------------------> B
	Block1 Uvarint uncomp size (V3 chunks and greater only)
	Block1 Uvarint length
	Block1 Meta Checksum
	...
	4B Meta offset ----------------------------> A
	*/

	// Loki chunks need to be loaded into memory, because some offsets are actually stored at the end.
	data := make([]byte, chunkHeader.DataLength)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, fmt.Errorf("failed to read rawData for Loki chunk into memory: %w", err)
	}

	if num := binary.BigEndian.Uint32(data[0:4]); num != 0x012EE56A {
		return nil, fmt.Errorf("invalid magic number: %0x", num)
	}

	// Chunk version is at position 4
	version := data[4]

	c, _ := chunkenc.NewByteChunk(data, 0, 0)
	encoding := c.Encoding()
	compressedSize := c.CompressedSize()
	uncompressedSize := c.UncompressedSize()
	from, through := c.Bounds()

	bs1 := c.Blocks(from, through)
	err := c.Close()
	if err != nil {
		return nil, err
	}

	bs2, err := parseBlocks(data, encoding, version)
	if err != nil {
		return nil, err
	}

	if len(bs1) != len(bs2) {
		return nil, fmt.Errorf("mismatch between number of blocks (API vs. internal parsing)")
	}

	bs := make([]Block, len(bs1))
	for i := range bs1 {
		bs[i].Block = bs1[i]
		bs[i].block = bs2[i]
	}

	return &LokiChunk{
		version:          version,
		encoding:         encoding,
		compressedSize:   compressedSize,
		uncompressedSize: uncompressedSize,
		blocks:           bs,
	}, nil
}
