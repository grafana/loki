package kfake

import (
	"time"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// CreatePartitions: v0-3
//
// Behavior:
// * Must be sent to the controller
// * Only supports increasing partition count, not decreasing
// * Supports explicit replica assignment (first replica is leader)
// * ValidateOnly: validates request without creating partitions
//
// Version notes:
// * v1: ThrottleMillis
// * v2: Flexible versions
// * v3: No changes

func init() { regKey(37, 0, 3) }

func (c *Cluster) handleCreatePartitions(creq *clientReq) (kmsg.Response, error) {
	var (
		b    = creq.cc.b
		req  = creq.kreq.(*kmsg.CreatePartitionsRequest)
		resp = req.ResponseKind().(*kmsg.CreatePartitionsResponse)
	)

	if err := c.checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	donet := func(t string, errCode int16) *kmsg.CreatePartitionsResponseTopic {
		st := kmsg.NewCreatePartitionsResponseTopic()
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

	// Build broker node ID to broker map for assignment validation
	brokerByNode := make(map[int32]*broker)
	for _, b := range c.bs {
		brokerByNode[b.node] = b
	}

	for _, rt := range req.Topics {
		if !c.allowedACL(creq, rt.Topic, kmsg.ACLResourceTypeTopic, kmsg.ACLOperationAlter) {
			donet(rt.Topic, kerr.TopicAuthorizationFailed.Code)
			continue
		}
		t, ok := c.data.tps.gett(rt.Topic)
		if !ok {
			donet(rt.Topic, kerr.UnknownTopicOrPartition.Code)
			continue
		}
		if rt.Count < int32(len(t)) {
			donet(rt.Topic, kerr.InvalidPartitions.Code)
			continue
		}

		existingPartitions := len(t)
		numNewPartitions := int(rt.Count) - existingPartitions

		// Validate assignment if provided
		if len(rt.Assignment) > 0 {
			if len(rt.Assignment) != numNewPartitions {
				donet(rt.Topic, kerr.InvalidReplicaAssignment.Code)
				continue
			}
			valid := true
			for _, a := range rt.Assignment {
				if len(a.Replicas) == 0 {
					valid = false
					break
				}
				for _, nodeID := range a.Replicas {
					if _, ok := brokerByNode[nodeID]; !ok {
						valid = false
						break
					}
				}
				if !valid {
					break
				}
			}
			if !valid {
				donet(rt.Topic, kerr.InvalidReplicaAssignment.Code)
				continue
			}
		}

		if !req.ValidateOnly {
			for i := range numNewPartitions {
				partNum := int32(existingPartitions) + int32(i)
				if len(rt.Assignment) > 0 {
					// Use explicit assignment: first replica is leader
					a := rt.Assignment[i]
					leader := brokerByNode[a.Replicas[0]]
					var followers []int32
					if len(a.Replicas) > 1 {
						followers = a.Replicas[1:]
					}
					pd := c.data.tps.mkp(rt.Topic, partNum, func() *partData {
						return &partData{
							p:               partNum,
							dir:             defLogDir,
							maxTimestampSeg: -1,
							maxTimestampIdx: -1,
							leader:          leader,
							followers:       followers,
							watch:           make(map[*watchFetch]struct{}),
							shareWatch:      make(map[*watchShareFetch]struct{}),
							createdAt:       time.Now(),
						}
					})
					pd.t = rt.Topic
				} else {
					pd := c.data.tps.mkp(rt.Topic, partNum, c.newPartData(partNum))
					pd.t = rt.Topic
				}
			}
		}
		donet(rt.Topic, 0)
	}

	if !req.ValidateOnly {
		c.notifyTopicChange()
		c.persistTopicsState()
	}

	return resp, nil
}
