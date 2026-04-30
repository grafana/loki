package handler

import (
	"reflect"
	"time"

	"github.com/chaudum/go-kaff/codec"
	"github.com/chaudum/go-kaff/store"
)

// handleFetch handles Fetch requests (API key 1, versions 4–11).
//
// ── Request format ─────────────────────────────────────────────────────────────
//
//	ReplicaID(int32) | MaxWaitMillis(int32) | MinBytes(int32)
//	MaxBytes(int32, v3+) | IsolationLevel(int8, v4+)
//	SessionID(int32, v7+) | SessionEpoch(int32, v7+)
//	Topics(array):
//	  Topic(string) | Partitions(array):
//	    Partition(int32) | CurrentLeaderEpoch(int32, v9+) | FetchOffset(int64)
//	    LogStartOffset(int64, v5+) | MaxBytes(int32)
//	ForgottenTopics(array, v7+):  Topic(string) | Partitions([]int32)
//	RackID(string, v11+)
//
// ── Response format ────────────────────────────────────────────────────────────
//
//	ThrottleTimeMs(int32, v1+) | ErrorCode(int16, v7+) | SessionID(int32, v7+)
//	Topics(array):
//	  Topic(string) | Partitions(array):
//	    Partition(int32) | ErrorCode(int16) | HighWatermark(int64)
//	    LastStableOffset(int64, v4+) | LogStartOffset(int64, v5+)
//	    AbortedTransactions(nullable array, v4+) | PreferredReadReplica(int32, v11+)
//	    RecordBatches(nullable bytes)
//
// Long-poll: when the available data is less than MinBytes, the handler
// blocks until either data arrives on any requested partition, the
// MaxWaitMillis timeout expires, or the broker shuts down.
func (rt *Router) handleFetch(hdr RequestHeader, r *codec.Reader, w *codec.Writer) {
	version := hdr.ApiVersion

	// ── Parse request ─────────────────────────────────────────────────────────
	r.ReadInt32() // ReplicaID (always -1 from clients)
	maxWaitMillis := r.ReadInt32()
	minBytes := r.ReadInt32()
	maxBytes := int32(1<<31 - 1)
	if version >= 3 {
		maxBytes = r.ReadInt32()
	}
	if version >= 4 {
		r.ReadInt8() // IsolationLevel (0=read_uncommitted; we ignore transactions)
	}
	var sessionID int32
	if version >= 7 {
		sessionID = r.ReadInt32()
		r.ReadInt32() // SessionEpoch — we do not implement fetch sessions
	}

	type fetchPartition struct {
		topic       string
		partitionID int32
		offset      int64
		maxBytes    int32
	}

	numTopics := r.ReadArrayLen()
	var fetchList []fetchPartition
	for i := int32(0); i < numTopics; i++ {
		topicName := r.ReadString()
		numParts := r.ReadArrayLen()
		for j := int32(0); j < numParts; j++ {
			partID := r.ReadInt32()
			if version >= 9 {
				r.ReadInt32() // CurrentLeaderEpoch
			}
			offset := r.ReadInt64()
			if version >= 5 {
				r.ReadInt64() // LogStartOffset
			}
			partMaxBytes := r.ReadInt32()
			fetchList = append(fetchList, fetchPartition{topicName, partID, offset, partMaxBytes})
		}
	}

	// ForgottenTopics (v7+) — consume and discard.
	if version >= 7 {
		n := r.ReadArrayLen()
		for i := int32(0); i < n; i++ {
			r.ReadString() // topic
			m := r.ReadArrayLen()
			for j := int32(0); j < m; j++ {
				r.ReadInt32()
			}
		}
	}
	// RackID (v11+).
	if version >= 11 {
		r.ReadString()
	}

	// ── Long-poll data collection ─────────────────────────────────────────────

	type partResult struct {
		topic       string
		partitionID int32
		records     []store.Record
		hwm         int64
		errorCode   int16
	}

	deadline := time.Now().Add(time.Duration(maxWaitMillis) * time.Millisecond)
	var results []partResult

	for {
		// Capture wait channels BEFORE reading to avoid missing notifications.
		waitChans := make([]<-chan struct{}, 0, len(fetchList))
		results = results[:0]
		totalBytes := 0

		for _, fp := range fetchList {
			res := partResult{topic: fp.topic, partitionID: fp.partitionID}

			topic, ok := rt.store.GetTopic(fp.topic)
			if !ok {
				res.errorCode = errUnknownTopicOrPartition
				results = append(results, res)
				continue
			}
			part, ok := topic.Partition(fp.partitionID)
			if !ok {
				res.errorCode = errUnknownTopicOrPartition
				results = append(results, res)
				continue
			}

			// Subscribe before reading (crucial for correctness).
			waitChans = append(waitChans, part.WaitChan())

			hwm := part.HighWaterMark()
			res.hwm = hwm

			if fp.offset < 0 || fp.offset > hwm {
				res.errorCode = errOffsetOutOfRange
				results = append(results, res)
				continue
			}

			// Determine per-partition byte cap.
			partMax := int(fp.maxBytes)
			if partMax <= 0 || int32(partMax) > maxBytes {
				partMax = int(maxBytes)
			}

			recs, _ := part.Read(fp.offset, partMax)
			res.records = recs
			var fetchBytes int64
			for _, rec := range recs {
				approx := len(rec.Key) + len(rec.Value) + 30
				totalBytes += approx
				fetchBytes += int64(approx)
			}
			if len(recs) > 0 {
				rt.metrics.RecordFetch(fp.topic, fp.partitionID, len(recs), fetchBytes)
			}
			results = append(results, res)
		}

		// Check exit conditions for the long-poll loop.
		remaining := time.Until(deadline)
		if totalBytes >= int(minBytes) || remaining <= 0 || maxWaitMillis == 0 {
			break
		}
		// If no valid partitions remain (e.g. topic deleted between iterations),
		// there is nothing to wait for — return immediately.
		if len(waitChans) == 0 {
			break
		}

		// Check for broker shutdown before blocking.
		select {
		case <-rt.ctx.Done():
			return
		default:
		}

		waitForFetchData(rt.ctx, deadline, waitChans)
	}

	// ── Write response ────────────────────────────────────────────────────────

	if version >= 1 {
		w.WriteInt32(0) // ThrottleTimeMs
	}
	if version >= 7 {
		w.WriteInt16(0) // Top-level ErrorCode = OK
		w.WriteInt32(sessionID)
	}

	// Group results by topic to match the response array structure.
	type topicGroup struct {
		name  string
		parts []partResult
	}
	groups := make(map[string]*topicGroup)
	var order []string
	for _, res := range results {
		if _, ok := groups[res.topic]; !ok {
			groups[res.topic] = &topicGroup{name: res.topic}
			order = append(order, res.topic)
		}
		groups[res.topic].parts = append(groups[res.topic].parts, res)
	}

	w.WriteArrayLen(int32(len(order)))
	for _, topicName := range order {
		tg := groups[topicName]
		w.WriteString(tg.name)
		w.WriteArrayLen(int32(len(tg.parts)))
		for _, pr := range tg.parts {
			w.WriteInt32(pr.partitionID)
			w.WriteInt16(pr.errorCode)
			w.WriteInt64(pr.hwm) // HighWatermark
			if version >= 4 {
				w.WriteInt64(pr.hwm) // LastStableOffset = HWM (no transactions)
			}
			if version >= 5 {
				w.WriteInt64(0) // LogStartOffset = 0
			}
			if version >= 4 {
				// AbortedTransactions = null (no transactional support).
				w.WriteArrayLen(-1)
			}
			if version >= 11 {
				w.WriteInt32(-1) // PreferredReadReplica = -1 (none)
			}

			// RecordBatches bytes field.
			if len(pr.records) > 0 {
				w.WriteBytes(encodeRecordBatch(pr.records))
			} else {
				w.WriteBytes(nil) // null → no records available
			}
		}
	}
}

// waitForFetchData blocks until at least one of the given wait channels is
// closed (new records available), the deadline passes, or the broker context
// is cancelled.
//
// channels must have been captured via Partition.WaitChan() BEFORE reading
// partition data to guarantee no notification is missed.
func waitForFetchData(ctx interface{ Done() <-chan struct{} }, deadline time.Time, channels []<-chan struct{}) {
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return
	}

	timer := time.NewTimer(remaining)
	defer timer.Stop()

	// Build a reflect.Select case set: one case per partition channel,
	// plus the timer and the context.
	cases := make([]reflect.SelectCase, 0, len(channels)+2)
	for _, ch := range channels {
		cases = append(cases, reflect.SelectCase{
			Dir:  reflect.SelectRecv,
			Chan: reflect.ValueOf(ch),
		})
	}
	cases = append(cases, reflect.SelectCase{
		Dir:  reflect.SelectRecv,
		Chan: reflect.ValueOf(timer.C),
	})
	cases = append(cases, reflect.SelectCase{
		Dir:  reflect.SelectRecv,
		Chan: reflect.ValueOf(ctx.Done()),
	})

	reflect.Select(cases)
}
