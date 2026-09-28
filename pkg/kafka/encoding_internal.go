package kafka

import (
	"errors"
	"fmt"

	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/logproto"

	"github.com/grafana/loki/pkg/push"
)

// EncodeInternal encodes a nested stream into one or more Kafka records, each holding as many
// entries as fit within maxSize.
//
// A record keeps the resources and scopes its entries arrived under, so an attribute on either is
// written once per record rather than once per entry. A resource or scope whose entries span
// records repeats its attributes in each, because a record has to stand alone; one contributing no
// entries to a record is left out of it.
func EncodeInternal(partitionID int32, tenantID string, stream logproto.InternalStreamAdapter, maxSize int) ([]*kgo.Record, error) {
	return EncodeInternalWithTopic("", partitionID, tenantID, stream, maxSize)
}

// EncodeInternalWithTopic is EncodeInternal for a caller that names the topic. An empty topic
// leaves it to the client's default.
func EncodeInternalWithTopic(topic string, partitionID int32, tenantID string, stream logproto.InternalStreamAdapter, maxSize int) ([]*kgo.Record, error) {
	// A stream that fits and holds no empty group is written as it stands. Packing it would
	// produce the same bytes, so this only saves the work of walking it.
	if stream.Size() <= maxSize && !hasEmptyGroups(&stream) {
		rec, err := marshalInternalToRecord(topic, partitionID, tenantID, stream)
		if err != nil {
			return nil, err
		}
		return []*kgo.Record{rec}, nil
	}

	var records []*kgo.Record

	// The record being filled.
	batch := logproto.InternalStreamAdapter{Labels: stream.Labels, Hash: stream.Hash}
	headerSize := batch.Size()

	// Its size, kept as the sizes of the messages open in it rather than as one total. Resources
	// and scopes are length-delimited, so an entry lengthens its scope, which can widen the varint
	// holding the scope's length, which lengthens its resource, which can widen its varint too.
	// Keeping each level means every widening is counted where it happens.
	var (
		closedResourcesSize int // resources the record has finished with
		resourceAttrsSize   int // attributes of the resource being filled
		closedScopesSize    int // scopes that resource has finished with
		scopeAttrsSize      int // attributes of the scope being filled
		entriesSize         int // entries in that scope
	)

	// recordSizeWith is what the record would marshal to holding this many bytes of entries in the scope
	// being filled: each level's payload wrapped in the frame its parent holds it under.
	recordSizeWith := func(entriesSize int) int {
		return headerSize + closedResourcesSize +
			submessage(resourceAttrsSize+closedScopesSize+submessage(scopeAttrsSize+entriesSize))
	}

	flush := func() error {
		rec, err := marshalInternalToRecord(topic, partitionID, tenantID, batch)
		if err != nil {
			return err
		}
		records = append(records, rec)

		batch.ResourceLogs = batch.ResourceLogs[:0]
		closedResourcesSize, closedScopesSize, entriesSize = 0, 0, 0
		return nil
	}

	for _, sourceResource := range stream.ResourceLogs {
		resourceAttrsSize = attrsSize(sourceResource.Attrs)

		// The resource and scope being filled, nil until an entry lands in one. That is what keeps
		// an empty one out of a record, and what reopens both in the next record after a flush.
		var resource *logproto.ResourceLogs

		for _, sourceScope := range sourceResource.ScopeLogs {
			scopeAttrsSize = attrsSize(sourceScope.Attrs)

			var scope *logproto.ScopeLogs

			// The record holds a run of this scope's entries, so it can take them as a subslice of
			// the source rather than appending them one by one. A flush begins a new run.
			runStart := 0
			entriesSize = 0

			for entryIdx := range sourceScope.Entries {
				entrySize := submessage(sourceScope.Entries[entryIdx].Size())

				recordSizeWithEntry := recordSizeWith(entriesSize + entrySize)
				if recordSizeWithEntry > maxSize {
					if len(batch.ResourceLogs) > 0 {
						if err := flush(); err != nil {
							return nil, err
						}
						resource, scope, runStart = nil, nil, entryIdx
						recordSizeWithEntry = recordSizeWith(entriesSize + entrySize)
					}
					// A record holding nothing but this entry is still over the limit, so no
					// amount of splitting will place it.
					if recordSizeWithEntry > maxSize {
						return nil, fmt.Errorf("record for stream %s holding a single entry exceeds maximum allowed size %d > %d",
							stream.Labels, recordSizeWithEntry, maxSize)
					}
				}

				if resource == nil {
					batch.ResourceLogs = append(batch.ResourceLogs, logproto.ResourceLogs{Attrs: sourceResource.Attrs})
					resource = &batch.ResourceLogs[len(batch.ResourceLogs)-1]
				}
				if scope == nil {
					resource.ScopeLogs = append(resource.ScopeLogs, logproto.ScopeLogs{Attrs: sourceScope.Attrs})
					scope = &resource.ScopeLogs[len(resource.ScopeLogs)-1]
				}

				scope.Entries = sourceScope.Entries[runStart : entryIdx+1]
				entriesSize += entrySize
			}

			if scope != nil {
				closedScopesSize += submessage(scopeAttrsSize + entriesSize)
			}
		}

		if resource != nil {
			closedResourcesSize += submessage(resourceAttrsSize + closedScopesSize)
			closedScopesSize = 0
		}
	}

	if len(batch.ResourceLogs) > 0 {
		if err := flush(); err != nil {
			return nil, err
		}
	}

	if len(records) == 0 {
		return nil, errors.New("no valid records created")
	}
	return records, nil
}

// topic can be empty in the case the client injects a default.
func marshalInternalToRecord(topic string, partitionID int32, tenantID string, stream logproto.InternalStreamAdapter) (*kgo.Record, error) {
	data, err := stream.Marshal()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal internal stream: %w", err)
	}
	return &kgo.Record{
		Topic:     topic,
		Key:       []byte(tenantID),
		Value:     data,
		Partition: partitionID,
	}, nil
}

// hasEmptyGroups reports whether the stream holds a resource with no scopes, or a scope with no entries.
func hasEmptyGroups(stream *logproto.InternalStreamAdapter) bool {
	for i := range stream.ResourceLogs {
		resource := stream.ResourceLogs[i]
		if len(resource.ScopeLogs) == 0 {
			return true
		}
		for j := range resource.ScopeLogs {
			if len(resource.ScopeLogs[j].Entries) == 0 {
				return true
			}
		}
	}
	return false
}

// attrsSize is what these attributes occupy inside the resource or scope holding them.
func attrsSize(attrs []push.LabelAdapter) int {
	size := 0
	for i := range attrs {
		size += submessage(attrs[i].Size())
	}
	return size
}

// submessage is the bytes a length-delimited field of this payload size occupies inside its
// parent: a one byte tag, the varint holding the size, then the payload itself.
func submessage(payloadSize int) int {
	return 1 + sovPush(uint64(payloadSize)) + payloadSize
}
