package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/chunkenc"
	"github.com/grafana/loki/v3/pkg/compression"
	"github.com/grafana/loki/v3/pkg/logproto"
)

var testCodecs = compression.Codecs()

var testFormats = []struct {
	chunkFormat  byte
	headBlockFmt chunkenc.HeadBlockFmt
}{
	{chunkFormat: chunkenc.ChunkFormatV2, headBlockFmt: chunkenc.OrderedHeadBlockFmt},
	{chunkFormat: chunkenc.ChunkFormatV3, headBlockFmt: chunkenc.UnorderedHeadBlockFmt},
	{chunkFormat: chunkenc.ChunkFormatV4, headBlockFmt: chunkenc.UnorderedWithStructuredMetadataHeadBlockFmt},
}

const (
	testEntries = 40
	// Small enough that testEntries is spread over several blocks.
	testBlockSize  = 256
	testTargetSize = 1 << 20
)

// buildChunk encodes a chunk holding testEntries lines, as Loki itself would
// write it.
func buildChunk(t *testing.T, chunkFormat byte, headBlockFmt chunkenc.HeadBlockFmt, codec compression.Codec) []byte {
	t.Helper()

	c := chunkenc.NewMemChunk(chunkFormat, codec, headBlockFmt, testBlockSize, testTargetSize)
	for i := 0; i < testEntries; i++ {
		entry := &logproto.Entry{
			Timestamp: time.Unix(int64(i), 0),
			Line:      fmt.Sprintf("line %02d of the test chunk", i),
		}
		if chunkFormat >= chunkenc.ChunkFormatV4 {
			entry.StructuredMetadata = []logproto.LabelAdapter{{Name: "trace_id", Value: strconv.Itoa(i)}}
		}

		dup, err := c.Append(entry)
		require.False(t, dup)
		require.NoError(t, err)
	}
	require.NoError(t, c.Close())

	data, err := c.Bytes()
	require.NoError(t, err)

	return data
}

func parseChunk(t *testing.T, data []byte) *LokiChunk {
	t.Helper()

	chunk, err := parseLokiChunk(&ChunkHeader{DataLength: uint32(len(data))}, bytes.NewReader(data))
	require.NoError(t, err)

	return chunk
}

// blockPayload returns the block's compressed data as a slice of data, so it can
// be written to in place.
func blockPayload(data []byte, block LokiBlock) []byte {
	return data[block.dataOffset : block.dataOffset+uint64(len(block.rawData))]
}

func repairChecksum(data []byte, block LokiBlock) {
	binary.BigEndian.PutUint32(data[block.dataOffset+uint64(len(block.rawData)):], crc32.Checksum(blockPayload(data, block), castagnoliTable))
}

// corruptSecondEntry overwrites the line length of the second entry in the given
// block with a value larger than the block itself, then repairs the block
// checksum so that the damage only shows up while parsing the entry stream.
//
// It reads the entry stream directly, so the chunk must be uncompressed and of a
// format that stores no structured metadata alongside each line.
func corruptSecondEntry(t *testing.T, data []byte, blockIdx int) {
	t.Helper()

	block := parseChunk(t, data).blocks[blockIdx]
	require.Greater(t, block.numEntries, uint64(1))

	payload := blockPayload(data, block)

	_, tsLen := binary.Varint(payload)
	lineLen, lineLenWidth := binary.Uvarint(payload[tsLen:])

	secondEntry := tsLen + lineLenWidth + int(lineLen)
	_, tsLen = binary.Varint(payload[secondEntry:])
	copy(payload[secondEntry+tsLen:], []byte{0xff, 0xff, 0xff, 0x7f})

	repairChecksum(data, block)
}

func TestParseLokiChunk(t *testing.T) {
	for _, format := range testFormats {
		for _, codec := range testCodecs {
			t.Run(fmt.Sprintf("chunkFormat:%d codec:%s", format.chunkFormat, codec), func(t *testing.T) {
				t.Parallel()

				chunk := parseChunk(t, buildChunk(t, format.chunkFormat, format.headBlockFmt, codec))

				require.Equal(t, format.chunkFormat, chunk.format)
				require.Equal(t, codec, chunk.encoding)
				require.Equal(t, chunk.metadataChecksum, chunk.computedMetadataChecksum)
				require.NotEmpty(t, chunk.blocks)

				entries := 0
				for ix, b := range chunk.blocks {
					require.NoErrorf(t, b.parseErr, "block %d", ix)
					require.Equalf(t, b.storedChecksum, b.computedChecksum, "block %d", ix)
					require.Lenf(t, b.entries, int(b.numEntries), "block %d", ix)
					entries += len(b.entries)
				}
				require.Equal(t, testEntries, entries)

				first := chunk.blocks[0].entries[0]
				require.Equal(t, "line 00 of the test chunk", first.line)
				if format.chunkFormat >= chunkenc.ChunkFormatV4 {
					require.Equal(t, []label{{name: "trace_id", val: "0"}}, first.structuredMetadata)
				}
			})
		}
	}
}

// A block whose entry stream cannot be parsed must be reported against that
// block, without taking the rest of the chunk with it, wherever it sits in the
// chunk.
func TestParseLokiChunkBlockParseError(t *testing.T) {
	data := buildChunk(t, chunkenc.ChunkFormatV3, chunkenc.UnorderedHeadBlockFmt, compression.None)

	blocks := len(parseChunk(t, data).blocks)
	require.Greater(t, blocks, 2, "need more than two blocks to cover a corrupt block that is not the last one")

	for _, tc := range []struct {
		name     string
		blockIdx int
	}{
		{name: "first block", blockIdx: 0},
		{name: "middle block", blockIdx: 1},
		{name: "last block", blockIdx: blocks - 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			corrupted := bytes.Clone(data)
			corruptSecondEntry(t, corrupted, tc.blockIdx)

			chunk := parseChunk(t, corrupted)
			require.Len(t, chunk.blocks, blocks)

			for ix, b := range chunk.blocks {
				require.Equalf(t, b.storedChecksum, b.computedChecksum, "block %d", ix)

				if ix != tc.blockIdx {
					require.NoErrorf(t, b.parseErr, "block %d", ix)
					require.Lenf(t, b.entries, int(b.numEntries), "block %d", ix)
					continue
				}

				require.Errorf(t, b.parseErr, "block %d", ix)
				// The entry read before the corrupt one is still returned.
				require.Lenf(t, b.entries, 1, "block %d", ix)
			}
		})
	}
}

// A block that cannot even be decompressed is reported the same way, with no
// entries.
func TestParseLokiChunkBlockDecompressionError(t *testing.T) {
	data := buildChunk(t, chunkenc.ChunkFormatV3, chunkenc.UnorderedHeadBlockFmt, compression.GZIP)

	blocks := parseChunk(t, data).blocks
	require.NotEmpty(t, blocks)

	payload := blockPayload(data, blocks[0])
	for i := range payload {
		payload[i] ^= 0xff
	}
	repairChecksum(data, blocks[0])

	chunk := parseChunk(t, data)
	require.Len(t, chunk.blocks, len(blocks))
	require.Error(t, chunk.blocks[0].parseErr)
	require.Empty(t, chunk.blocks[0].entries)

	for ix, b := range chunk.blocks[1:] {
		require.NoErrorf(t, b.parseErr, "block %d", ix+1)
	}
}
