package kfake

import (
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// ShareGroupDescribe: v0-1 (KIP-932)
//
// Behavior:
// * Describes share group state, members, and assignments
// * Routed to the group coordinator
// * Topic DESCRIBE ACL is all-or-nothing: if any assigned topic fails,
//   the entire group response is redacted (matching Java's behavior)
//
// Version notes:
// * v0: Initial share group describe (KIP-932)
// * v1: No protocol changes

func init() { regKey(77, 0, 1) }

func (c *Cluster) handleShareGroupDescribe(creq *clientReq) (kmsg.Response, error) {
	var (
		req  = creq.kreq.(*kmsg.ShareGroupDescribeRequest)
		resp = req.ResponseKind().(*kmsg.ShareGroupDescribeResponse)
	)

	if err := c.checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	for _, groupID := range req.GroupIDs {
		rg := kmsg.NewShareGroupDescribeResponseGroup()
		rg.GroupID = groupID

		// Coordinator check: ShareGroupDescribe is routed to the
		// group coordinator (matching Java's GroupCoordinatorService).
		if c.coordinator(groupID).node != creq.cc.b.node {
			rg.ErrorCode = kerr.NotCoordinator.Code
			resp.Groups = append(resp.Groups, rg)
			continue
		}

		// ACL: require GROUP DESCRIBE.
		if e := c.deny(creq, groupID, kmsg.ACLResourceTypeGroup, kmsg.ACLOperationDescribe, faultKey{group: groupID}); e != nil {
			rg.ErrorCode = e.Code
			resp.Groups = append(resp.Groups, rg)
			continue
		}

		sg := c.shareGroups.get(groupID)
		if sg == nil {
			rg.ErrorCode = kerr.GroupIDNotFound.Code
			resp.Groups = append(resp.Groups, rg)
			continue
		}

		rg = c.fillShareGroupDescribe(creq, sg, rg)

		if req.IncludeAuthorizedOperations {
			rg.AuthorizedOperations = c.groupAuthorizedOps(creq, groupID)
		}
		resp.Groups = append(resp.Groups, rg)
	}

	return resp, nil
}

// fillShareGroupDescribe fills one group's portion of a ShareGroupDescribe
// response. The topic DESCRIBE check is all or nothing: one denied topic
// replaces the whole group with a redacted response.
func (c *Cluster) fillShareGroupDescribe(creq *clientReq, sg *shareGroup, rg kmsg.ShareGroupDescribeResponseGroup) kmsg.ShareGroupDescribeResponseGroup {
	id2t := c.data.id2t
	if len(sg.members) == 0 {
		rg.GroupState = "Empty"
	} else {
		rg.GroupState = "Stable"
	}
	rg.GroupEpoch = sg.groupEpoch
	rg.AssignmentEpoch = sg.groupEpoch
	rg.Assignor = "simple"

	// Collect all assigned topic names across members.
	allTopics := make(map[string]struct{})
	for _, m := range sg.members {
		for tid := range m.assignment {
			if name := id2t[tid]; name != "" {
				allTopics[name] = struct{}{}
			}
		}
	}

	// Java does an all-or-nothing check: if the user
	// cannot DESCRIBE any assigned topic, the entire
	// group is replaced with a redacted response
	// containing only the error (no state/epoch/assignor
	// metadata leaked).
	for topic := range allTopics {
		if e := c.deny(creq, topic, kmsg.ACLResourceTypeTopic, kmsg.ACLOperationDescribe, faultKey{topic: topic}); e != nil {
			redacted := kmsg.NewShareGroupDescribeResponseGroup()
			redacted.GroupID = rg.GroupID
			redacted.ErrorCode = e.Code
			return redacted
		}
	}

	for _, m := range sg.members {
		sm := kmsg.NewShareGroupDescribeResponseGroupMember()
		sm.MemberID = m.memberID
		sm.RackID = m.rackID
		sm.MemberEpoch = m.memberEpoch
		sm.ClientID = m.clientID
		sm.ClientHost = m.clientHost
		sm.SubscribedTopicNames = m.subscribedTopics

		a := kmsg.NewShareGroupDescribeResponseGroupMemberAssignment()
		for tid, parts := range m.assignment {
			tp := kmsg.NewShareGroupDescribeResponseGroupMemberAssignmentTopicPartition()
			tp.TopicID = tid
			tp.Topic = id2t[tid]
			tp.Partitions = parts
			a.TopicPartitions = append(a.TopicPartitions, tp)
		}
		sm.Assignment = a
		rg.Members = append(rg.Members, sm)
	}
	return rg
}
