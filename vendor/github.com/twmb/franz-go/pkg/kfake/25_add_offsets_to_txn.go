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

	// ACL check: WRITE on TxnID
	if !c.allowedACL(creq, req.TransactionalID, kmsg.ACLResourceTypeTransactionalId, kmsg.ACLOperationWrite) {
		resp := req.ResponseKind().(*kmsg.AddOffsetsToTxnResponse)
		resp.ErrorCode = kerr.TransactionalIDAuthorizationFailed.Code
		return resp, nil
	}

	// ACL check: READ on Group
	if !c.allowedACL(creq, req.Group, kmsg.ACLResourceTypeGroup, kmsg.ACLOperationRead) {
		resp := req.ResponseKind().(*kmsg.AddOffsetsToTxnResponse)
		resp.ErrorCode = kerr.GroupAuthorizationFailed.Code
		return resp, nil
	}

	return c.pids.doAddOffsets(creq), nil
}
