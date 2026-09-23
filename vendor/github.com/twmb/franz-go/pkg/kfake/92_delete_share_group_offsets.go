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
// * Drops the group if it becomes truly empty
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

	if c.coordinator(req.GroupID).node != creq.cc.b.node {
		resp.ErrorCode = kerr.NotCoordinator.Code
		return resp, nil
	}

	// ACL: require GROUP DELETE.
	if e := c.deny(creq, req.GroupID, kmsg.ACLResourceTypeGroup, kmsg.ACLOperationDelete, faultKey{group: req.GroupID}); e != nil && creq.skipsWork(e) { // a timed-out delete falls through to the per-topic checks
		resp.ErrorCode = e.Code
		return resp, nil
	}

	sg := c.shareGroups.get(req.GroupID)
	if sg == nil {
		resp.ErrorCode = kerr.GroupIDNotFound.Code
		return resp, nil
	}

	if len(sg.members) > 0 {
		resp.ErrorCode = kerr.NonEmptyGroup.Code
		return resp, nil
	}

	for i := range req.Topics {
		rt := &req.Topics[i]
		rst := kmsg.NewDeleteShareGroupOffsetsResponseTopic()
		rst.Topic = rt.Topic
		id := c.data.t2id[rt.Topic]
		rst.TopicID = id

		e := c.deny(creq, rt.Topic, kmsg.ACLResourceTypeTopic, kmsg.ACLOperationRead, faultKey{topic: rt.Topic, topicID: id})
		if e != nil {
			rst.ErrorCode = e.Code
			if creq.skipsWork(e) { // a timed-out delete still deletes
				resp.Topics = append(resp.Topics, rst)
				continue
			}
		}
		if _, ok := c.data.tps[rt.Topic]; !ok {
			if e == nil {
				rst.ErrorCode = kerr.UnknownTopicOrPartition.Code
			}
		} else {
			delete(sg.partitions, rt.Topic)
		}
		resp.Topics = append(resp.Topics, rst)
	}

	sg.maybeQuit()

	return resp, nil
}
