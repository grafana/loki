package kfake

import (
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// LeaveGroup: v0-5
//
// Version notes:
// * v1: ThrottleMillis
// * v3: Batch members leave with InstanceID (KIP-345) - not fully implemented
// * v4: Flexible versions
// * v5: Reason field (KIP-800)

func init() { regKey(13, 0, 5) }

func (c *Cluster) handleLeaveGroup(creq *clientReq) (kmsg.Response, error) {
	req := creq.kreq.(*kmsg.LeaveGroupRequest)
	resp := req.ResponseKind().(*kmsg.LeaveGroupResponse)

	if err := c.checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	if c.groups.handleLeave(creq) {
		return nil, nil
	}
	resp.ErrorCode = kerr.GroupIDNotFound.Code
	return resp, nil
}
