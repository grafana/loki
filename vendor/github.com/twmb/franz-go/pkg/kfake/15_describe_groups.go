package kfake

import (
	"github.com/twmb/franz-go/pkg/kmsg"
)

func init() { regKey(15, 0, 5) }

func (c *Cluster) handleDescribeGroups(creq *clientReq) (kmsg.Response, error) {
	req := creq.kreq.(*kmsg.DescribeGroupsRequest)

	if err := checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	return c.groups.handleDescribe(creq), nil
}
