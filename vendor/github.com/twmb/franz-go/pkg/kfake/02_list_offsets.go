package kfake

import (
	"sort"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// ListOffsets: v0-10
//
// Timestamp special values, answered as a broker without tiered storage
// answers them:
// * -2: Earliest offset (log start offset)
// * -1: Latest offset (high watermark, or the LSO under read_committed)
// * -3: Max timestamp offset (KIP-734, v7+)
// * -4: Earliest local offset (KIP-405, v8+): the log start offset
// * -5: Latest tiered offset (KIP-1005, v9+): -1, nothing is tiered
// * -6: Earliest pending upload offset (KIP-1023, v11+): -1, nothing is tiered
// Any other negative timestamp, or a special value sent below the version
// that added it, is UNSUPPORTED_VERSION for that partition. A partition
// listed twice in one request is INVALID_REQUEST for both entries.
//
// ReplicaID -1 is a consumer: IsolationLevel applies, and the partition
// must be led by this broker. ReplicaID -2 is the debugging id: any
// replica answers, and IsolationLevel is ignored.
//
// Version notes:
// * v0: MaxNumOffsets and OldStyleOffsets, listed by segment modification time
// * v1: one offset per partition, found by record timestamp
// * v2: IsolationLevel for read_committed
// * v4: CurrentLeaderEpoch for fencing, LeaderEpoch in response
// * v6: Flexible versions
// * v7: Timestamp -3 for max timestamp (KIP-734)
// * v8: Timestamp -4 for local log start (KIP-405)
// * v9: Timestamp -5 for remote storage offset (KIP-1005)
// * v10: TimeoutMillis for remote storage lookups - not implemented

func init() { regKey(2, 0, 10) }

// listOffsetsMinVersion is the request version each special timestamp
// needs (ReplicaManager.timestampMinSupportedVersion).
var listOffsetsMinVersion = map[int64]int16{
	-2: 1,
	-1: 1,
	-3: 7,
	-4: 8,
	-5: 9,
	-6: 11,
}

func (c *Cluster) handleListOffsets(creq *clientReq) (kmsg.Response, error) {
	var (
		b   = creq.cc.b
		req = creq.kreq.(*kmsg.ListOffsetsRequest)
	)
	resp := req.ResponseKind().(*kmsg.ListOffsetsResponse)

	if err := c.checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	tidx := make(map[string]int)
	donet := func(t string) *kmsg.ListOffsetsResponseTopic {
		if i, ok := tidx[t]; ok {
			return &resp.Topics[i]
		}
		tidx[t] = len(resp.Topics)
		st := kmsg.NewListOffsetsResponseTopic()
		st.Topic = t
		resp.Topics = append(resp.Topics, st)
		return &resp.Topics[len(resp.Topics)-1]
	}
	donep := func(t string, p int32, errCode int16) *kmsg.ListOffsetsResponseTopicPartition {
		sp := kmsg.NewListOffsetsResponseTopicPartition()
		sp.Partition = p
		sp.ErrorCode = errCode
		st := donet(t)
		st.Partitions = append(st.Partitions, sp)
		return &st.Partitions[len(st.Partitions)-1]
	}

	type tp struct {
		t string
		p int32
	}
	seen := make(map[tp]int)
	for _, rt := range req.Topics {
		for _, rp := range rt.Partitions {
			seen[tp{rt.Topic, rp.Partition}]++
		}
	}
	readCommitted := req.ReplicaID == -1 && req.IsolationLevel == 1

	for _, rt := range req.Topics {
		tk := faultKey{topic: rt.Topic}
		if e := c.deny(creq, rt.Topic, kmsg.ACLResourceTypeTopic, kmsg.ACLOperationDescribe, tk); e != nil {
			for _, rp := range rt.Partitions {
				donep(rt.Topic, rp.Partition, e.Code)
			}
			continue
		}
		ps, ok := c.data.tps.gett(rt.Topic)
		for _, rp := range rt.Partitions {
			if e := creq.faults.check(tk.part(rp.Partition)); e != nil {
				donep(rt.Topic, rp.Partition, e.Code)
				continue
			}
			if req.Version >= 1 {
				if seen[tp{rt.Topic, rp.Partition}] > 1 {
					donep(rt.Topic, rp.Partition, kerr.InvalidRequest.Code)
					continue
				}
				if minVersion, ok := listOffsetsMinVersion[rp.Timestamp]; rp.Timestamp < 0 && (!ok || req.Version < minVersion) {
					donep(rt.Topic, rp.Partition, kerr.UnsupportedVersion.Code)
					continue
				}
			}
			if !ok {
				donep(rt.Topic, rp.Partition, kerr.UnknownTopicOrPartition.Code)
				continue
			}
			pd, ok := ps[rp.Partition]
			if !ok {
				donep(rt.Topic, rp.Partition, kerr.UnknownTopicOrPartition.Code)
				continue
			}
			// The epoch is checked before leadership (Partition.getLocalLog).
			if le := rp.CurrentLeaderEpoch; le != -1 {
				if le < pd.epoch {
					donep(rt.Topic, rp.Partition, kerr.FencedLeaderEpoch.Code)
					continue
				} else if le > pd.epoch {
					donep(rt.Topic, rp.Partition, kerr.UnknownLeaderEpoch.Code)
					continue
				}
			}
			if pd.leader != b && req.ReplicaID != -2 {
				donep(rt.Topic, rp.Partition, kerr.NotLeaderForPartition.Code)
				continue
			}

			// A partition with no answer keeps the defaults: offset -1,
			// timestamp -1, leader epoch -1.
			sp := donep(rt.Topic, rp.Partition, 0)
			if req.Version == 0 {
				sp.OldStyleOffsets = pd.legacyOffsetsBefore(rp.Timestamp, rp.MaxNumOffsets)
				continue
			}
			switch rp.Timestamp {
			case -2, -4:
				sp.Offset = pd.logStartOffset
				if c.cfg.synthetic != nil { // the canned batch is served from any offset
					sp.Offset = 0
				}
				sp.LeaderEpoch = pd.epoch
				// The epoch accompanying a listed offset is the epoch
				// of the record at that offset (a real broker answers
				// from its leader-epoch cache), not the partition's
				// current epoch: a freshly reset consumer must not
				// believe it consumed an epoch above the historical
				// records it is about to read.
				if segIdx, metaIdx, ok, atEnd := pd.searchOffset(pd.logStartOffset); ok && !atEnd {
					sp.LeaderEpoch = pd.segments[segIdx].index[metaIdx].epoch
				}
			case -1:
				sp.Offset = pd.highWatermark
				if readCommitted {
					sp.Offset = pd.lastStableOffset
				}
				if c.cfg.synthetic != nil {
					sp.Offset = syntheticEnd
				}
				sp.LeaderEpoch = pd.epoch
			case -5, -6:
			default:
				var (
					offset, timestamp int64
					epoch             int32
					found             bool
					err               error
				)
				if rp.Timestamp == -3 {
					offset, timestamp, epoch, found, err = c.offsetOfMaxTimestamp(pd)
				} else {
					offset, timestamp, epoch, found, err = c.offsetForTimestamp(pd, rp.Timestamp)
				}
				if err != nil {
					sp.ErrorCode = kerr.CorruptMessage.Code
					continue
				}
				// A record at or past the last fetchable offset is not
				// an answer (ReplicaManager.fetchOffset): under
				// read_committed, a record in an open transaction lists
				// as if nothing matched.
				lastFetchable := pd.highWatermark
				if readCommitted {
					lastFetchable = pd.lastStableOffset
				}
				if found && offset < lastFetchable {
					sp.Offset = offset
					sp.Timestamp = timestamp
					sp.LeaderEpoch = epoch
				}
			}
		}
	}
	return resp, nil
}

// offsetOfMaxTimestamp answers ListOffsets -3 the way a real broker does
// (UnifiedLog.fetchOffsetByTimestamp, RecordBatch.offsetOfMaxTimestamp):
// the segment with the greatest max timestamp, the earliest on a tie,
// then the first batch in it to reach that max, then the first record in
// that batch carrying it. The broker does not check the log start offset
// here, so the answer can be a record deleted from below. Returns found
// == false for an empty partition, or if the batch's header names a
// timestamp none of its records carry.
func (c *Cluster) offsetOfMaxTimestamp(pd *partData) (offset, timestamp int64, epoch int32, found bool, err error) {
	si := pd.maxTimestampSegment()
	if si < 0 || pd.segments[si].maxBatch.nbytes == 0 {
		return 0, 0, 0, false, nil
	}
	seg := &pd.segments[si]
	m := &seg.maxBatch
	batch, err := c.readBatchFull(pd, si, m)
	if err != nil {
		return 0, 0, 0, false, err
	}
	err = forEachBatchRecord(batch.RecordBatch, func(rec kmsg.Record) bool {
		if batch.FirstTimestamp+rec.TimestampDelta64 == seg.maxTimestamp {
			offset, found = batch.FirstOffset+int64(rec.OffsetDelta), true
		}
		return !found
	})
	if err != nil || !found {
		return 0, 0, 0, false, err
	}
	return offset, seg.maxTimestamp, m.epoch, true, nil
}

// offsetForTimestamp answers a ListOffsets timestamp query the way a real
// broker does. The broker commits to the first segment whose largest
// timestamp reaches ts (UnifiedLog.searchOffsetInLocalLog), then within
// it takes the first batch whose max timestamp reaches ts and the first
// record in that batch at or after ts and at or after the log start
// offset (FileRecords.searchForTimestamp). If that batch's qualifying
// records were all deleted from below, the scan moves on to the next
// batch, but never to the next segment: the broker answers that nothing
// was found. Returns found == false in that case, and if no segment
// reaches ts.
func (c *Cluster) offsetForTimestamp(pd *partData, ts int64) (offset, timestamp int64, epoch int32, found bool, err error) {
	// Both running maxes are monotonic: the first binary search lands
	// on the first segment whose max reaches ts, the second on the
	// first batch in it that can hold a record at or after ts. The loop
	// then passes over a batch that cannot: one whose own max is below
	// ts, or one deleted from below (a snapshot load keeps such batches
	// in the index; the broker likewise starts its scan no earlier than
	// the log start offset).
	si := sort.Search(len(pd.segments), func(i int) bool {
		return pd.segments[i].maxEarlierTimestamp >= ts
	})
	if si == len(pd.segments) {
		return 0, 0, 0, false, nil
	}
	seg := &pd.segments[si]
	mi := sort.Search(len(seg.index), func(i int) bool {
		return seg.index[i].maxEarlierTimestamp >= ts
	})
	for ; mi < len(seg.index); mi++ {
		m := &seg.index[mi]
		if m.maxTimestamp < ts || m.firstOffset+int64(m.lastOffsetDelta) < pd.logStartOffset {
			continue
		}
		var batch *partBatch
		if batch, err = c.readBatchFull(pd, si, m); err != nil {
			return 0, 0, 0, false, err
		}
		err = forEachBatchRecord(batch.RecordBatch, func(rec kmsg.Record) bool {
			recTimestamp := batch.FirstTimestamp + rec.TimestampDelta64
			recOffset := batch.FirstOffset + int64(rec.OffsetDelta)
			if recTimestamp >= ts && recOffset >= pd.logStartOffset {
				offset, timestamp, epoch, found = recOffset, recTimestamp, m.epoch, true
			}
			return !found
		})
		if err != nil || found {
			return offset, timestamp, epoch, found, err
		}
	}
	return 0, 0, 0, false, nil
}
