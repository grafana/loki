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
	donep := func(t string, p int32, errCode int16) *kmsg.DeleteRecordsResponseTopicPartition {
		sp := kmsg.NewDeleteRecordsResponseTopicPartition()
		sp.Partition = p
		sp.ErrorCode = errCode
		st := donet(t)
		st.Partitions = append(st.Partitions, sp)
		return &st.Partitions[len(st.Partitions)-1]
	}

	for _, rt := range req.Topics {
		if !c.allowedACL(creq, rt.Topic, kmsg.ACLResourceTypeTopic, kmsg.ACLOperationDelete) {
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
			to := rp.Offset
			if to == -1 {
				to = pd.highWatermark
			}
			if to < pd.logStartOffset || to > pd.highWatermark {
				donep(rt.Topic, rp.Partition, kerr.OffsetOutOfRange.Code)
				continue
			}
			// logStartOffset is not persisted live. On crash
			// recovery without a snapshot it resets to 0,
			// matching real Kafka unclean leader election.
			// The snapshot captures it on clean shutdown.
			pd.logStartOffset = to
			c.trimLeft(pd)
			sp := donep(rt.Topic, rp.Partition, 0)
			sp.LowWatermark = to
		}
	}

	return resp, nil
}
