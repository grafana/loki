package kfake

import (
	"strings"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// ShareGroupHeartbeat: v0-1 (KIP-932)
//
// Behavior:
// * epoch 0: Join group (create member, compute initial assignment)
// * epoch -1: Leave group (release records, rebalance remaining members)
// * epoch >0: Regular heartbeat (subscription changes, assignment delivery)
// * Validates memberID format (non-empty, <=36 chars, client-generated UUID)
//
// Version notes:
// * v0: Initial share group heartbeat (KIP-932)
// * v1: No protocol changes, version bump only

func init() { regKey(76, 0, 1) }

func (c *Cluster) handleShareGroupHeartbeat(creq *clientReq) (kmsg.Response, error) {
	req := creq.kreq.(*kmsg.ShareGroupHeartbeatRequest)
	if err := c.checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	errResp := func(code int16) (kmsg.Response, error) {
		resp := req.ResponseKind().(*kmsg.ShareGroupHeartbeatResponse)
		resp.ErrorCode = code
		return resp, nil
	}

	if strings.TrimSpace(req.GroupID) == "" {
		return errResp(kerr.InvalidRequest.Code)
	}
	if ke := c.validateGroup(creq, req.GroupID); ke != nil {
		return errResp(ke.Code)
	}
	if e := c.deny(creq, req.GroupID, kmsg.ACLResourceTypeGroup, kmsg.ACLOperationRead, faultKey{group: req.GroupID}); e != nil {
		return errResp(e.Code)
	}
	if req.MemberID == "" || len(req.MemberID) > 36 {
		return errResp(kerr.InvalidRequest.Code)
	}
	if req.MemberEpoch < -1 {
		return errResp(kerr.InvalidRequest.Code)
	}
	if req.RackID != nil && strings.TrimSpace(*req.RackID) == "" {
		return errResp(kerr.InvalidRequest.Code)
	}
	if req.MemberEpoch == 0 && len(req.SubscribedTopicNames) == 0 {
		return errResp(kerr.InvalidRequest.Code)
	}
	for _, topic := range req.SubscribedTopicNames {
		if e := c.deny(creq, topic, kmsg.ACLResourceTypeTopic, kmsg.ACLOperationDescribe, faultKey{topic: topic}); e != nil {
			return errResp(e.Code)
		}
	}

	return c.shareGroups.handleHeartbeat(creq), nil
}
