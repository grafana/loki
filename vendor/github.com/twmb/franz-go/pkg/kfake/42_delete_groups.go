package kfake

import (
	"github.com/twmb/franz-go/pkg/kmsg"
)

func init() { regKey(42, 0, 2) }

func (c *Cluster) handleDeleteGroups(creq *clientReq) (kmsg.Response, error) {
	req := creq.kreq.(*kmsg.DeleteGroupsRequest)

	if err := checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	return c.groups.handleDelete(creq), nil
}
