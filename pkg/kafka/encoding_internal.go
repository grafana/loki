package kafka

import (
	"errors"
	"fmt"
	"iter"

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
	// produce the same bytes, so this only saves the work of rebuilding it entry by entry.
	if stream.Size() <= maxSize && !hasEmptyGroups(&stream) {
		rec, err := marshalInternalToRecord(topic, partitionID, tenantID, stream)
		if err != nil {
			return nil, err
		}
		return []*kgo.Record{rec}, nil
	}

	var records []*kgo.Record
	batch := newInternalBatch(stream.Labels, stream.Hash)

	flush := func() error {
		rec, err := marshalInternalToRecord(topic, partitionID, tenantID, batch.stream)
		if err != nil {
			return err
		}
		records = append(records, rec)
		batch.reset()
		return nil
	}

	for place, entry := range placements(&stream) {
		entryLen := entry.Size()

		if !batch.fits(place, entryLen, maxSize) {
			if batch.entries > 0 {
				if err := flush(); err != nil {
					return nil, err
				}
			}
			// A record holding nothing but this entry is still over the limit, so no amount of
			// splitting will place it.
			if !batch.fits(place, entryLen, maxSize) {
				return nil, fmt.Errorf("record for stream %s holding a single entry exceeds maximum allowed size %d > %d",
					stream.Labels, batch.size+batch.growthFor(place, entryLen), maxSize)
			}
		}
		batch.add(place, entry, entryLen)
	}

	if batch.entries > 0 {
		if err := flush(); err != nil {
			return nil, err
		}
	}

	if len(records) == 0 {
		return nil, errors.New("no valid records created")
	}
	return records, nil
}

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
		resource := &stream.ResourceLogs[i]
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

// placements yields where each entry of a stream sits, in the order the entries appear.
// Resources and scopes are iterated in order, each exhausted before the next.
func placements(stream *logproto.InternalStreamAdapter) iter.Seq2[placement, *push.Entry] {
	return func(yield func(placement, *push.Entry) bool) {
		for resourceIdx := range stream.ResourceLogs {
			resource := &stream.ResourceLogs[resourceIdx]
			resourceAttrsOnly := logproto.ResourceLogs{Attrs: resource.Attrs}
			resourceAttrsLen := resourceAttrsOnly.Size()

			for scopeIdx := range resource.ScopeLogs {
				scope := &resource.ScopeLogs[scopeIdx]
				scopeAttrsOnly := logproto.ScopeLogs{Attrs: scope.Attrs}
				place := placement{
					resourceIdx:      resourceIdx,
					scopeIdx:         scopeIdx,
					resourceAttrs:    resource.Attrs,
					resourceAttrsLen: resourceAttrsLen,
					scopeAttrs:       scope.Attrs,
					scopeAttrsLen:    scopeAttrsOnly.Size(),
				}

				for entryIdx := range scope.Entries {
					if !yield(place, &scope.Entries[entryIdx]) {
						return
					}
				}
			}
		}
	}
}

// placement is where an entry sits in the source stream, and the attributes a record repeats to
// carry it. The indices tell a batch whether it already holds that resource and scope.
type placement struct {
	resourceIdx      int
	scopeIdx         int
	resourceAttrs    []push.LabelAdapter
	resourceAttrsLen int
	scopeAttrs       []push.LabelAdapter
	scopeAttrsLen    int
}

// internalBatch accumulates entries into a stream mirroring the source's nesting, tracking that
// stream's serialised size as it goes.
//
// The size cannot be summed the way the flat encoder sums entry sizes. Resources and scopes are
// length-delimited, so an entry lengthens its scope, which can widen the varint holding the
// scope's length, which lengthens the resource, which can widen its varint too. resourceLen and
// scopeLen hold the lengths being filled so each widening is counted.
//
// A resource or scope is opened only as an entry is placed in it. That is what keeps an empty one
// out of a record: a scope with no entries has no entry to place, so nothing opens it.
//
// Only the resource and scope being filled are tracked, so each has to be exhausted before the
// next begins, which is the order placements yields.
type internalBatch struct {
	stream logproto.InternalStreamAdapter
	size   int

	// The stream's own bytes, the labels and hash, which every record carries and reset returns to.
	headerLen int

	resourceLen int
	scopeLen    int
	entries     int

	// Where the resource and scope being filled sit in the source, so an entry from elsewhere is
	// known to need a new one. Negative until the first entry is placed.
	currResourceIdx int
	currScopeIdx    int
}

func newInternalBatch(labels string, hash uint64) *internalBatch {
	stream := logproto.InternalStreamAdapter{Labels: labels, Hash: hash}
	b := &internalBatch{stream: stream, headerLen: stream.Size()}
	b.reset()
	return b
}

