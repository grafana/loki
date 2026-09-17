package kfake

import (
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// Metadata: v0-13
//
// Behavior:
// * Null topics returns all topics (v1+)
// * Empty topics returns no topics
// * AllowAutoTopicCreation is respected if cluster config allows it
// * Topics can be requested by name or by ID (v10+)
//
// Version notes:
// * v1: ControllerID, IsInternal, Rack
// * v2: ClusterID
// * v4: AllowAutoTopicCreation
// * v5: OfflineReplicas
// * v7: LeaderEpoch
// * v8: AuthorizedOperations (ACLs)
// * v9: Flexible versions
// * v10: TopicID
// * v13: Top-level ErrorCode for rebootstrapping (KIP-1102) - not implemented

func init() { regKey(3, 0, 13) }

func (c *Cluster) handleMetadata(creq *clientReq) (kmsg.Response, error) {
	req := creq.kreq.(*kmsg.MetadataRequest)
	resp := req.ResponseKind().(*kmsg.MetadataResponse)

	if err := c.checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	for _, b := range c.bs {
		sb := kmsg.NewMetadataResponseBroker()
		sb.NodeID = b.node
		sb.Host, sb.Port = b.hostport()
		sb.Rack = &brokerRack
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
		if v, ok := c.data.tcfgs[t]["kfake.is_internal"]; ok && v != nil && *v == "true" {
			st.IsInternal = true
		}
		st.ErrorCode = errCode
		if req.IncludeTopicAuthorizedOperations {
			st.AuthorizedOperations = c.topicAuthorizedOps(creq, t)
		}
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
	// Dedupe after name resolution so a request that specifies both a
	// topic name and its TopicID produces one response entry with one
	// set of partitions (not a merged entry with partitions duplicated).
	seenTopic := make(map[string]bool, len(req.Topics))
	for _, rt := range req.Topics {
		var topic string
		var ok bool
		// If topic ID is present, we ignore any provided topic.
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

		if !c.allowedACL(creq, topic, kmsg.ACLResourceTypeTopic, kmsg.ACLOperationDescribe) {
			donet(topic, rt.TopicID, kerr.TopicAuthorizationFailed.Code)
			continue
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

		if seenTopic[topic] {
			continue
		}
		seenTopic[topic] = true

		id := c.data.t2id[topic]
		for p, pd := range ps {
			okp(topic, id, p, pd)
		}
	}
	if req.Topics == nil && c.data.tps != nil {
		for topic, ps := range c.data.tps {
			// For listing all topics, filter to only authorized topics
			if !c.allowedACL(creq, topic, kmsg.ACLResourceTypeTopic, kmsg.ACLOperationDescribe) {
				continue
			}
			id := c.data.t2id[topic]
			for p, pd := range ps {
				okp(topic, id, p, pd)
			}
		}
	}

	if req.IncludeClusterAuthorizedOperations {
		resp.AuthorizedOperations = c.clusterAuthorizedOps(creq)
	}

	return resp, nil
}
