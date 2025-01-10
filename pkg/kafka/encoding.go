// Package kafka provides encoding and decoding functionality for Loki's Kafka integration.
package kafka

import (
	"errors"
	"fmt"
	math_bits "math/bits"
	"sync"

	"github.com/twmb/franz-go/pkg/kgo"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

const (
	metadataTopicSuffix = ".metadata"
)

var (
	encoderPool = sync.Pool{
		New: func() any {
			return &logproto.Stream{}
		},
	}

	metadataPool = sync.Pool{
		New: func() any {
			return &logproto.StreamMetadata{}
		},
	}
)

// Encode converts a logproto.Stream into one or more Kafka records.
// It handles splitting large streams into multiple records if necessary.
//
// The encoding process works as follows:
// 1. If the stream size is smaller than maxSize, it's encoded into a single record.
// 2. For larger streams, it splits the entries into multiple batches, each under maxSize.
// 3. The data is wrapped in a Kafka record with the tenant ID as the key.
//
// The format of each record is:
// - Key: Tenant ID (used for routing, not for partitioning)
// - Value: Protobuf serialized logproto.Stream
// - Partition: As specified in the partitionID parameter
//
// Parameters:
// - partitionID: The Kafka partition ID for the record
// - tenantID: The tenant ID for the stream
// - stream: The logproto.Stream to be encoded
// - maxSize: The maximum size of each Kafka record
func Encode(partitionID int32, tenantID string, stream logproto.Stream, maxSize int) ([]*kgo.Record, error) {
	reqSize := stream.Size()

	// Fast path for small requests
	if reqSize <= maxSize {
		rec, err := marshalWriteRequestToRecord(partitionID, tenantID, stream)
		if err != nil {
			return nil, err
		}
		return []*kgo.Record{rec}, nil
	}

	var records []*kgo.Record
	batch := encoderPool.Get().(*logproto.Stream)
	defer encoderPool.Put(batch)

	batch.Labels = stream.Labels
	batch.Hash = stream.Hash

	if batch.Entries == nil {
		batch.Entries = make([]logproto.Entry, 0, 1024)
	}
	batch.Entries = batch.Entries[:0]
	labelsSize := batch.Size()
	currentSize := labelsSize

	for i, entry := range stream.Entries {
		l := entry.Size()
		// Size of the entry in the stream
		entrySize := 1 + l + sovPush(uint64(l))

		// Check if a single entry is too big
		if entrySize > maxSize || (i == 0 && currentSize+entrySize > maxSize) {
			return nil, fmt.Errorf("single entry size (%d) exceeds maximum allowed size (%d)", entrySize, maxSize)
		}

		if currentSize+entrySize > maxSize {
			// Current stream is full, create a record and start a new stream
			if len(batch.Entries) > 0 {
				rec, err := marshalWriteRequestToRecord(partitionID, tenantID, *batch)
				if err != nil {
					return nil, err
				}
				records = append(records, rec)
			}
			// Reset currentStream
			batch.Entries = batch.Entries[:0]
			currentSize = labelsSize
		}
		batch.Entries = append(batch.Entries, entry)
		currentSize += entrySize
	}

	// Handle any remaining entries
	if len(batch.Entries) > 0 {
		rec, err := marshalWriteRequestToRecord(partitionID, tenantID, *batch)
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

func marshalWriteRequestToRecord(partitionID int32, tenantID string, stream logproto.Stream) (*kgo.Record, error) {
	data, err := stream.Marshal()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal stream: %w", err)
	}

	return &kgo.Record{
		Key:       []byte(tenantID),
		Value:     data,
		Partition: partitionID,
	}, nil
}

// Decoder is responsible for decoding Kafka record data back into logproto.Stream format.
// It caches parsed labels for efficiency.
type Decoder struct {
	stream *logproto.Stream
	cache  *lru.Cache[string, labels.Labels]
}

func NewDecoder() (*Decoder, error) {
	cache, err := lru.New[string, labels.Labels](5000)
	if err != nil {
		return nil, fmt.Errorf("failed to create LRU cache: %w", err)
	}
	return &Decoder{
		stream: &logproto.Stream{},
		cache:  cache,
	}, nil
}

// Decode converts a Kafka record's byte data back into a logproto.Stream and labels.Labels.
// The decoding process works as follows:
// 1. Unmarshal the data into a logproto.Stream.
// 2. Parse and cache the labels for efficiency in future decodes.
//
// Returns the decoded logproto.Stream, parsed labels, and any error encountered.
func (d *Decoder) Decode(data []byte) (logproto.Stream, labels.Labels, error) {
	d.stream.Entries = d.stream.Entries[:0]
	if err := d.stream.Unmarshal(data); err != nil {
		return logproto.Stream{}, nil, fmt.Errorf("failed to unmarshal stream: %w", err)
	}

	var ls labels.Labels
	if cachedLabels, ok := d.cache.Get(d.stream.Labels); ok {
		ls = cachedLabels
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

// DecodeWithoutLabels converts a Kafka record's byte data back into a logproto.Stream without parsing labels.
func (d *Decoder) DecodeWithoutLabels(data []byte) (logproto.Stream, error) {
	d.stream.Entries = d.stream.Entries[:0]
	if err := d.stream.Unmarshal(data); err != nil {
		return logproto.Stream{}, fmt.Errorf("failed to unmarshal stream: %w", err)
	}
	return *d.stream, nil
}

// sovPush calculates the size of varint-encoded uint64.
// It is used to determine the number of bytes needed to encode a uint64 value
// in Protocol Buffers' variable-length integer format.
func sovPush(x uint64) (n int) {
	return (math_bits.Len64(x|1) + 6) / 7
}

// EncodeStreamMetadata encodes the stream metadata into a Kafka record
// using the tenantID as the key and partition as the target partition
func EncodeStreamMetadata(partition int32, topic string, tenantID string, stream logproto.Stream) *kgo.Record {
	// Validate stream
	if stream.Labels == "" || stream.Hash == 0 {
		return nil
	}

	// Get metadata from pool
	metadata := metadataPool.Get().(*logproto.StreamMetadata)
	defer metadataPool.Put(metadata)

	// Transform stream into metadata
	metadata.StreamHash = stream.Hash

	// Encode the metadata into a byte slice
	value, err := metadata.Marshal()
	if err != nil {
		// Since we're in a function that returns a *kgo.Record, we can't return an error.
		// The best we can do is return nil which will fail later.
		return nil
	}

	return &kgo.Record{
		Key:       []byte(tenantID),
		Value:     value,
		Partition: partition,
		Topic:     MetadataTopicFor(topic),
	}
}

// DecodeStreamMetadata decodes a Kafka record into a StreamMetadata.
// It returns the decoded metadata and any error encountered.
func DecodeStreamMetadata(record *kgo.Record) (*logproto.StreamMetadata, error) {
	if record == nil {
		return nil, errors.New("nil record")
	}

	if record.Value == nil {
		return nil, errors.New("nil record value")
	}

	metadata := metadataPool.Get().(*logproto.StreamMetadata)
	if err := metadata.Unmarshal(record.Value); err != nil {
		metadataPool.Put(metadata)
		return nil, fmt.Errorf("failed to unmarshal stream metadata: %w", err)
	}

	return metadata, nil
}

// MetadataTopicFor returns the metadata topic name for the given topic.
func MetadataTopicFor(topic string) string {
	return topic + metadataTopicSuffix
}
