package kfake

import (
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// TxnOffsetCommit: v0-5
//
// Behavior:
// * Stages offset commits as part of a transaction
// * Offsets are applied on EndTxn commit, discarded on abort
// * v3+: GenerationID/MemberID validation for zombie fencing (KIP-447)
// * v5+: Implicit group addition to transaction (KIP-890)
//
// Version notes:
// * v2: ThrottleMillis
// * v3: Flexible versions, GenerationID and MemberID for zombie fencing (KIP-447)
// * v4-5: No field changes; v5 enables implicit group addition (KIP-890)

func init() { regKey(28, 0, 5) }

func (c *Cluster) handleTxnOffsetCommit(creq *clientReq) (kmsg.Response, error) {
	req := creq.kreq.(*kmsg.TxnOffsetCommitRequest)

	if err := c.checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	errResp := func(errCode int16) kmsg.Response {
		resp := req.ResponseKind().(*kmsg.TxnOffsetCommitResponse)
		for _, rt := range req.Topics {
			st := kmsg.NewTxnOffsetCommitResponseTopic()
			st.Topic = rt.Topic
			for _, rp := range rt.Partitions {
				sp := kmsg.NewTxnOffsetCommitResponseTopicPartition()
				sp.Partition = rp.Partition
				sp.ErrorCode = errCode
				st.Partitions = append(st.Partitions, sp)
			}
			resp.Topics = append(resp.Topics, st)
		}
		return resp
	}

	// ACL check: WRITE on TxnID
	if !c.allowedACL(creq, req.TransactionalID, kmsg.ACLResourceTypeTransactionalId, kmsg.ACLOperationWrite) {
		return errResp(kerr.TransactionalIDAuthorizationFailed.Code), nil
	}

	// ACL check: READ on Group
	if !c.allowedACL(creq, req.Group, kmsg.ACLResourceTypeGroup, kmsg.ACLOperationRead) {
		return errResp(kerr.GroupAuthorizationFailed.Code), nil
	}

	// ACL check: READ on each Topic
	for _, rt := range req.Topics {
		if !c.allowedACL(creq, rt.Topic, kmsg.ACLResourceTypeTopic, kmsg.ACLOperationRead) {
			return errResp(kerr.TopicAuthorizationFailed.Code), nil
		}
	}

	return c.pids.doTxnOffsetCommit(creq), nil
}
