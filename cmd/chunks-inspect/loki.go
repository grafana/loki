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
	chunkFormatV4
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

type label struct {
	name string
	val  string
}

type LokiEntry struct {
	timestamp          int64
	line               string
	structuredMetadata []label
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

	//Magic Number
	if num := binary.BigEndian.Uint32(data[0:4]); num != 0x012EE56A {
		return nil, fmt.Errorf("invalid magic number: %0x", num)
	}

	// Chunk format is at position 4
	f := data[4]

	// Compression
	compression, err := getCompression(f, data[5])
	if err != nil {
		return nil, fmt.Errorf("failed to read compression: %w", err)
	}

	// return &LokiChunk{encoding: compression}, nil

	// This was copied from memchunk.go newByteChunk() as a helper function to use here also
	// In format >=v4 there are multiple metadata sections at the end of the chunk file,
	// the offset and length of each is stored as the last bytes in the file
	readSectionLenAndOffset := func(idx int) (uint64, uint64) {
		lenAndOffsetPos := len(data) - (idx * 16)
		lenAndOffset := data[lenAndOffsetPos : lenAndOffsetPos+16]
		return binary.BigEndian.Uint64(lenAndOffset[:8]), binary.BigEndian.Uint64(lenAndOffset[8:])
	}

	// Chunk formats v1-v3 had a single metadata section which was stored at the end of the chunk with the last 8 bytes a pointer to the beginning of the metadata table section
	metasOffset := uint64(0)
	metasLen := uint64(0)
	if f < chunkFormatV4 {
		// Metadata table is at the end, read the last 8 bytes to get the offset to the start of the table
		metasOffset = binary.BigEndian.Uint64(data[len(data)-8:])
		// Exclude the last 8 bytes which is the offset, and the 4 bytes before that which is the checksum from the length
		metasLen = uint64(len(data)-(8+4)) - metasOffset
	} else {
		// the chunk metas section is index 1
		metasLen, metasOffset = readSectionLenAndOffset(1)
	}

	// Read metadata
	metadata := data[metasOffset : metasOffset+metasLen]

	// Read metadata block checksum, the last 4 bytes before the last 8 bytes containing the metadata table start offset
	metaChecksum := binary.BigEndian.Uint32(data[metasOffset+metasLen:])
	computedMetaChecksum := crc32.Checksum(metadata, castagnoliTable)

	// If chunkFormat >= v4 we also need to read the structured metadata section
	var structuredMetadataSymbols []string
	if f >= chunkFormatV4 {
		// the chunk metas section is index 2
		structuredMetadataLength, structuredMetadataOffset := readSectionLenAndOffset(2)

		//expCRC := binary.BigEndian.Uint32(data[structuredMetadataOffset+structuredMetadataLength:])
		//computedMetaChecksum := crc32.Checksum(lb, castagnoliTable)

		structuredMetadata := data[structuredMetadataOffset : structuredMetadataOffset+structuredMetadataLength]

		// Structured Metadata is "normalized" or "compressed" by storing an index to a string with each log line, and then all the strings in the chunk metadata section
		// Here we need to extract the list of string from the metadata to be used for look ups when decompressing the log lines
		// First we read the number of symbols
		symbols, n := binary.Uvarint(structuredMetadata)
		if n <= 0 {
			return nil, fmt.Errorf("failed to read number of labels in structured metadata")
		}
		structuredMetadata = structuredMetadata[n:]

		// Next we need to decompress the list of strings
		lr, err := compression.readerFn(bytes.NewReader(structuredMetadata))
		decompressed, err := io.ReadAll(lr)
		if err != nil {
			return nil, err
		}

		structuredMetadataSymbols = make([]string, 0, symbols)
		// Read every label and add it to a map for easy lookup
		for i := 0; i < int(symbols); i++ {
			// Read the length of the string
			strLen, read := binary.Uvarint(decompressed)
			if read <= 0 {
				return nil, fmt.Errorf("expected to find a length for a structured metadata string but did not find one")
			}
			decompressed = decompressed[read:]

			// Read the bytes of the string and advance the buffer
			str := string(decompressed[:strLen])
			decompressed = decompressed[strLen:]
			// Append to our slice of symbols
			structuredMetadataSymbols = append(structuredMetadataSymbols, str)
		}
	}

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

	expectedOffset := uint64(0)
	for ix := 0; ix < int(blocks); ix++ {
		block := LokiBlock{}
		// Read number of entries in block
		block.numEntries, metadata, err = readUvarint(err, metadata)
		// Read block minimum time
		block.minT, metadata, err = readVarint(err, metadata)
		// Read block max time
		block.maxT, metadata, err = readVarint(err, metadata)
		// Read offset to block data
		block.dataOffset, metadata, err = readUvarint(err, metadata)
		if expectedOffset == 0 {
			expectedOffset = block.dataOffset
		}
		if f >= chunkFormatV3 {
			// Read uncompressed size
			block.uncompSize, metadata, err = readUvarint(err, metadata)
		}
		// Read block length
		dataLength := uint64(0)
		dataLength, metadata, err = readUvarint(err, metadata)

		if err != nil {
			return nil, err
		}

		offset := block.dataOffset
		if offset != expectedOffset {
			fmt.Printf("Incorrect offset. expected %v actual %v\n", expectedOffset, offset)
			offset = expectedOffset
		}

		block.rawData = data[offset : offset+dataLength]
		block.storedChecksum = binary.BigEndian.Uint32(data[offset+dataLength : offset+dataLength+4])
		block.computedChecksum = crc32.Checksum(block.rawData, castagnoliTable)
		block.originalData, block.entries, err = parseLokiBlock(f, compression, block.rawData, structuredMetadataSymbols)
		lokiChunk.blocks = append(lokiChunk.blocks, block)

		expectedOffset += dataLength + 4
	}

	return lokiChunk, nil
}

func parseLokiBlock(format byte, compression Encoding, data []byte, symbols []string) ([]byte, []LokiEntry, error) {
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
		line := string(decompressed[0:lineLength])
		decompressed = decompressed[lineLength:]

		var structuredMetdata []label
		if format >= chunkFormatV4 {
			// The length of the symbols section is encoded first, but we don't really need it here because everything is a Uvarint
			// Read it to advance the buffer to the next element.
			_, decompressed, err = readUvarint(err, decompressed)

			// Read number of structured metadata pairs
			var structuredMetadataPairs uint64
			structuredMetadataPairs, decompressed, err = readUvarint(err, decompressed)
			if err != nil {
				return origDecompressed, nil, err
			}
			structuredMetdata = make([]label, 0, structuredMetadataPairs)
			// Read all the pairs
			for i := 0; i < int(structuredMetadataPairs); i++ {
				var nameIdx uint64
				nameIdx, decompressed, err = readUvarint(err, decompressed)
				if err != nil {
					return origDecompressed, nil, err
				}
				var valIdx uint64
				valIdx, decompressed, err = readUvarint(err, decompressed)
				if err != nil {
					return origDecompressed, nil, err
				}
				lbl := label{name: symbols[nameIdx], val: symbols[valIdx]}
				structuredMetdata = append(structuredMetdata, lbl)
			}
		}

		entries = append(entries, LokiEntry{
			timestamp:          timestamp,
			line:               line,
			structuredMetadata: structuredMetdata,
		})

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
