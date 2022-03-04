package chunk

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"unsafe"

	"github.com/golang/snappy"
	jsoniter "github.com/json-iterator/go"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	errs "github.com/weaveworks/common/errors"

	"github.com/grafana/loki/pkg/prom1/storage/metric"
	prom_chunk "github.com/grafana/loki/pkg/storage/chunk/encoding"
)

const (
	ErrInvalidChecksum = errs.Error("invalid chunk checksum")
	ErrWrongMetadata   = errs.Error("wrong chunk metadata")
	ErrMetadataLength  = errs.Error("chunk metadata wrong length")
	ErrDataLength      = errs.Error("chunk data wrong length")
	ErrSliceOutOfRange = errs.Error("chunk can't be sliced out of its data range")
)

var castagnoliTable = crc32.MakeTable(crc32.Castagnoli)

func errInvalidChunkID(s string) error {
	return errors.Errorf("invalid chunk ID %q", s)
}

// Chunk contains encoded timeseries data
type Chunk struct {
	// These two fields will be missing from older chunks (as will the hash).
	// On fetch we will initialise these fields from the DynamoDB key.
	Fingerprint model.Fingerprint `json:"fingerprint"`
	UserID      string            `json:"userID"`

	// These fields will be in all chunks, including old ones.
	From    model.Time    `json:"from"`
	Through model.Time    `json:"through"`
	Metric  labels.Labels `json:"metric"`

	// The hash is not written to the external storage either.  We use
	// crc32, Castagnoli table.  See http://www.evanjones.ca/crc32c.html.
	// For old chunks, ChecksumSet will be false.
	ChecksumSet bool   `json:"-"`
	Checksum    uint32 `json:"-"`

	// We never use Delta encoding (the zero value), so if this entry is
	// missing, we default to DoubleDelta.
	Encoding prom_chunk.Encoding `json:"encoding"`
	Data     prom_chunk.Chunk    `json:"-"`

	// The encoded version of the chunk, held so we don't need to re-encode it
	encoded []byte
}

// NewChunk creates a new chunk
func NewChunk(userID string, fp model.Fingerprint, metric labels.Labels, c prom_chunk.Chunk, from, through model.Time) Chunk {
	return Chunk{
		Fingerprint: fp,
		UserID:      userID,
		From:        from,
		Through:     through,
		Metric:      metric,
		Encoding:    c.Encoding(),
		Data:        c,
	}
}

// ParseExternalKey is used to construct a partially-populated chunk from the
// key in DynamoDB.  This chunk can then be used to calculate the key needed
// to fetch the Chunk data from Memcache/S3, and then fully populate the chunk
// with decode().
//
// Pre-checksums, the keys written to DynamoDB looked like
// `<fingerprint>:<start time>:<end time>` (aka the ID), and the key for
// memcache and S3 was `<user id>/<fingerprint>:<start time>:<end time>.
// Finger prints and times were written in base-10.
//
// Post-checksums, externals keys become the same across DynamoDB, Memcache
// and S3.  Numbers become hex encoded.  Keys look like:
// `<user id>/<fingerprint>:<start time>:<end time>:<checksum>`.
//
// v12+, fingerprint is now a prefix to support better read and write request parallelization:
// `<user>/<fprint>/<start>:<end>:<checksum>`
func ParseExternalKey(userID, externalKey string) (Chunk, error) {
	if !strings.Contains(externalKey, "/") { // pre-checksum
		return parseLegacyChunkID(userID, externalKey)
	} else if strings.Count(externalKey, "/") == 2 { // v12+
		return parseNewerExternalKey(userID, externalKey)
	} else { // post-checksum
		return parseNewExternalKey(userID, externalKey)
	}
}

