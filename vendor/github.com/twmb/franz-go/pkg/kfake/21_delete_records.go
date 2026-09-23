package kfake

import (
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// DeleteRecords: v0-2
//
// Behavior:
// * Advances log start offset for partitions
// * Offset -1 means delete up to high watermark
//
// Version notes:
// * v1: ThrottleMillis
// * v2: Flexible versions

func init() { regKey(21, 0, 2) }

func (c *Cluster) handleDeleteRecords(creq *clientReq) (kmsg.Response, error) {
	var (
		b   = creq.cc.b
		req = creq.kreq.(*kmsg.DeleteRecordsRequest)
	)
	resp := req.ResponseKind().(*kmsg.DeleteRecordsResponse)

	if err := c.checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	tidx := make(map[string]int)
	donet := func(t string) *kmsg.DeleteRecordsResponseTopic {
		if i, ok := tidx[t]; ok {
			return &resp.Topics[i]
		}
		tidx[t] = len(resp.Topics)
		st := kmsg.NewDeleteRecordsResponseTopic()
		st.Topic = t
		resp.Topics = append(resp.Topics, st)
		return &resp.Topics[len(resp.Topics)-1]
	}
	// A fault can answer a partition before the work runs. The work's own
	// answer for that partition must not add an entry or replace the code.
	answered := make(map[tp]int)
	donep := func(t string, p int32, errCode int16) *kmsg.DeleteRecordsResponseTopicPartition {
		st := donet(t)
		if i, ok := answered[tp{t, p}]; ok {
			return &st.Partitions[i]
		}
		answered[tp{t, p}] = len(st.Partitions)
		sp := kmsg.NewDeleteRecordsResponseTopicPartition()
		sp.Partition = p
		sp.ErrorCode = errCode
		st.Partitions = append(st.Partitions, sp)
		return &st.Partitions[len(st.Partitions)-1]
	}

	for _, rt := range req.Topics {
		if e := c.deny(creq, rt.Topic, kmsg.ACLResourceTypeTopic, kmsg.ACLOperationDelete, faultKey{topic: rt.Topic}); e != nil && creq.skipsWork(e) { // a timed-out delete falls through to the per-partition checks
			for _, rp := range rt.Partitions {
				donep(rt.Topic, rp.Partition, e.Code)
			}
			continue
		}
		ps, ok := c.data.tps.gett(rt.Topic)
		for _, rp := range rt.Partitions {
			if e := creq.faults.check(faultKey{topic: rt.Topic}.part(rp.Partition)); e != nil {
				donep(rt.Topic, rp.Partition, e.Code)
				if creq.skipsWork(e) { // a timed-out delete still deletes
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
			if pd.leader != b {
				donep(rt.Topic, rp.Partition, kerr.NotLeaderForPartition.Code)
				continue
			}
			to, e := c.deleteRecords(pd, rp.Offset)
			if e != nil {
				donep(rt.Topic, rp.Partition, e.Code)
				continue
			}
			sp := donep(rt.Topic, rp.Partition, 0)
			sp.LowWatermark = to
		}
	}

	return resp, nil
}

// deleteRecords truncates the partition to offset, as DeleteRecords does. An
// offset of -1 truncates to the high watermark. We return the new log start
// offset, which the response reports as the low watermark.
func (c *Cluster) deleteRecords(pd *partData, offset int64) (int64, *kerr.Error) {
	to := offset
	if to == -1 {
		to = pd.highWatermark
	}
	if to < pd.logStartOffset || to > pd.highWatermark {
		return 0, kerr.OffsetOutOfRange
	}
	// logStartOffset is not persisted live. On crash recovery without a
	// snapshot it resets to 0, matching real Kafka unclean leader
	// election. The snapshot captures it on clean shutdown.
	pd.logStartOffset = to
	c.trimLeft(pd)
	return to, nil
}
