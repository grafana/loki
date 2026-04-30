package handler

import "github.com/chaudum/go-kaff/codec"

// handleListOffsets handles ListOffsets requests (API key 2, versions 1–5).
//
// ── Request format ─────────────────────────────────────────────────────────────
//
//	ReplicaID(int32) | IsolationLevel(int8, v2+)
//	Topics(array):
//	  Topic(string) | Partitions(array):
//	    Partition(int32) | CurrentLeaderEpoch(int32, v4+) | Timestamp(int64)
//
// Timestamp sentinels:
//
//	-2  earliest offset (log start = 0 for go-kaff)
//	-1  latest offset (high-water mark)
//
// ── Response format ────────────────────────────────────────────────────────────
//
//	ThrottleTimeMs(int32, v2+)
//	Topics(array):
//	  Topic(string) | Partitions(array):
//	    Partition(int32) | ErrorCode(int16)
//	    Timestamp(int64, v1+) | Offset(int64, v1+) | LeaderEpoch(int32, v4+)
const (
	timestampEarliest int64 = -2
	timestampLatest   int64 = -1
)

func (rt *Router) handleListOffsets(hdr RequestHeader, r *codec.Reader, w *codec.Writer) {
	version := hdr.ApiVersion

	r.ReadInt32() // ReplicaID (always -1 from clients; ignored)
	if version >= 2 {
		r.ReadInt8() // IsolationLevel (ignored)
	}

	type partResult struct {
		partitionID int32
		errorCode   int16
		timestamp   int64
		offset      int64
		leaderEpoch int32
	}
	type topicResult struct {
		name       string
		partitions []partResult
	}

	numTopics := r.ReadArrayLen()
	topics := make([]topicResult, 0, numTopics)

	for i := int32(0); i < numTopics; i++ {
		topicName := r.ReadString()
		numParts := r.ReadArrayLen()
		tr := topicResult{name: topicName, partitions: make([]partResult, 0, numParts)}

		for j := int32(0); j < numParts; j++ {
			partID := r.ReadInt32()
			if version >= 4 {
				r.ReadInt32() // CurrentLeaderEpoch (ignored)
			}
			ts := r.ReadInt64()

			pr := partResult{
				partitionID: partID,
				timestamp:   -1, // returned timestamp sentinel
				leaderEpoch: -1,
			}

			topic, ok := rt.store.GetTopic(topicName)
			if !ok {
				pr.errorCode = errUnknownTopicOrPartition
				tr.partitions = append(tr.partitions, pr)
				continue
			}
			part, ok := topic.Partition(partID)
			if !ok {
				pr.errorCode = errUnknownTopicOrPartition
				tr.partitions = append(tr.partitions, pr)
				continue
			}

			switch ts {
			case timestampEarliest:
				pr.offset = part.LogStartOffset() // always 0
			case timestampLatest:
				pr.offset = part.HighWaterMark()
			default:
				// Any other timestamp: not supported in Phase 2 — return latest.
				pr.offset = part.HighWaterMark()
			}

			tr.partitions = append(tr.partitions, pr)
		}
		topics = append(topics, tr)
	}

	// ── Write response ────────────────────────────────────────────────────────

	if version >= 2 {
		w.WriteInt32(0) // ThrottleTimeMs
	}

	w.WriteArrayLen(int32(len(topics)))
	for _, tr := range topics {
		w.WriteString(tr.name)
		w.WriteArrayLen(int32(len(tr.partitions)))
		for _, pr := range tr.partitions {
			w.WriteInt32(pr.partitionID)
			w.WriteInt16(pr.errorCode)
			if version >= 1 {
				w.WriteInt64(pr.timestamp)   // -1 = no meaningful timestamp
				w.WriteInt64(pr.offset)
			}
			if version >= 4 {
				w.WriteInt32(pr.leaderEpoch) // -1 = no epoch tracking
			}
		}
	}
}