// pre-checksum
func parseLegacyChunkID(userID, key string) (Chunk, error) {
	parts := strings.Split(key, ":")
	if len(parts) != 3 {
		return Chunk{}, errInvalidChunkID(key)
	}
	fingerprint, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		return Chunk{}, err
	}
	from, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return Chunk{}, err
	}
	through, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return Chunk{}, err
	}
	return Chunk{
		UserID:      userID,
		Fingerprint: model.Fingerprint(fingerprint),
		From:        model.Time(from),
		Through:     model.Time(through),
	}, nil
}

// post-checksum
func parseNewExternalKey(userID, key string) (Chunk, error) {
	userIdx := strings.Index(key, "/")
	if userIdx == -1 || userIdx+1 >= len(key) {
		return Chunk{}, errInvalidChunkID(key)
	}
	if userID != key[:userIdx] {
		return Chunk{}, errors.WithStack(ErrWrongMetadata)
	}
	hexParts := key[userIdx+1:]
	partsBytes := unsafeGetBytes(hexParts)
	h0, i := readOneHexPart(partsBytes)
	if i == 0 || i+1 >= len(partsBytes) {
		return Chunk{}, errInvalidChunkID(key)
	}
	fingerprint, err := strconv.ParseUint(unsafeGetString(h0), 16, 64)
	if err != nil {
		return Chunk{}, err
	}
	partsBytes = partsBytes[i+1:]
	h1, i := readOneHexPart(partsBytes)
	if i == 0 || i+1 >= len(partsBytes) {
		return Chunk{}, errInvalidChunkID(key)
	}
	from, err := strconv.ParseInt(unsafeGetString(h1), 16, 64)
	if err != nil {
		return Chunk{}, err
	}
	partsBytes = partsBytes[i+1:]
	h2, i := readOneHexPart(partsBytes)
	if i == 0 || i+1 >= len(partsBytes) {
		return Chunk{}, errInvalidChunkID(key)
	}
	through, err := strconv.ParseInt(unsafeGetString(h2), 16, 64)
	if err != nil {
		return Chunk{}, err
	}
	checksum, err := strconv.ParseUint(unsafeGetString(partsBytes[i+1:]), 16, 32)
	if err != nil {
		return Chunk{}, err
	}
	return Chunk{
		UserID:      userID,
		Fingerprint: model.Fingerprint(fingerprint),
		From:        model.Time(from),
		Through:     model.Time(through),
		Checksum:    uint32(checksum),
		ChecksumSet: true,
	}, nil
}

// v12+
func parseNewerExternalKey(userID, key string) (Chunk, error) {
	// Parse user
	userIdx := strings.Index(key, "/")
	if userIdx == -1 || userIdx+1 >= len(key) {
		return Chunk{}, errInvalidChunkID(key)
	}
	if userID != key[:userIdx] {
		return Chunk{}, errors.WithStack(ErrWrongMetadata)
	}
	hexParts := key[userIdx+1:]
	partsBytes := unsafeGetBytes(hexParts)
	// Parse fingerprint
	h, i := readOneHexPart(partsBytes)
	if i == 0 || i+1 >= len(partsBytes) {
		return Chunk{}, errors.Wrap(errInvalidChunkID(key), "decoding fingerprint")
	}
	fingerprint, err := strconv.ParseUint(unsafeGetString(h), 16, 64)
	if err != nil {
		return Chunk{}, errors.Wrap(err, "parsing fingerprint")
	}
	partsBytes = partsBytes[i+1:]
	// Parse start
	h, i = readOneHexPart(partsBytes)
	if i == 0 || i+1 >= len(partsBytes) {
		return Chunk{}, errors.Wrap(errInvalidChunkID(key), "decoding start")
	}
	from, err := strconv.ParseInt(unsafeGetString(h), 16, 64)
	if err != nil {
		return Chunk{}, errors.Wrap(err, "parsing start")
	}
	partsBytes = partsBytes[i+1:]
	// Parse through
	h, i = readOneHexPart(partsBytes)
	if i == 0 || i+1 >= len(partsBytes) {
		return Chunk{}, errors.Wrap(errInvalidChunkID(key), "decoding through")
	}
	through, err := strconv.ParseInt(unsafeGetString(h), 16, 64)
	if err != nil {
		return Chunk{}, errors.Wrap(err, "parsing through")
	}
	partsBytes = partsBytes[i+1:]
	// Parse checksum
	checksum, err := strconv.ParseUint(unsafeGetString(partsBytes), 16, 64)
	if err != nil {
		return Chunk{}, errors.Wrap(err, "parsing checksum")
	}
	return Chunk{
		UserID:      userID,
		Fingerprint: model.Fingerprint(fingerprint),
		From:        model.Time(from),
		Through:     model.Time(through),
		Checksum:    uint32(checksum),
		ChecksumSet: true,
	}, nil
}

