package kfake

import (
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// SyncGroup: v0-5
//
// Version notes:
// * v1: ThrottleMillis
// * v3: InstanceID for static membership (KIP-345) - not implemented
// * v4: Flexible versions
// * v5: ProtocolType and Protocol in request/response

func init() { regKey(14, 0, 5) }

func (c *Cluster) handleSyncGroup(creq *clientReq) (kmsg.Response, error) {
	req := creq.kreq.(*kmsg.SyncGroupRequest)
	resp := req.ResponseKind().(*kmsg.SyncGroupResponse)

	if err := c.checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	if c.groups.handleSync(creq) {
		return nil, nil
	}
	resp.ErrorCode = kerr.GroupIDNotFound.Code
	return resp, nil
}
