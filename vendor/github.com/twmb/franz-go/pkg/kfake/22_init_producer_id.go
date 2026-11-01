package kfake

import (
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// InitProducerID: v0-5
//
// Behavior:
// * Allocates producer ID and epoch for idempotent/transactional producers
// * Handles transaction ID registration for transactional producers
//
// Version notes:
// * v2: ThrottleMillis
// * v3: ProducerID and ProducerEpoch in request for existing producers
// * v4: Flexible versions
// * v5: ProducerEpoch in response for KIP-890 epoch bumping

func init() { regKey(22, 0, 5) }

func (c *Cluster) handleInitProducerID(creq *clientReq) (kmsg.Response, error) {
	req := creq.kreq.(*kmsg.InitProducerIDRequest)

	if err := c.checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	// ACL check: transactional requires WRITE on TxnID, non-transactional requires
	// IDEMPOTENT_WRITE on Cluster or WRITE on any Topic.
	if req.TransactionalID != nil {
		if !c.allowedACL(creq, *req.TransactionalID, kmsg.ACLResourceTypeTransactionalId, kmsg.ACLOperationWrite) {
			resp := req.ResponseKind().(*kmsg.InitProducerIDResponse)
			resp.ErrorCode = kerr.TransactionalIDAuthorizationFailed.Code
			return resp, nil
		}
	} else {
		// Non-transactional: need idempotent write on cluster or write on any topic
		if !c.allowedClusterACL(creq, kmsg.ACLOperationIdempotentWrite) && !c.anyAllowedACL(creq, kmsg.ACLResourceTypeTopic, kmsg.ACLOperationWrite) {
			resp := req.ResponseKind().(*kmsg.InitProducerIDResponse)
			resp.ErrorCode = kerr.ClusterAuthorizationFailed.Code
			return resp, nil
		}
	}

	return c.pids.doInitProducerID(creq), nil
}
