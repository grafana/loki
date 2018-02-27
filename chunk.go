package chunk

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"strconv"
	"strings"
	"sync"

	"github.com/golang/snappy"
	ot "github.com/opentracing/opentracing-go"
	otlog "github.com/opentracing/opentracing-go/log"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	prom_chunk "github.com/weaveworks/cortex/pkg/prom1/storage/local/chunk"
	"github.com/weaveworks/cortex/pkg/prom1/storage/metric"

	errs "github.com/weaveworks/common/errors"
	"github.com/weaveworks/cortex/pkg/util"
)

// Errors that decode can return
const (
	ErrInvalidChunkID  = errs.Error("invalid chunk ID")
	ErrInvalidChecksum = errs.Error("invalid chunk checksum")
	ErrWrongMetadata   = errs.Error("wrong chunk metadata")
)

var castagnoliTable = crc32.MakeTable(crc32.Castagnoli)

// Chunk contains encoded timeseries data
type Chunk struct {
	// These two fields will be missing from older chunks (as will the hash).
	// On fetch we will initialise these fields from the DynamoDB key.
	Fingerprint model.Fingerprint `json:"fingerprint"`
	UserID      string            `json:"userID"`

	// These fields will be in all chunks, including old ones.
	From    model.Time   `json:"from"`
	Through model.Time   `json:"through"`
	Metric  model.Metric `json:"metric"`

	// The hash is not written to the external storage either.  We use
	// crc32, Castagnoli table.  See http://www.evanjones.ca/crc32c.html.
	// For old chunks, ChecksumSet will be false.
	ChecksumSet bool   `json:"-"`
	Checksum    uint32 `json:"-"`

	// We never use Delta encoding (the zero value), so if this entry is
	// missing, we default to DoubleDelta.
	Encoding prom_chunk.Encoding `json:"encoding"`
	Data     prom_chunk.Chunk    `json:"-"`

	// This flag is used for very old chunks, where the metadata is read out
	// of the index.
	metadataInIndex bool
}