// reset empties the batch for the next record but keeps the slices it has grown, so a stream split
// many ways allocates one set rather than one per record. The record just written owns its bytes,
// so nothing reused here is still read.
func (b *internalBatch) reset() {
	b.stream.ResourceLogs = b.stream.ResourceLogs[:0]
	b.size = b.headerLen
	b.resourceLen = 0
	b.scopeLen = 0
	b.entries = 0
	b.currResourceIdx = -1
	b.currScopeIdx = -1
}

// openResource extends the batch by one resource, reusing one left from an earlier record so its
// scope and entry slices keep their capacity.
func (b *internalBatch) openResource() *logproto.ResourceLogs {
	if len(b.stream.ResourceLogs) == cap(b.stream.ResourceLogs) {
		b.stream.ResourceLogs = append(b.stream.ResourceLogs, logproto.ResourceLogs{})
	} else {
		b.stream.ResourceLogs = b.stream.ResourceLogs[:len(b.stream.ResourceLogs)+1]
	}
	resource := &b.stream.ResourceLogs[len(b.stream.ResourceLogs)-1]
	resource.ScopeLogs = resource.ScopeLogs[:0]
	return resource
}

// openScope does the same one level down.
func openScope(resource *logproto.ResourceLogs) *logproto.ScopeLogs {
	if len(resource.ScopeLogs) == cap(resource.ScopeLogs) {
		resource.ScopeLogs = append(resource.ScopeLogs, logproto.ScopeLogs{})
	} else {
		resource.ScopeLogs = resource.ScopeLogs[:len(resource.ScopeLogs)+1]
	}
	scope := &resource.ScopeLogs[len(resource.ScopeLogs)-1]
	scope.Entries = scope.Entries[:0]
	return scope
}

// growthFor is how many bytes the batch grows by taking this entry.
func (b *internalBatch) growthFor(place placement, entryLen int) int {
	growth, _, _ := b.plan(place, entryLen)
	return growth
}

func (b *internalBatch) fits(place placement, entryLen, maxSize int) bool {
	return b.size+b.growthFor(place, entryLen) <= maxSize
}

// submessage is the bytes a length-delimited field of this payload length occupies inside its
// parent: a one byte tag, the varint holding the length, then the payload itself.
func submessage(payloadLen int) int {
	return 1 + sovPush(uint64(payloadLen)) + payloadLen
}

// needsNewResource reports whether an entry from a placement belongs to a new resource.
func (b *internalBatch) needsNewResource(place placement) bool {
	return place.resourceIdx != b.currResourceIdx
}

// needsNewScope reports whether an entry from a placement belongs to a new scope.
// A new resource always requires a new scope.
func (b *internalBatch) needsNewScope(place placement) bool {
	return b.needsNewResource(place) || place.scopeIdx != b.currScopeIdx
}

// plan is the growth of taking this entry, and the lengths its scope and resource end up with.
//
// A new entry can cause cascading size changes on upper levels due to the nested nature of data.
// The size changes are tracked as differential of what the size would be after adding the entry minus what it was before.
func (b *internalBatch) plan(place placement, entryLen int) (growth, scopeLen, resourceLen int) {
	scopeLen, scopeWas := place.scopeAttrsLen, 0
	if !b.needsNewScope(place) {
		scopeLen, scopeWas = b.scopeLen, submessage(b.scopeLen)
	}
	scopeLen += submessage(entryLen)

	// Its resource, and what it cost the stream before. The entry costs the resource the change
	// in the scope's cost, when the scope's varint grew wider.
	resourceLen, resourceWas := place.resourceAttrsLen, 0
	if !b.needsNewResource(place) {
		resourceLen, resourceWas = b.resourceLen, submessage(b.resourceLen)
	}
	resourceLen += submessage(scopeLen) - scopeWas

	// The stream grows the same way one level further up if the resource's
	// own varint grew wider.
	return submessage(resourceLen) - resourceWas, scopeLen, resourceLen
}

func (b *internalBatch) add(place placement, entry *push.Entry, entryLen int) {
	growth, scopeLen, resourceLen := b.plan(place, entryLen)

	switch {
	case b.needsNewResource(place):
		resource := b.openResource()
		resource.Attrs = place.resourceAttrs
		scope := openScope(resource)
		scope.Attrs = place.scopeAttrs
		scope.Entries = append(scope.Entries, *entry)

	case b.needsNewScope(place):
		resource := &b.stream.ResourceLogs[len(b.stream.ResourceLogs)-1]
		scope := openScope(resource)
		scope.Attrs = place.scopeAttrs
		scope.Entries = append(scope.Entries, *entry)

	default:
		resource := &b.stream.ResourceLogs[len(b.stream.ResourceLogs)-1]
		scope := &resource.ScopeLogs[len(resource.ScopeLogs)-1]
		scope.Entries = append(scope.Entries, *entry)
	}

	b.size += growth
	b.scopeLen = scopeLen
	b.resourceLen = resourceLen
	b.currResourceIdx = place.resourceIdx
	b.currScopeIdx = place.scopeIdx
	b.entries++
}
