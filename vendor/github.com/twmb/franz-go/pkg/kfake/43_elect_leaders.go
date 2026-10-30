package kfake

import (
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// ElectLeaders: v0-2
//
// Behavior:
// * Rotates partition leader to the next broker, bumps epoch
// * Single-broker clusters are a no-op (leader stays the same)
// * Null topics triggers election for all partitions
// * ACL: Cluster ALTER required
//
// Version notes:
// * v0: Initial version
// * v1: ElectionType field (KIP-460), top-level ErrorCode
// * v2: Flexible versions

func init() { regKey(43, 0, 2) }

func (c *Cluster) handleElectLeaders(creq *clientReq) (kmsg.Response, error) {
	req := creq.kreq.(*kmsg.ElectLeadersRequest)
	resp := req.ResponseKind().(*kmsg.ElectLeadersResponse)

	if err := c.checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	if !c.allowedClusterACL(creq, kmsg.ACLOperationAlter) {
		resp.ErrorCode = kerr.ClusterAuthorizationFailed.Code
		return resp, nil
	}

	donep := func(topic string, partition int32, errCode int16) {
		for i := range resp.Topics {
			if resp.Topics[i].Topic == topic {
				sp := kmsg.NewElectLeadersResponseTopicPartition()
				sp.Partition = partition
				sp.ErrorCode = errCode
				resp.Topics[i].Partitions = append(resp.Topics[i].Partitions, sp)
				return
			}
		}
		st := kmsg.NewElectLeadersResponseTopic()
		st.Topic = topic
		sp := kmsg.NewElectLeadersResponseTopicPartition()
		sp.Partition = partition
		sp.ErrorCode = errCode
		st.Partitions = append(st.Partitions, sp)
		resp.Topics = append(resp.Topics, st)
	}

	elect := func(t string, p int32, pd *partData) {
		next := (pd.leader.bsIdx + 1) % len(c.bs)
		pd.leader = c.bs[next]
		pd.epoch++
		donep(t, p, 0)
	}

	if req.Topics == nil {
		c.data.tps.each(func(t string, p int32, pd *partData) {
			elect(t, p, pd)
		})
	} else {
		for i := range req.Topics {
			rt := &req.Topics[i]
			for _, p := range rt.Partitions {
				pd, ok := c.data.tps.getp(rt.Topic, p)
				if !ok {
					donep(rt.Topic, p, kerr.UnknownTopicOrPartition.Code)
					continue
				}
				elect(rt.Topic, p, pd)
			}
		}
	}

	return resp, nil
}
