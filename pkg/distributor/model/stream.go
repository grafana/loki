package model

import (
	"sort"

	"github.com/grafana/loki/v3/pkg/logproto"
)

type KeyedStream struct {
	logproto.Stream
	HashKey      uint32                 // for Kafka partitioning
	ParsedLabels logproto.LabelsAdapter // for caching parsed labels
}

func labelsToHash(labels logproto.LabelsAdapter) uint64 {
	lbs := logproto.FromLabelAdaptersToLabels(labels)
	sort.Sort(lbs)
	return lbs.Hash()
}

func labelsToString(labels logproto.LabelsAdapter) string {
	lbs := logproto.FromLabelAdaptersToLabels(labels)
	sort.Sort(lbs)
	return lbs.String()
}

type StreamsBuilder struct {
	internal map[uint64]*KeyedStream
}

func NewStreamsBuilder(size int, streams ...KeyedStream) *StreamsBuilder {
	c := &StreamsBuilder{
		internal: make(map[uint64]*KeyedStream, max(size, len(streams))),
	}
	for i := range streams {
		c.AddStream(streams[i])
	}
	return c
}

func (sb *StreamsBuilder) Len() int {
	return len(sb.internal)
}

func (sb *StreamsBuilder) recomputeHash() {
	for _, v := range sb.internal {
		v.Hash = labelsToHash(v.ParsedLabels)
		v.Labels = labelsToString(v.ParsedLabels)
	}
}

func (sb *StreamsBuilder) AddStream(s KeyedStream) *KeyedStream {
	if s.Hash == 0 {
		s.Hash = labelsToHash(s.ParsedLabels)
	}
	stream, found := sb.internal[s.Hash]
	if !found {
		sb.internal[s.Hash] = &s
	} else {
		stream.Entries = append(stream.Entries, s.Entries...)
	}
	return stream
}

func (sb *StreamsBuilder) RemoveStream(s KeyedStream) bool {
	if s.Hash == 0 {
		s.Hash = labelsToHash(s.ParsedLabels)
	}
	_, found := sb.internal[s.Hash]
	delete(sb.internal, s.Hash)
	return found
}

func (sb *StreamsBuilder) AddStreamWithLabels(labels logproto.LabelsAdapter, s KeyedStream) *KeyedStream {
	hash := labelsToHash(labels)
	stream, found := sb.internal[hash]
	if !found {
		stream = &KeyedStream{
			Stream: logproto.Stream{
				Labels:  labelsToString(labels),
				Entries: s.Entries,
				Hash:    hash,
			},
			ParsedLabels: labels,
		}
		sb.internal[hash] = stream
	} else {
		stream.Entries = append(stream.Entries, s.Entries...)
	}
	return stream
}

func (sb *StreamsBuilder) AddEntry(labels logproto.LabelsAdapter, entry logproto.Entry) *KeyedStream {
	hash := labelsToHash(labels)
	stream, found := sb.internal[hash]
	if !found {
		stream = &KeyedStream{
			Stream: logproto.Stream{
				Labels:  labelsToString(labels),
				Entries: []logproto.Entry{entry},
				Hash:    hash,
			},
			ParsedLabels: labels,
		}
		sb.internal[hash] = stream
	} else {
		stream.Entries = append(stream.Entries, entry)
	}
	return stream
}

// Build() returns a slice of streams from the builder and removes returned
// items from the builder so that it is empty after calling this function.
func (sb *StreamsBuilder) Build() []KeyedStream {
	streams := make([]KeyedStream, 0, len(sb.internal))
	for k, v := range sb.internal {
		// Discard streams with no entries
		if len(v.Entries) > 0 {
			streams = append(streams, *v)
		}
		delete(sb.internal, k)
	}
	return streams
}
