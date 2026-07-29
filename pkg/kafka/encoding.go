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

var (
	encoderPool = sync.Pool{
		New: func() any {
			return &logproto.Stream{}
		},
	}
)

// minEntrySize is the marshalled size of the smallest possible entry in a stream: one byte of
// field tag plus one byte of length for an empty entry message.
const minEntrySize = 2

// sharedStructuredMetadataSetsSize returns the number of bytes the stream's shared structured
// metadata pool takes in a marshalled record. Only used to build error messages.
func sharedStructuredMetadataSetsSize(stream logproto.Stream) int {
	size := 0
	for i := range stream.SharedStructuredMetadataSets {
		l := stream.SharedStructuredMetadataSets[i].Size()
		size += 1 + l + sovPush(uint64(l))
	}
	return size
}

// Encode converts a logproto.Stream into one or more Kafka records.
// It handles splitting large streams into multiple records if necessary.
//
// The encoding process works as follows:
// 1. If the stream size is smaller than maxSize, it's encoded into a single record.
// 2. For larger streams, it splits the entries into multiple batches, each under maxSize.
// 3. The data is wrapped in a Kafka record with the tenant ID as the key.
//
// Every record produced for a stream carries the full SharedStructuredMetadataSets pool of
// that stream, so each record stays self-contained: consumers can resolve the effective
// structured metadata of an entry without having to correlate records. It also keeps the
// SharedResourceRef and SharedScopeRef of every entry valid as is, since they index the same
// pool in every record.
//
// A split record therefore carries pool sets that none of its own entries reference. That is
// an accepted trade: pools hold at most a handful of sets per stream, whereas re-indexing the
// references of each batch against a pruned pool is easy to get subtly wrong and would have to
// be repeated for every batch. The cost is that the pool is duplicated once per record when a
// stream is split, and its marshalled size is charged to every record's budget.
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
	return EncodeWithTopic("", partitionID, tenantID, stream, maxSize)
}