// NewChunk creates a new chunk
func NewChunk(userID string, fp model.Fingerprint, metric model.Metric, c prom_chunk.Chunk, from, through model.Time) Chunk {
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
func ParseExternalKey(userID, externalKey string) (Chunk, error) {
	if !strings.Contains(externalKey, "/") {
		return parseLegacyChunkID(userID, externalKey)
	}
	chunk, err := parseNewExternalKey(externalKey)
	if err != nil {
		return Chunk{}, err
	}
	if chunk.UserID != userID {
		return Chunk{}, errors.WithStack(ErrWrongMetadata)
	}
	return chunk, nil
}

func parseLegacyChunkID(userID, key string) (Chunk, error) {
	parts := strings.Split(key, ":")
	if len(parts) != 3 {
		return Chunk{}, errors.WithStack(ErrInvalidChunkID)
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

func parseNewExternalKey(key string) (Chunk, error) {
	parts := strings.Split(key, "/")
	if len(parts) != 2 {
		return Chunk{}, errors.WithStack(ErrInvalidChunkID)
	}
	userID := parts[0]
	hexParts := strings.Split(parts[1], ":")
	if len(hexParts) != 4 {
		return Chunk{}, errors.WithStack(ErrInvalidChunkID)
	}
	fingerprint, err := strconv.ParseUint(hexParts[0], 16, 64)
	if err != nil {
		return Chunk{}, err
	}
	from, err := strconv.ParseInt(hexParts[1], 16, 64)
	if err != nil {
		return Chunk{}, err
	}
	through, err := strconv.ParseInt(hexParts[2], 16, 64)
	if err != nil {
		return Chunk{}, err
	}
	checksum, err := strconv.ParseUint(hexParts[3], 16, 32)
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

// ExternalKey returns the key you can use to fetch this chunk from external
// storage. For newer chunks, this key includes a checksum.
func (c *Chunk) ExternalKey() string {
	// Some chunks have a checksum stored in dynamodb, some do not.  We must
	// generate keys appropriately.
	if c.ChecksumSet {
		// This is the inverse of parseNewExternalKey.
		return fmt.Sprintf("%s/%x:%x:%x:%x", c.UserID, uint64(c.Fingerprint), int64(c.From), int64(c.Through), c.Checksum)
	}
	// This is the inverse of parseLegacyExternalKey, with "<user id>/" prepended.
	// Legacy chunks had the user ID prefix on s3/memcache, but not in DynamoDB.
	// See comment on parseExternalKey.
	return fmt.Sprintf("%s/%d:%d:%d", c.UserID, uint64(c.Fingerprint), int64(c.From), int64(c.Through))
}

var writerPool = sync.Pool{
	New: func() interface{} { return snappy.NewWriter(nil) },
}

// Encode writes the chunk out to a big write buffer, then calculates the checksum.
func (c *Chunk) Encode() ([]byte, error) {
	var buf bytes.Buffer

	// Write 4 empty bytes first - we will come back and put the len in here.
	metadataLenBytes := [4]byte{}
	if _, err := buf.Write(metadataLenBytes[:]); err != nil {
		return nil, err
	}

	// Encode chunk metadata into snappy-compressed buffer
	writer := writerPool.Get().(*snappy.Writer)
	defer writerPool.Put(writer)
	writer.Reset(&buf)
	if err := json.NewEncoder(writer).Encode(c); err != nil {
		return nil, err
	}

	// Write the metadata length back at the start of the buffer.
	binary.BigEndian.PutUint32(metadataLenBytes[:], uint32(buf.Len()))
	copy(buf.Bytes(), metadataLenBytes[:])

	// Write the data length
	dataLenBytes := [4]byte{}
	binary.BigEndian.PutUint32(dataLenBytes[:], uint32(prom_chunk.ChunkLen))
	if _, err := buf.Write(dataLenBytes[:]); err != nil {
		return nil, err
	}

	// And now the chunk data
	if err := c.Data.Marshal(&buf); err != nil {
		return nil, err
	}

	// Now work out the checksum
	output := buf.Bytes()
	c.ChecksumSet = true
	c.Checksum = crc32.Checksum(output, castagnoliTable)
	return output, nil
}

// DecodeContext holds data that can be re-used between decodes of different chunks
type DecodeContext struct {
	reader  *snappy.Reader
	metrics map[model.Fingerprint]model.Metric
}

// NewDecodeContext creates a new, blank, DecodeContext
func NewDecodeContext() *DecodeContext {
	return &DecodeContext{
		reader:  snappy.NewReader(nil),
		metrics: make(map[model.Fingerprint]model.Metric),
	}
}

// If we have decoded a chunk with the same fingerprint before, re-use its Metric, otherwise parse it
func (dc *DecodeContext) metric(fingerprint model.Fingerprint, buf []byte) (model.Metric, error) {
	metric, found := dc.metrics[fingerprint]
	if !found {
		err := json.NewDecoder(bytes.NewReader(buf)).Decode(&metric)
		if err != nil {
			return nil, errors.Wrap(err, "while parsing chunk metric")
		}
		dc.metrics[fingerprint] = metric
	}
	return metric, nil
}

// Decode the chunk from the given buffer, and confirm the chunk is the one we
// expected.
func (c *Chunk) Decode(decodeContext *DecodeContext, input []byte) error {
	// Legacy chunks were written with metadata in the index.
	if c.metadataInIndex {
		var err error
		c.Data, err = prom_chunk.NewForEncoding(prom_chunk.DoubleDelta)
		if err != nil {
			return err
		}
		return c.Data.UnmarshalFromBuf(input)
	}

	// First, calculate the checksum of the chunk and confirm it matches
	// what we expected.
	if c.ChecksumSet && c.Checksum != crc32.Checksum(input, castagnoliTable) {
		return errors.WithStack(ErrInvalidChecksum)
	}

	// Now unmarshal the chunk metadata.
	r := bytes.NewReader(input)
	var metadataLen uint32
	if err := binary.Read(r, binary.BigEndian, &metadataLen); err != nil {
		return err
	}
	var tempMetadata struct {
		Chunk
		RawMetric json.RawMessage `json:"metric"` // Override to defer parsing
	}
	decodeContext.reader.Reset(&io.LimitedReader{
		N: int64(metadataLen),
		R: r,
	})
	err := json.NewDecoder(decodeContext.reader).Decode(&tempMetadata)
	if err != nil {
		return err
	}

	// Next, confirm the chunks matches what we expected.  Easiest way to do this
	// is to compare what the decoded data thinks its external ID would be, but
	// we don't write the checksum to s3, so we have to copy the checksum in.
	if c.ChecksumSet {
		tempMetadata.Checksum, tempMetadata.ChecksumSet = c.Checksum, c.ChecksumSet
		if !equalByKey(*c, tempMetadata.Chunk) {
			return errors.WithStack(ErrWrongMetadata)
		}
	}
	*c = tempMetadata.Chunk
	c.Metric, err = decodeContext.metric(tempMetadata.Fingerprint, tempMetadata.RawMetric)
	if err != nil {
		return err
	}

	// Older chunks always used DoubleDelta and did not write Encoding
	// to JSON, so override if it has the zero value (Delta)
	if c.Encoding == prom_chunk.Delta {
		c.Encoding = prom_chunk.DoubleDelta
	}

	// Finally, unmarshal the actual chunk data.
	c.Data, err = prom_chunk.NewForEncoding(c.Encoding)
	if err != nil {
		return err
	}

	var dataLen uint32
	if err := binary.Read(r, binary.BigEndian, &dataLen); err != nil {
		return err
	}

	return c.Data.Unmarshal(&io.LimitedReader{
		N: int64(dataLen),
		R: r,
	})
}

func chunksToMatrix(ctx context.Context, chunks []Chunk, from, through model.Time) (model.Matrix, error) {
	sp, ctx := ot.StartSpanFromContext(ctx, "chunksToMatrix")
	defer sp.Finish()
	sp.LogFields(otlog.Int("chunks", len(chunks)))

	// Group chunks by series, sort and dedupe samples.
	sampleStreams := map[model.Fingerprint]*model.SampleStream{}
	for _, c := range chunks {
		ss, ok := sampleStreams[c.Fingerprint]
		if !ok {
			ss = &model.SampleStream{
				Metric: c.Metric,
			}
			sampleStreams[c.Fingerprint] = ss
		}

		samples, err := c.Samples(from, through)
		if err != nil {
			return nil, err
		}

		ss.Values = util.MergeSampleSets(ss.Values, samples)
	}
	sp.LogFields(otlog.Int("sample streams", len(sampleStreams)))

	matrix := make(model.Matrix, 0, len(sampleStreams))
	for _, ss := range sampleStreams {
		matrix = append(matrix, &model.SampleStream{
			Metric: ss.Metric,
			Values: ss.Values,
		})
	}

	return matrix, nil
}

// Samples returns all SamplePairs for the chunk.
func (c *Chunk) Samples(from, through model.Time) ([]model.SamplePair, error) {
	it := c.Data.NewIterator()
	interval := metric.Interval{OldestInclusive: from, NewestInclusive: through}
	return prom_chunk.RangeValues(it, interval)
}
