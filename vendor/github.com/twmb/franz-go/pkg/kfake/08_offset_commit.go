package kfake

import (
	"github.com/twmb/franz-go/pkg/kmsg"
)

func init() { regKey(8, 0, 8) }

func (c *Cluster) handleOffsetCommit(creq *clientReq) (kmsg.Response, error) {
	req := creq.kreq.(*kmsg.OffsetCommitRequest)

	if err := checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	c.groups.handleOffsetCommit(creq)
	return nil, nil
}