func readOneHexPart(hex []byte) (part []byte, i int) {
	for i < len(hex) {
		if hex[i] != ':' && hex[i] != '/' {
			i++
			continue
		}
		return hex[:i], i
	}
	return nil, 0
}

func unsafeGetBytes(s string) []byte {
	var buf []byte
	p := unsafe.Pointer(&buf)
	*(*string)(p) = s
	(*reflect.SliceHeader)(p).Cap = len(s)
	return buf
}

func unsafeGetString(buf []byte) string {
	return *((*string)(unsafe.Pointer(&buf)))
}

var writerPool = sync.Pool{
	New: func() interface{} { return snappy.NewBufferedWriter(nil) },
}

// Encode writes the chunk into a buffer, and calculates the checksum.
func (c *Chunk) Encode() error {
	return c.EncodeTo(nil)
}

// EncodeTo is like Encode but you can provide your own buffer to use.
func (c *Chunk) EncodeTo(buf *bytes.Buffer) error {
	if buf == nil {
		buf = bytes.NewBuffer(nil)
	}
	// Write 4 empty bytes first - we will come back and put the len in here.
	metadataLenBytes := [4]byte{}
	if _, err := buf.Write(metadataLenBytes[:]); err != nil {
		return err
	}

	// Encode chunk metadata into snappy-compressed buffer
	writer := writerPool.Get().(*snappy.Writer)
	defer writerPool.Put(writer)
	writer.Reset(buf)
	json := jsoniter.ConfigFastest
	if err := json.NewEncoder(writer).Encode(c); err != nil {
		return err
	}
	writer.Close()

	// Write the metadata length back at the start of the buffer.
	// (note this length includes the 4 bytes for the length itself)
	metadataLen := buf.Len()
	binary.BigEndian.PutUint32(metadataLenBytes[:], uint32(metadataLen))
	copy(buf.Bytes(), metadataLenBytes[:])

	// Write another 4 empty bytes - we will come back and put the len in here.
	dataLenBytes := [4]byte{}
	if _, err := buf.Write(dataLenBytes[:]); err != nil {
		return err
	}

	// And now the chunk data
	if err := c.Data.Marshal(buf); err != nil {
		return err
	}

	// Now write the data len back into the buf.
	binary.BigEndian.PutUint32(dataLenBytes[:], uint32(buf.Len()-metadataLen-4))
	copy(buf.Bytes()[metadataLen:], dataLenBytes[:])

	// Now work out the checksum
	c.encoded = buf.Bytes()
	c.ChecksumSet = true
	c.Checksum = crc32.Checksum(c.encoded, castagnoliTable)
	return nil
}

// Encoded returns the buffer created by Encoded()
func (c *Chunk) Encoded() ([]byte, error) {
	if c.encoded == nil {
		if err := c.Encode(); err != nil {
			return nil, err
		}
	}
	return c.encoded, nil
}

// DecodeContext holds data that can be re-used between decodes of different chunks
type DecodeContext struct {
	reader *snappy.Reader
}

// NewDecodeContext creates a new, blank, DecodeContext
func NewDecodeContext() *DecodeContext {
	return &DecodeContext{
		reader: snappy.NewReader(nil),
	}
}

