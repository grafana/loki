package kfake

import (
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// OffsetForLeaderEpoch: v3-4
//
// Behavior:
// * Returns the end offset for a given leader epoch
// * Used by clients to detect log truncation (KIP-320)
// * Only supports consumer replica ID (-1)
//
// Version notes:
// * v3: CurrentLeaderEpoch for fencing
// * v4: Flexible versions

func init() { regKey(23, 3, 4) }

func (c *Cluster) handleOffsetForLeaderEpoch(creq *clientReq) (kmsg.Response, error) {
	var (
		b    = creq.cc.b
		req  = creq.kreq.(*kmsg.OffsetForLeaderEpochRequest)
		resp = req.ResponseKind().(*kmsg.OffsetForLeaderEpochResponse)
	)

	if err := c.checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	tidx := make(map[string]int)
	donet := func(t string) *kmsg.OffsetForLeaderEpochResponseTopic {
		if i, ok := tidx[t]; ok {
			return &resp.Topics[i]
		}
		tidx[t] = len(resp.Topics)
		st := kmsg.NewOffsetForLeaderEpochResponseTopic()
		st.Topic = t
		resp.Topics = append(resp.Topics, st)
		return &resp.Topics[len(resp.Topics)-1]
	}
	donep := func(t string, p int32, errCode int16) *kmsg.OffsetForLeaderEpochResponseTopicPartition {
		sp := kmsg.NewOffsetForLeaderEpochResponseTopicPartition()
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
			if req.ReplicaID >= 0 {
				donep(rt.Topic, rp.Partition, kerr.UnknownServerError.Code)
				continue
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

			// If the user is requesting our current epoch, we return the HWM.
			if rp.LeaderEpoch == pd.epoch {
				sp.LeaderEpoch = pd.epoch
				sp.EndOffset = pd.highWatermark
				continue
			}

			// If our epoch was bumped before anything was
			// produced, return the epoch and a start offset of 0.
			if !pd.hasBatches() {
				sp.LeaderEpoch = pd.epoch
				sp.EndOffset = 0
				if rp.LeaderEpoch > pd.epoch {
					sp.LeaderEpoch = -1
					sp.EndOffset = -1
				}
				continue
			}

			// Two-level binary search for the first batch with epoch > requested.
			nextEpoch := rp.LeaderEpoch + 1
			si, mi, cur := pd.findBatchMeta(int64(nextEpoch), func(m *batchMeta) int64 { return int64(m.epoch) })

			// Requested epoch is not yet known: keep -1 returns.
			if cur == nil {
				sp.LeaderEpoch = -1
				sp.EndOffset = -1
				continue
			}

			// Next epoch is actually the first epoch: return the
			// requested epoch and the LSO.
			if si == 0 && mi == 0 {
				sp.LeaderEpoch = rp.LeaderEpoch
				sp.EndOffset = pd.logStartOffset
				continue
			}

			// Get the batch before cur.
			var prev *batchMeta
			if mi > 0 {
				prev = &pd.segments[si].index[mi-1]
			} else {
				prevSeg := &pd.segments[si-1]
				prev = &prevSeg.index[len(prevSeg.index)-1]
			}
			sp.LeaderEpoch = prev.epoch
			sp.EndOffset = cur.firstOffset
		}
	}
	return resp, nil
}
