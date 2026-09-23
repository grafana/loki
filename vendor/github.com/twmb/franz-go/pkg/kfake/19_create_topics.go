package kfake

import (
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// CreateTopics: v0-7
//
// Behavior:
// * Must be sent to the controller
// * ReplicaAssignment: uses len(assignment) as partition count; NumPartitions
//   and ReplicationFactor must be -1 when provided
// * Topic name collision detection: topics that differ only in . vs _ are rejected
// * ValidateOnly (v1+): performs validation without creating topics
//
// Version notes:
// * v1: ValidateOnly, ErrorMessage in response
// * v2: ThrottleMillis
// * v4: Creation defaults (KIP-464) - using broker defaults for -1 values
// * v5: Configs in response (KIP-525), flexible versions
// * v7: TopicID in response

func init() { regKey(19, 0, 7) }

func (c *Cluster) handleCreateTopics(creq *clientReq) (kmsg.Response, error) {
	var (
		b   = creq.cc.b
		req = creq.kreq.(*kmsg.CreateTopicsRequest)
	)
	resp := req.ResponseKind().(*kmsg.CreateTopicsResponse)

	if err := c.checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	// Check if user has CREATE on CLUSTER (allows creating any topic)
	clusterCreate := c.allowedClusterACL(creq, kmsg.ACLOperationCreate)

	// A fault can answer a topic before the work runs. The work's own
	// answer for that topic must not add an entry or replace the code.
	answered := make(map[string]int)
	donet := func(t string, errCode int16) *kmsg.CreateTopicsResponseTopic {
		if i, ok := answered[t]; ok {
			return &resp.Topics[i]
		}
		answered[t] = len(resp.Topics)
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

	// Build normalized name map for collision detection within request
	normalizedInReq := make(map[string]string) // normalized -> original
	for _, rt := range req.Topics {
		normalizedInReq[normalizeTopicName(rt.Topic)] = rt.Topic
	}

	for _, rt := range req.Topics {
		// ACL check: cluster CREATE or topic CREATE
		tk := faultKey{topic: rt.Topic}
		e := creq.faults.check(tk)
		if !clusterCreate {
			e = c.deny(creq, rt.Topic, kmsg.ACLResourceTypeTopic, kmsg.ACLOperationCreate, tk)
		}
		if e != nil {
			donet(rt.Topic, e.Code)
			if creq.skipsWork(e) { // a timed-out create still creates the topic
				continue
			}
		}
		var (
			nparts    int
			nreplicas int
			configs   map[string]*string
		)
		if nparts, nreplicas, configs, e = c.createTopic(&rt, normalizedInReq, req.ValidateOnly); e != nil {
			donet(rt.Topic, e.Code)
			continue
		}

		st := donet(rt.Topic, 0)
		if !req.ValidateOnly {
			st.TopicID = c.data.t2id[rt.Topic]
		}
		st.NumPartitions = int32(nparts)
		st.ReplicationFactor = int16(nreplicas)
		for k, v := range configs {
			c := kmsg.NewCreateTopicsResponseTopicConfig()
			c.Name = k
			c.Value = v
			st.Configs = append(st.Configs, c)
		}
	}

	if !req.ValidateOnly {
		c.notifyTopicChange()
		c.refreshCompactTicker()
		c.persistTopicsState()
	}

	return resp, nil
}

// createTopic runs CreateTopics's per-topic work: it validates the name, the
// counts and any manual assignment, then creates the topic. inReq maps a
// normalized name to the name another topic in the same request claimed, and
// is nil outside a request. A validate-only create runs every check and
// creates nothing.
//
// The counts and configs we return are the ones we used, which the response
// reports. The caller runs notifyTopicChange, refreshCompactTicker and
// persistTopicsState once, after its whole batch.
func (c *Cluster) createTopic(rt *kmsg.CreateTopicsRequestTopic, inReq map[string]string, validateOnly bool) (int, int, map[string]*string, *kerr.Error) {
	if _, ok := c.data.tps.gett(rt.Topic); ok {
		return 0, 0, nil, kerr.TopicAlreadyExists
	}
	// Topics that differ only in . vs _ collide, either with an existing
	// topic or with another topic in the same request.
	normalized := normalizeTopicName(rt.Topic)
	if existing, ok := c.data.tnorms[normalized]; ok && existing != rt.Topic {
		return 0, 0, nil, kerr.InvalidTopicException
	}
	if orig, ok := inReq[normalized]; ok && orig != rt.Topic {
		return 0, 0, nil, kerr.InvalidTopicException
	}
	var nparts, nreplicas int
	if len(rt.ReplicaAssignment) > 0 {
		// When ReplicaAssignment is provided, NumPartitions and
		// ReplicationFactor must be -1 (per Kafka validation).
		if rt.NumPartitions != -1 || rt.ReplicationFactor != -1 {
			return 0, 0, nil, kerr.InvalidRequest
		}
		// Validate: consecutive 0-based partition IDs, non-empty replicas.
		valid := true
		ids := make(map[int32]struct{}, len(rt.ReplicaAssignment))
		for _, ra := range rt.ReplicaAssignment {
			if _, dup := ids[ra.Partition]; dup {
				valid = false
				break
			}
			ids[ra.Partition] = struct{}{}
			if len(ra.Replicas) == 0 {
				valid = false
				break
			}
		}
		if valid {
			for i := range int32(len(rt.ReplicaAssignment)) {
				if _, ok := ids[i]; !ok {
					valid = false
					break
				}
			}
		}
		if !valid {
			return 0, 0, nil, kerr.InvalidReplicaAssignment
		}
		nparts = len(rt.ReplicaAssignment)
		nreplicas = len(rt.ReplicaAssignment[0].Replicas)
	} else {
		if int(rt.ReplicationFactor) > len(c.bs) {
			return 0, 0, nil, kerr.InvalidReplicationFactor
		}
		if rt.NumPartitions == 0 {
			return 0, 0, nil, kerr.InvalidPartitions
		}
		nparts = int(rt.NumPartitions)
		if nparts < 0 {
			nparts = c.cfg.defaultNumParts
		}
		nreplicas = int(rt.ReplicationFactor)
		if nreplicas < 0 {
			nreplicas = 3
			if nreplicas > len(c.bs) {
				nreplicas = len(c.bs)
			}
		}
	}

	configs := make(map[string]*string)
	for _, rc := range rt.Configs {
		configs[rc.Name] = rc.Value
	}

	// ValidateOnly (v1+): skip actual creation
	if !validateOnly {
		c.data.mkt(rt.Topic, nparts, nreplicas, configs)
	}
	return nparts, nreplicas, configs, nil
}
