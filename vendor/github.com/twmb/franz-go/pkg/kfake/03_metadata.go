package kfake

import (
	"net"
	"strconv"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

func init() { regKey(3, 0, 12) }

func (c *Cluster) handleMetadata(kreq kmsg.Request) (kmsg.Response, error) {
	req := kreq.(*kmsg.MetadataRequest)
	resp := req.ResponseKind().(*kmsg.MetadataResponse)

	if err := checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	for _, b := range c.bs {
		sb := kmsg.NewMetadataResponseBroker()
		h, p, _ := net.SplitHostPort(b.ln.Addr().String())
		p32, _ := strconv.Atoi(p)
		sb.NodeID = b.node
		sb.Host = h
		sb.Port = int32(p32)
		resp.Brokers = append(resp.Brokers, sb)
	}

	resp.ClusterID = &c.cfg.clusterID
	resp.ControllerID = c.controller.node

	id2t := make(map[uuid]string)
	tidx := make(map[string]int)

	donet := func(t string, id uuid, errCode int16) *kmsg.MetadataResponseTopic {
		if i, ok := tidx[t]; ok {
			return &resp.Topics[i]
		}
		id2t[id] = t
		tidx[t] = len(resp.Topics)
		st := kmsg.NewMetadataResponseTopic()
		if t != "" {
			st.Topic = kmsg.StringPtr(t)
		}
		st.TopicID = id
		st.ErrorCode = errCode
		resp.Topics = append(resp.Topics, st)
		return &resp.Topics[len(resp.Topics)-1]
	}
	donep := func(t string, id uuid, p int32, errCode int16) *kmsg.MetadataResponseTopicPartition {
		sp := kmsg.NewMetadataResponseTopicPartition()
		sp.Partition = p
		sp.ErrorCode = errCode
		st := donet(t, id, 0)
		st.Partitions = append(st.Partitions, sp)
		return &st.Partitions[len(st.Partitions)-1]
	}
	okp := func(t string, id uuid, p int32, pd *partData) {
		nreplicas := c.data.treplicas[t]
		if nreplicas > len(c.bs) {
			nreplicas = len(c.bs)
		}

		sp := donep(t, id, p, 0)
		sp.Leader = pd.leader.node
		sp.LeaderEpoch = pd.epoch

		for i := 0; i < nreplicas; i++ {
			idx := (pd.leader.bsIdx + i) % len(c.bs)
			sp.Replicas = append(sp.Replicas, c.bs[idx].node)
		}
		sp.ISR = sp.Replicas
	}

	allowAuto := req.AllowAutoTopicCreation && c.cfg.allowAutoTopic
	for _, rt := range req.Topics {
		var topic string
		var ok bool
		// If topic ID is present, we ignore any provided topic.
		// Duplicate topics are merged into one response topic.
		// Topics with no topic and no ID are ignored.
		if rt.TopicID != noID {
			if topic, ok = c.data.id2t[rt.TopicID]; !ok {
				donet("", rt.TopicID, kerr.UnknownTopicID.Code)
				continue
			}
		} else if rt.Topic == nil {
			continue
		} else {
			topic = *rt.Topic
		}

		ps, ok := c.data.tps.gett(topic)
		if !ok {
			if !allowAuto {
				donet(topic, rt.TopicID, kerr.UnknownTopicOrPartition.Code)
				continue
			}
			c.data.mkt(topic, -1, -1, nil)
			ps, _ = c.data.tps.gett(topic)
		}

		id := c.data.t2id[topic]
		for p, pd := range ps {
			okp(topic, id, p, pd)
		}
	}
	if req.Topics == nil && c.data.tps != nil {
		for topic, ps := range c.data.tps {
			id := c.data.t2id[topic]
			for p, pd := range ps {
				okp(topic, id, p, pd)
			}
		}
	}

	return resp, nil
}
