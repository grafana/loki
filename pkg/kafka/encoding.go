// Package kafka provides encoding and decoding functionality for Loki's Kafka integration.
package kafka

import (
	"bytes"
	"errors"
	"fmt"
	math_bits "math/bits"

	"github.com/golang/snappy"
	"github.com/twmb/franz-go/pkg/kgo"

	lru "github.com/hashicorp/golang-lru"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

const defaultMaxSize = 10 << 20 // 10 MB

// Encoder is responsible for encoding logproto.Stream data into Kafka records.
// It handles compression and splitting of large streams into multiple records.
type Encoder struct {
	writer  *snappy.Writer
	buff    []byte
	batch   logproto.Stream
	maxSize int
}

// NewEncoderWithMaxSize creates a new Encoder with a specified maximum record size.
// If maxSize is <= 0, it defaults to 10MB.
func NewEncoderWithMaxSize(maxSize int) *Encoder {
	if maxSize <= 0 {
		maxSize = defaultMaxSize
	}
	return &Encoder{
		writer:  snappy.NewBufferedWriter(nil),
		buff:    make([]byte, 0, maxSize),
		maxSize: maxSize,
	}
}

// NewEncoder creates a new Encoder with the default maximum record size of 10MB.
func NewEncoder() *Encoder {
	return NewEncoderWithMaxSize(defaultMaxSize)
}

// Encode converts a logproto.Stream into one or more Kafka records.
// It handles splitting large streams into multiple records if necessary.
//
// The encoding process works as follows:
// 1. If the stream size is smaller than maxSize, it's encoded into a single record.
// 2. For larger streams, it splits the entries into multiple batches, each under maxSize.
// 3. Each batch is compressed using Snappy compression.
// 4. The compressed data is wrapped in a Kafka record with the tenant ID as the key.
//
// The format of each record is:
// - Key: Tenant ID (used for routing, not for partitioning)
// - Value: Snappy-compressed protobuf serialized logproto.Stream
// - Partition: As specified in the partitionID parameter
func (e *Encoder) Encode(partitionID int32, tenantID string, stream logproto.Stream) ([]*kgo.Record, error) {
	reqSize := stream.Size()

	// Fast path for small requests
	if reqSize <= e.maxSize {
		rec, err := e.marshalWriteRequestToRecord(partitionID, tenantID, stream, reqSize)
		if err != nil {
			return nil, err
		}
		return []*kgo.Record{rec}, nil
	}

	var records []*kgo.Record
	e.batch.Labels = stream.Labels
	e.batch.Hash = stream.Hash

	if e.batch.Entries == nil {
		e.batch.Entries = make([]logproto.Entry, 0, 1024)
	}
	e.batch.Entries = e.batch.Entries[:0]
	labelsSize := e.batch.Size()
	currentSize := labelsSize

	for i, entry := range stream.Entries {
		l := entry.Size()
		// Size of the entry in the stream
		entrySize := 1 + l + sovPush(uint64(l))

		// Check if a single entry is too big
		if entrySize > e.maxSize || (i == 0 && currentSize+entrySize > e.maxSize) {
			return nil, fmt.Errorf("single entry size (%d) exceeds maximum allowed size (%d)", entrySize, e.maxSize)
		}

		if currentSize+entrySize > e.maxSize {
			// Current stream is full, create a record and start a new stream
			if len(e.batch.Entries) > 0 {
				rec, err := e.marshalWriteRequestToRecord(partitionID, tenantID, e.batch, currentSize)
				if err != nil {
					return nil, err
				}
				records = append(records, rec)
			}
			// Reset currentStream
			e.batch.Entries = e.batch.Entries[:0]
			currentSize = labelsSize
		}
		fmt.Println("size", entrySize, "currentSize", currentSize, "maxSize", e.maxSize)
		e.batch.Entries = append(e.batch.Entries, entry)
		currentSize += entrySize
	}

	// Handle any remaining entries
	if len(e.batch.Entries) > 0 {
		rec, err := e.marshalWriteRequestToRecord(partitionID, tenantID, e.batch, currentSize)
		if err != nil {
			return nil, err
		}
		records = append(records, rec)
	}

	if len(records) == 0 {
		return nil, errors.New("no valid records created")
	}

	return records, nil
}

func (e *Encoder) marshalWriteRequestToRecord(partitionID int32, tenantID string, stream logproto.Stream, size int) (*kgo.Record, error) {
	// todo(cyriltovena): We could consider a better format to store the data avoiding all the allocations.
	// Using Apache Arrow could be a good option.
	e.buff = e.buff[:size]
	n, err := stream.MarshalToSizedBuffer(e.buff)
	if err != nil {
		return nil, fmt.Errorf("failed to serialise write request: %w", err)
	}
	e.buff = e.buff[:n]
	buffer := bytes.NewBuffer(make([]byte, 0, n/2))
	e.writer.Reset(buffer)
	if _, err := e.writer.Write(e.buff); err != nil {
		return nil, fmt.Errorf("failed to write data to buffer: %w", err)
	}
	if err := e.writer.Flush(); err != nil {
		return nil, fmt.Errorf("failed to flush writer: %w", err)
	}

	return &kgo.Record{
		// We don't partition based on the key, so the value here doesn't make any difference.
		Key:       []byte(tenantID),
		Value:     buffer.Bytes(),
		Partition: partitionID,
	}, nil
}

// Decoder is responsible for decoding Kafka record data back into logproto.Stream format.
// It handles decompression and caches parsed labels for efficiency.
type Decoder struct {
	stream      *logproto.Stream
	cache       *lru.Cache
	snappReader *snappy.Reader
	reader      *bytes.Reader

	buff *bytes.Buffer
}

// NewDecoder creates a new Decoder with default settings.
func NewDecoder() (*Decoder, error) {
	return NewDecoderWithExpectedSize(defaultMaxSize)
}

// NewDecoderWithExpectedSize creates a new Decoder with a specified expected record size.
func NewDecoderWithExpectedSize(expectedSize int) (*Decoder, error) {
	cache, err := lru.New(5000) // Set LRU size to 5000, adjust as needed
	if err != nil {
		return nil, fmt.Errorf("failed to create LRU cache: %w", err)
	}
	return &Decoder{
		stream:      &logproto.Stream{},
		cache:       cache,
		snappReader: snappy.NewReader(nil),
		reader:      bytes.NewReader(nil),
		buff:        bytes.NewBuffer(make([]byte, 0, expectedSize)),
	}, nil
}

// Decode converts a Kafka record's byte data back into a logproto.Stream and labels.Labels.
// The decoding process works as follows:
// 1. Decompress the data using Snappy decompression.
// 2. Unmarshal the decompressed data into a logproto.Stream.
// 3. Parse and cache the labels for efficiency in future decodes.
//
// Returns the decoded logproto.Stream, parsed labels, and any error encountered.
func (d *Decoder) Decode(data []byte) (logproto.Stream, labels.Labels, error) {
	d.stream.Entries = d.stream.Entries[:0]
	d.reader.Reset(data)
	d.snappReader.Reset(d.reader)
	d.buff.Reset()
	if _, err := d.buff.ReadFrom(d.snappReader); err != nil {
		return logproto.Stream{}, nil, fmt.Errorf("failed to read from snappy reader: %w", err)
	}
	if err := d.stream.Unmarshal(d.buff.Bytes()); err != nil {
		return logproto.Stream{}, nil, fmt.Errorf("failed to unmarshal stream: %w", err)
	}

	var ls labels.Labels
	if cachedLabels, ok := d.cache.Get(d.stream.Labels); ok {
		ls = cachedLabels.(labels.Labels)
	} else {
		var err error
		ls, err = syntax.ParseLabels(d.stream.Labels)
		if err != nil {
			return logproto.Stream{}, nil, fmt.Errorf("failed to parse labels: %w", err)
		}
		d.cache.Add(d.stream.Labels, ls)
	}

	return *d.stream, ls, nil
}

// sovPush calculates the size of varint-encoded uint64.
// It is used to determine the number of bytes needed to encode a uint64 value
// in Protocol Buffers' variable-length integer format.
//
// The function works as follows:
// 1. It uses math_bits.Len64 to find the position of the most significant bit.
// 2. It adds 1 to ensure non-zero values are handled correctly.
// 3. It adds 6 and divides by 7 to calculate the number of 7-bit groups needed.
//
// This is an optimization for Protocol Buffers encoding, avoiding the need to
// actually encode the number to determine its encoded size.
func sovPush(x uint64) (n int) {
	return (math_bits.Len64(x|1) + 6) / 7
}
