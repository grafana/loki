package main

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"

	"github.com/golang/snappy"
	"github.com/klauspost/compress/flate"
	"github.com/klauspost/compress/zstd"
	"github.com/pierrec/lz4"
)

type Encoding struct {
	code     int
	name     string
	readerFn func(io.Reader) (io.Reader, error)
}

func (e Encoding) String() string {
	return e.name
}

// The table gets initialized with sync.Once but may still cause a race
// with any other use of the crc32 package anywhere. Thus we initialize it
// before.
var castagnoliTable *crc32.Table

func init() {
	castagnoliTable = crc32.MakeTable(crc32.Castagnoli)
}

var (
	encNone     = Encoding{code: 0, name: "none", readerFn: func(reader io.Reader) (io.Reader, error) { return reader, nil }}
	encGZIP     = Encoding{code: 1, name: "gzip", readerFn: func(reader io.Reader) (io.Reader, error) { return gzip.NewReader(reader) }}
	encDumb     = Encoding{code: 2, name: "dumb", readerFn: func(reader io.Reader) (io.Reader, error) { return reader, nil }}
	encLZ4      = Encoding{code: 3, name: "lz4", readerFn: func(reader io.Reader) (io.Reader, error) { return lz4.NewReader(reader), nil }}
	encSnappy   = Encoding{code: 4, name: "snappy", readerFn: func(reader io.Reader) (io.Reader, error) { return snappy.NewReader(reader), nil }}
	enclz4_256k = Encoding{code: 5, name: "lz4-256k", readerFn: func(reader io.Reader) (io.Reader, error) { return lz4.NewReader(reader), nil }}
	enclz4_1M   = Encoding{code: 6, name: "lz4-1M", readerFn: func(reader io.Reader) (io.Reader, error) { return lz4.NewReader(reader), nil }}
	enclz4_4M   = Encoding{code: 7, name: "lz4-4M", readerFn: func(reader io.Reader) (io.Reader, error) { return lz4.NewReader(reader), nil }}
	encFlate    = Encoding{code: 8, name: "flate", readerFn: func(reader io.Reader) (io.Reader, error) { return flate.NewReader(reader), nil }}
	encZstd     = Encoding{code: 9, name: "zstd", readerFn: func(reader io.Reader) (io.Reader, error) {
		r, err := zstd.NewReader(reader)
		if err != nil {
			panic(err)
		}
		return r, nil
	}}

	Encodings = []Encoding{encNone, encGZIP, encDumb, encLZ4, encSnappy, enclz4_256k, enclz4_1M, enclz4_4M, encFlate, encZstd}
)

const (
	_ byte = iota
	chunkFormatV1
	chunkFormatV2
	chunkFormatV3
)

type LokiChunk struct {
	format   byte
	encoding Encoding

	blocks []LokiBlock

	metadataChecksum         uint32
	computedMetadataChecksum uint32
}

type LokiBlock struct {
	numEntries uint64 // number of log lines in this block
	minT       int64  // minimum timestamp, unix nanoseconds
	maxT       int64  // max timestamp, unix nanoseconds

	dataOffset uint64 // offset in the data-part of chunks file

	uncompSize uint64 // size of the original data uncompressed

	rawData      []byte // data as stored in chunk file, compressed
	originalData []byte // data uncompressed from rawData

	// parsed rawData
	entries          []LokiEntry
	storedChecksum   uint32
	computedChecksum uint32
}

type LokiEntry struct {
	timestamp int64
	line      string
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

	// Chunk format is at position 4
	f := data[4]

	compression, err := getCompression(f, data[5])
	if err != nil {
		return nil, fmt.Errorf("failed to read compression: %w", err)
	}

	// return &LokiChunk{encoding: compression}, nil

	metasOffset := binary.BigEndian.Uint64(data[len(data)-8:])

	metadata := data[metasOffset : len(data)-(8+4)]

	metaChecksum := binary.BigEndian.Uint32(data[len(data)-12 : len(data)-8])
	computedMetaChecksum := crc32.Checksum(metadata, castagnoliTable)

	blocks, n := binary.Uvarint(metadata)
	if n <= 0 {
		return nil, fmt.Errorf("failed to read number of blocks")
	}
	metadata = metadata[n:]

	lokiChunk := &LokiChunk{
		format:                   f,
		encoding:                 compression,
		metadataChecksum:         metaChecksum,
		computedMetadataChecksum: computedMetaChecksum,
	}

	for ix := 0; ix < int(blocks); ix++ {
		block := LokiBlock{}
		block.numEntries, metadata, err = readUvarint(err, metadata)
		block.minT, metadata, err = readVarint(err, metadata)
		block.maxT, metadata, err = readVarint(err, metadata)
		block.dataOffset, metadata, err = readUvarint(err, metadata)
		if f >= chunkFormatV3 {
			block.uncompSize, metadata, err = readUvarint(err, metadata)
		}
		dataLength := uint64(0)
		dataLength, metadata, err = readUvarint(err, metadata)

		if err != nil {
			return nil, err
		}

		block.rawData = data[block.dataOffset : block.dataOffset+dataLength]
		block.storedChecksum = binary.BigEndian.Uint32(data[block.dataOffset+dataLength : block.dataOffset+dataLength+4])
		block.computedChecksum = crc32.Checksum(block.rawData, castagnoliTable)
		block.originalData, block.entries, err = parseLokiBlock(compression, block.rawData)
		lokiChunk.blocks = append(lokiChunk.blocks, block)
	}

	return lokiChunk, nil
}

func parseLokiBlock(compression Encoding, data []byte) ([]byte, []LokiEntry, error) {
	r, err := compression.readerFn(bytes.NewReader(data))
	if err != nil {
		return nil, nil, err
	}

	decompressed, err := io.ReadAll(r)
	origDecompressed := decompressed
	if err != nil {
		return nil, nil, err
	}

	entries := []LokiEntry(nil)
	for len(decompressed) > 0 {
		var timestamp int64
		var lineLength uint64

		timestamp, decompressed, err = readVarint(err, decompressed)
		lineLength, decompressed, err = readUvarint(err, decompressed)
		if err != nil {
			return origDecompressed, nil, err
		}

		if len(decompressed) < int(lineLength) {
			return origDecompressed, nil, fmt.Errorf("not enough line data, need %d, got %d", lineLength, len(decompressed))
		}

		entries = append(entries, LokiEntry{
			timestamp: timestamp,
			line:      string(decompressed[0:lineLength]),
		})

		decompressed = decompressed[lineLength:]
	}

	return origDecompressed, entries, nil
}

func readVarint(prevErr error, buf []byte) (int64, []byte, error) {
	if prevErr != nil {
		return 0, buf, prevErr
	}

	val, n := binary.Varint(buf)
	if n <= 0 {
		return 0, nil, fmt.Errorf("varint: %d", n)
	}
	return val, buf[n:], nil
}

func readUvarint(prevErr error, buf []byte) (uint64, []byte, error) {
	if prevErr != nil {
		return 0, buf, prevErr
	}

	val, n := binary.Uvarint(buf)
	if n <= 0 {
		return 0, nil, fmt.Errorf("varint: %d", n)
	}
	return val, buf[n:], nil
}

func getCompression(format byte, code byte) (Encoding, error) {
	if format == chunkFormatV1 {
		return encGZIP, nil
	}

	if format >= chunkFormatV2 {
		for _, e := range Encodings {
			if e.code == int(code) {
				return e, nil
			}
		}

		return encNone, fmt.Errorf("unknown encoding: %d", code)
	}

	return encNone, fmt.Errorf("unknown format: %d", format)
}
