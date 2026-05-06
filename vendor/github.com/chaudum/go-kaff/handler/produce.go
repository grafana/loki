package handler

import (
	"time"

	"github.com/chaudum/go-kaff/codec"
)

// handleProduce handles Produce requests (API key 0, versions 3–8).
//
// ── Request format ─────────────────────────────────────────────────────────────
//
//	v3+: TransactionID(nullable string) | Acks(int16) | TimeoutMillis(int32)
//	     | Topics(array)
//	       Topic: Name(string) | Partitions(array)
//	         Partition: PartitionIndex(int32) | Records(bytes)
//
// The Records field is a raw byte blob of one or more concatenated
// RecordBatch entries (magic=2, see recordbatch.go).
//
// ── Response format ────────────────────────────────────────────────────────────
//
//	Topics(array)
//	  Topic: Name(string) | Partitions(array)
//	    Partition: PartitionIndex(int32) | ErrorCode(int16) | BaseOffset(int64)
//	             | LogAppendTime(int64, v2+) | LogStartOffset(int64, v5+)
//	             | ErrorRecords(array, v8+) | ErrorMessage(nullable string, v8+)
//	ThrottleTimeMs(int32, v1+)
func (rt *Router) handleProduce(hdr RequestHeader, r *codec.Reader, w *codec.Writer) {
	version := hdr.ApiVersion

	// TransactionID (v3+, nullable string; we ignore it but must consume it).
	if version >= 3 {
		r.ReadNullableString()
	}

	_ = r.ReadInt16() // always acknowledge immediately (Acks is ignored)
	_ = r.ReadInt32() // TimeoutMillis — ignored (no replication to wait on)

	// ── Parse and append topics/partitions ────────────────────────────────────

	type partResult struct {
		partitionID   int32
		errorCode     int16
		baseOffset    int64
		logAppendTime int64
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
			rawRecords := r.ReadBytes() // raw RecordBatch blob

			pr := partResult{
				partitionID:   partID,
				logAppendTime: -1, // CreateTime mode
			}

			if r.Err() != nil {
				tr.partitions = append(tr.partitions, pr)
				continue
			}

			// Resolve partition in the store.
			topic, ok := rt.store.GetTopic(topicName)
			if !ok && rt.broker.AutoCreateTopics {
				topic, _ = rt.store.GetOrCreateTopic(topicName, rt.broker.DefaultPartitions, rt.broker.MaxBytesPerPartition)
				ok = topic != nil
			}
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

			// Decode the raw RecordBatch bytes into individual Records.
			records, err := decodeRecordBatches(rawRecords)
			if err != nil || len(records) == 0 {
				// Empty or corrupt batch: treat as a no-op produce.
				// baseOffset = current HWM (the next offset to be assigned).
				pr.baseOffset = part.HighWaterMark()
				tr.partitions = append(tr.partitions, pr)
				continue
			}

			// Strip producer-assigned offsets; Partition.Append will reassign them.
			now := time.Now()
			for k := range records {
				records[k].Offset = 0 // will be overwritten by Append
				if records[k].Timestamp.IsZero() {
					records[k].Timestamp = now
				}
			}

			pr.baseOffset = part.Append(records)

			// Metrics.
			var totalBytes int64
			for _, rec := range records {
				totalBytes += int64(len(rec.Key) + len(rec.Value))
			}
			rt.metrics.RecordProduce(topicName, partID, len(records), totalBytes)
			rt.metrics.SetPartitionHighWaterMark(topicName, partID, part.HighWaterMark())

			tr.partitions = append(tr.partitions, pr)
		}
		topics = append(topics, tr)
	}

	// ── Write response ────────────────────────────────────────────────────────

	w.WriteArrayLen(int32(len(topics)))
	for _, tr := range topics {
		w.WriteString(tr.name)
		w.WriteArrayLen(int32(len(tr.partitions)))
		for _, pr := range tr.partitions {
			w.WriteInt32(pr.partitionID)
			w.WriteInt16(pr.errorCode)
			w.WriteInt64(pr.baseOffset)
			if version >= 2 {
				w.WriteInt64(pr.logAppendTime) // -1 = not log-append-time mode
			}
			if version >= 5 {
				w.WriteInt64(0) // LogStartOffset = 0 (no eviction)
			}
			if version >= 8 {
				w.WriteArrayLen(0)         // ErrorRecords = empty
				w.WriteNullableString(nil) // ErrorMessage = null
			}
		}
	}
	if version >= 1 {
		w.WriteInt32(0) // ThrottleTimeMs
	}
}
