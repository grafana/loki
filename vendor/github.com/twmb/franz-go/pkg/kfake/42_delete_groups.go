package kfake

import (
	"github.com/twmb/franz-go/pkg/kmsg"
)

// DeleteGroups: v0-3
//
// Version notes:
// * v1: ThrottleMillis
// * v2: Flexible versions
// * v3: Per-group ErrorMessage (we leave it unset)

func init() { regKey(42, 0, 3) }

func (c *Cluster) handleDeleteGroups(creq *clientReq) (kmsg.Response, error) {
	req := creq.kreq.(*kmsg.DeleteGroupsRequest)

	if err := c.checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	return c.groups.handleDelete(creq), nil
}
