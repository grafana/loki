package handler

import (
	"github.com/chaudum/go-kaff/codec"
	"github.com/chaudum/go-kaff/store"
)

// handleOffsetCommit handles OffsetCommit requests (API key 8, versions 2–7).
//
// ── Request format ─────────────────────────────────────────────────────────────
//
//	GroupId(string) | GenerationId(int32, v1+) | MemberId(string, v1+)
//	RetentionTimeMs(int64, v2–v4; removed in v5)
//	GroupInstanceId(nullable string, v7+)
//	Topics(array):
//	  Name(string) | Partitions(array):
//	    PartitionIndex(int32) | CommittedOffset(int64)
//	    CommittedLeaderEpoch(int32, v6+) | CommittedMetadata(nullable string)
//
// ── Response format ────────────────────────────────────────────────────────────
//
//	ThrottleTimeMs(int32, v3+)
//	Topics(array):
//	  Name(string) | Partitions(array): PartitionIndex(int32) | ErrorCode(int16)
func (rt *Router) handleOffsetCommit(hdr RequestHeader, r *codec.Reader, w *codec.Writer) {
	version := hdr.ApiVersion

	// ── Parse request ─────────────────────────────────────────────────────────
	groupID := r.ReadString()
	generationID := int32(-1)
	memberID := ""
	if version >= 1 {
		generationID = r.ReadInt32()
		memberID = r.ReadString()
	}
	if version >= 2 && version <= 4 {
		r.ReadInt64() // RetentionTimeMs — not needed for in-memory store
	}
	if version >= 7 {
		r.ReadNullableString() // GroupInstanceId — ignored
	}

	type partOffset struct {
		partitionID int32
		offset      int64
	}
	type topicOffsets struct {
		name       string
		partitions []partOffset
	}

	numTopics := r.ReadArrayLen()
	topics := make([]topicOffsets, 0, numTopics)
	for i := int32(0); i < numTopics; i++ {
		topicName := r.ReadString()
		numParts := r.ReadArrayLen()
		to := topicOffsets{name: topicName, partitions: make([]partOffset, 0, numParts)}
		for j := int32(0); j < numParts; j++ {
			partID := r.ReadInt32()
			offset := r.ReadInt64()
			if version >= 6 {
				r.ReadInt32() // CommittedLeaderEpoch — ignored
			}
			r.ReadNullableString() // CommittedMetadata — not stored
			to.partitions = append(to.partitions, partOffset{partID, offset})
		}
		topics = append(topics, to)
	}

	if r.Err() != nil {
		return
	}

	// ── Commit offsets ────────────────────────────────────────────────────────
	group := rt.store.GetOrCreateGroup(groupID)

	offsets := make(map[store.TopicPartition]int64)
	for _, t := range topics {
		for _, p := range t.partitions {
			offsets[store.TopicPartition{Topic: t.name, Partition: p.partitionID}] = p.offset
		}
	}
	topLevelErr := group.CommitOffsets(generationID, memberID, offsets)

	// ── Write response ────────────────────────────────────────────────────────
	if version >= 3 {
		w.WriteInt32(0) // ThrottleTimeMs
	}
	w.WriteArrayLen(int32(len(topics)))
	for _, t := range topics {
		w.WriteString(t.name)
		w.WriteArrayLen(int32(len(t.partitions)))
		for _, p := range t.partitions {
			w.WriteInt32(p.partitionID)
			w.WriteInt16(topLevelErr) // same error for all partitions
		}
	}
}
