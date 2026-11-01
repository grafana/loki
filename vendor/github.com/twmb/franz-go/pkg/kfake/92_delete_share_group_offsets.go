package kfake

import (
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// DeleteShareGroupOffsets: v0 (KIP-932)
//
// Behavior:
// * Removes all share partition state for specified topics
// * Only works on empty groups (no active members)
// * Shuts down the manage goroutine if the group becomes truly empty
//   (no members and no partition state)

func init() { regKey(92, 0, 0) }

func (c *Cluster) handleDeleteShareGroupOffsets(creq *clientReq) (kmsg.Response, error) {
	var (
		req  = creq.kreq.(*kmsg.DeleteShareGroupOffsetsRequest)
		resp = req.ResponseKind().(*kmsg.DeleteShareGroupOffsetsResponse)
	)

	if err := c.checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	// Coordinator check: DeleteShareGroupOffsets is routed to the
	// share coordinator.
	if c.coordinator(req.GroupID).node != creq.cc.b.node {
		resp.ErrorCode = kerr.NotCoordinator.Code
		return resp, nil
	}

	// ACL: require GROUP DELETE.
	if !c.allowedACL(creq, req.GroupID, kmsg.ACLResourceTypeGroup, kmsg.ACLOperationDelete) {
		resp.ErrorCode = kerr.GroupAuthorizationFailed.Code
		return resp, nil
	}

	sg := c.shareGroups.get(req.GroupID)
	if sg == nil {
		resp.ErrorCode = kerr.GroupIDNotFound.Code
		return resp, nil
	}

	// Pre-lookup topic IDs and ACL results while in run() where
	// c.data is safe.
	type deleteTopicInfo struct {
		id      uuid
		exists  bool
		aclDeny bool
	}
	topicInfo := make(map[string]deleteTopicInfo, len(req.Topics))
	for _, rt := range req.Topics {
		info := deleteTopicInfo{id: c.data.t2id[rt.Topic]}
		if !c.allowedACL(creq, rt.Topic, kmsg.ACLResourceTypeTopic, kmsg.ACLOperationRead) {
			info.aclDeny = true
		}
		if _, ok := c.data.tps[rt.Topic]; ok {
			info.exists = true
		}
		topicInfo[rt.Topic] = info
	}

	if !sg.waitControl(func() {
		if len(sg.members) > 0 {
			resp.ErrorCode = kerr.NonEmptyGroup.Code
			return
		}

		sg.mu.Lock()
		for i := range req.Topics {
			rt := &req.Topics[i]
			rst := kmsg.NewDeleteShareGroupOffsetsResponseTopic()
			rst.Topic = rt.Topic
			info := topicInfo[rt.Topic]
			rst.TopicID = info.id

			if info.aclDeny {
				rst.ErrorCode = kerr.TopicAuthorizationFailed.Code
				resp.Topics = append(resp.Topics, rst)
				continue
			}
			if !info.exists {
				rst.ErrorCode = kerr.UnknownTopicOrPartition.Code
				resp.Topics = append(resp.Topics, rst)
				continue
			}

			delete(sg.partitions, rt.Topic)
			resp.Topics = append(resp.Topics, rst)
		}
		sg.mu.Unlock() // not deferred: maybeQuit below acquires sg.mu

		sg.maybeQuit()
	}) {
		resp.ErrorCode = kerr.GroupIDNotFound.Code
	}

	// Clean up from shareGroups.gs if the group shut down.
	select {
	case <-sg.quitCh:
		delete(c.shareGroups.gs, req.GroupID)
	default:
	}

	return resp, nil
}
