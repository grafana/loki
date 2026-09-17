package kfake

import (
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// AddPartitionsToTxn: v0-5
//
// Behavior:
// * Registers partitions as part of an ongoing transaction
// * Must be called before producing to partitions (pre-KIP-890 clients)
// * KIP-890 clients (v4+) can skip this and use implicit partition addition
//
// Version notes:
// * v3: Flexible versions
// * v4: Batched transactions support (KIP-890)
// * v5: Epoch bumping support (KIP-890)

func init() { regKey(24, 0, 5) }

func (c *Cluster) handleAddPartitionsToTxn(creq *clientReq) (kmsg.Response, error) {
	req := creq.kreq.(*kmsg.AddPartitionsToTxnRequest)

	if err := c.checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	errResp := func(errCode int16) *kmsg.AddPartitionsToTxnResponse {
		resp := req.ResponseKind().(*kmsg.AddPartitionsToTxnResponse)
		for _, rt := range req.Topics {
			st := kmsg.NewAddPartitionsToTxnResponseTopic()
			st.Topic = rt.Topic
			for _, p := range rt.Partitions {
				sp := kmsg.NewAddPartitionsToTxnResponseTopicPartition()
				sp.Partition = p
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

	// ACL check: WRITE on each Topic
	for _, rt := range req.Topics {
		if !c.allowedACL(creq, rt.Topic, kmsg.ACLResourceTypeTopic, kmsg.ACLOperationWrite) {
			return errResp(kerr.TopicAuthorizationFailed.Code), nil
		}
	}

	return c.pids.doAddPartitions(creq), nil
}
