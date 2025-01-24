package kfake

import (
	"github.com/twmb/franz-go/pkg/kmsg"
)

func init() { regKey(11, 0, 9) }

func (c *Cluster) handleJoinGroup(creq *clientReq) (kmsg.Response, error) {
	req := creq.kreq.(*kmsg.JoinGroupRequest)

	if err := checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	c.groups.handleJoin(creq)
	return nil, nil
}
