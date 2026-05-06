package handler

// RecordBatch encoding and decoding for the Kafka magic=2 record format.
//
// Wire layout of a RecordBatch:
//
//	FirstOffset          int64   (8)
//	BatchLength          int32   (4)  — byte count of everything that follows
//	PartitionLeaderEpoch int32   (4)
//	Magic                int8    (1)  = 2
//	CRC                  int32   (4)  — CRC32C of everything from Attributes onward
//	Attributes           int16   (2)
//	LastOffsetDelta      int32   (4)
//	FirstTimestamp       int64   (8)
//	MaxTimestamp         int64   (8)
//	ProducerID           int64   (8)
//	ProducerEpoch        int16   (2)
//	FirstSequence        int32   (4)
//	NumRecords           int32   (4)
//	Records              []byte  (variable) — varint-encoded Record entries
//
// Wire layout of an individual Record (inside Records bytes):
//
//	Length          zigzag-varint  — byte count of everything that follows
//	Attributes      int8  = 0
//	TimestampDelta  zigzag-varlong (millis delta from batch FirstTimestamp)
//	OffsetDelta     zigzag-varint  (delta from batch FirstOffset)
//	Key             zigzag-varint(len) + bytes  (-1 = null)
//	Value           zigzag-varint(len) + bytes
//	NumHeaders      zigzag-varint
//	[Header:  zigzag-varint(len)+key  zigzag-varint(len)+value]*

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"time"

	"github.com/klauspost/compress/s2"
	"github.com/pierrec/lz4/v4"

	"github.com/chaudum/go-kaff/codec"
	"github.com/chaudum/go-kaff/store"
)

var crc32cTable = crc32.MakeTable(crc32.Castagnoli)

// ── Encode ────────────────────────────────────────────────────────────────────

// encodeRecordBatch builds a single Kafka RecordBatch (magic=2) from records.
// All records must already have their Offset fields set.
// Returns nil when records is empty.
func encodeRecordBatch(records []store.Record) []byte {
	if len(records) == 0 {
		return nil
	}

	firstOffset := records[0].Offset
	firstTS := records[0].Timestamp.UnixMilli()
	maxTS := records[len(records)-1].Timestamp.UnixMilli()

	// Encode individual records into a temporary buffer.
	recWriter := codec.NewWriter()
	for i, rec := range records {
		appendRecord(recWriter, rec, firstOffset, firstTS, int32(i))
	}
	recBytes := recWriter.Bytes()

	// Build the post-CRC section (Attributes → end).
	postCRC := codec.NewWriter()
	postCRC.WriteInt16(0)                       // Attributes (no compression, CreateTime)
	postCRC.WriteInt32(int32(len(records) - 1)) // LastOffsetDelta
	postCRC.WriteInt64(firstTS)                 // FirstTimestamp
	postCRC.WriteInt64(maxTS)                   // MaxTimestamp
	postCRC.WriteInt64(-1)                      // ProducerID  (-1 = no producer)
	postCRC.WriteInt16(-1)                      // ProducerEpoch
	postCRC.WriteInt32(-1)                      // FirstSequence
	postCRC.WriteInt32(int32(len(records)))     // NumRecords
	postCRC.WriteRaw(recBytes)
	postCRCBytes := postCRC.Bytes()

	crc := crc32.Checksum(postCRCBytes, crc32cTable)

	// BatchLength = PartitionLeaderEpoch(4) + Magic(1) + CRC(4) + len(postCRC).
	batchLength := int32(4 + 1 + 4 + len(postCRCBytes))

	out := codec.NewWriter()
	out.WriteInt64(firstOffset) // FirstOffset
	out.WriteInt32(batchLength) // BatchLength
	out.WriteInt32(-1)          // PartitionLeaderEpoch (-1 for client-produced)
	out.WriteInt8(2)            // Magic = 2
	out.WriteInt32(int32(crc))  // CRC32C
	out.WriteRaw(postCRCBytes)
	return out.Bytes()
}

// appendRecord serialises one Record into w using the Kafka varint record format.
func appendRecord(w *codec.Writer, rec store.Record, firstOffset, firstTS int64, idx int32) {
	// Encode body first so we can prefix it with its varint length.
	body := codec.NewWriter()
	body.WriteInt8(0)                                      // Attributes
	body.WriteVarLong(rec.Timestamp.UnixMilli() - firstTS) // TimestampDelta
	body.WriteVarInt(int32(rec.Offset - firstOffset))      // OffsetDelta
	body.WriteVarintBytes(rec.Key)                         // Key
	body.WriteVarintBytes(rec.Value)                       // Value
	body.WriteVarInt(int32(len(rec.Headers)))              // NumHeaders
	for _, h := range rec.Headers {
		body.WriteVarintString(h.Key)
		body.WriteVarintBytes(h.Value)
	}
	bodyBytes := body.Bytes()

	w.WriteVarInt(int32(len(bodyBytes))) // Length prefix
	w.WriteRaw(bodyBytes)
}

// ── Decode ────────────────────────────────────────────────────────────────────

