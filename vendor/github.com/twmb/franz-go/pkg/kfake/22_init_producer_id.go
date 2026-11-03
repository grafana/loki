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
	var e *kerr.Error
	if req.TransactionalID != nil {
		txnID := *req.TransactionalID
		e = c.deny(creq, txnID, kmsg.ACLResourceTypeTransactionalId, kmsg.ACLOperationWrite, faultKey{txnID: txnID, misrouted: !c.isCoordinator(creq, txnID)})
	} else {
		// Non-transactional: need idempotent write on cluster or write on any topic
		if !c.allowedClusterACL(creq, kmsg.ACLOperationIdempotentWrite) && !c.anyAllowedACL(creq, kmsg.ACLResourceTypeTopic, kmsg.ACLOperationWrite) {
			e = kerr.ClusterAuthorizationFailed
		} else {
			e = creq.faults.check(faultKey{})
		}
	}
	if e != nil {
		resp := req.ResponseKind().(*kmsg.InitProducerIDResponse)
		resp.ErrorCode = e.Code
		return resp, nil
	}
	return c.pids.doInitProducerID(creq), nil
}
