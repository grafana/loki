package kfake

import (
	"sort"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

func init() { regKey(23, 3, 4) }

func (c *Cluster) handleOffsetForLeaderEpoch(b *broker, kreq kmsg.Request) (kmsg.Response, error) {
	req := kreq.(*kmsg.OffsetForLeaderEpochRequest)
	resp := req.ResponseKind().(*kmsg.OffsetForLeaderEpochResponse)

	if err := checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	tidx := make(map[string]int)
	donet := func(t string, errCode int16) *kmsg.OffsetForLeaderEpochResponseTopic {
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
		st := donet(t, 0)
		st.Partitions = append(st.Partitions, sp)
		return &st.Partitions[len(st.Partitions)-1]
	}

	for _, rt := range req.Topics {
		ps, ok := c.data.tps.gett(rt.Topic)
		for _, rp := range rt.Partitions {
			if req.ReplicaID != -1 {
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
			if rp.CurrentLeaderEpoch < pd.epoch {
				donep(rt.Topic, rp.Partition, kerr.FencedLeaderEpoch.Code)
				continue
			} else if rp.CurrentLeaderEpoch > pd.epoch {
				donep(rt.Topic, rp.Partition, kerr.UnknownLeaderEpoch.Code)
				continue
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
			if len(pd.batches) == 0 {
				sp.LeaderEpoch = pd.epoch
				sp.EndOffset = 0
				if rp.LeaderEpoch > pd.epoch {
					sp.LeaderEpoch = -1
					sp.EndOffset = -1
				}
				continue
			}

			// What is the largest epoch after the requested epoch?
			nextEpoch := rp.LeaderEpoch + 1
			idx, _ := sort.Find(len(pd.batches), func(idx int) int {
				batchEpoch := pd.batches[idx].epoch
				switch {
				case nextEpoch <= batchEpoch:
					return -1
				default:
					return 1
				}
			})

			// Requested epoch is not yet known: keep -1 returns.
			if idx == len(pd.batches) {
				sp.LeaderEpoch = -1
				sp.EndOffset = -1
				continue
			}

			// Next epoch is actually the first epoch: return the
			// requested epoch and the LSO.
			if idx == 0 {
				sp.LeaderEpoch = rp.LeaderEpoch
				sp.EndOffset = pd.logStartOffset
				continue
			}

			sp.LeaderEpoch = pd.batches[idx-1].epoch
			sp.EndOffset = pd.batches[idx].FirstOffset
		}
	}
	return resp, nil
}
