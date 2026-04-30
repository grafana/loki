package handler

import (
	"github.com/chaudum/go-kaff/codec"
	"github.com/chaudum/go-kaff/store"
)

// handleOffsetFetch handles OffsetFetch requests (API key 9, versions 1–5).
//
// A null topics array (v2+) means "fetch all committed offsets for the group".
//
// ── Request format ─────────────────────────────────────────────────────────────
//
//	GroupId(string)
//	Topics(array, nullable in v2+):
//	  Name(string) | PartitionIndexes(array of int32)
//
// ── Response format ────────────────────────────────────────────────────────────
//
//	ThrottleTimeMs(int32, v3+)
//	Topics(array):
//	  Name(string) | Partitions(array):
//	    PartitionIndex(int32) | CommittedOffset(int64)
//	    CommittedLeaderEpoch(int32, v5+) | Metadata(nullable string) | ErrorCode(int16)
//	ErrorCode(int16, v2+)
func (rt *Router) handleOffsetFetch(hdr RequestHeader, r *codec.Reader, w *codec.Writer) {
	version := hdr.ApiVersion

	// ── Parse request ─────────────────────────────────────────────────────────
	groupID := r.ReadString()

	// v2+ may send a null array meaning "all partitions".
	type reqPartition struct {
		topic     string
		partition int32
	}
	var requested []reqPartition
	fetchAll := false

	numTopics := r.ReadArrayLen()
	if numTopics < 0 {
		// null array → fetch all (v2+)
		fetchAll = true
	} else {
		for i := int32(0); i < numTopics; i++ {
			topicName := r.ReadString()
			numParts := r.ReadArrayLen()
			for j := int32(0); j < numParts; j++ {
				requested = append(requested, reqPartition{topicName, r.ReadInt32()})
			}
		}
	}

	if r.Err() != nil {
		return
	}

	// ── Fetch offsets ─────────────────────────────────────────────────────────
	group := rt.store.GetOrCreateGroup(groupID)

	var tps []store.TopicPartition
	if !fetchAll {
		tps = make([]store.TopicPartition, len(requested))
		for i, rp := range requested {
			tps[i] = store.TopicPartition{Topic: rp.topic, Partition: rp.partition}
		}
	}
	committed := group.FetchOffsets(tps) // nil → all

	// ── Build response ────────────────────────────────────────────────────────
	// Group results by topic.
	type partResult struct {
		partitionID int32
		offset      int64
	}
	topicMap := make(map[string][]partResult)
	var topicOrder []string

	if fetchAll {
		for tp, off := range committed {
			if _, exists := topicMap[tp.Topic]; !exists {
				topicOrder = append(topicOrder, tp.Topic)
			}
			topicMap[tp.Topic] = append(topicMap[tp.Topic], partResult{tp.Partition, off})
		}
	} else {
		for _, rp := range requested {
			if _, exists := topicMap[rp.topic]; !exists {
				topicOrder = append(topicOrder, rp.topic)
			}
			offset := committed[store.TopicPartition{Topic: rp.topic, Partition: rp.partition}]
			topicMap[rp.topic] = append(topicMap[rp.topic], partResult{rp.partition, offset})
		}
	}

	// ── Write response ────────────────────────────────────────────────────────
	if version >= 3 {
		w.WriteInt32(0) // ThrottleTimeMs
	}

	w.WriteArrayLen(int32(len(topicOrder)))
	for _, topicName := range topicOrder {
		w.WriteString(topicName)
		parts := topicMap[topicName]
		w.WriteArrayLen(int32(len(parts)))
		for _, p := range parts {
			w.WriteInt32(p.partitionID)
			w.WriteInt64(p.offset) // -1 if not committed
			if version >= 5 {
				w.WriteInt32(-1) // CommittedLeaderEpoch = -1 (no epoch tracking)
			}
			w.WriteNullableString(nil) // Metadata = null
			w.WriteInt16(errNone)
		}
	}

	if version >= 2 {
		w.WriteInt16(errNone) // top-level ErrorCode
	}
}
