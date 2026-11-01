package kfake

import (
	"maps"

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
// * Uses waitControl to safely read manage goroutine state
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
		if !c.allowedACL(creq, groupID, kmsg.ACLResourceTypeGroup, kmsg.ACLOperationDescribe) {
			rg.ErrorCode = kerr.GroupAuthorizationFailed.Code
			resp.Groups = append(resp.Groups, rg)
			continue
		}

		sg := c.shareGroups.get(groupID)
		if sg == nil {
			rg.ErrorCode = kerr.GroupIDNotFound.Code
			resp.Groups = append(resp.Groups, rg)
			continue
		}

		// Snapshot id2t before entering manage() via waitControl.
		// c.data is only safe to read in run(), and waitControl's
		// adminCh drain could mutate c.data concurrently with
		// the manage() closure.
		id2t := make(map[uuid]string, len(c.data.id2t))
		maps.Copy(id2t, c.data.id2t)

		if !sg.waitControl(func() {
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
				if !c.allowedACL(creq, topic, kmsg.ACLResourceTypeTopic, kmsg.ACLOperationDescribe) {
					rg = kmsg.NewShareGroupDescribeResponseGroup()
					rg.GroupID = groupID
					rg.ErrorCode = kerr.TopicAuthorizationFailed.Code
					return
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
		}) {
			// Group's manage goroutine quit -- treat as dead.
			rg.GroupState = "Dead"
			rg.ErrorCode = kerr.GroupIDNotFound.Code
		}

		if req.IncludeAuthorizedOperations {
			rg.AuthorizedOperations = c.groupAuthorizedOps(creq, groupID)
		}

		resp.Groups = append(resp.Groups, rg)
	}

	return resp, nil
}
