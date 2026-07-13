package kfake

import (
	"github.com/twmb/franz-go/pkg/kmsg"
)

// JoinGroup: v0-9
//
// Version notes:
// * v1: RebalanceTimeoutMillis
// * v4: MEMBER_ID_REQUIRED for initial join (KIP-394)
// * v5: InstanceID for static membership (KIP-345) - not implemented
// * v6: Flexible versions
// * v7: ProtocolType in response
// * v8: Reason field (KIP-800)
// * v9: SkipAssignment (KIP-814) - not implemented

func init() { regKey(11, 0, 9) }

func (c *Cluster) handleJoinGroup(creq *clientReq) (kmsg.Response, error) {
	req := creq.kreq.(*kmsg.JoinGroupRequest)

	if err := c.checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	c.groups.handleJoin(creq)
	return nil, nil
}