// decodeRecordBatches parses one or more concatenated RecordBatch blobs from
// data and returns the individual records.  Only magic=2 (RecordBatch format,
// Kafka 0.11+) is supported; older MessageSet formats are silently skipped.
// CRC validation is skipped in Phase 2.
func decodeRecordBatches(data []byte) ([]store.Record, error) {
	var out []store.Record
	for len(data) >= 12 {
		firstOffset := int64(binary.BigEndian.Uint64(data[0:8]))
		batchLength := int(binary.BigEndian.Uint32(data[8:12]))
		total := 12 + batchLength
		if total > len(data) {
			return out, fmt.Errorf("recordbatch: truncated batch (claimed %d bytes, have %d)", batchLength, len(data)-12)
		}
		batch := data[12:total]
		data = data[total:]

		recs, err := decodeSingleBatch(batch, firstOffset)
		if err != nil {
			// Corrupt batch — stop parsing this blob but return what we have.
			return out, err
		}
		out = append(out, recs...)
	}
	return out, nil
}

// compressionCodec extracts the compression codec from the Attributes field.
// Bits 0–2 of Attributes encode the codec:
//
//	0 = none, 1 = gzip, 2 = snappy, 3 = lz4, 4 = zstd
func compressionCodec(attributes int16) int {
	return int(attributes & 0x07)
}

// decompressRecords decompresses a records payload according to the codec
// encoded in the batch Attributes.  Returns the payload unchanged for codec 0
// (no compression).
func decompressRecords(codec int, data []byte) ([]byte, error) {
	switch codec {
	case 0: // none
		return data, nil
	case 1: // gzip
		gr, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("recordbatch: gzip reader: %w", err)
		}
		deferred, err := io.ReadAll(gr)
		if err != nil {
			return nil, fmt.Errorf("recordbatch: gzip decompress: %w", err)
		}
		return deferred, nil
	case 2: // snappy — raw snappy/S2 block format (as used by magic=2 record batches)
		out, err := s2.Decode(nil, data)
		if err != nil {
			return nil, fmt.Errorf("recordbatch: snappy decompress: %w", err)
		}
		return out, nil
	case 3: // lz4
		r := lz4.NewReader(bytes.NewReader(data))
		out, err := io.ReadAll(r)
		if err != nil {
			return nil, fmt.Errorf("recordbatch: lz4 decompress: %w", err)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("recordbatch: unsupported compression codec %d", codec)
	}
}

// decodeSingleBatch parses the body of one RecordBatch (everything after
// the 12-byte FirstOffset+BatchLength header).
func decodeSingleBatch(batch []byte, firstOffset int64) ([]store.Record, error) {
	// Minimum: PartitionLeaderEpoch(4) + Magic(1) + CRC(4) + 40 bytes post-CRC = 49
	if len(batch) < 49 {
		return nil, fmt.Errorf("recordbatch: batch too short (%d bytes)", len(batch))
	}

	magic := batch[4] // PartitionLeaderEpoch(4), then Magic(1)
	if magic != 2 {
		return nil, nil // MessageSet format — skip silently
	}

	// CRC is at bytes 5-8; we skip verification in Phase 2.
	// Post-CRC section starts at offset 9.
	pc := batch[9:]
	// Attributes(2) + LastOffsetDelta(4) + FirstTimestamp(8) + MaxTimestamp(8)
	// + ProducerID(8) + ProducerEpoch(2) + FirstSequence(4) + NumRecords(4) = 40 bytes
	attributes := int16(binary.BigEndian.Uint16(pc[0:2]))
	firstTS := int64(binary.BigEndian.Uint64(pc[6:14]))
	numRecords := int32(binary.BigEndian.Uint32(pc[36:40]))
	recData := pc[40:]

	// Decompress the records payload when the batch was compressed.
	if codecType := compressionCodec(attributes); codecType != 0 {
		var err error
		recData, err = decompressRecords(codecType, recData)
		if err != nil {
			return nil, err
		}
	}

	r := codec.NewReader(recData)
	var out []store.Record
	for i := int32(0); i < numRecords; i++ {
		rec, err := decodeRecord(r, firstOffset, firstTS)
		if err != nil {
			return nil, fmt.Errorf("recordbatch: record %d/%d: %w", i+1, numRecords, err)
		}
		if r.Err() != nil {
			return nil, fmt.Errorf("recordbatch: record %d/%d: %w", i+1, numRecords, r.Err())
		}
		out = append(out, rec)
	}
	return out, nil
}

// decodeRecord reads one record from r, computing its absolute offset and
// timestamp from the batch-level firstOffset and firstTS values.
func decodeRecord(r *codec.Reader, firstOffset, firstTS int64) (store.Record, error) {
	l := r.ReadVarInt()
	bodyLen := int32(l)

	if r.Err() != nil {
		return store.Record{}, r.Err()
	}
	if bodyLen < 0 {
		return store.Record{}, fmt.Errorf("recordbatch: negative record length %d", bodyLen)
	}

	body := r.ReadFull(int(bodyLen))
	if r.Err() != nil {
		return store.Record{}, r.Err()
	}

	br := codec.NewReader(body)
	_ = br.ReadInt8()                     // Attributes (unused)
	tsDelta := br.ReadVarInt()            // int64 (zigzag varlong)
	offsetDelta := int32(br.ReadVarInt()) // int64 → int32
	key := br.ReadVarintBytes()
	value := br.ReadVarintBytes()

	numHeaders := int32(br.ReadVarInt())
	var headers []store.Header
	for i := int32(0); i < numHeaders; i++ {
		k := br.ReadVarintString()
		v := br.ReadVarintBytes()
		headers = append(headers, store.Header{Key: k, Value: v})
	}

	if err := br.Err(); err != nil {
		return store.Record{}, err
	}

	return store.Record{
		Offset:    firstOffset + int64(offsetDelta),
		Timestamp: time.UnixMilli(firstTS + tsDelta),
		Key:       key,
		Value:     value,
		Headers:   headers,
	}, nil
}
