package kfake

import (
	"github.com/twmb/franz-go/pkg/kmsg"
)

// DescribeGroups: v0-6
//
// Version notes:
// * v1: ThrottleMillis
// * v3: IncludeAuthorizedOperations (KIP-430)
// * v4: InstanceID for static membership (KIP-345) - not implemented
// * v5: Flexible versions
// * v6: ErrorMessage in response (KIP-1043)

func init() { regKey(15, 0, 6) }

func (c *Cluster) handleDescribeGroups(creq *clientReq) (kmsg.Response, error) {
	req := creq.kreq.(*kmsg.DescribeGroupsRequest)

	if err := c.checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	return c.groups.handleDescribe(creq), nil
}
