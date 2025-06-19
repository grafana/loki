package kfake

import (
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// TODO
//
// * Return InvalidTopicException when names collide

func init() { regKey(19, 0, 7) }

func (c *Cluster) handleCreateTopics(b *broker, kreq kmsg.Request) (kmsg.Response, error) {
	req := kreq.(*kmsg.CreateTopicsRequest)
	resp := req.ResponseKind().(*kmsg.CreateTopicsResponse)

	if err := checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	donet := func(t string, errCode int16) *kmsg.CreateTopicsResponseTopic {
		st := kmsg.NewCreateTopicsResponseTopic()
		st.Topic = t
		st.ErrorCode = errCode
		resp.Topics = append(resp.Topics, st)
		return &resp.Topics[len(resp.Topics)-1]
	}
	donets := func(errCode int16) {
		for _, rt := range req.Topics {
			donet(rt.Topic, errCode)
		}
	}

	if b != c.controller {
		donets(kerr.NotController.Code)
		return resp, nil
	}

	uniq := make(map[string]struct{})
	for _, rt := range req.Topics {
		if _, ok := uniq[rt.Topic]; ok {
			donets(kerr.InvalidRequest.Code)
			return resp, nil
		}
		uniq[rt.Topic] = struct{}{}
	}

	for _, rt := range req.Topics {
		if _, ok := c.data.tps.gett(rt.Topic); ok {
			donet(rt.Topic, kerr.TopicAlreadyExists.Code)
			continue
		}
		if len(rt.ReplicaAssignment) > 0 {
			donet(rt.Topic, kerr.InvalidReplicaAssignment.Code)
			continue
		}
		if int(rt.ReplicationFactor) > len(c.bs) {
			donet(rt.Topic, kerr.InvalidReplicationFactor.Code)
			continue
		}
		if rt.NumPartitions == 0 {
			donet(rt.Topic, kerr.InvalidPartitions.Code)
			continue
		}
		configs := make(map[string]*string)
		for _, c := range rt.Configs {
			configs[c.Name] = c.Value
		}
		c.data.mkt(rt.Topic, int(rt.NumPartitions), int(rt.ReplicationFactor), configs)
		st := donet(rt.Topic, 0)
		st.TopicID = c.data.t2id[rt.Topic]
		st.NumPartitions = int32(len(c.data.tps[rt.Topic]))
		st.ReplicationFactor = int16(c.data.treplicas[rt.Topic])
		for k, v := range configs {
			c := kmsg.NewCreateTopicsResponseTopicConfig()
			c.Name = k
			c.Value = v
			// Source?
			st.Configs = append(st.Configs, c)
		}
	}

	return resp, nil
}
