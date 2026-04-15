package kfake

import (
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// TODO
//
// * Transactional IDs
// * v3+

func init() { regKey(22, 0, 4) }

func (c *Cluster) handleInitProducerID(kreq kmsg.Request) (kmsg.Response, error) {
	var (
		req  = kreq.(*kmsg.InitProducerIDRequest)
		resp = req.ResponseKind().(*kmsg.InitProducerIDResponse)
	)

	if err := checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	if req.TransactionalID != nil {
		resp.ErrorCode = kerr.UnknownServerError.Code
		return resp, nil
	}

	pid := c.pids.create(nil)
	resp.ProducerID = pid.id
	resp.ProducerEpoch = pid.epoch
	return resp, nil
}