// Decode the chunk from the given buffer, and confirm the chunk is the one we
// expected.
func (c *Chunk) Decode(decodeContext *DecodeContext, input []byte) error {
	// First, calculate the checksum of the chunk and confirm it matches
	// what we expected.
	if c.ChecksumSet && c.Checksum != crc32.Checksum(input, castagnoliTable) {
		return errors.WithStack(ErrInvalidChecksum)
	}

	// Now unmarshal the chunk metadata.
	r := bytes.NewReader(input)
	var metadataLen uint32
	if err := binary.Read(r, binary.BigEndian, &metadataLen); err != nil {
		return errors.Wrap(err, "when reading metadata length from chunk")
	}
	var tempMetadata Chunk
	decodeContext.reader.Reset(r)
	json := jsoniter.ConfigFastest
	err := json.NewDecoder(decodeContext.reader).Decode(&tempMetadata)
	if err != nil {
		return errors.Wrap(err, "when decoding chunk metadata")
	}
	metadataRead := len(input) - r.Len()
	// Older versions of Cortex included the initial length word; newer versions do not.
	if !(metadataRead == int(metadataLen) || metadataRead == int(metadataLen)+4) {
		return errors.Wrapf(ErrMetadataLength, "expected %d, got %d", metadataLen, metadataRead)
	}

	// Next, confirm the chunks matches what we expected.  Easiest way to do this
	// is to compare what the decoded data thinks its external ID would be, but
	// we don't write the checksum to s3, so we have to copy the checksum in.
	if c.ChecksumSet {
		tempMetadata.Checksum, tempMetadata.ChecksumSet = c.Checksum, c.ChecksumSet
		if !equalByKey(*c, tempMetadata) {
			return errors.WithStack(ErrWrongMetadata)
		}
	}
	*c = tempMetadata

	// Older chunks always used DoubleDelta and did not write Encoding
	// to JSON, so override if it has the zero value (Delta)
	if c.Encoding == prom_chunk.Delta {
		c.Encoding = prom_chunk.DoubleDelta
	}

	// Finally, unmarshal the actual chunk data.
	c.Data, err = prom_chunk.NewForEncoding(c.Encoding)
	if err != nil {
		return errors.Wrap(err, "when creating new chunk")
	}

	var dataLen uint32
	if err := binary.Read(r, binary.BigEndian, &dataLen); err != nil {
		return errors.Wrap(err, "when reading data length from chunk")
	}

	c.encoded = input
	remainingData := input[len(input)-r.Len():]
	if int(dataLen) != len(remainingData) {
		return ErrDataLength
	}

	return c.Data.UnmarshalFromBuf(remainingData[:int(dataLen)])
}

func equalByKey(a, b Chunk) bool {
	return a.UserID == b.UserID && a.Fingerprint == b.Fingerprint &&
		a.From == b.From && a.Through == b.Through && a.Checksum == b.Checksum
}

// Samples returns all SamplePairs for the chunk.
func (c *Chunk) Samples(from, through model.Time) ([]model.SamplePair, error) {
	it := c.Data.NewIterator(nil)
	interval := metric.Interval{OldestInclusive: from, NewestInclusive: through}
	return prom_chunk.RangeValues(it, interval)
}

// Slice builds a new smaller chunk with data only from given time range (inclusive)
func (c *Chunk) Slice(from, through model.Time) (*Chunk, error) {
	// there should be atleast some overlap between chunk interval and slice interval
	if from > c.Through || through < c.From {
		return nil, ErrSliceOutOfRange
	}

	pc, err := c.Data.Rebound(from, through)
	if err != nil {
		return nil, err
	}

	nc := NewChunk(c.UserID, c.Fingerprint, c.Metric, pc, from, through)
	return &nc, nil
}

func intervalsOverlap(interval1, interval2 model.Interval) bool {
	if interval1.Start > interval2.End || interval2.Start > interval1.End {
		return false
	}

	return true
}
