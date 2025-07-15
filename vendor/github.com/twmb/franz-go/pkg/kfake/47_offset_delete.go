package kfake

import (
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

func init() { regKey(47, 0, 0) }

func (c *Cluster) handleOffsetDelete(creq *clientReq) (kmsg.Response, error) {
	req := creq.kreq.(*kmsg.OffsetDeleteRequest)
	resp := req.ResponseKind().(*kmsg.OffsetDeleteResponse)

	if err := checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	if c.groups.handleOffsetDelete(creq) {
		return nil, nil
	}
	resp.ErrorCode = kerr.GroupIDNotFound.Code
	return resp, nil
}
