package kfake

import (
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

func init() { regKey(14, 0, 5) }

func (c *Cluster) handleSyncGroup(creq *clientReq) (kmsg.Response, error) {
	req := creq.kreq.(*kmsg.SyncGroupRequest)
	resp := req.ResponseKind().(*kmsg.SyncGroupResponse)

	if err := checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	if c.groups.handleSync(creq) {
		return nil, nil
	}
	resp.ErrorCode = kerr.GroupIDNotFound.Code
	return resp, nil
}
