package kfake

import (
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// ListOffsets: v0-10
//
// Timestamp special values:
// * -2: Earliest offset (log start offset)
// * -1: Latest offset (high watermark or LSO depending on isolation level)
// * -3: Max timestamp offset (KIP-734, v7+)
//
// Version notes:
// * v2: IsolationLevel for read_committed
// * v4: CurrentLeaderEpoch for fencing, LeaderEpoch in response
// * v6: Flexible versions
// * v7: Timestamp -3 for max timestamp (KIP-734)
// * v8: Timestamp -4 for local log start (KIP-405) - tiered storage, not implemented
// * v9: Timestamp -5 for remote storage offset (KIP-1005) - tiered storage, not implemented
// * v10: TimeoutMillis for remote storage lookups - not implemented

func init() { regKey(2, 0, 10) }

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

	for _, rt := range req.Topics {
		if !c.allowedACL(creq, rt.Topic, kmsg.ACLResourceTypeTopic, kmsg.ACLOperationDescribe) {
			for _, rp := range rt.Partitions {
				donep(rt.Topic, rp.Partition, kerr.TopicAuthorizationFailed.Code)
			}
			continue
		}
		ps, ok := c.data.tps.gett(rt.Topic)
		for _, rp := range rt.Partitions {
			if !ok {
				donep(rt.Topic, rp.Partition, kerr.UnknownTopicOrPartition.Code)
				continue
			}
			pd, ok := ps[rp.Partition]
			if !ok {
				donep(rt.Topic, rp.Partition, kerr.UnknownTopicOrPartition.Code)
				continue
			}
			if pd.leader != b {
				donep(rt.Topic, rp.Partition, kerr.NotLeaderForPartition.Code)
				continue
			}
			if le := rp.CurrentLeaderEpoch; le != -1 {
				if le < pd.epoch {
					donep(rt.Topic, rp.Partition, kerr.FencedLeaderEpoch.Code)
					continue
				} else if le > pd.epoch {
					donep(rt.Topic, rp.Partition, kerr.UnknownLeaderEpoch.Code)
					continue
				}
			}

			sp := donep(rt.Topic, rp.Partition, 0)
			sp.LeaderEpoch = pd.epoch
			switch rp.Timestamp {
			case -2:
				sp.Offset = pd.logStartOffset
			case -1:
				if req.IsolationLevel == 1 {
					sp.Offset = pd.lastStableOffset
				} else {
					sp.Offset = pd.highWatermark
				}
			case -3:
				// KIP-734: Return offset and timestamp of record with max timestamp
				m := pd.maxTimestampBatch()
				if m == nil {
					sp.Offset = -1
					sp.Timestamp = -1
				} else {
					sp.Offset = m.firstOffset + int64(m.lastOffsetDelta)
					sp.Timestamp = m.maxTimestamp
				}
			default:
				// Two-level binary search for the first batch whose maxTimestamp >= requested timestamp.
				segIdx, _, meta := pd.findBatchMeta(rp.Timestamp, func(m *batchMeta) int64 { return m.maxTimestamp })
				if meta == nil {
					sp.Offset = -1
				} else {
					sp.Offset = meta.firstOffset
					sp.Timestamp = meta.firstTimestamp
					// Read the full batch to iterate records for precise timestamp
					batch, err := c.readBatchFull(pd, segIdx, meta)
					if err != nil {
						sp.ErrorCode = kerr.CorruptMessage.Code
						continue
					}
					err = forEachBatchRecord(batch.RecordBatch, func(rec kmsg.Record) error {
						timestamp := batch.FirstTimestamp + rec.TimestampDelta64
						offset := batch.FirstOffset + int64(rec.OffsetDelta)
						if timestamp <= rp.Timestamp {
							sp.Offset = offset
							sp.Timestamp = timestamp
						}
						return nil
					})
					if err != nil {
						sp.ErrorCode = kerr.CorruptMessage.Code
						continue
					}
				}
			}
		}
	}
	return resp, nil
}
