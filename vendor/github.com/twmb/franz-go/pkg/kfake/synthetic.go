package kfake

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"math"
	"math/rand"
	"strconv"

	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
)

const (
	// batchHeaderSize is the size of a v2 record batch header, from the
	// first offset through NumRecords.
	batchHeaderSize = 61

	// syntheticEnd is the high watermark, last stable offset, and listed
	// end offset of every partition in a synthetic cluster. It is far
	// enough out that a consumer starting at the end still fetches for as
	// long as you care to run.
	syntheticEnd = 1 << 62

	// syntheticFill is how many record bytes we generate when you do not
	// say how many records you want.
	syntheticFill = 1 << 20

	// syntheticMax caps the value bytes we generate, so a typo in Records
	// or RecordBytes cannot allocate gigabytes.
	syntheticMax = 64 << 20

	// syntheticTimestamp is the one timestamp every generated batch
	// carries. It is fixed so the bytes are identical every run.
	syntheticTimestamp = 1600000000000

	// syntheticRecordBytes is the default size of a generated value.
	syntheticRecordBytes = 100
)

// syntheticFetch holds the canned batch a SyntheticFetch cluster serves.
type syntheticFetch struct {
	spec    SyntheticBatch
	batch   []byte // a complete v2 record batch, generated or given
	records int64  // records in batch, the offset step between copies
}

func (s *syntheticFetch) init() error {
	if s.spec.Batch != nil {
		return s.initGiven()
	}
	return s.initGenerated()
}

func (s *syntheticFetch) initGiven() error {
	b := s.spec
	if b.Records != 0 || b.RecordBytes != 0 || b.RandomFrac != 0 || b.Compression != (kgo.CompressionCodec{}) {
		return errors.New("SyntheticBatch.Batch is served as is, but another field is set alongside it")
	}
	if len(b.Batch) < batchHeaderSize {
		return fmt.Errorf("SyntheticBatch.Batch is %d bytes, below the %d byte v2 record batch header", len(b.Batch), batchHeaderSize)
	}
	if b.Batch[16] != 2 {
		return fmt.Errorf("SyntheticBatch.Batch has magic %d, we serve magic 2", b.Batch[16])
	}
	records := int64(int32(binary.BigEndian.Uint32(b.Batch[57:61])))
	if records < 1 {
		return fmt.Errorf("SyntheticBatch.Batch says it holds %d records, we need at least one", records)
	}
	s.batch = b.Batch
	s.records = records
	return nil
}

func (s *syntheticFetch) initGenerated() error {
	b := s.spec
	if f := b.RandomFrac; math.IsNaN(f) || f < 0 || f > 1 {
		return fmt.Errorf("SyntheticBatch.RandomFrac %v is outside [0, 1]", f)
	}
	if b.Records < 0 {
		return fmt.Errorf("SyntheticBatch.Records %d is negative", b.Records)
	}
	if b.RecordBytes < 0 {
		return fmt.Errorf("SyntheticBatch.RecordBytes %d is negative", b.RecordBytes)
	}
	recordBytes := b.RecordBytes
	if recordBytes == 0 {
		recordBytes = syntheticRecordBytes
	}
	// We compare by division: Records times RecordBytes can overflow.
	if int64(recordBytes) > syntheticMax || b.Records > 0 && int64(b.Records) > syntheticMax/int64(recordBytes) {
		return fmt.Errorf("SyntheticBatch.Records * RecordBytes is above the %d byte cap", syntheticMax)
	}
	compressor, err := kgo.DefaultCompressor(b.Compression)
	if err != nil {
		return fmt.Errorf("SyntheticBatch.Compression: %w", err)
	}

	// One random stream across the batch, from a fixed seed: every record
	// gets different bytes, the random part cannot compress across
	// records, and the same batch comes out every run.
	rng := rand.New(rand.NewSource(0))
	var (
		recs  []byte
		value = make([]byte, recordBytes)
		split = recordBytes - int(float64(recordBytes)*b.RandomFrac)
		n     int32
	)
	for b.Records > 0 && int(n) < b.Records || b.Records == 0 && len(recs) < syntheticFill {
		formatValue(int64(n), value[:split])
		rng.Read(value[split:])
		r := kmsg.Record{OffsetDelta: n, Value: value}
		// Length counts the bytes after the length varint itself, so we
		// serialize once with Length 0 to measure and once with it set.
		r.Length = int32(len(r.AppendTo(nil)) - 1)
		recs = append(recs, r.AppendTo(nil)...)
		n++
	}

	var attrs int16
	if compressor != nil {
		var dst bytes.Buffer
		compressed, used := compressor.Compress(&dst, recs)
		if used == kgo.CodecError {
			return errors.New("compressing the synthetic batch failed")
		}
		recs, attrs = compressed, int16(used)
	}
	rb := kmsg.RecordBatch{
		PartitionLeaderEpoch: -1,
		Magic:                2,
		Attributes:           attrs,
		LastOffsetDelta:      n - 1,
		FirstTimestamp:       syntheticTimestamp,
		MaxTimestamp:         syntheticTimestamp,
		ProducerID:           -1,
		ProducerEpoch:        -1,
		FirstSequence:        -1,
		NumRecords:           n,
		Records:              recs,
	}
	s.batch = sealBatch(rb.AppendTo(nil))
	s.records = int64(n)
	return nil
}

// appendBatches appends copies of the canned batch to dst: copy i lists first
// offset offset+i*records and the given leader epoch. Both fields sit before
// the CRC, so the copies keep the CRC we built. We always append one copy,
// then as many more as fit in room.
func (s *syntheticFetch) appendBatches(dst []byte, offset int64, epoch int32, room int) []byte {
	for i := int64(0); ; i++ {
		at := len(dst)
		dst = append(dst, s.batch...)
		binary.BigEndian.PutUint64(dst[at:at+8], uint64(offset+i*s.records))
		binary.BigEndian.PutUint32(dst[at+12:at+16], uint32(epoch))
		if len(dst)+len(s.batch) > room {
			return dst
		}
	}
}

// sealBatch patches a serialized v2 record batch's Length and CRC. Length
// covers every byte after the first offset and the length itself, and the
// Castagnoli CRC covers from Attributes onward.
func sealBatch(raw []byte) []byte {
	binary.BigEndian.PutUint32(raw[8:12], uint32(len(raw)-12))
	binary.BigEndian.PutUint32(raw[17:21], crc32.Checksum(raw[21:], crc32c))
	return raw
}

// formatValue fills v with num in decimal followed by a space, repeated, the
// way examples/bench fills the values it produces.
func formatValue(num int64, v []byte) {
	var buf [20]byte // max int64 takes 19 bytes, then we add a space
	b := strconv.AppendInt(buf[:0], num, 10)
	b = append(b, ' ')
	n := copy(v, b)
	for n != len(v) {
		n += copy(v[n:], b)
	}
}
