package kfake

import (
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// AddOffsetsToTxn: v0-4
//
// Behavior:
// * Registers a consumer group's offsets as part of an ongoing transaction
// * Must be called before TxnOffsetCommit
//
// Version notes:
// * v2: ThrottleMillis
// * v3: Flexible versions
// * v4: No changes

func init() { regKey(25, 0, 4) }

func (c *Cluster) handleAddOffsetsToTxn(creq *clientReq) (kmsg.Response, error) {
	req := creq.kreq.(*kmsg.AddOffsetsToTxnRequest)

	if err := c.checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	errResp := func(e *kerr.Error) kmsg.Response {
		resp := req.ResponseKind().(*kmsg.AddOffsetsToTxnResponse)
		resp.ErrorCode = e.Code
		return resp
	}

	// ACL checks: WRITE on TxnID, READ on Group. Faults fire only on the
	// transaction coordinator; elsewhere doAddOffsets answers
	// NOT_COORDINATOR.
	misrouted := !c.isCoordinator(creq, req.TransactionalID)
	if e := c.deny(creq, req.TransactionalID, kmsg.ACLResourceTypeTransactionalId, kmsg.ACLOperationWrite, faultKey{txnID: req.TransactionalID, misrouted: misrouted}); e != nil {
		return errResp(e), nil
	}
	if e := c.deny(creq, req.Group, kmsg.ACLResourceTypeGroup, kmsg.ACLOperationRead, faultKey{txnID: req.TransactionalID, group: req.Group, misrouted: misrouted}); e != nil {
		return errResp(e), nil
	}

	return c.pids.doAddOffsets(creq), nil
}
