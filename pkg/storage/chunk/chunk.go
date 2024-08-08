package chunk

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"unsafe"

	errs "errors"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/golang/snappy"
	jsoniter "github.com/json-iterator/go"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/logproto"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

var (
	ErrInvalidChecksum = errs.New("invalid chunk checksum")
	ErrWrongMetadata   = errs.New("wrong chunk metadata")
	ErrMetadataLength  = errs.New("chunk metadata wrong length")
	ErrDataLength      = errs.New("chunk data wrong length")
	ErrSliceOutOfRange = errs.New("chunk can't be sliced out of its data range")
	ErrChunkDecode     = errs.New("error decoding freshly created chunk")
)

var castagnoliTable = crc32.MakeTable(crc32.Castagnoli)

func errInvalidChunkID(s string) error {
	return errors.Errorf("invalid chunk ID %q", s)
}

// Chunk contains encoded timeseries data
type Chunk struct {
	logproto.ChunkRef

	Metric labels.Labels `json:"metric"`

	// We never use Delta encoding (the zero value), so if this entry is
	// missing, we default to DoubleDelta.
	Encoding Encoding `json:"encoding"`
	Data     Data     `json:"-"`

	// The encoded version of the chunk, held so we don't need to re-encode it
	encoded []byte
}

// NewChunk creates a new chunk
func NewChunk(userID string, fp model.Fingerprint, metric labels.Labels, c Data, from, through model.Time) Chunk {
	return Chunk{
		ChunkRef: logproto.ChunkRef{
			Fingerprint: uint64(fp),
			UserID:      userID,
			From:        from,
			Through:     through,
		},
		Metric:   metric,
		Encoding: c.Encoding(),
		Data:     c,
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
	if strings.Count(externalKey, "/") == 2 { // v12+
		return parseNewerExternalKey(userID, externalKey)
	}
	return parseNewExternalKey(userID, externalKey)
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
		ChunkRef: logproto.ChunkRef{
			UserID:      userID,
			Fingerprint: fingerprint,
			From:        model.Time(from),
			Through:     model.Time(through),
			Checksum:    uint32(checksum),
		},
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
		ChunkRef: logproto.ChunkRef{
			UserID:      userID,
			Fingerprint: fingerprint,
			From:        model.Time(from),
			Through:     model.Time(through),
			Checksum:    uint32(checksum),
		},
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
	return c.EncodeTo(nil, util_log.Logger)
}

// EncodeTo is like Encode but you can provide your own buffer to use.
func (c *Chunk) EncodeTo(buf *bytes.Buffer, log log.Logger) error {
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
	c.Checksum = crc32.Checksum(c.encoded, castagnoliTable)

	newCh := Chunk{
		ChunkRef: logproto.ChunkRef{
			UserID:      c.UserID,
			Fingerprint: c.Fingerprint,
			From:        c.From,
			Through:     c.Through,
			Checksum:    c.Checksum,
		},
	}

	if err := newCh.Decode(NewDecodeContext(), c.encoded); err != nil {
		externalKey := fmt.Sprintf(
			"%s/%x/%x:%x:%x",
			c.UserID,
			c.Fingerprint,
			int64(c.From),
			int64(c.Through),
			c.Checksum,
		)
		level.Error(log).
			Log("msg", "error decoding freshly created chunk", "err", err, "key", externalKey)

		return ErrChunkDecode
	}

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
	if c.Checksum != crc32.Checksum(input, castagnoliTable) {
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

	tempMetadata.Checksum = c.Checksum
	if !equalByKey(*c, tempMetadata) {
		return errors.WithStack(ErrWrongMetadata)
	}

	*c = tempMetadata

	// Finally, unmarshal the actual chunk data.
	c.Data, err = NewForEncoding(c.Encoding)
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
