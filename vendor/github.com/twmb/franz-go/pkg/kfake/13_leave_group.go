package kfake

import (
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

func init() { regKey(13, 0, 5) }

func (c *Cluster) handleLeaveGroup(creq *clientReq) (kmsg.Response, error) {
	req := creq.kreq.(*kmsg.LeaveGroupRequest)
	resp := req.ResponseKind().(*kmsg.LeaveGroupResponse)

	if err := checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	if c.groups.handleLeave(creq) {
		return nil, nil
	}
	resp.ErrorCode = kerr.GroupIDNotFound.Code
	return resp, nil
}
