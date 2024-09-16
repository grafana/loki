package kfake

import (
	"github.com/twmb/franz-go/pkg/kmsg"
)

func init() { regKey(9, 0, 8) }

func (c *Cluster) handleOffsetFetch(creq *clientReq) (kmsg.Response, error) {
	req := creq.kreq.(*kmsg.OffsetFetchRequest)

	if err := checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	return c.groups.handleOffsetFetch(creq), nil
}
