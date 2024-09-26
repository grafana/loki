package kfake

import (
	"sort"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

func init() { regKey(2, 0, 7) }

func (c *Cluster) handleListOffsets(b *broker, kreq kmsg.Request) (kmsg.Response, error) {
	req := kreq.(*kmsg.ListOffsetsRequest)
	resp := req.ResponseKind().(*kmsg.ListOffsetsResponse)

	if err := checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	tidx := make(map[string]int)
	donet := func(t string, errCode int16) *kmsg.ListOffsetsResponseTopic {
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
		st := donet(t, 0)
		st.Partitions = append(st.Partitions, sp)
		return &st.Partitions[len(st.Partitions)-1]
	}

	for _, rt := range req.Topics {
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
			default:
				// returns the index of the first batch _after_ the requested timestamp
				idx, _ := sort.Find(len(pd.batches), func(idx int) int {
					maxEarlier := pd.batches[idx].maxEarlierTimestamp
					switch {
					case maxEarlier > rp.Timestamp:
						return -1
					case maxEarlier == rp.Timestamp:
						return 0
					default:
						return 1
					}
				})
				if idx == len(pd.batches) {
					sp.Offset = -1
				} else {
					sp.Offset = pd.batches[idx].FirstOffset
				}
			}
		}
	}
	return resp, nil
}