func EncodeWithTopic(topic string, partitionID int32, tenantID string, stream logproto.Stream, maxSize int) ([]*kgo.Record, error) {
	// Stream.Size() accounts for the labels, the hash, the entries (references included) and
	// the stream-level shared structured metadata pool.
	reqSize := stream.Size()

	// Fast path for small requests: the whole stream, pool included, is marshalled into a
	// single record.
	if reqSize <= maxSize {
		rec, err := marshalWriteRequestToRecord(topic, partitionID, tenantID, stream)
		if err != nil {
			return nil, err
		}
		return []*kgo.Record{rec}, nil
	}

	var records []*kgo.Record
	batch := encoderPool.Get().(*logproto.Stream)
	defer func() {
		// Don't let the encoder pool pin the caller's shared structured metadata pool.
		batch.SharedStructuredMetadataSets = nil
		encoderPool.Put(batch)
	}()

	batch.Labels = stream.Labels
	batch.Hash = stream.Hash
	// The whole pool goes into every record, so that the references carried by the entries of
	// each batch keep resolving against it.
	batch.SharedStructuredMetadataSets = stream.SharedStructuredMetadataSets

	if batch.Entries == nil {
		batch.Entries = make([]logproto.Entry, 0, 1024)
	}
	batch.Entries = batch.Entries[:0]
	// Fixed per-record cost: labels, hash and the shared structured metadata pool, which every
	// record pays for since it is duplicated across all of them.
	baseSize := batch.Size()
	currentSize := baseSize

	// Only streams that actually carry a pool are held to the two checks below, which weigh
	// every entry against the full per-record base rather than against maxSize alone.
	//
	// The base of a pool-less stream is just its labels and hash, and the pre-existing behavior
	// there is to weigh only the *first* entry against it, so a bigger entry later in the stream
	// is silently flushed into an oversized record. That is a real bug, but it predates the
	// pool and fixing it here would change what every existing producer emits. Streams without a
	// pool therefore keep exactly the encoding, and the exact error message, they have today;
	// the fix belongs to follow-up work that can weigh it for all traffic at once.
	hasSharedPool := len(stream.SharedStructuredMetadataSets) > 0

	// Splitting can never help if the fixed per-record cost alone does not leave room for the
	// smallest possible entry: every record we would emit would be over the limit. Fail
	// explicitly rather than silently producing oversized records.
	if hasSharedPool && baseSize+minEntrySize > maxSize {
		return nil, fmt.Errorf(
			"per-record base size (%d bytes, of which %d bytes of shared structured metadata pool) leaves no room for a single entry within the maximum allowed size (%d)",
			baseSize, sharedStructuredMetadataSetsSize(stream), maxSize,
		)
	}

	for i, entry := range stream.Entries {
		l := entry.Size()
		// Size of the entry in the stream
		entrySize := 1 + l + sovPush(uint64(l))

		// Check whether a single entry can be encoded at all.
		switch {
		case hasSharedPool:
			// Every record repeats the pool, so an entry only fits if it fits alongside it:
			// checking this for every entry and not just the first one is what keeps a large
			// entry in the middle of the stream from being flushed into an oversized record of
			// its own.
			if baseSize+entrySize > maxSize {
				return nil, fmt.Errorf(
					"single entry size (%d) plus the per-record base size (%d bytes, of which %d bytes of shared structured metadata pool) exceeds maximum allowed size (%d)",
					entrySize, baseSize, sharedStructuredMetadataSetsSize(stream), maxSize,
				)
			}
		case entrySize > maxSize || (i == 0 && currentSize+entrySize > maxSize):
			return nil, fmt.Errorf("single entry size (%d) exceeds maximum allowed size (%d)", entrySize, maxSize)
		}

		if currentSize+entrySize > maxSize {
			// Current stream is full, create a record and start a new stream
			if len(batch.Entries) > 0 {
				rec, err := marshalWriteRequestToRecord(topic, partitionID, tenantID, *batch)
				if err != nil {
					return nil, err
				}
				records = append(records, rec)
			}
			// Reset currentStream
			batch.Entries = batch.Entries[:0]
			currentSize = baseSize
		}
		batch.Entries = append(batch.Entries, entry)
		currentSize += entrySize
	}

	// Handle any remaining entries
	if len(batch.Entries) > 0 {
		rec, err := marshalWriteRequestToRecord(topic, partitionID, tenantID, *batch)
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

// topic can be empty in the case the client injects a default.
func marshalWriteRequestToRecord(topic string, partitionID int32, tenantID string, stream logproto.Stream) (*kgo.Record, error) {
	data, err := stream.Marshal()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal stream: %w", err)
	}

	return &kgo.Record{
		Topic:     topic,
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
	// The stream is reused across calls and Unmarshal appends to repeated fields, so every
	// repeated field is truncated first: otherwise a record would inherit the entries and the
	// shared structured metadata pool of the previous one, and the references of its entries
	// would resolve against a pool holding stale sets. This upholds the buffer reuse contract
	// rather than fixing an observed bug - both production consumers go through
	// DecodeWithoutLabels, which unmarshals into a fresh stream. The references themselves live
	// inside the entries, so truncating those takes care of them.
	d.stream.Entries = d.stream.Entries[:0]
	d.stream.SharedStructuredMetadataSets = d.stream.SharedStructuredMetadataSets[:0]
	if err := d.stream.Unmarshal(data); err != nil {
		return logproto.Stream{}, labels.EmptyLabels(), fmt.Errorf("failed to unmarshal stream: %w", err)
	}

	var ls labels.Labels
	if cachedLabels, ok := d.cache.Get(d.stream.Labels); ok {
		ls = cachedLabels
	} else {
		var err error
		ls, err = syntax.ParseLabels(d.stream.Labels)
		if err != nil {
			return logproto.Stream{}, labels.EmptyLabels(), fmt.Errorf("failed to parse labels: %w", err)
		}
		d.cache.Add(d.stream.Labels, ls)
	}

	return *d.stream, ls, nil
}

// DecodeWithoutLabels converts a Kafka record's byte data back into a logproto.Stream without parsing labels.
func (d *Decoder) DecodeWithoutLabels(data []byte) (logproto.Stream, error) {
	if len(data) == 0 {
		return logproto.Stream{}, errors.New("empty data received")
	}

	stream := logproto.Stream{}
	if err := stream.Unmarshal(data); err != nil {
		return logproto.Stream{}, fmt.Errorf("failed to unmarshal stream: %w", err)
	}

	return stream, nil
}

// sovPush calculates the size of varint-encoded uint64.
// It is used to determine the number of bytes needed to encode a uint64 value
// in Protocol Buffers' variable-length integer format.
func sovPush(x uint64) (n int) {
	return (math_bits.Len64(x|1) + 6) / 7
}
